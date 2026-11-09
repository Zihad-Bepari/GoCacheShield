'use strict';

const { CacheShield, ErrNotFound } = require('./index');

/**
 * Creates a Nest.js Interceptor or middleware helper for GoCacheShield.
 *
 * Usage in Nest.js:
 * 
 * @Injectable()
 * export class ProductService {
 *   private shield = new CacheShield({ maxConcurrentLoads: 20 });
 *
 *   async getProduct(id: string) {
 *     return this.shield.getOrLoad(`product:${id}`, 300, () => this.db.find(id));
 *   }
 * }
 */
function createNestCacheShield(options = {}) {
  return new CacheShield(options);
}

module.exports = {
  createNestCacheShield
};
