import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { routeApplication, sharedViewApplication } from './navigation.ts';
import { legacyRoute, resolveLegacyRoute } from './legacy.ts';

const entries = [{ id: 'build', surface: 'studio' }, { id: 'knowledge', surface: 'work' }, { id: 'runs' }, { id: 'platform', surface: 'tenant' }];
const reader = (records, reads = []) => async (type, id) => {
  reads.push([type, id]);
  if (!records.has(`${type}/${id}`)) throw new Error(`/v1/records/${type}/${id}: HTTP 404`);
  return { record: { id } };
};

test('shared tools return to their primary app even from an old surface-qualified link', () => {
  for (const [view, owner] of [['explorer', 'core'], ['saved', 'core'], ['observations', 'data-connection'], ['lineage', 'data-connection'], ['assistant', 'agents']]) {
    const route = { view, params: { surface: 'studio' } };
    assert.equal(sharedViewApplication(route), owner);
    assert.equal(routeApplication(route, [...entries, { id: owner }], sharedViewApplication(route)), owner);
  }
  assert.equal(sharedViewApplication({ view: 'home' }), undefined);
});

test('an explicit Knowledge choice beats the old Flow surface until its own route arrives', () => {
  const old = { view: 'flow', params: { id: 'LITE-NATIVE-P', surface: 'studio' } };
  assert.equal(routeApplication(old, entries, 'build', 'knowledge'), undefined);
  assert.equal(routeApplication({ view: 'knowledge' }, entries, 'knowledge', 'knowledge'), 'knowledge');
  assert.equal(routeApplication(old, entries, 'build'), 'build');
});

test('an exact view owner wins over a legacy surface, including Runs under the old tenant projection', () => {
  assert.equal(routeApplication({ view: 'automation-operations', params: { surface: 'tenant' } }, entries, 'runs'), 'runs');
  assert.equal(routeApplication({ view: 'definition', params: { surface: 'studio' } }, entries), 'build');
  assert.equal(routeApplication({ view: 'record' }, entries, undefined, 'knowledge'), undefined);
});

test('retired builder links retain their entity and application without runtime-ID guessing', async () => {
  const route = { view: 'workflow', params: { id: 'a:b', application: 'APP', ignored: 'old' } };
  const expected = { view: 'flow', params: { id: 'a:b', application: 'APP' } };
  assert.deepEqual(legacyRoute(route), expected);
  assert.deepEqual(await resolveLegacyRoute(route, { builder: true, read: () => { throw new Error('must not probe a known builder alias'); } }), expected);
  assert.equal(legacyRoute({ view: 'constructor' }), undefined);
});

test('old runtime flow links resolve the actual record type even when the ID has no separators', async () => {
  const route = { view: 'flow', params: { id: 'FLOW-SAMPLE', application: 'APP' } };
  const reads = [];
  const result = await resolveLegacyRoute(route, { builder: true, read: reader(new Set(['flow.instance/FLOW-SAMPLE']), reads) });
  assert.deepEqual(result, { ...route, view: 'flow-instance' });
  assert.deepEqual(reads, [['build.process', 'FLOW-SAMPLE'], ['flow.instance', 'FLOW-SAMPLE']]);
});

test('a builder record keeps its identity, including colon IDs and identical IDs in two record types', async () => {
  const route = { view: 'flow', params: { id: 'source:7' } };
  const reads = [];
  assert.equal(await resolveLegacyRoute(route, { builder: true, read: reader(new Set(['build.process/source:7', 'flow.instance/source:7']), reads) }), route);
  assert.deepEqual(reads, [['build.process', 'source:7']]);
  const draft = { view: 'flow', params: { id: 'new' } };
  assert.equal(await resolveLegacyRoute(draft, { builder: true, read: () => { throw new Error('new draft has no record'); } }), draft);
});

test('operator-only flow links do not probe unauthorized builder records; refused reads do not invent an instance', async () => {
  const route = { view: 'flow', params: { id: 'RUN' } };
  const reads = [];
  assert.deepEqual(await resolveLegacyRoute(route, { builder: false, read: reader(new Set(['flow.instance/RUN']), reads) }), { ...route, view: 'flow-instance' });
  assert.deepEqual(reads, [['flow.instance', 'RUN']]);
  assert.equal(await resolveLegacyRoute(route, { builder: false, read: async () => { throw new Error('HTTP 401'); } }), route);
  assert.equal(await resolveLegacyRoute(route, { builder: true, read: async () => ({ record: null }) }), route);
  await assert.rejects(resolveLegacyRoute(route, { builder: true, read: async () => { throw new Error('offline'); } }), /offline/);
});

test('legacy automation lists use the original surface or saved owner; builder entity links stay builder-owned', async () => {
  for (const [params, application, builder] of [[{ surface: 'tenant', tab: 'deliveries' }, undefined, true], [undefined, 'runs', true], [undefined, undefined, false]]) {
    const result = await resolveLegacyRoute({ view: 'automation', params }, { builder, application, read: async () => { throw new Error('list has no entity'); } });
    assert.equal(result.view, 'automation-operations');
    if (params?.tab) assert.equal(result.params.tab, params.tab);
  }
  for (const params of [{ surface: 'studio' }, { id: 'PROCESS' }, { application: 'APP' }]) {
    const route = { view: 'automation', params };
    assert.equal(await resolveLegacyRoute(route, { builder: true, application: 'runs', read: async () => undefined }), route);
  }
});

test('built-in explicit views have one global declaration across chrome, Builder and platform packages', () => {
  const ids = new Map();
  for (const path of ['../chrome.tsx', '../../../../packages/build/src/index.tsx', '../../../../packages/govern/src/index.tsx']) {
    const source = readFileSync(new URL(path, import.meta.url), 'utf8');
    // These explicit declaration rows are the common contract of all three
    // owners. This check needs no compiler service or package loading.
    for (const [, id] of source.matchAll(/\{\s*id:\s*"([^"]+)"\s*,\s*title:[\s\S]*?\brender:/g)) {
      assert.ok(!ids.has(id), `view ${id} belongs to both ${ids.get(id)} and ${path}`);
      ids.set(id, path);
    }
  }
  assert.equal(ids.get('flow'), '../../../../packages/build/src/index.tsx');
  assert.equal(ids.get('flow-instance'), '../chrome.tsx');
  assert.equal(ids.get('automation-operations'), '../../../../packages/govern/src/index.tsx');
});
