"""Retain the platform telemetry recovery case without an industry app."""
from migration_rehearsal import require, wait


def output(builder, instance):
    rows = builder.call('records/flow.output?limit=100')['records']
    return next((row for row in rows if row['instance'] == instance and row['name'] == 'liveTelemetryStats'), {})


def prepare(builder, suffix, sql):
    typ = 'build.telemetry' + suffix
    flow = 'build.nativestream' + suffix
    table = 'native_stream_' + suffix
    sql('create table ' + table + '(seq bigint primary key,event_id text,plant text,device text,at timestamptz,reading double precision,target text); grant select on ' + table + ' to rehearsal_source')
    builder.decide('build.object.create', 'build.object', 'NATIVE-O', {'name': typ[6:], 'title': 'Accepted telemetry results', 'fields': [{'name': 'reading', 'title': 'Reading', 'type': 'decimal'}]})
    builder.decide('build.object.publish', 'build.object', 'NATIVE-O', {})
    for number in range(100):
        builder.decide(typ + '.create', typ, 'SENSOR-' + str(number), {'reading': 0})
    builder.decide('build.connection.create', 'build.connection', 'STREAM-DB', {'name': 'streamdb', 'title': 'Stream database', 'kind': 'postgres', 'address': 'postgres://rehearsal_source@postgres/platform', 'secret': 'SINK'})
    builder.decide('build.connection.check', 'build.connection', 'STREAM-DB', {})
    wait(lambda: builder.record('build.connection', 'STREAM-DB'), lambda row: row.get('record', {}).get('state') == 'ready', 'Controlled PostgreSQL source did not become ready')
    builder.decide('build.source.create', 'build.source', 'NATIVE-SOURCE', {'name': 'nativetelemetry', 'title': 'Seven-node telemetry', 'connection': 'STREAM-DB', 'profile': 'table', 'entity': table, 'since': 'seq', 'stream': True})
    builder.decide('build.source.publish', 'build.source', 'NATIVE-SOURCE', {})
    builder.decide('build.process.create', 'build.process', 'NATIVE-P', {
        'name': flow[6:], 'title': 'Seven-node retained telemetry',
        'continuous': {'Source': 'nativetelemetry', 'Batch': 512, 'State': 16777216, 'FrameBytes': 67108864, 'checkpointEvery': 5000, 'DeadLetter': True, 'deadLetterMaxRecords': 10000, 'deadLetterTtlMs': 86400000, 'errorPolicy': 'deadLetter', 'deadLetterRetry': True, 'concurrency': 8, 'entry': 'alertsloop',
                       'Intake': {'sourceRecord': 'NATIVE-SOURCE', 'key': 'event_id', 'partition': ['plant', 'device'], 'eventTime': 'at', 'value': 'reading', 'fields': ['plant', 'device', 'target']},
                       'Window': {'node': 'windowstate', 'windowMs': 30000, 'slideMs': 5000, 'watermarkMs': 2000, 'maxRecords': 10000, 'lateEvents': 'sideOutput'},
                       'aggregate': {'node': 'aggregate', 'kernel': 'wasm:f32-window-stats', 'signal': 'reading', 'group': ['plant', 'device'], 'measures': ['count', 'sum', 'mean', 'min', 'max', 'above'], 'above': 11.5},
                       'threshold': {'node': 'threshold', 'field': 'mean', 'high': 11.5, 'low': 9.5, 'debounceMs': 3000, 'severity': 'High'},
                       'output': {'node': 'publishstats', 'name': 'liveTelemetryStats', 'mode': 'replace'}},
        'steps': [{'name': 'alertsloop', 'kind': 'foreach', 'collection': {'source': 'input', 'path': ['triggered']}, 'body': 'emitalert', 'maxIterations': 10000, 'concurrency': 1, 'next': 'completed'},
                  {'name': 'emitalert', 'kind': 'action', 'act': typ + '.edit', 'target': {'source': 'item', 'path': ['target']}, 'inputs': {'reading': {'source': 'item', 'path': ['reading']}}, 'retryAttempts': 3, 'retryBackoffMs': 250},
                  {'name': 'completed', 'kind': 'end'}]})
    builder.decide('build.page.create', 'build.page', 'NATIVE-OUTPUT-PAGE', {
        'name': 'nativestats' + suffix, 'title': 'Retained telemetry output', 'object': 'flow.output',
        'sections': [{'widget': 'table', 'title': 'Original output assets', 'fields': ['name', 'app', 'instance', 'flow', 'version', 'batch']},
                     {'widget': 'detail', 'title': 'Accepted window statistics', 'fields': ['instance', 'version', 'batch', 'value']},
                     {'widget': 'table', 'title': 'Original Flow versions', 'object': 'flow.instance', 'fields': ['flow', 'version', 'release', 'state']}]})
    candidate = builder.candidate([{'kind': 'flow', 'id': 'NATIVE-P'}, {'kind': 'page', 'id': 'NATIVE-OUTPUT-PAGE'}])
    builder.activate(candidate)
    sql("insert into " + table + " select n,'native-'||n,'P','D'||(n-1),now()-interval '3 seconds',12.5000001,'SENSOR-'||(n-1) from generate_series(1,100) n")
    instance = flow + ':continuous-v1'
    wait(lambda: builder.call('records/' + typ + '?limit=200'), lambda rows: len(rows.get('records', [])) == 100 and all(row['reading'] == 12.5 for row in rows['records']), 'The original f32 telemetry did not apply all 100 business actions', timeout=90)
    result = wait(lambda: output(builder, instance), lambda row: bool(row.get('id')), 'The retained output asset was not created')
    history = builder.record('flow.output', result['id'])
    values = [history['record'].get('value')] + [value for change in history['history'] for field in change.get('fields', []) if field['field'] == 'value' for value in [field.get('before'), field.get('after')]]
    require(any(isinstance(value, list) and len(value) == 100 and all(row['mean'] == 12.5 and row['count'] == 1 for row in value) for value in values), 'The accepted output history lost its 100 original kernel results')
    builder.decide('build.source.pause', 'build.source', 'NATIVE-SOURCE', {})
    builder.decide('flow.instance.cancel', 'flow.instance', instance, {})
    stopped = builder.record('flow.instance', instance)['record']
    require(stopped['state'] == 'canceled' and stopped['resumable'], 'The telemetry version cannot resume')
    return {'type': typ, 'instance': instance, 'output': result['id'], 'table': table}


def resume(builder, saved, sql):
    instance = saved['instance']
    before = builder.record('flow.instance', instance)['record']
    require(before['batch']['source']['cursor'] == '100', 'Recovered telemetry lost its original source position')
    builder.decide('flow.instance.resume', 'flow.instance', instance, {})
    builder.decide('build.source.publish', 'build.source', 'NATIVE-SOURCE', {})
    sql("insert into " + saved['table'] + " values (101,'native-recovery','P','D0',now()-interval '3 seconds',13,'SENSOR-0')")
    wait(lambda: builder.record('flow.instance', instance), lambda row: row.get('record', {}).get('batch', {}).get('source', {}).get('cursor') == '101', 'The retained telemetry did not continue at the next source row', timeout=60)
    wait(lambda: output(builder, instance), lambda row: any(value['mean'] == 13 for value in row.get('value', [])), 'The recovered window did not publish its accepted output', timeout=60)
    current = builder.record('flow.instance', instance)['record']
    require(current['version'] == before['version'] and current['release'] == before['release'] and current['batch']['consumed'] == 101, 'Recovery changed the original telemetry version or duplicated its input')
    builder.decide('build.source.pause', 'build.source', 'NATIVE-SOURCE', {})
    builder.decide('flow.instance.cancel', 'flow.instance', instance, {})
