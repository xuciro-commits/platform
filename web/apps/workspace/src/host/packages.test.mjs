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
  assert.ok(before.some((p) => p.role === 'builder'));
  assert.ok(after.every((p) => p.public));
  assert.ok(after.includes(packages.find((p) => p.public && p.serves.includes('enterprise'))));
  assert.ok(availablePackages(['build'], { build: 'publisher' }).some((p) => p.role === 'publisher'));
});
