#!/usr/bin/env python3
"""Create private local development configuration once; never reset existing state."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import secrets
import shlex
import stat
import sys

ROOT = Path(__file__).resolve().parents[2]
FORMAT = 'platform.local.v1'


def read_env(path):
    values = {}
    for line in Path(path).read_text().splitlines():
        if not line.strip() or line.startswith('#'):
            continue
        key, separator, raw = line.partition('=')
        tokens = shlex.split(raw)
        if not separator or not key.isidentifier() or len(tokens) != 1 or key in values:
            raise RuntimeError('Invalid local configuration; no values were changed.')
        values[key] = tokens[0]
    return values


def private_write(path, data):
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    with path.open('x', encoding='utf-8') as output:
        os.chmod(path, 0o600)
        output.write(data)


def json_text(value):
    return json.dumps(value, ensure_ascii=False, indent=2) + '\n'


def validate(root):
    local = root / '.build/local'
    env_path = root / 'deploy/local/.env'
    if env_path.is_symlink() or not env_path.is_file() or stat.S_IMODE(env_path.stat().st_mode) != 0o600:
        raise RuntimeError('Local .env must be an existing private file (0600).')
    config = read_env(env_path)
    if config.get('PLATFORM_LOCAL_FORMAT') != FORMAT or config.get('PLATFORM_LOCAL_ROOT') != str(local) or config.get('PLATFORM_TENANTS_FILE') != str(local / 'tenants.json'):
        raise RuntimeError('Old or inconsistent local configuration; explicitly retire the old environment before creating a new one.')
    manifest_path = local / 'manifest.json'
    if not manifest_path.is_file():
        raise RuntimeError('Incomplete private configuration; restore the original files instead of regenerating keys.')
    if manifest_path.is_symlink() or stat.S_IMODE(manifest_path.stat().st_mode) != 0o600:
        raise RuntimeError('The private configuration manifest must have mode 0600.')
    manifest = json.loads(manifest_path.read_text())
    if manifest.get('format') != FORMAT or not manifest.get('files'):
        raise RuntimeError('Invalid local configuration manifest.')
    for relative, digest in manifest['files'].items():
        path = root / relative
        if not path.resolve().is_relative_to(root) or path.is_symlink() or not path.is_file() or stat.S_IMODE(path.stat().st_mode) != 0o600 or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise RuntimeError('Private configuration is missing or changed; keep its original keys and update configuration deliberately.')
    tenants = local / 'tenants.json'
    if not tenants.is_file() or stat.S_IMODE(tenants.stat().st_mode) != 0o600 or not isinstance(json.loads(tenants.read_text()), list):
        raise RuntimeError('The original local tenant configuration is unavailable.')
    return config


def bootstrap(root=None, check=False):
    root = Path(root or ROOT).resolve()
    local, env_path = root / '.build/local', root / 'deploy/local/.env'
    if env_path.exists() or local.exists():
        return validate(root)
    if check:
        raise RuntimeError('Local configuration has not been initialized; run make bootstrap.')
    os.umask(0o077)
    local.mkdir(parents=True, mode=0o700)
    suffix = secrets.token_hex(6)
    tenant = 'dev-' + suffix
    accounts = [{'email': name + '@platform.test', 'member': 'dev-' + name, 'password': 'Dev-' + secrets.token_urlsafe(24)} for name in ['admin', 'builder', 'user']]
    roles = {
        'dev-admin': [('platform', 'admin'), ('ai', 'admin'), ('agent', 'admin'), ('enterprise', 'admin'), ('work', 'admin'), ('build', 'publisher')],
        'dev-builder': [('build', 'builder'), ('build', 'integrator')],
        'dev-user': [],
    }
    tenants = [{'id': tenant, 'name': 'Development', 'template': 'blank', 'seats': [
        {'id': account['member'], 'subjects': ['user:' + account['email']], 'grants': [{'app': app, 'role': role} for app, role in roles[account['member']]]}
        for account in accounts
    ]}]
    public_url = 'http://localhost:8495'
    config = {
        'PLATFORM_LOCAL_FORMAT': FORMAT, 'PLATFORM_LOCAL_ROOT': str(local),
        'PLATFORM_TENANTS_FILE': str(local / 'tenants.json'), 'PLATFORM_DATA_NAMESPACE': 'platform-dev-' + suffix,
        'PLATFORM_PUBLIC_URL': public_url, 'PLATFORM_PORT': '8495', 'PLATFORM_DATABASE_NAME': 'platform_dev',
        'PLATFORM_HOST_ADMINS': 'user:admin@platform.test', 'PLATFORM_RUN_UID': str(os.getuid()), 'PLATFORM_RUN_GID': str(os.getgid()),
        'POSTGRES_PASSWORD': secrets.token_hex(24), 'PLATFORM_S3_ACCESS_KEY': 'platform' + secrets.token_hex(6),
        'PLATFORM_S3_SECRET_KEY': secrets.token_hex(32), 'PLATFORM_CREDENTIAL_KEY': secrets.token_hex(32),
        'PLATFORM_PERSONAL_TOKEN_KEY': secrets.token_hex(32), 'PG_PORT': '5433', 'IDP_PORT': '8480', 'FILES_PORT': '9000',
    }
    client = {'id': 'platform-web', 'name': 'Platform', 'enabled': True, 'confidential': False,
              'redirect_uris': [public_url + '/'], 'post_logout_redirect_uris': [public_url + '/'], 'allowed_origins': [public_url],
              'access_token_alg': 'EdDSA', 'id_token_alg': 'EdDSA', 'auth_code_lifetime': 60, 'access_token_lifetime': 900,
              'scopes': ['openid', 'email', 'profile'], 'default_scopes': ['openid', 'email', 'profile'],
              'force_mfa': False, 'flows_enabled': ['authorization_code', 'refresh_token'], 'challenges': ['S256']}
    # Rauthy creates its bootstrap administrator before importing users.
    users = [{'email': a['email'], 'given_name': a['member'].removeprefix('dev-').capitalize(), 'password': {'Plain': a['password']},
              'roles': [], 'enabled': True, 'email_verified': True} for a in accounts[1:]]
    rauthy = f'''[bootstrap]
admin_email = 'admin@platform.test'
password_plain = '{accounts[0]['password']}'
bootstrap_dir = '/app/bootstrap'

[cluster]
node_id = 1
nodes = ["1 localhost:8100 localhost:8200"]
secret_raft = '{secrets.token_hex(24)}'
secret_api = '{secrets.token_hex(24)}'
insecure_cookie = true

[encryption]
keys = ['k1/{base64.b64encode(secrets.token_bytes(32)).decode()}']
key_active = 'k1'

[server]
scheme = 'http'
port_http = 8080
pub_url = 'localhost:8495'

[access]
cookie_mode = 'danger-insecure'

[webauthn]
rp_id = 'localhost'
rp_origin = '{public_url}'
'''
    content = {
        local / 'tenants.json': json_text(tenants),
        local / 'accounts.json': json_text({'format': FORMAT, 'publicUrl': public_url, 'tenant': tenant, 'accounts': accounts}),
        local / 'rauthy/config.toml': rauthy,
        local / 'rauthy/bootstrap/users.json': json_text(users),
        local / 'rauthy/bootstrap/clients.json': json_text([client]),
        local / 'credentials.md': '# 本地开发测试账号\n\n入口：' + public_url + '\n\n业务工作区：`' + tenant + '`\n\n| 账号 | 密码 | 权限 |\n|---|---|---|\n' + '\n'.join(
            '| ' + a['email'] + ' | `' + a['password'] + '` | ' + description + ' |' for a, description in zip(accounts, ['平台和宿主管理；无默认业务角色', '构建与集成；无宿主管理', '普通成员；无业务角色'])
        ) + '\n\n这些密码仅供本机开发测试。首次登录管理员可能需要确认个人资料。AI服务、业务包与业务角色均需另行配置。\n日常启动保持本文件、密钥和数据不变；不要提交或公开本文件。\n',
        env_path: '\n'.join(key + '=' + shlex.quote(value) for key, value in config.items()) + '\n',
    }
    for path, data in content.items():
        private_write(path, data)
    manifest = {'format': FORMAT, 'files': {str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest() for path in content if path != local / 'tenants.json'}}
    private_write(local / 'manifest.json', json_text(manifest))
    return validate(root)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='validate without creating configuration')
    args = parser.parse_args(argv)
    bootstrap(check=args.check)
    print('Private local configuration ready. Accounts: .build/local/credentials.md')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, RuntimeError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
