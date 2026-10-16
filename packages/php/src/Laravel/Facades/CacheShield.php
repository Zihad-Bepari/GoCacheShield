<?php

namespace GoCacheShield\Laravel\Facades;

use Illuminate\Support\Facades\Facade;

/**
 * @method static mixed getOrLoad(string $key, int $ttlSeconds, \Closure $loader)
 * @method static void invalidate(string $key)
 *
 * @see \GoCacheShield\CacheShield
 */
class CacheShield extends Facade
{
    protected static function getFacadeAccessor()
    {
        return 'cacheshield';
    }
}
