#!/usr/bin/env bash
# Rebuild and verify the two owner-facing solution hosts without clearing data.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
if [[ -n $(git status --porcelain -- capabilities/server apps solutions protocols contract web/packages web/apps/workspace/src) ]]; then
  echo 'Commit source changes before updating the local hosts, so their revision is unambiguous.' >&2
  exit 1
fi
export PLATFORM_REVISION
PLATFORM_REVISION=$(git rev-parse HEAD)
mkdir -p .build/local-update
pnpm --dir web/apps/workspace build
docker compose -f deploy/local/compose.yaml config --quiet
docker compose -f deploy/local/compose.yaml up -d --no-deps --build hospitality-server manufacturing-server
python3 - <<'PY'
from pathlib import Path
import datetime, hashlib, json, os, re, subprocess, time, urllib.request
root = Path.cwd()
revision = os.environ['PLATFORM_REVISION']
dist = root / 'web/apps/workspace/dist'
html = (dist / 'index.html').read_bytes()
assets = sorted(set(re.findall(r'(?:src|href)="(/assets/[^"]+)"', html.decode())))
compose = ['docker', 'compose', '-f', 'deploy/local/compose.yaml']
hosts = []
for service, port in [('hospitality-server', 8495), ('manufacturing-server', 8490)]:
    container = subprocess.check_output([*compose, 'ps', '-q', service], text=True).strip()
    image = subprocess.check_output(['docker', 'inspect', '--format', '{{.Image}}', container], text=True).strip()
    stamped = subprocess.check_output(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', image], text=True).strip()
    if stamped != revision:
        raise SystemExit(f'{service}: image revision {stamped} differs from {revision}')
    base = f'http://localhost:{port}'
    deadline = time.monotonic() + 180
    while True:
        try:
            with urllib.request.urlopen(base + '/healthz', timeout=5) as response:
                health = json.load(response)
            if health.get('status') != 'ok' or health.get('quarantined') != 0:
                raise SystemExit(f'{service}: unhealthy tenant state {health}')
            break
        except OSError:
            if time.monotonic() >= deadline:
                raise
            time.sleep(2)
    with urllib.request.urlopen(base + '/', timeout=15) as response:
        if response.read() != html:
            raise SystemExit(f'{service}: served HTML does not match the workspace build')
    for asset in assets:
        with urllib.request.urlopen(base + asset, timeout=15) as response:
            if response.read() != (dist / asset.lstrip('/')).read_bytes():
                raise SystemExit(f'{service}: stale asset {asset}')
    started = subprocess.check_output(['docker', 'inspect', '--format', '{{.State.StartedAt}}', container], text=True).strip()
    hosts.append({'service': service, 'url': base, 'revision': stamped, 'image': image, 'container': container, 'started': started, 'health': health, 'matchedAssets': len(assets)})
    print(f'{base}: {revision[:12]}, healthy, HTML + {len(assets)} assets verified')
report = {'revision': revision, 'checkedAtUTC': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'htmlSHA256': hashlib.sha256(html).hexdigest(), 'hosts': hosts}
(root / '.build/local-update' / (revision + '.json')).write_text(json.dumps(report, indent=2) + '\n')
PY
