import test from 'node:test';
import assert from 'node:assert/strict';
import { applicationEntry, applicationUIInfo, composeApplications, extendApplications, pageApplication, resolveApplicationRoute } from './applications.ts';

test('one functional app combines original views and menu groups while preserving role-specific homes', () => {
  const connection = { view: 'connection' }, connector = { view: 'integrations' };
  const editor = { id: 'connection', render: () => 'original editor' };
  const runtime = { id: 'integrations', render: () => 'original connectors' };
  const authoring = {
    id: 'data-connection', serves: ['build'], title: 'Data Connection', home: connection,
    for: host => host.roles.includes('builder'), views: [editor], opens: { 'build.connection': 'connection' },
    nav: () => [{ label: 'Connections', items: [{ label: 'Connections', route: connection }] }],
  };
  const administration = {
    id: 'data-connection', serves: ['platform'], title: 'Data Connection', home: connector,
    for: host => host.roles.includes('admin'), views: [runtime],
    nav: () => [{ label: 'Connections', items: [{ label: 'Connectors', route: connector }] }],
  };
  const [app] = composeApplications([authoring, administration]);
  const builder = { roles: ['builder'] }, admin = { roles: ['admin'] }, both = { roles: ['builder', 'admin'] };
  assert.equal(composeApplications([authoring, administration]).length, 1);
  assert.deepEqual(app.serves, ['build', 'platform']);
  assert.equal(app.views[0], editor);
  assert.equal(app.views[1], runtime);
  assert.equal(app.homeFor(builder), connection);
  assert.equal(app.homeFor(admin), connector);
  assert.equal(app.for({ roles: [] }), false);
  assert.deepEqual(app.nav(builder), authoring.nav());
  assert.deepEqual(app.nav(admin), administration.nav());
  assert.equal(app.nav(both).length, 1);
  assert.deepEqual(app.nav(both)[0].items.map(item => item.route), [connection, connector]);
  assert.deepEqual(app.opens, authoring.opens);
});

const native = (id = 'mes', serves = [id]) => ({
  id, serves, title: id, home: { view: 'craft' },
  views: [{ id: 'craft', title: () => 'Routings', render: () => 'native' }, { id: 'sfc', title: params => params.id ?? 'SFC', render: () => 'detail' }],
  opens: { 'mes.sfc': 'sfc' }, nav: () => [{ label: 'Operations', items: [{ label: 'Routings', route: { view: 'craft' } }] }],
});
const nativeDefinition = (app = 'mes') => ({ ref: { app, kind: 'app', name: app }, source: 'code', application: { name: app, title: app, pages: [] } });
const extension = (name, { entry, replacements = [], pages = ['routes', 'capacity'], groups } = {}) => ({
  ref: { app: 'build', kind: 'app', name }, source: 'tenant',
  application: { name, title: name, extends: { app: 'mes', kind: 'app', name: 'mes' }, entry, replacements, pages, groups },
});
const page = name => ({ ref: { app: 'build', kind: 'page', name }, source: 'tenant', page: { title: name } });
const metadata = (...apps) => applicationUIInfo(apps, {});

test('UI metadata uses only loaded contributions allowed by their original for and nav', () => {
  const code = native();
  const hidden = { ...native('hidden'), for: () => false, nav: () => { throw new Error('hidden navigation must not be read'); } };
  const host = {};
  assert.deepEqual(applicationUIInfo([code, hidden], host), [{
    id: 'mes', title: 'mes', serves: ['mes'], home: code.home, sections: code.nav(),
    views: [{ id: 'craft', title: 'Routings' }, { id: 'sfc', title: 'SFC' }], opens: { 'mes.sfc': 'sfc' },
  }]);
});

test('published extensions retain the original entry and views, replace its menu route, and append new pages in stable order', () => {
  const code = native();
  const takeover = extension('z-routing', { replacements: [{ view: 'craft', page: 'routes' }] });
  const added = extension('a-capacity', { pages: ['capacity'], groups: [{ title: 'Planning', pages: ['capacity'] }] });
  const definitions = [nativeDefinition(), takeover, added, page('routes'), page('capacity')];
  const [app] = extendApplications([code], definitions, metadata(code));
  assert.equal(app.id, code.id);
  assert.equal(app.views, code.views);
  assert.equal(app.opens, code.opens);
  assert.deepEqual(app.home, { view: 'page', params: { app: 'build', kind: 'page', name: 'routes', application: 'build:z-routing' } });
  const sections = app.nav({});
  assert.deepEqual(sections.map(section => section.label), ['Operations', 'a-capacity', 'z-routing']);
  assert.deepEqual(sections[0].items[0], { label: 'Routings', route: app.home });
  assert.equal(sections[1].items[0].label, 'Planning');
  assert.deepEqual(sections[2].items.map(item => item.label), ['capacity']);
  assert.equal(applicationEntry(takeover, [code]), 'mes');
});

test('replacements resolve deep links and restored tabs without changing runtime application or record context', () => {
  const code = native();
  const application = extension('routing', { replacements: [{ view: 'craft', page: 'routes' }] });
  const definitions = [nativeDefinition(), application, page('routes')];
  const old = { view: 'craft', params: { id: 'R-7', instance: 'session-2' } };
  const resolved = resolveApplicationRoute(old, definitions, metadata(code), 'mes');
  assert.deepEqual(resolved, { view: 'page', params: { id: 'R-7', instance: 'session-2', app: 'build', kind: 'page', name: 'routes', application: 'build:routing' } });
  assert.equal(pageApplication(resolved, definitions, undefined, [code]), 'mes');
  assert.equal(resolveApplicationRoute(resolved, definitions, metadata(code)), resolved);
  const restored = { view: 'page', params: { app: 'build', kind: 'page', name: 'routes' } };
  assert.equal(resolveApplicationRoute(restored, definitions, metadata(code), 'mes').params.application, 'build:routing');
  assert.equal(pageApplication({ view: 'application-runs', params: { app: 'build', name: 'routing', application: 'build:routing' } }, definitions, undefined, [code]), 'mes');
});

test('missing publication, unreadable pages, a different original AssetRef and an unavailable entry never replace native routes', () => {
  const code = native();
  const original = { view: 'craft' };
  const application = extension('routing', { replacements: [{ view: 'craft', page: 'routes' }] });
  for (const definitions of [
    [nativeDefinition(), page('routes')],
    [nativeDefinition(), application],
    [{ ...nativeDefinition(), ref: { app: 'mes', kind: 'app', name: 'other' } }, application, page('routes')],
    [nativeDefinition(), { ...application, application: { ...application.application, entry: 'unavailable' } }, page('routes')],
  ]) assert.equal(resolveApplicationRoute(original, definitions, metadata(code)), original);
});

test('published replacements keep old URLs and menus working after the original renderer is removed', () => {
  const code = { ...native(), home: { view: 'queue' }, views: [], nav: () => [{ label: 'Production', items: [{ label: 'Shop orders', route: { view: 'orders' } }] }] };
  const application = extension('shop-orders', { pages: ['orders-page'], replacements: [{ view: 'orders', page: 'orders-page' }] });
  const definitions = [nativeDefinition(), application, page('orders-page')];
  const info = metadata(code);
  const expected = { view: 'page', params: { app: 'build', kind: 'page', name: 'orders-page', application: 'build:shop-orders' } };
  assert.deepEqual(resolveApplicationRoute({ view: 'orders' }, definitions, info), expected);
  const [app] = extendApplications([code], definitions, info);
  assert.deepEqual(app.nav({})[0].items[0].route, expected);
  assert.equal(app.nav({}).length, 1);
  assert.equal(pageApplication(expected, definitions, undefined, [code]), 'mes');
});

test('hidden registered views and another entry\'s views are not captured by an extension', () => {
  const original = { view: 'private-orders' };
  const code = { ...native(), views: [{ id: 'private-orders', title: () => 'Hidden orders', render: () => null }] };
  const application = extension('shop-orders', { replacements: [{ view: original.view, page: 'routes' }] });
  const definitions = [nativeDefinition(), application, page('routes')];
  assert.equal(resolveApplicationRoute(original, definitions, metadata(code)), original);
  const foreign = { ...native('crm'), views: [{ id: 'private-orders', title: () => 'CRM orders', render: () => null }] };
  const linked = { ...code, views: [], nav: () => [{ label: 'Related', items: [{ label: 'CRM orders', route: original }] }] };
  assert.equal(resolveApplicationRoute(original, definitions, metadata(linked, foreign)), original);
  assert.equal(extendApplications([linked, foreign], definitions, metadata(linked, foreign))[0].nav({})[0].items[0].route, original);
});

test('explicit existing entries disambiguate functional contributions without using owner prefixes as a new registry', () => {
  const application = extension('routing');
  application.application.extends = { app: 'platform', kind: 'app', name: 'platform' };
  const entries = [native('runs', ['platform', 'flow']), native('data-connection', ['platform'])];
  assert.equal(applicationEntry(application, entries), undefined);
  application.application.entry = 'runs';
  assert.equal(applicationEntry(application, entries), 'runs');
  application.application.extends = { app: 'flow', kind: 'app', name: 'flow' };
  delete application.application.entry;
  assert.equal(applicationEntry(application, entries), 'runs');
});

test('standalone applications keep explicit page membership when they reuse the same page', () => {
  const apps = ['first', 'second'].map(name => ({ ref: { app: 'build', kind: 'app', name }, source: 'tenant', application: { name, title: name, pages: ['routes'] } }));
  const route = { view: 'page', params: { app: 'build', kind: 'page', name: 'routes', application: 'build:second' } };
  assert.equal(pageApplication(route, apps, 'build:first'), 'build:second');
  const old = { view: 'page', params: { app: 'build', kind: 'page', name: 'routes' } };
  assert.equal(pageApplication(old, apps), undefined);
  assert.equal(pageApplication(old, apps, 'build:first'), 'build:first');
});
