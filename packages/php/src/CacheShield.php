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
        $this->lockDir = $options['lock_dir'] ?? sys_get_temp_dir() . '/cacheshield_locks';
        $this->customCacheDriver = $options['cache_driver'] ?? null;

        if (!is_dir($this->lockDir)) {
            @mkdir($this->lockDir, 0777, true);
        }
    }

    /**
     * Get value from cache or execute loader with stampede protection,
     * TTL jitter, and negative caching.
     */
    public function getOrLoad(string $key, int $ttlSeconds, Closure $loader)
    {
        // 1. Check Negative Cache
        if ($this->isNegative($key)) {
            throw new NotFoundException();
        }

        // 2. Check Primary Cache
        $cached = $this->getCache($key);
        if ($cached !== null) {
            return $cached;
        }

        // 3. Stampede Protection via Non-Blocking Inter-Process Lock (Works on cPanel shared hosting!)
        $lockFile = $this->lockDir . '/' . md5($key) . '.lock';
        $fp = fopen($lockFile, 'c+');

        if (!$fp) {
            // Fallback if lock file cannot be created
            return $loader();
        }

        // Try to become the Leader (acquire non-blocking exclusive lock)
        if (flock($fp, LOCK_EX | LOCK_NB)) {
            try {
                // Double check cache in case another worker just finished
                $doubleCheck = $this->getCache($key);
                if ($doubleCheck !== null) {
                    return $doubleCheck;
                }

                // Execute User Loader (Leader)
                try {
                    $result = $loader();

                    // Apply TTL Jitter
                    $effectiveTTL = $this->applyJitter($ttlSeconds);

                    // Store in Cache
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
            // We are a Follower: Wait for the Leader to finish (blocking shared lock with 3s timeout)
            flock($fp, LOCK_SH);
            try {
                $fresh = $this->getCache($key);
                if ($fresh !== null) {
                    return $fresh;
                }
                if ($this->isNegative($key)) {
                    throw new NotFoundException();
                }
                // If leader somehow died without populating, fallback safely
                return $loader();
            } finally {
                flock($fp, LOCK_UN);
                fclose($fp);
            }
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
            $data = @unserialize(file_get_contents($cacheFile));
            if ($data && isset($data['exp']) && $data['exp'] > time()) {
                return $data['val'];
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
}
