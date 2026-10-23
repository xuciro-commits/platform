#!/usr/bin/env python3
"""Exercise declared applications through their ordinary owners on disposable PostgreSQL."""
import datetime
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from migration_rehearsal import Member, oidc_token, prepare, request, require, resume, retry, state, wait
import telemetry_rehearsal as telemetry

ROOT = Path(__file__).resolve().parents[2]
COMPOSE = ['docker', 'compose', '-f', str(ROOT / 'deploy/local/compose.yaml'), '-f', os.environ['PLATFORM_REHEARSAL_LIMITS_FILE']]
COUNTS = {'models': 0, 'observations': 0}


class Samples(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def answer(self, value):
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps(value).encode())

    def do_GET(self):
        self.answer({'data': [{'id': 'probe'}]} if self.path.endswith('/models') else {'ok': True, 'messages': [], 'records': []})

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))) or b'{}')
        if self.path.endswith('/chat/completions'):
            COUNTS['models'] += 1
            if body.get('tools'):
                message = {'tool_calls': [{'id': 'ask-target', 'type': 'function', 'function': {'name': 'ask', 'arguments': json.dumps({'question': 'Which confirmed record and facts should I review?', 'answers': ['Provide the target'], 'rationale': 'A confirmed target is required before a business change.'})}}]}
            else:
                message = {'content': json.dumps({'summary': 'Review the actual record and confirm missing facts with the responsible person.', 'category': 'review', 'review': True, 'confidence': 0.9})}
            self.answer({'choices': [{'message': message}], 'usage': {'prompt_tokens': 10, 'completion_tokens': 10, 'cost': 0.001}})
        elif body.get('mode') == 'intervals':
            COUNTS['observations'] += 1
            since = datetime.datetime.fromisoformat(body['since'].replace('Z', '+00:00'))
            points = []
            for index in range(30):
                stamp = (since + datetime.timedelta(seconds=index * 10)).isoformat().replace('+00:00', 'Z')
                points.append({'at': stamp, 'receivedAt': stamp, 'quality': 'good', 'value': 'down' if 2 <= index < 10 else 'run'})
            self.answer({'intervals': {'since': body['since'], 'until': body['until'], 'revision': 'rehearsal-1', 'coverage': {'availableSince': body['since'], 'completeUntil': body['until'], 'good': 30}, 'points': points}})
        else:
            self.answer({'messages': [], 'records': []})


def compose(*args, **kwargs):
    return subprocess.run(COMPOSE + list(args), cwd=ROOT, check=True, **kwargs)


def sql(statement):
    return subprocess.check_output(COMPOSE + ['exec', '-T', 'postgres', 'psql', '-v', 'ON_ERROR_STOP=1', '-U', 'platform', '-d', 'platform', '-Atc', statement], cwd=ROOT, text=True).strip()


def ready(members):
    for member in members:
        wait(lambda: member.call('me'), lambda value: value.get('tenantId') == member.tenant, 'The configured identity did not recover', timeout=120)
        health = member.call('health')
        require(health.get('status') in ['ok', 'degraded'], 'Tenant recovery: ' + str(health))


def install(builder, industry, fixture):
    descriptor_path = ROOT / 'deploy/packages' / (industry + '.json')
    descriptor = json.loads(descriptor_path.read_text())
    builder.decide('platform.package.install', 'platform.package', descriptor['id'], {'id': descriptor['id'], 'version': descriptor['version']})
    drafts = builder.call('packages/' + descriptor['id'] + '/drafts', {'version': descriptor['version'], 'key': 'rehearsal-drafts'}, timeout=60)['drafts']
    builder.decide('ai.provider.add', 'ai.provider', 'fixture', {'kind': 'local', 'baseUrl': fixture, 'wire': 'openai'})
    builder.decide('ai.model.enable', 'ai.model', 'fixture/probe', {'access': 'users'})
    for key in ['ai/app-model', 'agent/model']:
        builder.decide('platform.setting.set', 'platform.setting', key, {'value': 'fixture/probe'})
    for draft in drafts:
        if draft['ref']['kind'] == 'evaluation-set':
            builder.decide('build.testplan.edit', 'build.testplan', draft['id'], {'model': 'fixture/probe'})
        elif draft['ref']['kind'] == 'connection':
            source = next(asset['body'] for asset in descriptor['blueprint']['assets'] if asset['ref'] == draft['ref'])
            binding = {'address': fixture, 'allowPrivate': True}
            if source.get('credential'):
                binding['secret'] = 'SINK'
            builder.decide('build.connection.edit', 'build.connection', draft['id'], binding)
            builder.decide('build.connection.check', 'build.connection', draft['id'], {})
    for draft in drafts:
        if draft['ref']['kind'] == 'connection':
            wait(lambda: builder.record('build.connection', draft['id']), lambda row: row.get('record', {}).get('state') == 'ready', 'Fixture connection was not checked')
    identity = builder.candidate([{'kind': d['ref']['kind'], 'id': d['id']} for d in drafts])
    reports = []
    for draft in drafts:
        ref = draft['ref']
        kind = ref['kind']
        if kind not in ['evaluation-set', 'agent']:
            continue
        request = {'candidateId': identity, 'key': str(uuid.uuid4())}
        request.update({'planId': ref['name']} if kind == 'evaluation-set' else {'agent': ref['app'] + '.' + ref['name']})
        report = builder.call('releases/evaluations', request, timeout=60)
        require(report.get('id'), 'Original evaluator did not start: ' + str(report))
        reports.append(('build.evaluation' if kind == 'evaluation-set' else 'agent.evaluation', report['id']))
    for typ, report in reports:
        def measured(row):
            value = row.get('record', {})
            return value.get('candidate') == identity and (value.get('state') == 'passed' if typ == 'build.evaluation' else value.get('state') == 'done' and value.get('score') == 1 and value.get('fails') == 0 and value.get('differs') == 0)
        wait(lambda: builder.record(typ, report), measured, 'The original evaluator did not pass', timeout=180)
    builder.activate(identity)
    subprocess.run([sys.executable, str(ROOT / 'deploy/local/initialize.py'), '--package', str(descriptor_path), '--host', builder.server, '--tenant', builder.tenant, '--run-key', 'rehearsal-samples'], env={**os.environ, 'PLATFORM_INITIALIZE_TOKEN': builder.token}, check=True)
    return identity


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
    server = ThreadingHTTPServer(('0.0.0.0', 0), Samples)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    fixture = 'http://host.docker.internal:' + str(server.server_port)
    issuer = 'http://localhost:' + os.environ['IDP_PORT'] + '/auth/v1/'
    fixtures = []
    try:
        wait(lambda: request(issuer + '.well-known/openid-configuration'), lambda value: value.get('issuer'), 'The identity provider did not become ready', timeout=120)
        sql("create role rehearsal_source login password 'sinkLocalOnly0000000000000000000000000000000000000000000000000000'; grant connect on database platform to rehearsal_source; grant usage on schema public to rehearsal_source")
        for industry, port, tenant, admin, user, member_id in [
            ('hospitality', os.environ['HOSPITALITY_PORT'], 'hotel-a-build', 'manager@hotel.test', 'desk@hotel.test', 'desk-1'),
            ('manufacturing', os.environ['MANUFACTURING_PORT'], 'plant-sz-build', 'sup@plant.test', 'op1@plant.test', 'op-l1'),
        ]:
            builder = Member('http://127.0.0.1:' + port, tenant, oidc_token(admin, issuer))
            operator = Member(builder.server, tenant, oidc_token(user, issuer))
            ready([builder])
            wait(lambda: operator.call('me'), lambda value: value.get('tenantId') == tenant, 'The configured operator could not enter its tenant', timeout=120)
            require(builder.call('me')['principalId'] != operator.call('me')['principalId'], 'The operator must be a distinct identity')
            builder.decide('platform.member.grant', 'platform.member', member_id, {'app': 'build', 'role': 'user'})
            # Prepare the platform's retained stream before the industry
            # package. Recovery still restores both in this same tenant.
            retained_telemetry = telemetry.prepare(builder, industry, sql)
            release = install(builder, industry, fixture)
            migration = prepare(builder, operator, industry)
            records = [('build.source', 'NATIVE-SOURCE'), ('build.process', 'NATIVE-P'), ('flow.instance', retained_telemetry['instance']), ('flow.output', retained_telemetry['output'])]
            if industry == 'hospitality':
                builder.decide('build.crmcustomer.create', 'build.crmcustomer', 'rehearsal-customer', {'name': 'Recovery customer', 'kind': 'company'})
                builder.decide('build.crmopportunity.create', 'build.crmopportunity', 'rehearsal-opportunity', {'customer': 'rehearsal-customer', 'title': 'Retained opportunity'})
                tomorrow = datetime.date.today() + datetime.timedelta(days=1)
                operator.decide('build.pmsreservation.create', 'build.pmsreservation', 'rehearsal-reservation', {'guest': 'Recovery guest', 'roomtype': 'sampledaily', 'unit': 'day', 'mode': 'booked', 'dayfrom': tomorrow.isoformat(), 'dayuntil': (tomorrow + datetime.timedelta(days=1)).isoformat()})
                records += [('build.crmopportunity', 'rehearsal-opportunity'), ('build.pmsreservation', 'rehearsal-reservation')]
            else:
                resources = builder.call('records/build.mesresource?limit=100')['records']
                require(resources, 'The declared MES sample did not initialize')
                builder.decide('build.mesdevice.create', 'build.mesdevice', 'rehearsal-device', {'name': 'Recovery controller', 'resource': resources[0]['id'], 'device': 'rehearsal-plc', 'unit': 'L1', 'notify': True, 'remindhours': 1})
                until = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace('+00:00', 'Z')
                observed = builder.call('observations/build/messtates/evaluate', {'record': 'rehearsal-device', 'until': until, 'key': 'rehearsal-observation'})
                require(observed.get('receipt'), 'Observation was not accepted: ' + str(observed))
                downtime = wait(lambda: builder.call('records/build.mesdowntime?limit=100'), lambda rows: bool(rows.get('records')), 'Accepted observation did not produce downtime')
                records += [('build.mesdevice', 'rehearsal-device')] + [('build.mesdowntime', row['id']) for row in downtime['records']]
                records += [('build.erpitem', 'samplesteel'), ('build.erpitem', 'samplepump')]
            builder.decide('build.object.create', 'build.object', 'RECOVERY-O', {'name': 'recoverycase', 'title': 'Recovery case', 'fields': [{'name': 'note', 'title': 'Note', 'type': 'text'}]})
            builder.decide('build.object.publish', 'build.object', 'RECOVERY-O', {})
            operator.decide('build.recoverycase.create', 'build.recoverycase', 'edge', {'note': 'Before backup'})
            records.append(('build.recoverycase', 'edge'))
            offline = operator.draft('build.recoverycase.edit', 'build.recoverycase', 'edge', {'note': 'After backup'})
            fixtures.append({'builder': builder, 'operator': operator, 'migration': migration, 'records': records, 'release': release, 'offline': offline, 'industry': industry, 'telemetry': retained_telemetry})
        members = [f['builder'] for f in fixtures]
        before = [snapshot(f['builder'], f['migration'], f['records']) for f in fixtures]
        calls = dict(COUNTS)
        compose('restart', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
        ready(members)
        require([snapshot(f['builder'], f['migration'], f['records']) for f in fixtures] == before, 'Restart changed business records, histories or the pending migration')
        require(COUNTS == calls, 'Restart repeated a saved model or observation result')
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
        sql("update snapshots set state=decode('00','hex') where tenant='hotel-a-build'")
        sql("delete from snapshots where tenant='plant-sz-build'")
        compose('start', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
        isolated = fixtures[0]['builder']
        wait(lambda: isolated.call('health'), lambda health: health.get('status') == 'quarantined', 'Corrupt checkpoint did not isolate its tenant', timeout=120)
        neighbor = Member(isolated.server, 'hotel-test-build', isolated.token)
        ready([neighbor])
        require(isolated.call('health').get('status') == 'quarantined', 'The healthy neighbor must not hide the isolated tenant')
        recovered = isolated.call('recovery/retry', {}, timeout=60)
        require(recovered.get('status') == 'ok', 'The original operator recovery did not rebuild its tenant: ' + str(recovered))
        ready(members)
        require([snapshot(f['builder'], f['migration'], f['records']) for f in fixtures] == after, 'Full recovery changed durable application state')
        require(COUNTS == calls, 'Recovery reran a saved model or observation result')
        for f in fixtures:
            retry(f['builder'], f['migration'])
            resume(f['builder'], f['operator'], f['migration'], f['industry'])
        compose('restart', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
        ready(members)
        for f in fixtures:
            require(f['builder'].record('build.migration', 'MIGRATION-RUN')['record']['state'] == 'complete', 'Completed migration did not recover')
            telemetry.resume(f['builder'], f['telemetry'], sql)
        subprocess.run(['pnpm', '--filter', '@platform/e2e', 'exec', 'playwright', 'test', '--config', 'playwright.deploy.config.ts'], cwd=ROOT / 'web', env={**os.environ, 'PLATFORM_DEPLOY_PASSWORD': 'Plant-Local-1'}, check=True)
        print('ok   common host: actual OIDC, declared packages, restart, PostgreSQL backup, original edge keys, retained observations, isolated recovery and old/new flows; fixed providers verify mechanisms only', flush=True)
    finally:
        server.shutdown()
        server.server_close()


if __name__ == '__main__':
    main()
