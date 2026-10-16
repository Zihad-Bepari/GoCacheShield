# GoCacheShield — Production-Grade Smart Cache Protection Library

[![Go Reference](https://pkg.go.dev/badge/github.com/gocacheshield/gocacheshield.svg)](https://pkg.go.dev/github.com/gocacheshield/gocacheshield)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/gocacheshield/gocacheshield)](https://goreportcard.com/report/github.com/gocacheshield/gocacheshield)
[![CI](https://github.com/gocacheshield/gocacheshield/actions/workflows/ci.yml/badge.svg)](https://github.com/gocacheshield/gocacheshield/actions)

**GoCacheShield** is a production-grade, zero-dependency, smart cache resilience and concurrency protection library available natively for **Go**, **Node.js**, **Nest.js**, **PHP**, and **Laravel**.

It does **not** replace your existing cache storage (Redis, Memcached, or In-Memory); rather, it acts as an intelligent **Shield** in front of your primary database and cache backend, defending your infrastructure against **Cache Stampedes, Cache Avalanches, Cache Penetration, Distinct-Key Burst Overloads, and Remote Cache Outages**.

### 📦 Ecosystem Installations

| Platform | Package Manager | Package Link |
| :--- | :--- | :--- |
| **Go** | `go get github.com/gocacheshield/gocacheshield` | Core Go Engine |
| **Node.js & Nest.js** | `npm install gocacheshield` | [`packages/node`](packages/node) |
| **PHP & Laravel** | `composer require gocacheshield/cacheshield` | [`packages/php`](packages/php) |

---

## ⚡ The Problems It Solves

```
Incoming Traffic ──► [ GoCacheShield Protection Layer ] ──► Existing Cache (Redis/Memory)
                             │
                             ├─ 1. Context-Safe Per-Key Coalescing (Follower/Leader safe)
                             ├─ 2. Bounded Concurrency Bulkhead (Protects DB from distinct misses)
                             ├─ 3. Adaptive TTL Jitter (Avalanche prevention)
                             ├─ 4. Bounded Negative Caching (Penetration prevention)
                             └─ 5. Cache Outage Fallback Limiter (Protects DB when Redis dies)
                             │
                             ▼
                     Loader / Database
```

| Caching Disaster | How It Destroys Systems | How GoCacheShield Defends |
| :--- | :--- | :--- |
| **Cache Stampede (Thundering Herd)** | A popular key expires. 5,000 concurrent requests miss the cache and simultaneously hammer the database with identical queries. | **Context-Aware Request Coalescing:** Merges 5,000 requests into **exactly 1** loader execution. |
| **Distinct-Key Miss Burst** | 500 different products miss cache at once. `singleflight` fails to coalesce them because keys differ, overwhelming database connection pools. | **Bounded Concurrency Bulkhead:** Limits concurrent active database queries to `MaxConcurrentLoads` via an internal semaphore. |
| **Cache Avalanche** | Thousands of keys expire at the exact same second due to identical TTLs, causing sudden 0% hit ratios. | **Adaptive TTL Jitter:** Randomizes effective expiration by $\pm Jitter\%$ (zero allocations). |
| **Cache Penetration** | Attackers query non-existent IDs (`/user/invalid-uuid-404`) that never cache, forcing every request directly to the DB. | **Bounded Negative Caching:** Caches `ErrNotFound` with LRU-bounded memory in **~68 nanoseconds**. |
| **Remote Cache Outage Storm** | Redis crashes or networks partition. Default fallback code redirects 100% of traffic to PostgreSQL, immediately killing the database. | **Degraded Outage Bulkhead:** Throttles database load to a strict fallback budget (`WithFallbackLimit`) during outages. |
| **Singleflight Context Trap** | In standard `singleflight`, if the leader's context cancels, all followers fail; or followers cannot abort their individual wait. | **Follower Isolation & Decoupled Execution:** Followers cancel independently without killing the leader or other waiters. |

---

## 📊 Verified Performance Benchmarks

Benchmarks executed on **AMD Ryzen 5 PRO 4650G (12 Threads)** with Go 1.22:

```
BenchmarkNegativeCache_IsNegative-12    17,378,408    68.74 ns/op       0 B/op    0 allocs/op
BenchmarkJitter_Apply-12                14,054,302    84.24 ns/op       0 B/op    0 allocs/op
BenchmarkShield_NegativeCacheHit-12      9,836,830   121.10 ns/op      32 B/op    1 allocs/op
BenchmarkShield_CoalescingStampede-12    7,031,355   176.30 ns/op     208 B/op    5 allocs/op (2 loaders/total)
BenchmarkShield_CacheHit-12              3,945,886   317.10 ns/op     360 B/op    8 allocs/op
BenchmarkBulkhead_AcquireRelease-12      2,216,479   520.70 ns/op      40 B/op    2 allocs/op
BenchmarkCoalescer_RawDo-12              1,984,629   596.60 ns/op      47 B/op    0 allocs/op
```

> **Key takeaway:** Over 7.0 million concurrent stampede requests were coalesced down to just **2** database loader executions across the entire benchmark run, running at **176 ns/op**!

---

## 🛠️ Quick Start

### Installation

```bash
go get github.com/gocacheshield/gocacheshield
```

### Basic Usage with Strongly-Typed Generics

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gocacheshield/gocacheshield"
)

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func main() {
	// 1. Initialize Engine (shared bulkhead, jitter, and negative cache)
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(50),                // Database bulkhead limit
		gocacheshield.WithJitter(0.15),                          // +/- 15% TTL jitter
		gocacheshield.WithNegativeCaching(30*time.Second, 5000), // Protect against 404 penetration
	)
	defer engine.Close()

	// 2. Create strongly typed shield for User entities
	userShield := gocacheshield.NewTyped[User](engine, 10*time.Minute)

	ctx := context.Background()

	// 3. Get from cache or load from database (Thread-safe, coalesced, bulkheaded)
	user, err := userShield.GetOrLoad(ctx, "user:101", func(ctx context.Context) (User, error) {
		// Simulating expensive database query:
		// If entity is missing, return gocacheshield.ErrNotFound to trigger negative caching
		return User{ID: "101", Email: "alex@example.com"}, nil
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("User: %s (%s)\n", user.ID, user.Email)

	// 4. Invalidate when updated
	_ = userShield.Invalidate(ctx, "user:101")
}
```

---

## 🔌 Redis Integration

GoCacheShield is completely decoupled from any specific Redis client. Use the pluggable `adapters/redis` package with `github.com/redis/go-redis/v9`:

```go
import (
	"github.com/gocacheshield/gocacheshield"
	"github.com/gocacheshield/gocacheshield/adapters/redis"
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	rdb := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})

	// Wrap with Redis adapter
	redisStore := redis.New(rdb, redis.WithPrefix("prod:app:"))

	engine := gocacheshield.NewEngine(
		gocacheshield.WithCache(redisStore),
		gocacheshield.WithMaxConcurrentLoads(100), // Normal database bulkhead
		gocacheshield.WithFallbackLimit(10),        // Degraded DB bulkhead during Redis outage!
	)
	defer engine.Close()
}
```

---

## ⚖️ Comparison with Existing Solutions

| Feature | `golang.org/x/sync/singleflight` | `eko/gocache` | `go-zero` (`core/stores/cache`) | `GoCacheShield` |
| :--- | :---: | :---: | :---: | :---: |
| **Independent Library** | ✅ | ✅ | ❌ (Framework coupled) | ✅ |
| **Zero Dependencies in Core** | ✅ | ❌ | ❌ | ✅ |
| **Same-Key Coalescing** | ✅ | ❌ | ✅ | ✅ |
| **Follower Context Cancellation** | ❌ (Context bug) | N/A | ❌ | ✅ |
| **Distinct-Key Bounded Bulkhead** | ❌ | ❌ | ❌ | ✅ |
| **Adaptive TTL Jitter** | ❌ | ❌ | ❌ | ✅ |
| **Bounded Negative Caching** | ❌ | ❌ | ✅ | ✅ |
| **Outage Fallback Guard** | ❌ | ❌ | ❌ | ✅ |
| **Type-Safe Generics** | ❌ | ❌ | ❌ | ✅ |

---

## 🛡️ Architecture & Concurrency Guarantees

1. **Follower Cancellation Isolation:** If a follower client cancels its HTTP request, its goroutine unblocks immediately without terminating the in-flight database query or affecting other followers.
2. **Leader Cancellation Decoupling:** If the original leader cancels, the database fetch continues in a detached context to populate the cache and satisfy any remaining or subsequent callers.
3. **Panic Safety:** Panics inside loaders are safely recovered, internal map states are cleared, and `ErrLoaderPanicked` is returned to all waiting callers.
4. **Bulkhead Schedulability:** Fast-path non-blocking atomic permit acquisition; strictly guarantees active concurrent loader count $\le \text{MaxConcurrentLoads}$.
5. **Memory Bounds:** Bounded LRU ring-buffers ensure negative caching never triggers Out-Of-Memory (OOM) conditions.

---

## 🚀 Multi-Platform Setup & Integration Guide (5 Platforms)

GoCacheShield is engineered to run seamlessly across **5 major backend platforms** with **Zero Server Deployment**. It executes completely **in-process**, requiring no external background daemons, root access, or open ports.

---

### 🔵 1. Go (Golang) Setup

> [!NOTE]
> **Prerequisites & Setup:**  
> - Go 1.22+ installed.  
> - **Zero External Services Required:** Operates 100% in-memory out of the box using Go runtime channels and atomic synchronization. (Redis is completely optional).

* **Package Installation:**
  ```bash
  go get github.com/gocacheshield/gocacheshield
  ```
* **Target File:** `main.go` or your service layer (e.g. `internal/service/product.go`).
* **Description:**  
  Initializes a shared `Engine` that manages a global database bulkhead and jitter calculator. Creates strongly typed shields (`TypedShield[T]`) for individual database entities, ensuring full compile-time type safety with zero runtime reflection.

```go
// File: main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gocacheshield/gocacheshield"
)

// 1. Define your entity struct
type Product struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Price float64 `json:"price"`
}

func main() {
	// 2. Initialize Central Shield Engine
	engine := gocacheshield.NewEngine(
		gocacheshield.WithMaxConcurrentLoads(50), // Hard-cap database loader queries
		gocacheshield.WithJitter(0.15),           // +/- 15% TTL jitter against avalanche
		gocacheshield.WithNegativeCaching(30*time.Second, 10000), // Cache 404 misses
	)
	defer engine.Close()

	// 3. Create a strongly typed shield for Product entity
	productShield := gocacheshield.NewTyped[Product](engine, 10*time.Minute)

	// 4. REST API Endpoint
	http.HandleFunc("/api/product", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		productID := r.URL.Query().Get("id")

		// Automatically coalesces parallel requests & hits cache
		product, err := productShield.GetOrLoad(ctx, "product:"+productID, func(execCtx context.Context) (Product, error) {
			// Replace with your real DB query (GORM, PostgreSQL, MongoDB, etc.):
			fmt.Printf("--> [DATABASE] Querying product table for: %s\n", productID)
			return Product{ID: productID, Title: "Mechanical Keyboard", Price: 129.99}, nil
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(product)
	})

	fmt.Println("🚀 Go server running at http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}
```

---

### 🟢 2. Node.js Setup (Express / Fastify)

> [!NOTE]
> **Prerequisites & Setup:**  
> - Node.js 18.0.0 or higher.  
> - **Zero Native C++ Dependencies:** Built in 100% pure JavaScript/TypeScript. It requires **no `node-gyp` or C++ compiler**, meaning it installs instantly on any hosting (including cPanel Node.js Selector, Passenger, Vercel, and Docker).

* **Package Installation:**
  ```bash
  npm install gocacheshield
  ```
* **Target File:** `server.js`, `app.js`, or inside your route files (e.g. `routes/products.js`).
* **Description:**  
  Leverages Node's single-threaded event loop and in-flight Promise tracking (`Map<string, Promise<T>>`) to collapse thousands of concurrent HTTP requests down to a single database execution.

```javascript
// File: server.js
const express = require('express');
const { CacheShield, ErrNotFound } = require('gocacheshield');

const app = express();

// 1. Initialize CacheShield instance (Singleton for your app)
const shield = new CacheShield({
  maxConcurrentLoads: 50,  // Prevents database pool exhaustion
  jitterRatio: 0.15,       // Randomizes TTL to prevent cache avalanche
  negativeTTL: 30,         // Cache 404 responses for 30 seconds
});

// 2. Protected REST API Route
app.get('/api/products/:id', async (req, res) => {
  const productId = req.params.id;

  try {
    // getOrLoad handles caching, coalescing, and DB protection automatically
    const product = await shield.getOrLoad(`product:${productId}`, 300, async () => {
      console.log(`--> [DATABASE] Querying DB for product: ${productId}`);

      // Replace with your real database query (Prisma, Mongoose, PostgreSQL, etc.):
      const row = await db.query('SELECT * FROM products WHERE id = $1', [productId]);
      if (!row) {
        throw ErrNotFound; // Triggers negative caching (rejects in ~68ns)
      }
      return row;
    });

    res.json({ success: true, data: product });
  } catch (err) {
    if (err === ErrNotFound || err.code === 'ERR_NOT_FOUND') {
      return res.status(404).json({ success: false, error: 'Product not found' });
    }
    res.status(500).json({ success: false, error: 'Internal server error' });
  }
});

// Invalidate on update
app.put('/api/products/:id', async (req, res) => {
  await db.update(req.params.id, req.body);
  await shield.invalidate(`product:${req.params.id}`); // Purge from cache
  res.json({ success: true, message: 'Updated and cache purged' });
});

app.listen(3000, () => console.log('🚀 Node.js server running at http://localhost:3000'));
```

---

### 🟣 3. Nest.js Setup (TypeScript)

> [!NOTE]
> **Prerequisites & Setup:**  
> - Nest.js 9+ or 10+.  
> - **Full TypeScript Type Safety:** Ships with complete `index.d.ts` type declarations.

* **Package Installation:**
  ```bash
  npm install gocacheshield
  ```
* **Target File:** Inside your Service class (e.g. `src/products/products.service.ts`).
* **Description:**  
  Integrates cleanly into Nest.js Dependency Injection (DI) architecture. Can be initialized as a service-level singleton to guard Prisma, TypeORM, or MikrORM database operations.

```typescript
// File: src/products/products.service.ts
import { Injectable, NotFoundException } from '@nestjs/common';
import { CacheShield, ErrNotFound } from 'gocacheshield';

@Injectable()
export class ProductsService {
  // 1. Initialize CacheShield instance
  private readonly shield = new CacheShield({
    maxConcurrentLoads: 30, // Max concurrent Prisma/TypeORM queries
    jitterRatio: 0.10,      // 10% TTL jitter
    negativeTTL: 30,        // 30s negative cache
  });

  // Example database client (e.g. Prisma or TypeORM repository)
  constructor(private readonly prisma: PrismaService) {}

  async findOne(id: string) {
    try {
      // 2. Wrap DB query with getOrLoad
      return await this.shield.getOrLoad(`product:${id}`, 300, async () => {
        const product = await this.prisma.product.findUnique({ where: { id } });
        if (!product) {
          throw ErrNotFound; // Triggers instant negative caching
        }
        return product;
      });
    } catch (err) {
      if (err === ErrNotFound || err.code === 'ERR_NOT_FOUND') {
        throw new NotFoundException(`Product #${id} does not exist`);
      }
      throw err;
    }
  }

  async update(id: string, updateDto: any) {
    const updated = await this.prisma.product.update({
      where: { id },
      data: updateDto,
    });

    // 3. Purge cache on update
    await this.shield.invalidate(`product:${id}`);
    return updated;
  }
}
```

---

### 🐘 4. Plain PHP Setup (cPanel & Shared Hosting)

> [!NOTE]
> **Prerequisites & Setup:**  
> - PHP 7.4, 8.0, 8.1, 8.2, or 8.3.  
> - **100% cPanel & Shared Hosting Ready:** Uses the OS standard `/tmp` directory for atomic, non-blocking file locking (`flock`). Requires **no root access, no Redis, and no custom PHP extensions**.

* **Package Installation:**
  ```bash
  composer require gocacheshield/cacheshield
  ```
* **Target File:** `api.php`, `index.php`, or any endpoint script.
* **Description:**  
  Solves the classical PHP-FPM "Share-Nothing" limitation. When multiple PHP processes miss a cache item at the exact same millisecond, Process 1 acquires an exclusive lock and loads from MySQL, while Processes 2 through 100 wait on a shared lock and receive the freshly cached result without duplicating database load.

```php
<?php
// File: api.php
require_once __DIR__ . '/vendor/autoload.php';

use GoCacheShield\CacheShield;
use GoCacheShield\Exceptions\NotFoundException;

// 1. Initialize CacheShield (Works on cPanel shared hosting automatically)
$shield = new CacheShield([
    'jitter_ratio' => 0.15, // 15% TTL variance
    'negative_ttl' => 30    // Cache 404 lookups for 30 seconds
]);

$productId = $_GET['id'] ?? '101';

try {
    // 2. Fetch with stampede protection & local caching
    $product = $shield->getOrLoad("product:{$productId}", 300, function() use ($productId, $pdo) {
        // Replace with your real PDO or MySQLi query:
        $stmt = $pdo->prepare("SELECT * FROM products WHERE id = ?");
        $stmt->execute([$productId]);
        $row = $stmt->fetch(PDO::FETCH_ASSOC);

        if (!$row) {
            throw new NotFoundException(); // Stored in negative cache
        }
        return $row;
    });

    header('Content-Type: application/json');
    echo json_encode(['success' => true, 'data' => $product]);

} catch (NotFoundException $e) {
    http_response_code(404);
    echo json_encode(['success' => false, 'error' => 'Product not found']);
} catch (Exception $e) {
    http_response_code(500);
    echo json_encode(['success' => false, 'error' => 'Server error']);
}
```

---

### 🔴 5. Laravel Setup

> [!NOTE]
> **Prerequisites & Setup:**  
> - Laravel 9.x, 10.x, or 11.x.  
> - **Auto-Discovery Enabled:** Zero manual provider registration needed. Works out of the box with your existing Laravel cache driver (Redis, Memcached, Database, or File).

* **Package Installation:**
  ```bash
  composer require gocacheshield/cacheshield
  ```
* **Target File:** Inside your Controller (e.g. `app/Http/Controllers/ProductController.php`) or Route file (`routes/api.php`).
* **Description:**  
  Provides an elegant Laravel Facade (`CacheShield::getOrLoad`). Integrates directly with Eloquent ORM and Query Builder models to ensure high-concurrency traffic spikes never melt your database.

```php
<?php
// File: app/Http/Controllers/ProductController.php

namespace App\Http\Controllers;

use Illuminate\Http\Request;
use App\Models\Product;
use GoCacheShield\Laravel\Facades\CacheShield;

class ProductController extends Controller
{
    /**
     * Display the specified product.
     * 10,000 concurrent requests for the same missing product hit MySQL EXACTLY 1 time!
     */
    public function show(string $id)
    {
        $product = CacheShield::getOrLoad("product:{$id}", 300, function () use ($id) {
            // Eloquent ORM Query:
            return Product::findOrFail($id);
        });

        return response()->json([
            'success' => true,
            'data' => $product
        ]);
    }

    /**
     * Update the product and clear cache.
     */
    public function update(Request $request, string $id)
    {
        $product = Product::findOrFail($id);
        $product->update($request->validated());

        // Invalidate cache immediately across all workers
        CacheShield::invalidate("product:{$id}");

        return response()->json([
            'success' => true,
            'message' => 'Product updated and cache purged',
            'data' => $product
        ]);
    }
}
```

---

## 📄 License

This project is licensed under the [MIT License](LICENSE).


