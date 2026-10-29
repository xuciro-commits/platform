#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
if [[ -n $(git status --porcelain -- capabilities/server contract web deploy scripts Makefile) ]]; then
  echo 'Commit source changes before updating the local host.' >&2
  exit 1
fi
python3 deploy/local/bootstrap.py
export PLATFORM_REVISION
PLATFORM_REVISION=$(git rev-parse HEAD)
python3 - <<'PY'
import datetime, hashlib, importlib.util, json, os, re, subprocess, time, urllib.request
from pathlib import Path
root = Path.cwd()
spec = importlib.util.spec_from_file_location('environment', root / 'deploy/dev/environment.py')
environment = importlib.util.module_from_spec(spec)
spec.loader.exec_module(environment)
config = environment.configuration()
mode = os.environ.get('PLATFORM_LOCAL_MODE') or environment.selected_mode()
if mode not in ('native', 'docker'):
    raise SystemExit('PLATFORM_LOCAL_MODE must be native or docker.')
revision = os.environ['PLATFORM_REVISION']
base = config['PLATFORM_PUBLIC_URL']
if mode == 'native':
    environment.stop()
    subprocess.run(['make', 'local'], check=True)
    listeners = environment.native_listeners()
    binary = root / '.build/dev/platform-server'
    if not listeners or not environment.running_binary(listeners['api'], binary):
        raise SystemExit('The single native API is not running its current binary')
    metadata = subprocess.check_output(['go', 'version', '-m', str(binary)], text=True)
    if 'vcs.revision=' + revision not in metadata or 'vcs.modified=true' in metadata:
        raise SystemExit('The native binary differs from the committed revision')
else:
    environment.prepare_docker()
    subprocess.run(['pnpm', '--dir', 'web/apps/workspace', 'build'], check=True)
    subprocess.run(environment.COMPOSE + ['up', '-d', '--no-deps', '--build', 'platform-server'], check=True)
    container = subprocess.check_output(environment.COMPOSE + ['ps', '-q', 'platform-server'], text=True).strip()
    image = subprocess.check_output(['docker', 'inspect', '--format', '{{.Image}}', container], text=True).strip()
    stamp = subprocess.check_output(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', image], text=True).strip()
    if stamp != revision:
        raise SystemExit('The running image does not match the committed revision')
deadline = time.monotonic() + 180
while True:
    try:
        with urllib.request.urlopen(base + '/healthz', timeout=5) as response:
            health = json.load(response)
        with urllib.request.urlopen(base + '/auth/v1/.well-known/openid-configuration', timeout=5) as response:
            discovery = json.load(response)
        if health.get('status') == 'ok' and health.get('quarantined') == 0 and discovery.get('issuer') == base + '/auth/v1/':
            break
    except OSError:
        pass
    if time.monotonic() >= deadline:
        raise SystemExit('Platform or same-origin identity service did not become healthy')
    time.sleep(2)
with urllib.request.urlopen(base + '/', timeout=10) as response:
    html = response.read()
if mode == 'native':
    if b'/@vite/client' not in html:
        raise SystemExit('The selected native workspace is not Vite')
else:
    if html != (root / 'web/apps/workspace/dist/index.html').read_bytes():
        raise SystemExit('The served workspace differs from its build')
assets = sorted(set(re.findall(r'(?:src|href)="(/[^\"]+)"', html.decode())))
for asset in assets:
    with urllib.request.urlopen(base + asset, timeout=10) as response:
        body = response.read()
    if not body or mode == 'docker' and body != (root / 'web/apps/workspace/dist' / asset.lstrip('/')).read_bytes():
        raise SystemExit('The served page resource is missing or stale')
environment.write_state({'mode': mode, 'status': 'ready', 'url': base}) if mode == 'docker' else None
report = {'revision': revision, 'mode': mode, 'url': base, 'health': health, 'identityIssuer': discovery['issuer'], 'matchedAssets': len(assets), 'checkedAtUTC': datetime.datetime.now(datetime.timezone.utc).isoformat()}
output = root / '.build/local-update'
output.mkdir(parents=True, exist_ok=True)
(output / (revision + '.json')).write_text(json.dumps(report, indent=2) + '\n')
print(f'{base}: {revision[:12]}, healthy, same-origin login and {len(assets)} page resources verified')
PY
