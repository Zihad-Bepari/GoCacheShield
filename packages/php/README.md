# CacheShield for PHP & Laravel

Production-grade smart cache protection for PHP and Laravel — Stampede coalescing, TTL jitter, and penetration defense that works on **all environments, including cPanel and Shared Hosting**.

## Installation

```bash
composer require gocacheshield/cacheshield
```

## Laravel Usage

The package automatically registers via Laravel Package Discovery.

```php
use GoCacheShield\Laravel\Facades\CacheShield;
use App\Models\Product;

class ProductController extends Controller
{
    public function show($id)
    {
        // 10 concurrent requests for the same missing product run the DB query EXACTLY 1 time!
        $product = CacheShield::getOrLoad("product:{$id}", 300, function () use ($id) {
            return Product::findOrFail($id);
        });

        return response()->json($product);
    }

    public function update(Request $request, $id)
    {
        $product = Product::findOrFail($id);
        $product->update($request->all());

        // Invalidate cache
        CacheShield::invalidate("product:{$id}");

        return response()->json($product);
    }
}
```

## Plain PHP Usage (Works on cPanel & Shared Hosting)

```php
require_once __DIR__ . '/vendor/autoload.php';

use GoCacheShield\CacheShield;

$shield = new CacheShield();

$user = $shield->getOrLoad('user:101', 300, function() {
    // Database query here:
    return ['id' => 101, 'name' => 'Zihad'];
});
```
