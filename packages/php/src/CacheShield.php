<?php

namespace GoCacheShield;

use Closure;
use GoCacheShield\Exceptions\NotFoundException;
use GoCacheShield\Exceptions\TooManyLoadsException;

class CacheShield
{
    protected float $jitterRatio;
    protected int $negativeTTL;
    protected string $lockDir;
    protected $customCacheDriver;

    public function __construct(array $options = [])
    {
        $this->jitterRatio = $options['jitter_ratio'] ?? 0.10;
        $this->negativeTTL = $options['negative_ttl'] ?? 30;
        if (!empty($options['lock_dir'])) {
            $this->lockDir = $options['lock_dir'];
        } elseif (function_exists('storage_path') && is_dir(storage_path())) {
            $this->lockDir = storage_path('framework/cacheshield');
        } else {
            $this->lockDir = sys_get_temp_dir() . '/cacheshield_locks';
        }
        $this->customCacheDriver = $options['cache_driver'] ?? null;

        if (!is_dir($this->lockDir)) {
            @mkdir($this->lockDir, 0777, true);
        }
    }

    // Get from cache or execute loader with stampede protection
    public function getOrLoad(string $key, int $ttlSeconds, Closure $loader)
    {
        if (mt_rand(1, 100) === 1) {
            $this->cleanStaleFiles();
        }

        // check negative cache
        if ($this->isNegative($key)) {
            throw new NotFoundException();
        }

        // check cache store
        $cached = $this->getCache($key);
        if ($cached !== null) {
            return $cached;
        }

        // file lock for coalescing
        $lockFile = $this->lockDir . '/' . md5($key) . '.lock';
        $fp = fopen($lockFile, 'c+');

        if (!$fp) {
            return $loader();
        }

        // acquire exclusive lock for leader
        if (flock($fp, LOCK_EX | LOCK_NB)) {
            try {
                $doubleCheck = $this->getCache($key);
                if ($doubleCheck !== null) {
                    return $doubleCheck;
                }

                // execute loader
                try {
                    $result = $loader();
                    $effectiveTTL = $this->applyJitter($ttlSeconds);
                    $this->setCache($key, $result, $effectiveTTL);
                    $this->invalidateNegative($key);

                    return $result;
                } catch (NotFoundException $e) {
                    $this->markNegative($key, $this->negativeTTL);
                    throw $e;
                }
            } finally {
                flock($fp, LOCK_UN);
                fclose($fp);
            }
        } else {
            // follower wait loop
            $waitStart = microtime(true);
            $acquiredLock = false;

            while ((microtime(true) - $waitStart) < 3.0) {
                if (flock($fp, LOCK_SH | LOCK_NB)) {
                    $acquiredLock = true;
                    break;
                }
                usleep(5000);
            }

            if ($acquiredLock) {
                try {
                    $fresh = $this->getCache($key);
                    if ($fresh !== null) {
                        return $fresh;
                    }
                    if ($this->isNegative($key)) {
                        throw new NotFoundException();
                    }
                } finally {
                    flock($fp, LOCK_UN);
                    fclose($fp);
                }
            } else {
                @fclose($fp);
            }

            return $loader();
        }
    }

    public function invalidate(string $key): void
    {
        $this->deleteCache($key);
        $this->invalidateNegative($key);
    }

    protected function applyJitter(int $baseTTL): int
    {
        if ($baseTTL <= 0 || $this->jitterRatio <= 0) {
            return $baseTTL;
        }
        $factor = (mt_rand(0, 2000) / 1000.0) - 1.0; // [-1.0, 1.0]
        $delta = (int) round($baseTTL * $this->jitterRatio * $factor);
        return max(1, $baseTTL + $delta);
    }

    protected function markNegative(string $key, int $ttl): void
    {
        $this->setCache("neg:" . $key, true, $ttl);
    }

    protected function isNegative(string $key): bool
    {
        return $this->getCache("neg:" . $key) !== null;
    }

    protected function invalidateNegative(string $key): void
    {
        $this->deleteCache("neg:" . $key);
    }

    protected function getCache(string $key)
    {
        if ($this->customCacheDriver) {
            return ($this->customCacheDriver['get'])($key);
        }
        if (function_exists('apcu_fetch')) {
            $success = false;
            $val = apcu_fetch($key, $success);
            return $success ? $val : null;
        }
        // File-based cache for standard cPanel without Redis/APCu
        $cacheFile = $this->lockDir . '/' . md5('data:' . $key) . '.cache';
        if (file_exists($cacheFile)) {
            $rfp = @fopen($cacheFile, 'r');
            if ($rfp) {
                if (flock($rfp, LOCK_SH)) {
                    $raw = stream_get_contents($rfp);
                    flock($rfp, LOCK_UN);
                    fclose($rfp);
                    $data = @unserialize($raw, ['allowed_classes' => false]);
                    if (is_array($data) && isset($data['exp']) && $data['exp'] > time()) {
                        return $data['val'];
                    }
                } else {
                    fclose($rfp);
                }
            }
            @unlink($cacheFile);
        }
        return null;
    }

    protected function setCache(string $key, $value, int $ttl): void
    {
        if ($this->customCacheDriver) {
            ($this->customCacheDriver['set'])($key, $value, $ttl);
            return;
        }
        if (function_exists('apcu_store')) {
            apcu_store($key, $value, $ttl);
            return;
        }
        $cacheFile = $this->lockDir . '/' . md5('data:' . $key) . '.cache';
        $payload = [
            'exp' => time() + $ttl,
            'val' => $value
        ];
        @file_put_contents($cacheFile, serialize($payload), LOCK_EX);
    }

    protected function deleteCache(string $key): void
    {
        if ($this->customCacheDriver) {
            ($this->customCacheDriver['delete'])($key);
            return;
        }
        if (function_exists('apcu_delete')) {
            @apcu_delete($key);
        }
        $cacheFile = $this->lockDir . '/' . md5('data:' . $key) . '.cache';
        if (file_exists($cacheFile)) {
            @unlink($cacheFile);
        }
    }

    /**
     * Clean up stale temporary locks and expired files older than 10 minutes.
     */
    protected function cleanStaleFiles(): void
    {
        if (!is_dir($this->lockDir)) {
            return;
        }
        $files = @glob($this->lockDir . '/*');
        if (!$files) {
            return;
        }
        $now = time();
        foreach ($files as $file) {
            if (is_file($file) && ($now - @filemtime($file)) > 600) {
                @unlink($file);
            }
        }
    }
}
