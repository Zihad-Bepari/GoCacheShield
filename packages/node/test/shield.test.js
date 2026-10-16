const { test } = require('node:test');
const assert = require('node:assert/strict');
const { CacheShield, ErrNotFound, ErrTooManyLoads } = require('../src/index');

test('Stampede Coalescing: 50 parallel requests run loader exactly 1 time', async () => {
  const shield = new CacheShield();
  let loaderCount = 0;

  const loader = async () => {
    loaderCount++;
    await new Promise(r => setTimeout(r, 50));
    return { id: 101, title: 'Test Product' };
  };

  const promises = [];
  for (let i = 0; i < 50; i++) {
    promises.push(shield.getOrLoad('prod:101', 60, loader));
  }

  const results = await Promise.all(promises);

  assert.equal(loaderCount, 1, 'loader must be called exactly 1 time');
  assert.equal(results.length, 50);
  for (const r of results) {
    assert.equal(r.title, 'Test Product');
  }
});

test('Cache Hit: subsequent calls bypass loader completely', async () => {
  const shield = new CacheShield();
  let loaderCount = 0;

  const loader = async () => {
    loaderCount++;
    return 'value-1';
  };

  const v1 = await shield.getOrLoad('key1', 60, loader);
  assert.equal(v1, 'value-1');
  assert.equal(loaderCount, 1);

  const v2 = await shield.getOrLoad('key1', 60, loader);
  assert.equal(v2, 'value-1');
  assert.equal(loaderCount, 1, 'cache hit must not invoke loader');
});

test('Bounded Bulkhead: limits concurrent loader executions', async () => {
  const shield = new CacheShield({
    maxConcurrentLoads: 2,
    maxQueue: 0 // immediate rejection
  });

  let active = 0;
  let maxObserved = 0;

  const loader = async () => {
    active++;
    maxObserved = Math.max(maxObserved, active);
    await new Promise(r => setTimeout(r, 40));
    active--;
    return 'ok';
  };

  let rejected = 0;
  const promises = [];
  for (let i = 0; i < 6; i++) {
    promises.push(
      shield.getOrLoad(`key:${i}`, 60, loader).catch(err => {
        if (err === ErrTooManyLoads) rejected++;
      })
    );
  }

  await Promise.all(promises);

  assert.ok(maxObserved <= 2, `bulkhead must not exceed 2 concurrent loads, observed: ${maxObserved}`);
  assert.ok(rejected > 0, 'overflow requests must be rejected with ErrTooManyLoads');
});

test('Negative Caching: caches ErrNotFound to prevent penetration', async () => {
  const shield = new CacheShield({ negativeTTL: 1 });
  let loaderCount = 0;

  const loader = async () => {
    loaderCount++;
    throw ErrNotFound;
  };

  // 1st call throws ErrNotFound
  await assert.rejects(
    async () => await shield.getOrLoad('invalid:user', 60, loader),
    ErrNotFound
  );
  assert.equal(loaderCount, 1);

  // Next 5 calls throw ErrNotFound WITHOUT invoking loader
  for (let i = 0; i < 5; i++) {
    await assert.rejects(
      async () => await shield.getOrLoad('invalid:user', 60, loader),
      ErrNotFound
    );
  }

  assert.equal(loaderCount, 1, 'negative caching must prevent loader execution');
});

test('Invalidation: removes item from cache', async () => {
  const shield = new CacheShield();
  let counter = 0;

  const loader = async () => {
    return ++counter;
  };

  const v1 = await shield.getOrLoad('counter', 60, loader);
  assert.equal(v1, 1);

  await shield.invalidate('counter');

  const v2 = await shield.getOrLoad('counter', 60, loader);
  assert.equal(v2, 2, 'after invalidation, loader must reload fresh data');
});

test('createNestCacheShield helper returns working instance', async () => {
  const { createNestCacheShield } = require('../src/index');
  assert.equal(typeof createNestCacheShield, 'function');
  const nestShield = createNestCacheShield({ maxConcurrentLoads: 5 });
  const res = await nestShield.getOrLoad('nest-test', 30, async () => 'hello nest');
  assert.equal(res, 'hello nest');
});
