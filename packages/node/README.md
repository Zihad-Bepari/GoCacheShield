# CacheShield for Node.js & Nest.js

Smart cache protection for Node.js and Nest.js — Request coalescing (singleflight), bulkheading, TTL jitter & negative caching.

## Installation

```bash
npm install gocacheshield
```

## Node.js (Express / Fastify) Usage

```javascript
const { CacheShield, ErrNotFound } = require('gocacheshield');

const shield = new CacheShield({
  maxConcurrentLoads: 50,  // Database bulkhead
  jitterRatio: 0.15,       // +/- 15% TTL jitter
});

// In your route handler:
app.get('/products/:id', async (req, res) => {
  try {
    const product = await shield.getOrLoad(`product:${req.params.id}`, 300, async () => {
      const row = await db.query('SELECT * FROM products WHERE id = $1', [req.params.id]);
      if (!row) throw ErrNotFound;
      return row;
    });
    res.json(product);
  } catch (err) {
    if (err === ErrNotFound) return res.status(404).send('Not Found');
    res.status(500).send('Server Error');
  }
});
```

## Nest.js Usage

```typescript
import { Injectable } from '@nestjs/common';
import { CacheShield, ErrNotFound } from 'gocacheshield';

@Injectable()
export class ProductService {
  private shield = new CacheShield({
    maxConcurrentLoads: 30,
    jitterRatio: 0.10,
  });

  async findOne(id: string) {
    return this.shield.getOrLoad(`product:${id}`, 300, async () => {
      const item = await this.prisma.product.findUnique({ where: { id } });
      if (!item) throw ErrNotFound;
      return item;
    });
  }

  async update(id: string, data: any) {
    const updated = await this.prisma.product.update({ where: { id }, data });
    await this.shield.invalidate(`product:${id}`);
    return updated;
  }
}
```
