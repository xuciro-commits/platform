#!/usr/bin/env python3
"""Switch the two local solution hosts between Air/Vite and Compose (ADR-0081)."""
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / '.build/dev/environment.json'
LOGS = STATE.parent
COMPOSE = ['docker', 'compose', '-f', 'deploy/local/compose.yaml']
SOLUTIONS = [('hospitality', 18495, 8495), ('manufacturing', 18490, 8490)]


def run(command):
    subprocess.run(command, cwd=ROOT, check=True)


def read_state():
    try:
        return json.loads(STATE.read_text())
    except (OSError, ValueError):
        return {}


def stamp(pid):
    result = subprocess.run(['ps', '-p', str(pid), '-o', 'lstart=', '-o', 'command='],
                            capture_output=True, text=True)
    return result.stdout.strip() if result.returncode == 0 else ''


def alive(state):
    return bool(state.get('pid') and state.get('stamp') and stamp(state['pid']) == state['stamp'])


def ready():
    try:
        for _, _, port in SOLUTIONS:
            with urllib.request.urlopen(f'http://localhost:{port}/healthz', timeout=2) as response:
                health = json.load(response)
            if health.get('status') != 'ok' or health.get('quarantined') != 0:
                return False
            with urllib.request.urlopen(f'http://localhost:{port}/', timeout=2) as response:
                if b'/@vite/client' not in response.read():
                    return False
        return True
    except (OSError, ValueError):
        return False


def stop():
    state = read_state()
    if alive(state):
        os.kill(state['pid'], signal.SIGTERM)
        deadline = time.monotonic() + 15
        while alive(state):
            if time.monotonic() > deadline:
                raise RuntimeError('本地进程尚未退出，请查看 .build/dev/environment.log；未启动 Docker 宿主。')
            time.sleep(0.2)
    STATE.unlink(missing_ok=True)


def serve():
    children = []
    interrupted = False

    def interrupt(*_):
        nonlocal interrupted
        interrupted = True

    signal.signal(signal.SIGTERM, interrupt)
    signal.signal(signal.SIGINT, interrupt)
    pid = os.getpid()
    state = {'pid': pid, 'stamp': stamp(pid), 'status': 'starting'}
    STATE.write_text(json.dumps(state))
    try:
        for solution, api, web in SOLUTIONS:
            commands = [
                ('api', ['make', 'dev', f'SOLUTION={solution}', f'PORT.{solution}={api}']),
                ('web', ['make', 'web', f'SOLUTION={solution}', f'PORT.{solution}={api}', f'WEB_PORT={web}']),
            ]
            for kind, command in commands:
                with (LOGS / f'{solution}-{kind}.log').open('w') as log:
                    children.append(subprocess.Popen(command, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT,
                                                     start_new_session=True))
        deadline = time.monotonic() + 90
        while not interrupted:
            for child in children:
                if child.poll() is not None:
                    raise RuntimeError(f'本地进程退出（{child.returncode}），请查看 .build/dev/*-api.log 或 *-web.log。')
            if state['status'] == 'starting':
                if ready():
                    state['status'] = 'ready'
                    STATE.write_text(json.dumps(state))
                elif time.monotonic() > deadline:
                    raise RuntimeError('本地环境启动超时，请查看 .build/dev/*-api.log 或 *-web.log。')
            time.sleep(0.5)
    finally:
        # Air, Go and Vite may have descendants; stop each group we started.
        for child in children:
            try:
                os.killpg(child.pid, signal.SIGINT)
            except ProcessLookupError:
                pass
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline and any(child.poll() is None for child in children):
            time.sleep(0.1)
        for child in children:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            child.wait()
        STATE.unlink(missing_ok=True)


def local():
    if alive(read_state()) and ready():
        print('本地热更新已运行：酒店 http://localhost:8495 · 制造 http://localhost:8490')
        return
    air = os.environ.get('AIR') or shutil.which('air')
    if not air or not os.access(air, os.X_OK):
        raise RuntimeError('缺少 Air，请先运行 make setup（只需一次）。')
    stop()
    run(COMPOSE + ['stop', 'hospitality-server', 'manufacturing-server', 'wasm-worker', 'code-builder'])
    run(['make', 'infra', 'infra-compute'])
    LOGS.mkdir(parents=True, exist_ok=True)
    with (LOGS / 'environment.log').open('w') as log:
        supervisor = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), 'serve'], cwd=ROOT,
                                      stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    deadline = time.monotonic() + 100
    try:
        while time.monotonic() < deadline:
            if supervisor.poll() is not None:
                raise RuntimeError((LOGS / 'environment.log').read_text())
            state = read_state()
            if state.get('pid') == supervisor.pid and state.get('status') == 'ready':
                print('本地热更新已就绪：酒店 http://localhost:8495 · 制造 http://localhost:8490')
                print('Go 自动编译重启，前端 HMR；日志：.build/dev/；切回容器：make docker')
                return
            time.sleep(0.5)
        raise RuntimeError('本地环境启动超时，请查看 .build/dev/environment.log。')
    except BaseException:
        stop()
        raise


def docker():
    dirty = subprocess.check_output(['git', 'status', '--porcelain', '--', 'capabilities/server', 'apps',
                                    'solutions', 'protocols', 'contract', 'web/packages', 'web/apps/workspace/src'],
                                   cwd=ROOT, text=True)
    if dirty:
        raise RuntimeError('请先提交源码再切到 Docker，以便核对镜像版本；本地环境仍保留运行。')
    stop()
    run(COMPOSE + ['-p', 'platform-dev-compute', '-f', 'deploy/dev/compose.compute.yaml', 'stop',
                   'wasm-worker', 'code-builder'])
    run(COMPOSE + ['up', '-d', 'postgres', 'rauthy', 'rustfs', 'webhook-sink', 'wasm-worker', 'code-builder'])
    run(['deploy/local/update.sh'])


if __name__ == '__main__':
    try:
        {'local': local, 'docker': docker, 'serve': serve}[sys.argv[1]]()
    except (RuntimeError, subprocess.CalledProcessError, KeyError, IndexError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
