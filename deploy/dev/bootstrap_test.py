import importlib.util
import json
from pathlib import Path
import stat
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('bootstrap', Path(__file__).parents[1] / 'local/bootstrap.py')
bootstrap = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bootstrap)


class BootstrapTests(unittest.TestCase):
    def test_fresh_accounts_have_distinct_roles_and_private_stable_configuration(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = bootstrap.bootstrap(root)
            files = {p: p.read_bytes() for p in root.rglob('*') if p.is_file()}
            self.assertEqual(bootstrap.bootstrap(root, check=True), first)
            self.assertEqual(bootstrap.bootstrap(root), first)
            self.assertEqual(files, {p: p.read_bytes() for p in files})
            self.assertTrue(all(stat.S_IMODE(p.stat().st_mode) == 0o600 for p in files))
            accounts = json.loads((root / '.build/local/accounts.json').read_text())['accounts']
            self.assertEqual(len({a['password'] for a in accounts}), 3)
            self.assertTrue(all(len(a['password']) >= 30 for a in accounts))
            seats = json.loads((root / '.build/local/tenants.json').read_text())[0]['seats']
            self.assertEqual(seats[2]['grants'], [])
            self.assertEqual({g['role'] for g in seats[1]['grants']}, {'builder', 'integrator'})
            self.assertFalse(any(g['role'] == 'admin' for g in seats[1]['grants']))
            self.assertEqual(len(json.loads((root / '.build/local/rauthy/bootstrap/users.json').read_text())), 2)
            self.assertEqual(json.loads((root / '.build/local/rauthy/bootstrap/clients.json').read_text())[0]['redirect_uris'], ['http://localhost:8495/'])

    def test_missing_or_changed_configuration_is_refused_without_rotation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = bootstrap.bootstrap(root)
            before = (root / 'deploy/local/.env').read_bytes()
            (root / '.build/local/rauthy/config.toml').unlink()
            with self.assertRaises(RuntimeError):
                bootstrap.bootstrap(root)
            self.assertEqual((root / 'deploy/local/.env').read_bytes(), before)
            self.assertEqual(bootstrap.read_env(root / 'deploy/local/.env'), config)

    def test_existing_old_env_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'deploy/local/.env'
            path.parent.mkdir(parents=True)
            path.write_text('OLD=value\n')
            path.chmod(0o600)
            with self.assertRaises(RuntimeError):
                bootstrap.bootstrap(Path(directory))
            self.assertEqual(path.read_text(), 'OLD=value\n')
