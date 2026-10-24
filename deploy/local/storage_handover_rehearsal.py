#!/usr/bin/env python3
"""Use the rehearsal's stopped hosts and a second real PostgreSQL instance."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time

from storage_handover import Database, handover

ROOT = Path(__file__).resolve().parents[2]


def run(*args, **kwargs):
    return subprocess.run(list(args), check=True, **kwargs)


def main():
    project = os.environ['COMPOSE_PROJECT_NAME']
    if not project.startswith('platform-rehearsal-'):
        raise ValueError('Storage drill requires the owned rehearsal project')
    compose = ['docker', 'compose', '-f', str(ROOT / 'deploy/local/compose.yaml'), '-f', os.environ['PLATFORM_REHEARSAL_LIMITS_FILE']]
    run(*compose, 'stop', 'hospitality-server', 'manufacturing-server', stdout=subprocess.DEVNULL)
    source_container = subprocess.check_output([*compose, 'ps', '-q', 'postgres'], text=True).strip()
    configuration = json.loads(subprocess.check_output(['docker', 'inspect', source_container]))[0]
    network = next(iter(configuration['NetworkSettings']['Networks']))
    target_name = project + '-storage-target'
    directory = Path(sys.argv[1]) / 'storage-handover'
    run('docker', 'run', '-d', '--rm', '--name', target_name, '--network', network, '-p', '127.0.0.1::5432', '-e', 'POSTGRES_USER=platform', '-e', 'POSTGRES_PASSWORD=handover-local', '-e', 'POSTGRES_DB=handover', configuration['Image'], stdout=subprocess.DEVNULL)
    try:
        for _ in range(60):
            if subprocess.run(['docker', 'exec', target_name, 'pg_isready', '-U', 'platform'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(1)
        else:
            raise RuntimeError('The independent PostgreSQL target did not start')
        run('docker', 'exec', source_container, 'createdb', '-U', 'platform', 'storage_handover')
        source_external = 'postgres://platform:platform-local-only@127.0.0.1:' + os.environ['PG_PORT'] + '/storage_handover'
        port = subprocess.check_output(['docker', 'port', target_name, '5432/tcp'], text=True).strip().rsplit(':', 1)[1]
        target_external = 'postgres://platform:handover-local@127.0.0.1:' + port + '/handover'
        run('make', 'test-go', 'PKG=.', 'RUN=TestPostgresStorageHandoverPrepare', cwd=ROOT, env={**os.environ, 'PLATFORM_HANDOVER_PREPARE_DATABASE': source_external})
        os.environ['STORAGE_DRILL_SOURCE'] = 'postgres://platform:platform-local-only@postgres:5432/storage_handover'
        os.environ['STORAGE_DRILL_TARGET'] = 'postgres://platform:handover-local@' + target_name + ':5432/handover'
        files = 'http://127.0.0.1:' + os.environ['FILES_PORT'] + '/platform'
        os.environ['PLATFORM_S3_ACCESS_KEY'] = 'platform'
        os.environ['PLATFORM_S3_SECRET_KEY'] = 'platform-files-local-only'
        os.environ['PLATFORM_STORAGE_FILES_TOOL'] = str(ROOT / '.build/storage-files')
        source = Database('STORAGE_DRILL_SOURCE', source_container)
        target = Database('STORAGE_DRILL_TARGET', source_container)
        handover(source, target, directory, project, files, files)
        run('make', 'test-go', 'PKG=.', 'RUN=TestPostgresStorageHandoverVerify', cwd=ROOT, env={**os.environ, 'PLATFORM_HANDOVER_SOURCE_DATABASE': source_external, 'PLATFORM_HANDOVER_TARGET_DATABASE': target_external})
        # Real target corruption must keep both sides fenced, not just produce
        # a different status label. Use fresh databases for this negative case.
        run('docker', 'exec', source_container, 'createdb', '-U', 'platform', 'storage_mismatch')
        run('docker', 'exec', target_name, 'createdb', '-U', 'platform', 'storage_mismatch')
        os.environ['STORAGE_DRILL_SOURCE'] = 'postgres://platform:platform-local-only@postgres:5432/storage_mismatch'
        os.environ['STORAGE_DRILL_TARGET'] = 'postgres://platform:handover-local@' + target_name + ':5432/storage_mismatch'
        source, target = Database('STORAGE_DRILL_SOURCE', source_container), Database('STORAGE_DRILL_TARGET', source_container)
        source.sql("create table original_bytes(value bytea); insert into original_bytes values(decode('012345','hex'))")
        original_manifest = target.manifest
        def corrupt_target():
            target.sql("update original_bytes set value=decode('ff','hex')")
            return original_manifest()
        target.manifest = corrupt_target
        try:
            handover(source, target, directory / 'mismatch', project + '-mismatch', files, files)
        except ValueError as error:
            if 'verification differs' not in str(error):
                raise
        else:
            raise AssertionError('A changed target was activated')
        for database in (source, target):
            if database.sql("select to_regclass('public.platform_storage_fence') is not null") != 't':
                raise AssertionError('Failed handover lost its fence')
        print('ok   independent PostgreSQL handover, exact results and bytes, target suffix, stale-source fencing and corrupt-target refusal', flush=True)
    finally:
        run('docker', 'rm', '-f', target_name, stdout=subprocess.DEVNULL)


if __name__ == '__main__':
    main()
