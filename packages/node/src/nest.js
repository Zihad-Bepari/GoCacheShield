'use strict';

const { CacheShield, ErrNotFound } = require('./index');

/**
 * Creates a Nest.js CacheShield instance.
 *
 * Usage in Nest.js service:
 * 
 * @Injectable()
 * export class ProductService {
 *   private shield = createNestCacheShield({ maxConcurrentLoads: 20 });
 *
 *   async getProduct(id: string) {
 *     return this.shield.getOrLoad(`product:${id}`, 300, () => this.db.find(id));
 *   }
 * }
 */
function createNestCacheShield(options = {}) {
  return new CacheShield(options);
}

/**
 * NestJS Interceptor for automatic route caching with stampede protection.
 *
 * Usage on Controller or Handler:
 * ```typescript
 * @UseInterceptors(new CacheShieldInterceptor(new CacheShield(), { ttl: 120 }))
 * @Get('/products')
 * getProducts() { ... }
 * ```
 */
class CacheShieldInterceptor {
  constructor(shield, options = {}) {
    this.shield = shield || new CacheShield(options);
    this.ttl = options.ttl || 60;
    this.keyGenerator = options.keyGenerator || ((ctx) => {
      const req = ctx.switchToHttp().getRequest();
      return req.originalUrl || req.url || 'default_key';
    });
  }

  async intercept(context, next) {
    const http = context.switchToHttp();
    const req = http.getRequest();

    if (req && req.method !== 'GET' && req.method !== 'HEAD') {
      return next.handle();
    }

    const key = this.keyGenerator(context);
    try {
      let rxjs;
      try { rxjs = require('rxjs'); } catch (_) {}

      const data = await this.shield.getOrLoad(key, this.ttl, async () => {
        if (rxjs && rxjs.lastValueFrom) {
          return await rxjs.lastValueFrom(next.handle());
        }
        return await next.handle().toPromise();
      });

      if (rxjs && rxjs.of) {
        return rxjs.of(data);
      }
      return data;
    } catch (err) {
      if (err === ErrNotFound || err?.code === 'ERR_NOT_FOUND') {
        throw err;
      }
      return next.handle();
    }
  }
}

module.exports = {
  createNestCacheShield,
  CacheShieldInterceptor,
};
