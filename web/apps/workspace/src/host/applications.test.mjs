import test from 'node:test';
import assert from 'node:assert/strict';
import { composeApplications } from './applications.ts';

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
