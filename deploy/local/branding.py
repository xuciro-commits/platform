#!/usr/bin/env python3
"""Apply the platform theme and mark to the local identity service.

The colors are the platform tokens from web/packages/ui/src/themes/tokens.css.
"""
import importlib.util
import json
import secrets
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
LOCAL = ROOT / '.build/local'
MARK = ROOT / 'web/public/favicon.svg'
KEY_NAME = 'platform-branding'
DEADLINE_SECONDS = 30

THEME = {
    'border_radius': '4px',
    'light': {'text': [215, 13, 43], 'text_high': [213, 16, 13], 'bg': [220, 20, 97], 'bg_high': [0, 0, 100],
              'action': [215, 65, 50], 'accent': [215, 65, 50], 'error': [358, 58, 53], 'btn_text': 'white',
              'theme_sun': 'hsla(var(--action) / .7)', 'theme_moon': 'hsla(var(--accent) / .85)'},
    'dark': {'text': [216, 14, 71], 'text_high': [220, 20, 97], 'bg': [213, 16, 13], 'bg_high': [215, 14, 17],
             'action': [215, 85, 62], 'accent': [215, 100, 77], 'error': [358, 55, 67], 'btn_text': '#111418',
             'theme_sun': 'hsla(var(--action) / .7)', 'theme_moon': 'hsla(var(--accent) / .85)'},
}

def configuration():
    spec = importlib.util.spec_from_file_location('local_bootstrap', ROOT / 'deploy/local/bootstrap.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.bootstrap(root=ROOT)

def branding_key():
    path = LOCAL / 'rauthy/bootstrap/api_keys.json'
    if not path.is_file():
        return None
    for entry in json.loads(path.read_text()):
        secret = entry.get('secret')
        if entry.get('name') == KEY_NAME and isinstance(secret, dict):
            return secret.get('Plain')
    return None

def client_id():
    return json.loads((LOCAL / 'rauthy/bootstrap/clients.json').read_text())[0]['id']

def send(url, authorization, content_type=None, data=None, method='GET'):
    headers = {'Authorization': authorization}
    if content_type:
        headers['Content-Type'] = content_type
    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    with urllib.request.urlopen(request, timeout=10) as response:
        return response.status

def multipart(field, path):
    boundary = 'platform' + secrets.token_hex(16)
    head = f'--{boundary}\r\nContent-Disposition: form-data; name="{field}"; filename="{path.name}"\r\nContent-Type: image/svg+xml\r\n\r\n'.encode()
    return boundary, head + path.read_bytes() + f'\r\n--{boundary}--\r\n'.encode()

def apply(config, key):
    client = client_id()
    base = config['PLATFORM_PUBLIC_URL'].rstrip('/') + '/auth/v1'
    authorization = f'API-Key {KEY_NAME}${key}'
    theme = json.dumps({'client_id': client, **THEME}).encode()
    boundary, logo = multipart('logo', MARK)
    deadline = time.monotonic() + DEADLINE_SECONDS
    while True:
        try:
            send(f'{base}/theme/{client}', authorization, 'application/json', theme, 'PUT')
            send(f'{base}/clients/{client}/logo', authorization, f'multipart/form-data; boundary={boundary}', logo, 'PUT')
            return 'applied'
        except urllib.error.HTTPError as error:
            if error.code in (401, 403):
                return 'key'
            if error.code < 500 or time.monotonic() >= deadline:
                raise
        except OSError:
            if time.monotonic() >= deadline:
                return 'unreachable'
        time.sleep(0.5)

def main():
    config = configuration()
    key = branding_key()
    if not key:
        print('本地身份服务没有品牌访问键：数据卷在品牌功能之前创建，重建一次身份服务数据卷即可应用（见 deploy/local/README.md）。')
        return
    result = apply(config, key)
    if result == 'applied':
        print('登录页品牌已应用：平台主题与标记写入身份服务。')
    elif result == 'key':
        print('本地身份服务拒绝了品牌访问键：数据卷在品牌功能之前创建，重建一次身份服务数据卷即可应用（见 deploy/local/README.md）。')
    else:
        print('身份服务暂不可达，本次未应用登录页品牌。')

if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, RuntimeError) as error:
        print('登录页品牌未应用：' + str(error), file=sys.stderr)
