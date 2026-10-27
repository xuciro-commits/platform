#!/usr/bin/env python3
"""Run one Platform API and its single public workspace entry."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time
import urllib.request
import urllib.parse

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / '.build/dev/environment.json'
LOGS = STATE.parent
COMPOSE = ['docker', 'compose', '--env-file', 'deploy/local/.env', '-f', 'deploy/local/compose.yaml']
NATIVE_COMPOSE = [*COMPOSE, '-f', 'deploy/dev/compose.native.yaml']
API_PORT = 18495


def run(command):
    subprocess.run(command, cwd=ROOT, check=True)


def configuration():
    spec = importlib.util.spec_from_file_location('local_bootstrap', ROOT / 'deploy/local/bootstrap.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.bootstrap(root=ROOT)


def ports(state):
    return [('api', API_PORT), ('web', state.get('webPort', 8495))]


def public_url(state):
    return state.get('url', 'http://localhost:8495').rstrip('/')


def read_state():
    try:
        return json.loads(STATE.read_text())
    except (OSError, ValueError):
        return {}


def write_state(state):
    STATE.parent.mkdir(parents=True, exist_ok=True)
    STATE.write_text(json.dumps(state))


def selected_mode():
    state = read_state()
    return state.get('mode', 'docker')


def stamp(pid):
    result = subprocess.run(['ps', '-p', str(pid), '-o', 'lstart='],
                            capture_output=True, text=True)
    return ' '.join(result.stdout.split()) if result.returncode == 0 else ''


def _processes():
    result = subprocess.run(['ps', '-axo', 'pid=,ppid=,stat=,lstart='], capture_output=True, text=True, check=True)
    rows = [line.split() for line in result.stdout.splitlines()]
    return {int(row[0]): (int(row[1]), ' '.join(row[3:])) for row in rows if len(row) == 8 and not row[2].startswith('Z')}


def _matches(process, table):
    current = table.get(process.get('pid'))
    return bool(current and process.get('stamp') and current[1] == process['stamp'])


def _descends(pid, parent, table):
    while pid in table:
        if pid == parent:
            return True
        pid = table[pid][0]
    return False


def _owned(roots, table):
    parents = [root['pid'] for root in roots if _matches(root, table)]
    return {pid: {'pid': pid, 'stamp': row[1]} for pid, row in table.items()
            if any(_descends(pid, parent, table) for parent in parents)}


def _listeners(port):
    result = subprocess.run(['lsof', '-nP', '-t', f'-iTCP:{port}', '-sTCP:LISTEN'], capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise RuntimeError(f'无法核对本地端口 {port} 的进程归属。')
    return {int(pid) for pid in result.stdout.split()}


def alive(state):
    return _matches(state, _processes())


def native_listeners(state=None, table=None):
    state = read_state() if state is None else state
    table = _processes() if table is None else table
    if not _matches(state, table):
        raise RuntimeError('本地监督进程尚未运行。')
    bound, waiting = {}, False
    for kind, port in ports(state):
        child = next((item for item in state.get('children', []) if item['kind'] == kind), {})
        if not _matches(child, table) or table[child['pid']][0] != state['pid']:
            raise RuntimeError(f'Platform {kind} 不属于当前本地监督进程。')
        listeners = _listeners(port)
        if not listeners:
            waiting = True
        elif len(listeners) != 1 or not _descends(next(iter(listeners)), child['pid'], table):
            raise RuntimeError(f'端口 {port} 由其他进程占用（PID {sorted(listeners)}），不是当前 Platform {kind}。')
        else:
            bound[kind] = next(iter(listeners))
    return None if waiting else bound


def running_binary(pid, binary):
    binary = Path(binary).resolve()
    expected, inode = str(binary), None
    result = subprocess.run(['lsof', '-a', '-p', str(pid), '-d', 'txt', '-Ffin'], capture_output=True, text=True)
    for line in result.stdout.splitlines():
        if line.startswith('f'):
            inode = None
        elif line.startswith('i'):
            inode = line[1:]
        elif line == 'n' + expected and inode == str(binary.stat().st_ino):
            return True
    return False


def _healthy(state=None):
    base = public_url(read_state() if state is None else state)
    try:
        with urllib.request.urlopen(base + '/healthz', timeout=2) as response:
            health = json.load(response)
        if health.get('status') != 'ok' or health.get('quarantined') != 0:
            return False
        with urllib.request.urlopen(base + '/', timeout=2) as response:
            if b'/@vite/client' not in response.read():
                return False
        with urllib.request.urlopen(base + '/auth/v1/.well-known/openid-configuration', timeout=2) as response:
            identity = json.load(response)
        return identity.get('issuer') == base + '/auth/v1/'
    except (OSError, ValueError):
        return False


def ready(state=None):
    try:
        return bool(native_listeners(state)) and _healthy(state)
    except (OSError, RuntimeError):
        return False


def _stop_native_processes(roots, known=None, grace=8, first_signal=signal.SIGINT):
    known, sent = dict(known or {}), set()
    deadline, forced = time.monotonic() + grace, False
    while True:
        table = _processes()
        known.update(_owned([*roots, *known.values()], table))
        current = {pid: item for pid, item in known.items() if _matches(item, table)}
        if not current:
            break
        if time.monotonic() >= deadline:
            if forced:
                raise RuntimeError(f'本地进程尚未退出（PID {sorted(current)}），保留归属记录；未切换宿主。')
            forced, deadline = True, time.monotonic() + 2
        sig = signal.SIGKILL if forced else first_signal
        targets = current if forced else {pid: item for pid, item in current.items() if table[pid][0] not in current}
        for pid, item in targets.items():
            key = (pid, item['stamp'], sig)
            if key not in sent and stamp(pid) == item['stamp']:
                try:
                    os.kill(pid, sig)
                except ProcessLookupError:
                    pass
                sent.add(key)
        time.sleep(0.1)
    # Air starts its host in a separate process group. Check its captured
    # descendants, never kill an unrelated listener that acquired the port.
    table = _processes()
    for _, port in ports(read_state()):
        if any(_matches(known.get(pid, {}), table) for pid in _listeners(port)):
            raise RuntimeError(f'当前本地环境仍占用端口 {port}，未切换宿主。')


def stop():
    state = read_state()
    mode = selected_mode()
    _stop_native_processes([state, *state.get('children', [])], grace=15, first_signal=signal.SIGTERM)
    write_state({'mode': mode, 'status': 'stopped'})


def serve():
    config = configuration()
    children = []
    known = {}
    bound, last_probe = {}, 0
    interrupted = False

    def interrupt(*_):
        nonlocal interrupted
        interrupted = True

    signal.signal(signal.SIGTERM, interrupt)
    signal.signal(signal.SIGINT, interrupt)
    pid = os.getpid()
    state = {'mode': 'native', 'pid': pid, 'stamp': stamp(pid), 'status': 'starting', 'url': config['PLATFORM_PUBLIC_URL'], 'webPort': int(config['PLATFORM_PORT']), 'children': []}
    write_state(state)
    try:
        for _, port in ports(state):
            listeners = _listeners(port)
            if listeners:
                raise RuntimeError(f'端口 {port} 已被占用（PID {sorted(listeners)}）；请先停止对应宿主，本地启动不会接管其他进程。')
        air = os.environ.get('AIR') or shutil.which('air')
        node = shutil.which('node')
        vite = ROOT / 'web/apps/workspace/node_modules/vite/bin/vite.js'
        if not air or not node or not vite.is_file():
            raise RuntimeError('缺少 Air、Node 或 Vite，请先运行 make setup。')
        commands = [
            ('api', [air, '-c', 'deploy/dev/air.toml'], ROOT),
            ('web', [node, str(vite), '--host', '127.0.0.1', '--port', str(state['webPort'])], ROOT / 'web/apps/workspace'),
        ]
        env = {**os.environ, **config, 'PLATFORM_HOST': f'http://127.0.0.1:{API_PORT}'}
        for kind, command, cwd in commands:
            with (LOGS / f'platform-{kind}.log').open('w') as log:
                child = subprocess.Popen(command, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                children.append(child)
                state['children'].append({'kind': kind, 'pid': child.pid, 'stamp': stamp(child.pid)})
                write_state(state)
        while not interrupted:
            table = _processes()
            known = {pid: item for pid, item in known.items() if _matches(item, table)}
            known.update(_owned(state['children'], table))
            for child in children:
                if child.poll() is not None:
                    raise RuntimeError(f'本地进程退出（{child.returncode}），请查看 .build/dev/*-api.log 或 *-web.log。')
            now = time.monotonic()
            if state['status'] != 'ready' or now - last_probe >= 5 or any(not _matches(item, table) for item in bound.values()):
                listeners = native_listeners(state, table)
                last_probe = now
                bound = {pid: {'pid': pid, 'stamp': table[pid][1]}
                         for pid in (listeners or {}).values()}
                status = 'ready' if listeners and _healthy(state) else 'starting'
                if state['status'] != status:
                    state['status'] = status
                    write_state(state)
            time.sleep(0.5)
    finally:
        _stop_native_processes(state['children'], known)
        for child in children:
            child.wait()
        write_state({'mode': 'native', 'status': 'stopped'})


def local():
    config = configuration()
    if alive(read_state()) and ready() and public_url(read_state()) == config['PLATFORM_PUBLIC_URL'].rstrip('/'):
        write_state({**read_state(), 'mode': 'native'})
        print('本地热更新已运行：Platform ' + config['PLATFORM_PUBLIC_URL'])
        return
    air = os.environ.get('AIR') or shutil.which('air')
    if not air or not os.access(air, os.X_OK):
        raise RuntimeError('缺少 Air，请先运行 make setup（只需一次）。')
    stop()
    write_state({'mode': 'native', 'status': 'starting'})
    run(COMPOSE + ['stop', 'platform-server', 'wasm-worker', 'code-builder'])
    run(['make', 'infra', 'infra-compute'])
    LOGS.mkdir(parents=True, exist_ok=True)
    with (LOGS / 'environment.log').open('w') as log:
        supervisor = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), 'serve'], cwd=ROOT,
                                      stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        while True:
            if supervisor.poll() is not None:
                raise RuntimeError((LOGS / 'environment.log').read_text())
            state = read_state()
            if state.get('pid') == supervisor.pid and state.get('status') == 'ready' and ready(state):
                print('本地热更新已就绪：Platform ' + config['PLATFORM_PUBLIC_URL'])
                print('Go 自动编译重启，前端 HMR；日志：.build/dev/；切回容器：make docker')
                return
            time.sleep(0.5)
    except BaseException:
        stop()
        raise


def prepare_docker():
    configuration()
    stop()
    write_state({'mode': 'docker', 'status': 'starting'})
    run(COMPOSE + ['-p', 'platform-dev-compute', '-f', 'deploy/dev/compose.compute.yaml', 'down'])
    run(COMPOSE + ['up', '-d', '--wait', '--force-recreate', 'postgres', 'rauthy', 'rustfs', 'wasm-worker', 'code-builder'])


def docker():
    configuration()
    dirty = subprocess.check_output(['git', 'status', '--porcelain', '--', 'capabilities/server', 'apps',
                                    'protocols', 'contract', 'web/packages', 'web/apps/workspace/src'],
                                   cwd=ROOT, text=True)
    if dirty:
        raise RuntimeError('请先提交源码再切到 Docker，以便核对镜像版本；本地环境仍保留运行。')
    run(['env', 'PLATFORM_LOCAL_MODE=docker', 'deploy/local/update.sh'])


if __name__ == '__main__':
    try:
        {'local': local, 'docker': docker, 'serve': serve, 'prepare-docker': prepare_docker}[sys.argv[1]]()
    except (RuntimeError, subprocess.CalledProcessError, KeyError, IndexError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
