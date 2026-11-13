# GoCacheShield — Project Setup & Integration Guide

This guide provides step-by-step instructions for installing, configuring, and deploying **GoCacheShield** across all supported environments: **Go (Golang)**, **Node.js & Express**, **NestJS**, **PHP Native**, and **Laravel**.

---

## 📑 Table of Contents
1. [Supported Platforms & Installation Summary](#supported-platforms--installation-summary)
2. [1. Go (Golang) Setup](#1-go-golang-setup)
   - [Installation](#go-installation)
   - [Basic Engine Setup](#basic-engine-setup)
   - [Type-Safe Generics API (`TypedShield[T]`)](#type-safe-generics-api-typedshieldt)
   - [Production Redis Integration (`adapters/redis`)](#production-redis-integration-adaptersredis)
   - [Cache Invalidation](#go-cache-invalidation)
3. [2. Node.js & Express Setup](#2-nodejs--express-setup)
   - [Installation](#nodejs-installation)
   - [1-Line Plug-and-Play Express Middleware (`app.js`)](#1-line-plug-and-play-express-middleware-appjs)
   - [Custom `CacheShield` Instance Usage](#custom-cacheshield-instance-usage)
   - [Runtime Observability & Metrics](#nodejs-runtime-observability--metrics)
4. [3. NestJS Setup](#3-nestjs-setup)
   - [NestJS Installation](#nestjs-installation)
   - [NestJS Service & Controller Integration](#nestjs-service--controller-integration)
5. [4. PHP (Native / Pure) Setup](#4-php-native--pure-setup)
   - [Installation](#php-installation)
   - [Closure Pattern (`getOrLoad`)](#php-closure-pattern-getorload)
   - [APCu & File Stores for Shared Hosting](#php-apcu--file-stores-for-shared-hosting)
6. [5. Laravel Framework Setup](#5-laravel-framework-setup)
   - [Service Provider & Facade Registration](#laravel-service-provider--facade-registration)
   - [Route & Repository Usage](#laravel-route--repository-usage)
7. [Configuration Reference](#configuration-reference)
8. [Production Deployment & Tuning](#production-deployment--tuning)
   - [Redis Cluster Connection & Timeout Tuning](#redis-cluster-connection--timeout-tuning)
   - [Microservice Database Pool Alignment](#microservice-database-pool-alignment)
   - [cPanel & CloudLinux Shared Hosting Deployment](#cpanel--cloudlinux-shared-hosting-deployment)

---

## Supported Platforms & Installation Summary

| Platform | Package Manager | Direct GitHub Install Command | Minimum Runtime |
| :--- | :--- | :--- | :--- |
| **Go** | `go get` | `go get github.com/Zihad-Bepari/GoCacheShield` | Go 1.22+ |
| **Node.js** | `npm` / `yarn` / `pnpm` | `npm install github:Zihad-Bepari/GoCacheShield` | Node.js 18+ |
| **NestJS** | `npm` / `yarn` / `pnpm` | `npm install github:Zihad-Bepari/GoCacheShield` | NestJS 9+ |
| **PHP** | `composer` | `composer require zihad-bepari/gocacheshield:dev-main` | PHP 8.1+ |
| **Laravel** | `composer` | `composer require zihad-bepari/gocacheshield:dev-main` | Laravel 9, 10, 11 |

---

## 1. Go (Golang) Setup

### Go Installation

Add GoCacheShield to your `go.mod`:

```bash
go get github.com/Zihad-Bepari/GoCacheShield
```

### Basic Engine Setup

Initialize the core `Engine` with your chosen concurrency and protection parameters:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Zihad-Bepari/GoCacheShield"
)

func main() {
	// Initialize GoCacheShield Engine with in-memory store
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(50),                // Max 50 simultaneous DB queries
		gocacheshield.WithJitter(0.15),                          // +/- 15% TTL jitter to prevent avalanche
		gocacheshield.WithNegativeCaching(30*time.Second, 10000),// Protect against 404 penetration
		gocacheshield.WithFallbackLimit(10),                     // Fallback DB limit during Redis outages
	)
	defer engine.Close()

	ctx := context.Background()

	// Load raw bytes
	data, err := engine.GetOrLoadRaw(ctx, "user:101", 5*time.Minute, func(loadCtx context.Context) ([]byte, error) {
		// Executed ONLY on cache miss, merged across parallel callers
		return []byte(`{"id": 101, "name": "Alice"}`), nil
	})
	if err != nil {
		log.Fatalf("failed to get or load: %v", err)
	}

	fmt.Println("Result:", string(data))
}
```

### Type-Safe Generics API (`TypedShield[T]`)

Use `NewTyped[T]` to work with domain structs without writing manual serialization code:

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Zihad-Bepari/GoCacheShield"
)

type Product struct {
	ID    int64   `json:"id"`
	Title string  `json:"title"`
	Price float64 `json:"price"`
}

func main() {
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(100),
		gocacheshield.WithJitter(0.10),
	)
	defer engine.Close()

	// Create strongly-typed shield for Product
	productShield := gocacheshield.NewTyped[Product](engine, 10*time.Minute)

	ctx := context.Background()
	productID := int64(4521)
	cacheKey := fmt.Sprintf("product:%d", productID)

	// Fetch from cache or query database
	product, err := productShield.GetOrLoad(ctx, cacheKey, func(loadCtx context.Context) (Product, error) {
		// Simulating SQL database query:
		// row := db.QueryRowContext(loadCtx, "SELECT id, title, price FROM products WHERE id = $1", productID)
		// If not found in DB: return Product{}, gocacheshield.ErrNotFound
		return Product{ID: productID, Title: "Mechanical Keyboard", Price: 89.99}, nil
	})

	if err != nil {
		if err == gocacheshield.ErrNotFound {
			fmt.Println("Product not found (negative cached, DB protected)")
			return
		}
		panic(err)
	}

	fmt.Printf("Loaded Product: %+v\n", product)
}
```

### Production Redis Integration (`adapters/redis`)

Connect GoCacheShield to an external Redis cluster or standalone instance:

```go
package main

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/Zihad-Bepari/GoCacheShield"
	shieldRedis "github.com/Zihad-Bepari/GoCacheShield/adapters/redis"
)

func main() {
	// 1. Connect standard Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:6379",
		PoolSize:     50,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
	})

	// 2. Wrap with GoCacheShield Redis Adapter
	redisStore := shieldRedis.New(rdb)

	// 3. Mount in GoCacheShield Engine
	engine := gocacheshield.NewEngine(
		gocacheshield.WithStore(redisStore),
		gocacheshield.WithMaxConcurrentLoads(100),
		gocacheshield.WithJitter(0.15),
		gocacheshield.WithNegativeCaching(1*time.Minute, 20000),
		gocacheshield.WithFallbackLimit(15), // Protect DB if Redis connection drops
	)
	defer engine.Close()
}
```

### Go Cache Invalidation

Purge items from both the cache store and negative cache:

```go
err := productShield.Invalidate(ctx, "product:4521")
```

---

## 2. Node.js & Express Setup

### Node.js Installation

Install directly from GitHub into your Node.js project:

```bash
# Standard GitHub install:
npm install github:Zihad-Bepari/GoCacheShield
```

> **Note:** If you use yarn or pnpm:
> `yarn add github:Zihad-Bepari/GoCacheShield` or `pnpm add github:Zihad-Bepari/GoCacheShield`

### 1-Line Plug-and-Play Express Middleware (`app.js`)

To protect your Express application from Cache Stampedes, Thundering Herds, and 404 Penetration Attacks, simply plug `cacheShieldMiddleware` into `app.js`:

```javascript
// app.js
const express = require('express');
const { cacheShieldMiddleware } = require('gocacheshield');

const app = express();

// 🛡️ 1-Line Automated Route Caching & Anti-Stampede Protection (TTL: 60s):
app.use(cacheShieldMiddleware({ ttl: 60 }));

// Your normal endpoints execute safely without overloading the database:
app.get('/api/v1/articles/:id', async (req, res) => {
  // Executed ONLY on cache miss; duplicate requests are merged into 1 query
  const article = { id: req.params.id, title: 'Optimizing Microservices' };
  res.json(article);
});

app.listen(3000, () => console.log('Server running on port 3000'));
```

### Custom `CacheShield` Instance Usage

```javascript
const { CacheShield, MemoryStore } = require('gocacheshield');

// Configure custom instance
const shield = new CacheShield({
  store: new MemoryStore(10000), // Max 10,000 cached items
  maxConcurrentLoads: 50,        // Max 50 simultaneous DB loads
  jitterRatio: 0.15,             // +/- 15% TTL jitter
  negativeTTL: 30,               // 30 seconds for 404s
});

async function getUserProfile(userId) {
  const cacheKey = `user:profile:${userId}`;

  return await shield.getOrLoad(cacheKey, 300, async () => {
    // Database query executed ONLY on cache miss
    const user = await db.users.findById(userId);
    if (!user) {
      throw new Error('ERR_NOT_FOUND');
    }
    return user;
  });
}
```

### Node.js Runtime Observability & Metrics

Inspect live metrics:

```javascript
const stats = shield.getStats();
console.log({
  coalescedRequests: stats.coalescedRequests,
  savedDBQueries: stats.savedDBQueries,
  cacheHits: stats.cacheHits,
  negativeCacheHits: stats.negativeCacheHits,
  activeDBBulkhead: stats.activeDBBulkhead,
});
```

---

## 3. NestJS Setup

### NestJS Installation

Install directly from GitHub into your NestJS project:

```bash
npm install github:Zihad-Bepari/GoCacheShield
```

### NestJS Service & Controller Integration

Register GoCacheShield as an injectable service:

```typescript
// cache-shield.service.ts
import { Injectable, OnModuleDestroy } from '@nestjs/common';
import { createNestCacheShield, CacheShield } from 'gocacheshield';

@Injectable()
export class CacheShieldService implements OnModuleDestroy {
  public readonly shield: CacheShield;

  constructor() {
    this.shield = createNestCacheShield({
      maxConcurrentLoads: 100, // Max 100 parallel DB loads
      jitterRatio: 0.15,        // +/- 15% TTL jitter
      negativeTTL: 60,          // Cache 404s for 60s
    });
  }

  async onModuleDestroy() {
    await this.shield.close();
  }
}
```

Use inside any NestJS controller or service with automatic coalescing:

```typescript
// product.controller.ts
import { Controller, Get, Param } from '@nestjs/common';
import { CacheShieldService } from './cache-shield.service';

@Controller('products')
export class ProductController {
  constructor(private readonly cacheShield: CacheShieldService) {}

  @Get(':id')
  async getProduct(@Param('id') id: string) {
    return await this.cacheShield.shield.getOrLoad(`product:${id}`, 300, async () => {
      // Executed ONLY on cache miss; 100 concurrent requests coalesce into 1 DB query
      return await this.databaseFindProduct(id);
    });
  }

  private async databaseFindProduct(id: string) {
    return { id, name: 'Sample Product', price: 99.99 };
  }
}
```

---

## 4. PHP (Native / Pure) Setup

### PHP Installation

You can install GoCacheShield directly from GitHub into any PHP project using Composer.

#### Option A: Quick Command Line (Recommended)
Run these two commands in your project terminal:
```bash
composer config repositories.gocacheshield vcs https://github.com/Zihad-Bepari/GoCacheShield.git
composer require zihad-bepari/gocacheshield:dev-main
```

#### Option B: Add to `composer.json` Manually
Add GoCacheShield as a VCS repository inside your project's `composer.json`:

```json
{
  "repositories": [
    {
      "type": "vcs",
      "url": "https://github.com/Zihad-Bepari/GoCacheShield.git"
    }
  ],
  "require": {
    "zihad-bepari/gocacheshield": "dev-main"
  }
}
```

Then run:
```bash
composer update
```

### PHP Closure Pattern (`getOrLoad`)

```php
<?php

require_once __DIR__ . '/vendor/autoload.php';

use GoCacheShield\CacheShield;
use GoCacheShield\Exceptions\NotFoundException;

$shield = new CacheShield(
    maxConcurrentLoads: 20,
    jitterRatio: 0.15,
    negativeTtlSeconds: 30
);

$productId = 502;
$cacheKey = "product:{$productId}";

try {
    $product = $shield->getOrLoad($cacheKey, ttlSeconds: 600, loader: function () use ($productId) {
        // Query database
        $pdo = new PDO('mysql:host=localhost;dbname=shop', 'user', 'pass');
        $stmt = $pdo->prepare('SELECT id, name, price FROM products WHERE id = ?');
        $stmt->execute([$productId]);
        $row = $stmt->fetch(PDO::FETCH_ASSOC);

        if (!$row) {
            throw new NotFoundException("Product not found");
        }

        return $row;
    });

    header('Content-Type: application/json');
    echo json_encode($product);
} catch (NotFoundException $e) {
    http_response_code(404);
    echo json_encode(['error' => 'Product not found (Cached)']);
}
```

### PHP APCu & File Stores for Shared Hosting

For shared hosting / cPanel environments where Redis is unavailable, GoCacheShield supports native APCu shared memory or local filesystem storage in `/tmp` or `storage/`:

```php
// Use APCu shared memory store:
$shield = new CacheShield(store: 'apcu', maxConcurrentLoads: 15);

// Or file-based store:
$shield = new CacheShield(store: 'file', lockPath: sys_get_temp_dir());
```

---

## 5. Laravel Framework Setup

### Laravel Service Provider & Facade Registration

#### Laravel 11 (`bootstrap/providers.php`)
```php
return [
    App\Providers\AppServiceProvider::class,
    GoCacheShield\Laravel\CacheShieldServiceProvider::class,
];
```

#### Laravel 10 (`config/app.php`)
```php
'providers' => [
    // ...
    GoCacheShield\Laravel\CacheShieldServiceProvider::class,
],
'aliases' => [
    // ...
    'CacheShield' => GoCacheShield\Laravel\Facades\CacheShield::class,
],
```

### Laravel Route & Repository Usage

```php
use GoCacheShield\Laravel\Facades\CacheShield;

class ProductController extends Controller
{
    public function show(int $id)
    {
        $product = CacheShield::getOrLoad("product:{$id}", ttl: 300, loader: function () use ($id) {
            return Product::findOrFail($id);
        });

        return response()->json($product);
    }
}
```

---

## Configuration Reference

| Option | Go Option | Node.js Option | PHP Option | Default | Description |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Max Concurrent Loads** | `WithMaxConcurrentLoads(n)` | `maxConcurrentLoads` | `$maxConcurrentLoads` | `100` | Maximum simultaneous database queries allowed. |
| **TTL Jitter Ratio** | `WithJitter(ratio)` | `jitterRatio` | `$jitterRatio` | `0.10` *(±10%)* | Fractional delta applied to TTL to prevent avalanches. |
| **Negative Cache TTL** | `WithNegativeCaching(ttl, cap)` | `negativeTTL` | `$negativeTtlSeconds`| `30s` | Expiration for cached 404 responses. |
| **Negative Capacity** | `WithNegativeCaching(ttl, cap)` | `negativeCapacity` | `$negativeCapacity` | `10000` | Maximum entries retained in bounded LRU negative cache. |
| **Fallback Limit** | `WithFallbackLimit(n)` | `fallbackMaxConcurrent` | `$fallbackLimit` | `10` | Database query limit when cache backend (Redis) is down. |
| **Max Waiting Queue** | `WithMaxWaitingLoads(n, d)` | `maxQueue` | `$maxQueue` | `0` *(disabled)* | Maximum requests waiting for a database loader permit. |
| **Queue Timeout** | `WithMaxWaitingLoads(n, d)` | `queueTimeoutMs` | `$queueTimeoutMs` | `0` *(no timeout)*| Maximum duration a caller can wait in the loader queue. |

---

## Production Deployment & Tuning

### Redis Cluster Connection & Timeout Tuning

When deploying with Redis:
1. **Configure Aggressive Read Timeouts**: Set `ReadTimeout = 150ms-250ms`. If Redis lags, GoCacheShield safely fails over to the fallback bulkhead rather than hanging client requests.
2. **Connection Pool Sizing**: Set Redis `PoolSize` to at least `2 * MaxConcurrentLoads`.

### Microservice Database Pool Alignment

1. Set `WithMaxConcurrentLoads` equal to or slightly lower than your database connection pool size (e.g., PostgreSQL `max_connections = 100` -> `WithMaxConcurrentLoads: 50-70`).
2. This ensures that even under massive traffic spikes, your application can never exhaust database connection limits.

### cPanel & CloudLinux Shared Hosting Deployment

1. **Use APCu or Filesystem Stores**: Avoid requiring background Redis daemons.
2. **Cap Max Loads to 15**: Keep `maxConcurrentLoads: 15` to remain comfortably within CloudLinux Entry Process limits (typically 20–30 EPs).
3. **Writable Directories**: Ensure `storage/framework/cache/` or `/tmp` has write permissions for lock files and file cache storage.
