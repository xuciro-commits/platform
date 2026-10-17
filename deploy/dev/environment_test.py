import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
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

    def test_native_selection_survives_a_dead_supervisor_and_stop(self):
        environment.write_state({'mode': 'native', 'pid': 123, 'stamp': 'previous process'})
        with patch.object(environment, 'alive', return_value=False):
            environment.stop()
            self.assertEqual(environment.selected_mode(), 'native')
        self.assertNotIn('pid', environment.read_state())

    def test_a_running_legacy_native_environment_keeps_its_selection(self):
        environment.write_state({'pid': 123, 'stamp': 'legacy process'})
        with patch.object(environment, 'alive', return_value=True):
            self.assertEqual(environment.selected_mode(), 'native')

    def test_docker_preparation_remembers_selection_before_starting_dependencies(self):
        environment.write_state({'mode': 'native', 'status': 'stopped'})
        commands = []

        def run(command):
            self.assertEqual(environment.selected_mode(), 'docker')
            commands.append(command)

        with patch.object(environment, 'alive', return_value=False), patch.object(environment, 'run', side_effect=run):
            environment.prepare_docker()
        self.assertTrue(any('rustfs' in command and 'up' in command for command in commands))
        self.assertFalse(any('down' in command or '--volumes' in command for command in commands))

    def test_local_waits_for_live_supervisor_readiness_without_a_startup_deadline(self):
        ticks = [0]

        def advance(_):
            ticks[0] += 1
            if ticks[0] == 4:
                environment.write_state({'pid': 123, 'mode': 'native', 'status': 'ready'})

        with patch.object(environment, 'LOGS', Path(self.directory.name)), \
                patch.object(environment, 'alive', return_value=False), patch.object(environment, 'stop'), \
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


if __name__ == '__main__':
    unittest.main()
