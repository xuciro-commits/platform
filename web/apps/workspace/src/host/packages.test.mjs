import test from 'node:test';
import assert from 'node:assert/strict';
import { availablePackages, packages } from './packages.ts';

test('roles that do not change package availability keep the same loading scope', () => {
  const before = availablePackages(['core', 'enterprise', 'build'], { core: 'steward', enterprise: 'admin', build: 'builder' });
  const after = availablePackages(['core', 'enterprise', 'build'], { core: 'accountant', enterprise: 'admin', build: 'builder' });
  assert.deepEqual(after, before);
});

test('withdrawn builder packages are excluded while public governance remains', () => {
  const before = availablePackages(['build'], { build: 'builder' });
  const after = availablePackages(['build'], { build: 'user' });
  assert.ok(before.some((p) => p.roles?.includes('builder')));
  assert.ok(after.every((p) => p.public));
  assert.ok(after.includes(packages.find((p) => p.public && p.serves.includes('enterprise'))));
  assert.ok(availablePackages(['build'], { build: 'publisher' }).some((p) => p.roles?.includes('publisher')));
});

test('authoring role transitions retain one package while view permissions change', () => {
  const before = availablePackages(['build'], { build: 'builder' });
  for (const role of ['integrator', 'publisher']) {
    const after = availablePackages(['build'], { build: role });
    assert.deepEqual(after, before);
    assert.equal(after.find(p => p.serves.includes('build')), before.find(p => p.serves.includes('build')));
  }
});
