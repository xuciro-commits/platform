#!/usr/bin/env python3
"""Offline PostgreSQL handover. Hosts must be stopped; source remains fenced.

Connection URLs come from named environment variables, never command arguments.
PostgreSQL tools run inside an existing trusted container. No daily environment
is selected implicitly and no source database or bytes are deleted.
"""
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import tempfile
from urllib.parse import unquote, urlsplit

FENCE = 'public.platform_storage_fence'


def stream_digest(stream):
    digest = hashlib.sha256()
    while chunk := stream.read(1 << 20):
        digest.update(chunk)
    return digest.hexdigest()


def quote(value):
    return '"' + value.replace('"', '""') + '"'


class Database:
    def __init__(self, variable, container):
        url = urlsplit(os.environ[variable])
        if url.scheme not in ('postgres', 'postgresql') or not url.hostname or not url.path.strip('/') or url.query:
            raise ValueError('Use a PostgreSQL URL with database, without query options')
        values = [url.hostname, str(url.port or 5432), unquote(url.username or ''), unquote(url.password or ''), unquote(url.path[1:])]
        if any('\n' in x or '\r' in x for x in values):
            raise ValueError('Connection values cannot contain line breaks')
        self.header = ('\n'.join(values) + '\n').encode()
        self.container = container
        self.identity = (url.hostname, url.port or 5432, values[4])

    def run(self, program, args=(), data=b'', stdout=subprocess.PIPE):
        # read -r treats credentials as data; shell source contains no input values.
        script = 'read -r PGHOST; read -r PGPORT; read -r PGUSER; read -r PGPASSWORD; read -r PGDATABASE; export PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE; exec "$@"'
        return subprocess.run(['docker', 'exec', '-i', self.container, 'sh', '-c', script, 'storage-tools', program, *args], input=self.header + data, stdout=stdout, stderr=subprocess.PIPE, check=True)

    def sql(self, statement):
        return self.run('psql', ['-X', '-qAt', '-v', 'ON_ERROR_STOP=1'], statement.encode()).stdout.decode().strip()

    def tables(self):
        raw = self.sql("select coalesce(json_agg(array[n.nspname,c.relname] order by n.nspname,c.relname),'[]') from pg_class c join pg_namespace n on n.oid=c.relnamespace where c.relkind in ('r','p') and n.nspname not in ('pg_catalog','information_schema') and n.nspname not like 'pg_toast%' and c.oid<>coalesce(to_regclass('public.platform_storage_fence'),0)")
        return json.loads(raw)

    def fence(self, operation, empty=False):
        # Same database lock used by journal initialization. An earlier host is
        # still visible as a client backend; a later host sees the fence.
        condition = " or exists(select from pg_class c join pg_namespace n on n.oid=c.relnamespace where c.relkind in ('r','p') and n.nspname not in ('pg_catalog','information_schema') and n.nspname not like 'pg_toast%' and c.oid<>coalesce(to_regclass('public.platform_storage_fence'),0))" if empty else ''
        op = operation.replace("'", "''")
        self.sql("begin; select pg_advisory_xact_lock(1347174740,1); do $$ begin if exists(select from pg_stat_activity where datname=current_database() and pid<>pg_backend_pid() and backend_type='client backend')" + condition + " then raise exception 'Stop all writers; target must be empty'; end if; end $$; create table if not exists public.platform_storage_fence(operation text not null); insert into public.platform_storage_fence select '" + op + "' where not exists(select from public.platform_storage_fence); do $$ begin if exists(select from public.platform_storage_fence where operation<>'" + op + "') then raise exception 'Another handover owns this fence'; end if; end $$; commit;")

    def manifest(self):
        result = {}
        schema_dump = self.run('pg_dump', ['--schema-only', '--no-owner', '--no-privileges', '--exclude-table=' + FENCE]).stdout.decode()
        schema_lines = [line for line in schema_dump.splitlines() if line and not line.startswith(('--', '\\restrict ', '\\unrestrict '))]
        result['schema'] = hashlib.sha256('\n'.join(schema_lines).encode()).hexdigest()
        for schema, table in self.tables():
            name = quote(schema) + '.' + quote(table)
            with tempfile.TemporaryFile() as stream:
                # Include exact JSONB values, bytea, histories, keys and receipts,
                # independent of physical row order and database collation.
                self.run('psql', ['-X', '-q', '-v', 'ON_ERROR_STOP=1'], ("copy (select row_to_json(t)::text as row from " + name + " t order by row_to_json(t)::text collate \"C\") to stdout;").encode(), stdout=stream)
                stream.seek(0)
                result[name] = stream_digest(stream)
        sequences = json.loads(self.sql("select coalesce(json_agg(array[schemaname,sequencename] order by schemaname,sequencename),'[]') from pg_sequences where schemaname not in ('pg_catalog','information_schema')"))
        for schema, sequence in sequences:
            name = quote(schema) + '.' + quote(sequence)
            result['sequence:' + name] = self.sql('select last_value,is_called from ' + name)
        return result


def files_manifest(directory):
    if directory.startswith(('http://', 'https://')):
        tool = os.environ.get('PLATFORM_STORAGE_FILES_TOOL')
        if not tool:
            raise ValueError('S3 verification requires PLATFORM_STORAGE_FILES_TOOL (cmd/storage-files)')
        result = subprocess.check_output([tool, '-endpoint-env=PLATFORM_HANDOVER_FILES_ENDPOINT'], env={**os.environ, 'PLATFORM_HANDOVER_FILES_ENDPOINT': directory})
        return json.loads(result)
    root = Path(directory).resolve(strict=True)
    result = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise ValueError('File store contains a link or non-file object')
        if path.is_file():
            with path.open('rb') as stream:
                result[path.relative_to(root).as_posix()] = stream_digest(stream)
    return result


def handover(source, target, directory, operation, source_files, target_files):
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._:-]{0,127}', operation):
        raise ValueError('Use a non-empty handover identity of at most 128 safe characters')
    if source.identity == target.identity:
        raise ValueError('Source and target must be distinct databases')
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Files are already copied by the deployment owner, or remain the same shared
    # store. Compare every byte before and after SQL copy; never guess references.
    files = files_manifest(source_files)
    if files != files_manifest(target_files):
        raise ValueError('File stores differ; copy immutable bytes before handover')
    source.fence(operation)
    target.fence(operation, empty=True)
    before = source.manifest()
    dump = directory / 'platform.dump'
    with dump.open('wb') as stream:
        os.chmod(dump, 0o600)
        source.run('pg_dump', ['-Fc', '--no-owner', '--no-privileges', '--exclude-table=' + FENCE], stdout=stream)
    # Stream the dump after the connection prelude, without loading it in RAM.
    script = 'read -r PGHOST; read -r PGPORT; read -r PGUSER; read -r PGPASSWORD; read -r PGDATABASE; export PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE; exec pg_restore --exit-on-error --no-owner --no-privileges -d "$PGDATABASE"'
    with dump.open('rb') as stream, tempfile.TemporaryFile() as errors:
        process = subprocess.Popen(['docker', 'exec', '-i', target.container, 'sh', '-c', script], stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=errors)
        try:
            process.stdin.write(target.header)
            while chunk := stream.read(1 << 20):
                process.stdin.write(chunk)
            process.stdin.close()
            if process.wait() != 0:
                raise RuntimeError('PostgreSQL restore failed; both stores remain fenced')
        finally:
            if process.poll() is None:
                process.kill()
                process.wait()
    if before != target.manifest() or before != source.manifest() or files != files_manifest(source_files) or files != files_manifest(target_files):
        raise ValueError('Handover verification differs; both stores remain fenced')
    receipt = {'format': 'platform.storage-handover.v1', 'operation': operation, 'tables': before, 'files': files, 'sourceFenced': True}
    path = directory / 'handover.json'
    path.write_text(json.dumps(receipt, sort_keys=True, indent=2) + '\n')
    path.chmod(0o600)
    # Only the fully verified target may be opened. Source deliberately keeps
    # its fence even if the target later accepts new results or fails to start.
    target.sql('drop table ' + FENCE)
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-env', required=True)
    parser.add_argument('--target-env', required=True)
    parser.add_argument('--tool-container', required=True)
    parser.add_argument('--directory', required=True)
    parser.add_argument('--operation', required=True)
    parser.add_argument('--source-files', required=True)
    parser.add_argument('--target-files', required=True)
    args = parser.parse_args()
    handover(Database(args.source_env, args.tool_container), Database(args.target_env, args.tool_container), args.directory, args.operation, args.source_files, args.target_files)
    print('Verified target ready. Source remains fenced. Preserve deployment keys and configuration; start only the target.')


if __name__ == '__main__':
    main()
