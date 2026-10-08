import test from 'node:test';
import assert from 'node:assert/strict';
import { readPlaceholder } from './read-placeholder.ts';

test('metadata refresh retains a read but identity and resource changes do not', () => {
  const answer = { records: [{ id: 'private-record' }] };
  const old = ['credential-a', 'hotel-a', '/v1/enterprise', 'old-scope'];
  assert.equal(readPlaceholder([...old.slice(0, -1), 'new-scope'], answer, old), answer);
  for (const [index, value] of [[0, 'credential-b'], [1, 'hotel-test'], [2, '/v1/host/tenants']]) {
    const next = [...old]; next[index] = value;
    assert.equal(readPlaceholder(next, answer, old), undefined);
  }
  assert.equal(readPlaceholder(old, answer), undefined);
});

test('an inventory retains only the same mode and bound', () => {
  const answer = { records: [{ id: 'row' }] };
  const old = ['credential-a', 'hotel-a', '/v1/records/example', 'inventory', 100, 'old-scope'];
  assert.equal(readPlaceholder([...old.slice(0, -1), 'new-scope'], answer, old), answer);
  const next = [...old]; next[4] = 200;
  assert.equal(readPlaceholder(next, answer, old), undefined);
  assert.equal(readPlaceholder([old[0], old[1], old[2], 'new-scope'], answer, old), undefined);
});
