<?php

namespace GoCacheShield\Laravel;

use Illuminate\Support\ServiceProvider;
use GoCacheShield\CacheShield;
use Illuminate\Support\Facades\Cache;

class CacheShieldServiceProvider extends ServiceProvider
{
    public function register()
    {
        $this->app->singleton('cacheshield', function ($app) {
            return new CacheShield([
                'jitter_ratio' => config('cacheshield.jitter_ratio', 0.10),
                'negative_ttl' => config('cacheshield.negative_ttl', 30),
                // Integrate directly with Laravel's active Cache driver (Redis, Memcached, File, etc.)
                'cache_driver' => [
                    'get' => fn($key) => Cache::get($key),
                    'set' => fn($key, $val, $ttl) => Cache::put($key, $val, $ttl),
                    'delete' => fn($key) => Cache::forget($key),
                ]
            ]);
        });
    }

    public function boot()
    {
        // Auto-loaded in Laravel projects
    }
}
