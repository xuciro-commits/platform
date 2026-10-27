import importlib.util
import io
import json
from pathlib import Path
import signal
import tempfile
import unittest
from unittest.mock import Mock, patch
from types import SimpleNamespace


spec = importlib.util.spec_from_file_location('environment', Path(__file__).with_name('environment.py'))
environment = importlib.util.module_from_spec(spec)
spec.loader.exec_module(environment)


class EnvironmentModeTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        state = patch.object(environment, 'STATE', Path(self.directory.name) / 'environment.json')
        state.start()
        self.addCleanup(state.stop)
        config = patch.object(environment, 'configuration', return_value={'PLATFORM_PUBLIC_URL': 'http://localhost:8495', 'PLATFORM_PORT': '8495'})
        config.start()
        self.addCleanup(config.stop)

    def test_native_selection_survives_a_dead_supervisor_and_stop(self):
        environment.write_state({'mode': 'native', 'pid': 123, 'stamp': 'previous process'})
        with patch.object(environment, 'alive', return_value=False), patch.object(environment, '_stop_native_processes'):
            environment.stop()
            self.assertEqual(environment.selected_mode(), 'native')
        self.assertNotIn('pid', environment.read_state())

    def test_docker_preparation_remembers_selection_before_starting_dependencies(self):
        environment.write_state({'mode': 'native', 'status': 'stopped'})
        commands = []

        def run(command):
            self.assertEqual(environment.selected_mode(), 'docker')
            commands.append(command)

        with patch.object(environment, 'alive', return_value=False), patch.object(environment, '_stop_native_processes'), patch.object(environment, 'run', side_effect=run):
            environment.prepare_docker()
        self.assertTrue(any('rustfs' in command and 'up' in command for command in commands))
        self.assertFalse(any('--volumes' in command for command in commands))

    def test_local_waits_for_live_supervisor_readiness_without_a_startup_deadline(self):
        ticks = [0]

        def advance(_):
            ticks[0] += 1
            if ticks[0] == 4:
                environment.write_state({'pid': 123, 'mode': 'native', 'status': 'ready'})

        with patch.object(environment, 'LOGS', Path(self.directory.name)), \
                patch.object(environment, 'alive', return_value=False), patch.object(environment, 'stop'), \
                patch.object(environment, 'ready', return_value=True), \
                patch.object(environment, 'run'), patch.object(environment.os, 'access', return_value=True), \
                patch.dict(environment.os.environ, {'AIR': '/air'}), \
                patch.object(environment.subprocess, 'Popen', return_value=SimpleNamespace(pid=123, poll=lambda: None)), \
                patch.object(environment.time, 'sleep', side_effect=advance), \
                patch.object(environment.time, 'monotonic', side_effect=lambda: ticks[0] * 100):
            environment.local()
        self.assertEqual(ticks[0], 4)

    def test_local_reports_an_exited_supervisor_and_stops_its_children(self):
        with patch.object(environment, 'LOGS', Path(self.directory.name)), \
                patch.object(environment, 'alive', return_value=False), patch.object(environment, 'stop') as stop, \
                patch.object(environment, 'run'), patch.object(environment.os, 'access', return_value=True), \
                patch.dict(environment.os.environ, {'AIR': '/air'}), \
                patch.object(environment.subprocess, 'Popen', return_value=SimpleNamespace(pid=123, poll=lambda: 1)):
            with self.assertRaises(RuntimeError):
                environment.local()
        self.assertEqual(stop.call_count, 2)

    def native_fixture(self):
        table = {100: (1, 'supervisor'), 101: (100, 'platform-air'), 102: (100, 'platform-vite'), 201: (101, 'platform-api')}
        children = [{'kind': kind, 'pid': pid, 'stamp': table[pid][1]} for kind, pid in [('api', 101), ('web', 102)]]
        state = {'mode': 'native', 'pid': 100, 'stamp': 'supervisor', 'status': 'ready', 'children': children}
        listeners = {18495: {201}, 8495: {102}}
        return state, table, listeners

    def healthy_response(self, url, **_):
        body = json.dumps({'status': 'ok', 'quarantined': 0}).encode() if url.endswith('/healthz') else json.dumps({'issuer': 'http://localhost:8495/auth/v1/'}).encode() if url.endswith('openid-configuration') else b'<script src="/@vite/client"></script>'
        return io.BytesIO(body)

    def test_ready_requires_the_current_airs_real_listener_even_when_an_orphan_is_healthy(self):
        state, table, listeners = self.native_fixture()
        table[201] = (1, 'platform-api')
        with patch.object(environment, '_processes', return_value=table), \
                patch.object(environment, '_listeners', side_effect=lambda port: listeners[port]), \
                patch.object(environment.urllib.request, 'urlopen', side_effect=self.healthy_response) as read:
            self.assertFalse(environment.ready(state))
        read.assert_not_called()

    def test_ready_accepts_actual_child_listeners_and_checks_their_http_health(self):
        state, table, listeners = self.native_fixture()
        with patch.object(environment, '_processes', return_value=table), \
                patch.object(environment, '_listeners', side_effect=lambda port: listeners[port]), \
                patch.object(environment.urllib.request, 'urlopen', side_effect=self.healthy_response) as read:
            self.assertTrue(environment.ready(state))
            self.assertEqual(environment.native_listeners(state)['api'], 201)
        self.assertEqual(read.call_count, 3)

    def test_ready_rejects_another_startup_that_reused_a_child_pid(self):
        state, table, listeners = self.native_fixture()
        table[101] = (100, 'other-air')
        with patch.object(environment, '_processes', return_value=table), \
                patch.object(environment, '_listeners', side_effect=lambda port: listeners[port]), \
                patch.object(environment.urllib.request, 'urlopen') as read:
            self.assertFalse(environment.ready(state))
        read.assert_not_called()

    def test_stop_waits_for_air_cleanup_after_a_make_wrapper_exits_first(self):
        table = {10: (1, 'make'), 20: (10, 'air'), 30: (20, 'api'), 99: (1, 'foreign')}
        ticks, signals = [0], []

        def send(pid, sig):
            signals.append((pid, sig))
            if pid == 10:
                table.pop(10)
                table[20] = (1, 'air')

        def advance(_):
            ticks[0] += 1
            if ticks[0] == 3:
                table.pop(20)
                table.pop(30)

        with patch.object(environment, '_processes', side_effect=lambda: dict(table)), \
                patch.object(environment, 'stamp', side_effect=lambda pid: table.get(pid, (0, ''))[1]), \
                patch.object(environment, '_listeners', side_effect=lambda port: {99} if port == 18495 else set()), \
                patch.object(environment.os, 'kill', side_effect=send), \
                patch.object(environment.time, 'sleep', side_effect=advance), \
                patch.object(environment.time, 'monotonic', side_effect=lambda: ticks[0] / 10):
            environment._stop_native_processes([{'pid': 10, 'stamp': 'make'}])
        self.assertEqual(signals, [(10, signal.SIGINT), (20, signal.SIGINT)])
        self.assertEqual(ticks[0], 3)
        self.assertIn(99, table)

    def test_forced_shutdown_kills_only_captured_descendants_and_not_a_reused_pid(self):
        table = {10: (1, 'air'), 20: (10, 'api'), 30: (10, 'build'), 99: (1, 'foreign')}
        ticks, signals = [0], []

        def send(pid, sig):
            signals.append((pid, sig))
            if sig == signal.SIGKILL:
                table.pop(pid)

        def advance(_):
            ticks[0] += 1
            if ticks[0] == 1:
                table[20] = (1, 'reused-by-foreign')

        with patch.object(environment, '_processes', side_effect=lambda: dict(table)), \
                patch.object(environment, 'stamp', side_effect=lambda pid: table.get(pid, (0, ''))[1]), \
                patch.object(environment, '_listeners', side_effect=lambda port: {20, 99} if port == 18495 else set()), \
                patch.object(environment.os, 'kill', side_effect=send), \
                patch.object(environment.time, 'sleep', side_effect=advance), \
                patch.object(environment.time, 'monotonic', side_effect=lambda: ticks[0]):
            environment._stop_native_processes([{'pid': 10, 'stamp': 'air'}], grace=1)
        self.assertEqual(signals, [(10, signal.SIGINT), (10, signal.SIGKILL), (30, signal.SIGKILL)])
        self.assertEqual(set(table), {20, 99})

    def test_supervisor_starts_air_and_vite_directly_and_waits_for_their_cleanup(self):
        state, table, _ = self.native_fixture()
        processes = [SimpleNamespace(pid=child['pid'], poll=lambda: None, wait=Mock()) for child in state['children']]
        handlers, ticks = {}, [0]

        def advance(_):
            ticks[0] += 1
            if ticks[0] == 2:
                table.pop(201)
                table[203] = (101, 'reloaded-api')
            if ticks[0] == 4:
                handlers[signal.SIGTERM]()

        with patch.object(environment, 'LOGS', Path(self.directory.name)), \
                patch.object(environment.os, 'getpid', return_value=100), \
                patch.object(environment, 'stamp', side_effect=lambda pid: table[pid][1]), \
                patch.object(environment, '_processes', return_value=table), \
                patch.object(environment, '_listeners', return_value=set()), \
                patch.object(environment, 'native_listeners', side_effect=lambda *_: {'api': 201 if 201 in table else 203, 'web': 102}) as listeners, \
                patch.object(environment, '_healthy', return_value=True) as healthy, \
                patch.object(environment, 'run') as run, \
                patch.object(environment.shutil, 'which', side_effect=lambda name: {'air': '/air', 'node': '/node'}[name]), \
                patch.dict(environment.os.environ, {'AIR': '/air'}), \
                patch.object(environment.subprocess, 'Popen', side_effect=processes) as start, \
                patch.object(environment.signal, 'signal', side_effect=lambda sig, handler: handlers.update({sig: handler})), \
                patch.object(environment.time, 'sleep', side_effect=advance), \
                patch.object(environment.time, 'monotonic', side_effect=lambda: ticks[0] / 2), \
                patch.object(environment, '_stop_native_processes') as cleanup:
            environment.serve()
        self.assertEqual([call.args[0][0] for call in start.call_args_list], ['/air', '/node'])
        self.assertTrue(str(start.call_args_list[1].args[0][1]).endswith('/vite/bin/vite.js'))
        self.assertNotIn('SOLUTION', start.call_args_list[0].kwargs['env'])
        self.assertEqual(start.call_args_list[1].kwargs['env']['PLATFORM_HOST'], 'http://127.0.0.1:18495')
        run.assert_not_called()
        self.assertEqual(cleanup.call_args.args[0], state['children'])
        self.assertIn(203, cleanup.call_args.args[1])
        # Stable ready listeners do not trigger another lsof/HTTP probe each
        # half-second; an actual Air host replacement triggers one immediately.
        self.assertEqual(listeners.call_count, 2)
        self.assertEqual(healthy.call_count, 2)
        for process in processes:
            process.wait.assert_called_once()
        self.assertEqual(environment.read_state(), {'mode': 'native', 'status': 'stopped'})

    def test_running_image_must_match_the_binary_inode_not_only_its_path(self):
        binary = Path(self.directory.name) / 'main-server'
        binary.write_bytes(b'new build')
        inode = binary.stat().st_ino
        for image, expected in [(inode, True), (inode + 1, False)]:
            with self.subTest(image=image), patch.object(environment.subprocess, 'run', return_value=SimpleNamespace(stdout=f'p201\nftxt\ni{image}\nn{binary.resolve()}\n')):
                self.assertEqual(environment.running_binary(201, binary), expected)


if __name__ == '__main__':
    unittest.main()
