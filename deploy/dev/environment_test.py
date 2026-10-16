import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


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


if __name__ == '__main__':
    unittest.main()
