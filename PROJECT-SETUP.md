# GoCacheShield — Setup & Integration Guide

A simple, step-by-step guide to installing and using **GoCacheShield** in **Go**, **Node.js**, **NestJS**, **PHP**, and **Laravel**.

---

## 📑 Table of Contents
1. [Installation Summary](#1-installation-summary)
2. [Go Setup](#2-go-setup)
3. [Node.js & Express Setup](#3-nodejs--express-setup)
4. [NestJS Setup](#4-nestjs-setup)
5. [PHP (Plain) Setup](#5-php-plain-setup)
6. [Laravel Setup](#6-laravel-setup)
7. [Configuration Options](#7-configuration-options)

---

## 1. Installation Summary

| Platform | Command | Notes |
| :--- | :--- | :--- |
| **Go** | `go get github.com/Zihad-Bepari/GoCacheShield` | Go 1.22 or higher |
| **Node.js** | `npm install github:Zihad-Bepari/GoCacheShield` | Node 18 or higher |
| **NestJS** | `npm install github:Zihad-Bepari/GoCacheShield` | NestJS 9 or higher |
| **PHP / Laravel** | `composer require zihad-bepari/gocacheshield:dev-main` | PHP 8.1 or higher |

---

## 2. Go Setup

### Install:
```bash
go get github.com/Zihad-Bepari/GoCacheShield
```

### Basic Usage:
```go
package main

import (
    "context"
    "fmt"
    "time"
    "github.com/Zihad-Bepari/GoCacheShield"
)

func main() {
    engine := gocacheshield.NewEngine(
        gocacheshield.WithMaxConcurrentLoads(50), // Max 50 parallel DB queries
        gocacheshield.WithJitter(0.10),           // +/- 10% TTL jitter
    )

    ctx := context.Background()

    // 100 concurrent requests for this key only execute the loader ONCE
    val, err := engine.GetOrLoadRaw(ctx, "user:123", 5*time.Minute, func(c context.Context) ([]byte, error) {
        fmt.Println("Fetching from database...")
        return []byte(`{"id": 123, "name": "John"}`), nil
    })

    fmt.Println(string(val), err)
}
```

### Optional Redis Setup:
```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/Zihad-Bepari/GoCacheShield/adapters/redis"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
redisStore := redisadapter.New(rdb)

engine := gocacheshield.NewEngine(
    gocacheshield.WithStore(redisStore),
)
```

---

## 3. Node.js & Express Setup

### Install:
```bash
npm install github:Zihad-Bepari/GoCacheShield
```

### Express Middleware (`app.js`):
```javascript
const express = require('express');
const { cacheShieldMiddleware } = require('gocacheshield');

const app = express();

// Automatically caches all GET requests for 60 seconds with stampede protection
app.use(cacheShieldMiddleware({ ttl: 60 }));

app.get('/api/products/:id', (req, res) => {
  // Only called on cache miss; duplicate requests are merged
  res.json({ id: req.params.id, name: 'Phone' });
});

app.listen(3000);
```

---

## 4. NestJS Setup

### Route Interceptor on Controller:
```typescript
import { Controller, Get, UseInterceptors } from '@nestjs/common';
import { CacheShieldInterceptor, CacheShield } from 'gocacheshield';

@Controller('products')
@UseInterceptors(new CacheShieldInterceptor(new CacheShield(), { ttl: 60 }))
export class ProductController {
  @Get()
  getProducts() {
    return [{ id: 1, name: 'Laptop' }];
  }
}
```

### Service-level Usage:
```typescript
import { Injectable } from '@nestjs/common';
import { createNestCacheShield, CacheShield } from 'gocacheshield';

@Injectable()
export class ProductService {
  private shield: CacheShield = createNestCacheShield({ maxConcurrentLoads: 20 });

  async getProduct(id: string) {
    return await this.shield.getOrLoad(`product:${id}`, 300, async () => {
      return await this.dbFind(id);
    });
  }
}
```

---

## 5. PHP (Plain) Setup

### Install:
```bash
composer require zihad-bepari/gocacheshield:dev-main
```

### Usage:
```php
require 'vendor/autoload.php';

use GoCacheShield\CacheShield;

$shield = new CacheShield([
    'jitter_ratio' => 0.10,
    'negative_ttl' => 30,
]);

// 100 simultaneous requests will only run the database query ONCE
$data = $shield->getOrLoad('article_10', 300, function () {
    // Database query here
    return ['id' => 10, 'title' => 'Sample Article'];
});

print_r($data);
```

---

## 6. Laravel Setup

The ServiceProvider and Facade are registered automatically in Laravel.

### Usage in Controller:
```php
namespace App\Http\Controllers;

use GoCacheShield\Laravel\Facades\CacheShield;
use App\Models\Product;

class ProductController extends Controller
{
    public function show($id)
    {
        return CacheShield::getOrLoad("product:{$id}", 300, function () use ($id) {
            return Product::findOrFail($id);
        });
    }
}
```

---

## 7. Configuration Options

| Option | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `maxConcurrentLoads` | int | `50` | Maximum simultaneous database queries allowed |
| `jitterRatio` | float | `0.10` | Percentage of random TTL jitter (e.g. 0.10 = $\pm 10\%$) |
| `negativeTTL` | int/duration | `30s` | Seconds to cache 404 / not found queries to prevent penetration |
| `negativeCapacity` | int | `5000` | Max entries kept in negative cache |
| `waitTimeout` | duration | `1s` | Maximum wait time in queue before rejecting |
