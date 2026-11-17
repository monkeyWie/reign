"""Check the public build entry without invoking SDKs or real applications."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class BuildEntryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='reignhub-build-')
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name).resolve()
        self.repo = self.base / 'checkout with spaces'
        (self.repo / 'clients/nsgo').mkdir(parents=True)
        (self.repo / 'scripts').mkdir()
        for name in ('build.sh', 'go-toolexec.sh'):
            shutil.copy2(ROOT / 'scripts' / name, self.repo / 'scripts' / name)
        self.log = self.base / 'calls.jsonl'
        binaries = self.base / 'bin'
        binaries.mkdir()
        fake = '''
import json, os, pathlib, sys
wrapper = next((flag.split('=', 1)[1] for flag in os.environ.get('GOFLAGS', '').split() if flag.startswith('-toolexec=')), '')
call = {'tool': pathlib.Path(sys.argv[0]).name, 'args': sys.argv[1:], 'cwd': os.getcwd(),
        'version': os.environ.get('REIGNHUB_BUILD_VERSION'), 'number': os.environ.get('REIGNHUB_BUILD_NUMBER'),
        'gowork': os.environ.get('GOWORK'), 'wrapper': wrapper, 'wrapper_exists': pathlib.Path(wrapper).is_file()}
with open(os.environ['BUILD_TEST_LOG'], 'a') as log:
    log.write(json.dumps(call) + '\\n')
if call['tool'] == 'go':
    sys.exit(int(os.environ.get('BUILD_TEST_GO_EXIT', '0')))
'''
        for name in ('go', 'python3'):
            path = binaries / name
            path.write_text('#!' + sys.executable + '\n' + fake)
            path.chmod(0o700)
        self.env = {**os.environ, 'PATH': str(binaries) + os.pathsep + os.environ['PATH'],
                    'BUILD_TEST_LOG': str(self.log), 'GOWORK': 'inherited-workspace', 'GOFLAGS': '-mod=readonly'}

    def run_build(self, args, cwd=None, exit_code=0):
        result = subprocess.run(['sh', str(self.repo / 'scripts/build.sh'), *args],
                                cwd=cwd or self.repo, env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, exit_code, result.stderr)
        calls = [json.loads(line) for line in self.log.read_text().splitlines()]
        for call in calls:
            self.assertEqual(call['cwd'], str(self.repo / 'clients/nsgo'))
            self.assertEqual(call['gowork'], 'off')
            self.assertTrue(call['wrapper_exists'])
            self.assertFalse(Path(call['wrapper']).exists(), 'temporary wrapper must be cleaned up')
        return calls

    @staticmethod
    def output(call):
        index = len(call['args']) - 1 - call['args'][::-1].index('-output')
        return Path(call['args'][index + 1])

    def test_default_build_uses_root_artifacts_and_android_host(self):
        calls = self.run_build([])
        self.assertEqual([call['tool'] for call in calls], ['go', 'python3'])
        self.assertEqual(self.output(calls[0]), self.repo / 'dist')
        self.assertEqual(Path(calls[1]['args'][-1]), self.repo / 'dist')
        self.assertEqual(Path(calls[1]['args'][0]), self.repo / 'scripts/build-android-host.py')

    def test_web_build_from_module_still_defaults_to_root(self):
        calls = self.run_build(['web'], cwd=self.repo / 'clients/nsgo')
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]['args'][:5], ['tool', 'nsgo', 'build', '-target', 'web'])
        self.assertEqual(self.output(calls[0]), self.repo / 'dist')

    def test_android_output_and_version_forwarding(self):
        for flags in (['-output', 'packages with spaces', '-version', '1.2.3-rc.1', '-build-number', '42'],
                      ['-output=packages with spaces', '-version=1.2.3-rc.1', '-build-number=42']):
            with self.subTest(flags=flags):
                self.log.unlink(missing_ok=True)
                calls = self.run_build(['android_arm64', *flags], cwd=self.base)
                self.assertEqual([call['tool'] for call in calls], ['go', 'python3'])
                self.assertEqual(self.output(calls[0]), self.base / 'packages with spaces')
                self.assertEqual(Path(calls[1]['args'][-1]), self.base / 'packages with spaces')
                for call in calls:
                    self.assertEqual(call['version'], '1.2.3-rc.1')
                    self.assertEqual(call['number'], '42')

    def test_failed_build_does_not_repackage_android(self):
        self.env['BUILD_TEST_GO_EXIT'] = '7'
        calls = self.run_build(['android_arm64'], exit_code=7)
        self.assertEqual([call['tool'] for call in calls], ['go'])

    def test_absolute_output_is_preserved(self):
        destination = self.base / 'absolute output'
        calls = self.run_build(['web', '-output', str(destination)], cwd=self.repo / 'clients/nsgo')
        self.assertEqual(len(calls), 1)
        self.assertEqual(self.output(calls[0]), destination)


if __name__ == '__main__':
    unittest.main()
