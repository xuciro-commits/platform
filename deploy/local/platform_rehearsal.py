#!/usr/bin/env python3
"""Exercise platform recovery mechanisms independently of industry applications."""
import json
import os
from pathlib import Path
import subprocess
import sys

from migration_rehearsal import Member, oidc_token, prepare, request, require, resume, retry, state, wait
import telemetry_rehearsal as telemetry

ROOT = Path(__file__).resolve().parents[2]
COMPOSE = ['docker', 'compose', '-f', str(ROOT / 'deploy/local/compose.yaml'), '-f', os.environ['PLATFORM_REHEARSAL_LIMITS_FILE']]


def compose(*args, **kwargs):
    return subprocess.run(COMPOSE + list(args), cwd=ROOT, check=True, **kwargs)


def sql(statement):
    return subprocess.check_output(COMPOSE + ['exec', '-T', 'postgres', 'psql', '-v', 'ON_ERROR_STOP=1', '-U', 'platform', '-d', 'platform', '-Atc', statement], cwd=ROOT, text=True).strip()


def ready(members):
    for member in members:
        wait(lambda: member.call('me'), lambda value: value.get('tenantId') == member.tenant, 'The configured identity did not recover', timeout=120)
        health = member.call('health')
        require(health.get('status') in ['ok', 'degraded'], 'Tenant recovery: ' + str(health))


def snapshot(builder, migration, records):
    result = {'migration': state(builder, migration), 'records': []}
    for typ, identity in records:
        value = builder.record(typ, identity)
        require('record' in value, 'The retained record is unavailable: ' + typ + '/' + identity)
        result['records'].append(value)
    return result


def main():
    directory = Path(sys.argv[1]).resolve()
    require(os.environ.get('PLATFORM_DATA_NAMESPACE', '').startswith('platform-rehearsal-'), 'Recovery drill needs its own data namespace')
    issuer = 'http://localhost:' + os.environ['IDP_PORT'] + '/auth/v1/'
    fixtures = []
    wait(lambda: request(issuer + '.well-known/openid-configuration'), lambda value: value.get('issuer'), 'The identity provider did not become ready', timeout=120)
    sql("create role rehearsal_source login password 'sinkLocalOnly0000000000000000000000000000000000000000000000000000'; grant connect on database platform to rehearsal_source; grant usage on schema public to rehearsal_source")
    for port_env in ['SINK_PORT', 'SINK_PORT_HOSPITALITY']:
        sink_port = os.environ[port_env]
        wait(lambda: request('http://127.0.0.1:' + sink_port + '/received'), lambda value: isinstance(value, dict) and 'calls' in value, 'Embedded sink on ' + port_env + ' did not become ready', timeout=30)
        models = request('http://127.0.0.1:' + sink_port + '/v1/models')
        require(any(m.get('id') == 'echo' for m in models.get('data', [])), 'Embedded sink on ' + port_env + ' missing models fixture')
    environment = json.loads((ROOT / 'deploy/local/rehearsal-fixtures.json').read_text())
    for configuration in environment:
        suffix, tenant = configuration['suffix'], configuration['tenant']
        port = os.environ[configuration['portEnv']]
        admin, user, member_id = configuration['builder'], configuration['operator'], configuration['member']
        builder = Member('http://127.0.0.1:' + port, tenant, oidc_token(admin, issuer))
        operator = Member(builder.server, tenant, oidc_token(user, issuer))
        ready([builder])
        wait(lambda: operator.call('me'), lambda value: value.get('tenantId') == tenant, 'The configured operator could not enter its tenant', timeout=120)
        require(builder.call('me')['principalId'] != operator.call('me')['principalId'], 'The operator must be a distinct identity')
        builder.decide('platform.member.grant', 'platform.member', member_id, {'app': 'build', 'role': 'user'})
        retained_telemetry = telemetry.prepare(builder, suffix, sql)
        migration = prepare(builder, operator, suffix)
        records = [('build.source', 'NATIVE-SOURCE'), ('build.process', 'NATIVE-P'), ('flow.instance', retained_telemetry['instance']), ('flow.output', retained_telemetry['output'])]
        builder.decide('build.object.create', 'build.object', 'RECOVERY-O', {'name': 'recoverycase', 'title': 'Recovery case', 'fields': [{'name': 'note', 'title': 'Note', 'type': 'text'}]})
        builder.decide('build.object.publish', 'build.object', 'RECOVERY-O', {})
        operator.decide('build.recoverycase.create', 'build.recoverycase', 'edge', {'note': 'Before backup'})
        records.append(('build.recoverycase', 'edge'))
        offline = operator.draft('build.recoverycase.edit', 'build.recoverycase', 'edge', {'note': 'After backup'})
        fixtures.append({'builder': builder, 'operator': operator, 'migration': migration, 'records': records, 'offline': offline, 'suffix': suffix, 'telemetry': retained_telemetry})
    members = [f['builder'] for f in fixtures]
    before = [snapshot(f['builder'], f['migration'], f['records']) for f in fixtures]
    compose('restart', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
    ready(members)
    require([snapshot(f['builder'], f['migration'], f['records']) for f in fixtures] == before, 'Restart changed business records, histories or the pending migration')
    dump = directory / 'platform.dump'
    with dump.open('wb') as stream:
        compose('exec', '-T', 'postgres', 'pg_dump', '-U', 'platform', '-d', 'platform', '-Fc', '--exclude-schema=tenant_*', stdout=stream)
    for f in fixtures:
        require('record' in f['operator'].send(f['offline']), 'The exact edge write was not accepted')
    compose('stop', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
    compose('exec', '-T', 'postgres', 'dropdb', '-U', 'platform', 'platform')
    compose('exec', '-T', 'postgres', 'createdb', '-U', 'platform', 'platform')
    with dump.open('rb') as stream:
        compose('exec', '-T', 'postgres', 'pg_restore', '-U', 'platform', '-d', 'platform', '--no-owner', stdin=stream)
    compose('start', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
    ready(members)
    require([snapshot(f['builder'], f['migration'], f['records']) for f in fixtures] == before, 'Restored database differs from its backup')
    for f in fixtures:
        accepted = f['operator'].send(f['offline'])
        require('record' in accepted and f['operator'].send(f['offline'])['record'] == accepted['record'], 'The retained edge key did not return the original receipt')
        row = f['builder'].record('build.recoverycase', 'edge')
        require(row['record']['note'] == 'After backup' and row['record']['revision'] == 2, 'The edge write was lost or duplicated')
    after = [snapshot(f['builder'], f['migration'], f['records']) for f in fixtures]
    compose('stop', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
    sql("update snapshots set state=decode('00','hex') where tenant='" + fixtures[0]['builder'].tenant.replace("'", "''") + "'")
    sql("delete from snapshots where tenant='" + fixtures[1]['builder'].tenant.replace("'", "''") + "'")
    compose('start', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
    isolated = fixtures[0]['builder']
    wait(lambda: isolated.call('health'), lambda health: health.get('status') == 'quarantined', 'Corrupt checkpoint did not isolate its tenant', timeout=120)
    neighbor = Member(isolated.server, environment[0]['neighbor'], isolated.token)
    ready([neighbor])
    require(isolated.call('health').get('status') == 'quarantined', 'The healthy neighbor must not hide the isolated tenant')
    recovered = isolated.call('recovery/retry', {}, timeout=60)
    require(recovered.get('status') == 'ok', 'The original operator recovery did not rebuild its tenant: ' + str(recovered))
    ready(members)
    require([snapshot(f['builder'], f['migration'], f['records']) for f in fixtures] == after, 'Full recovery changed durable application state')
    for f in fixtures:
        retry(f['builder'], f['migration'])
        resume(f['builder'], f['operator'], f['migration'], f['suffix'])
    compose('restart', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
    ready(members)
    for f in fixtures:
        require(f['builder'].record('build.migration', 'MIGRATION-RUN')['record']['state'] == 'complete', 'Completed migration did not recover')
        telemetry.resume(f['builder'], f['telemetry'], sql)
    subprocess.run(['pnpm', '--filter', '@platform/e2e', 'exec', 'playwright', 'test', '--config', 'playwright.deploy.config.ts'], cwd=ROOT / 'web', env={**os.environ, 'PLATFORM_DEPLOY_PASSWORD': 'Plant-Local-1'}, check=True)
    print('ok   common host: actual OIDC, embedded sink stand-in, generic declarations, restart, PostgreSQL backup, original edge keys, retained telemetry outputs, isolated recovery and old/new flows', flush=True)


if __name__ == '__main__':
    main()
