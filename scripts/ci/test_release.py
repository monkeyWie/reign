import contextlib
import io
import os
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import release


class ReleaseContractTests(unittest.TestCase):
    def test_metadata_and_tag_validation(self):
        with patch.dict(os.environ, {'GITHUB_REF': 'refs/tags/v1.2.3-rc.1', 'GITHUB_RUN_NUMBER': '21', 'GITHUB_OUTPUT': ''}):
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                release.metadata(None)
            self.assertIn('version=1.2.3-rc.1\nbuild_number=21\n', out.getvalue())
        for ref in ('refs/tags/vnext', 'refs/tags/v1.2', 'refs/tags/1.2.3', 'refs/tags/v1.2.3/extra'):
            with patch.dict(os.environ, {'GITHUB_REF': ref, 'GITHUB_OUTPUT': ''}), self.assertRaises(SystemExit):
                release.metadata(None)

    def test_publication_rejects_missing_extra_and_corrupted_assets(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            v = '1.2.3'
            names = [f'reign-{v}-{t.replace("_", "-")}' + ('.zip' if t.startswith('windows_') else '.tar.gz') for t in release.DESKTOP]
            names += [f'reign-web-worker-{v}.tar.gz', f'reign-{v}-ios-unsigned.ipa', f'reign-{v}-android-arm64-debug.apk']
            names += [f'reign-probe-{v}-linux-{a}.tar.gz' for a in ('amd64', 'arm64')]
            args = SimpleNamespace(version=v, output=tmp)
            for name in names:
                path = output / name
                path.write_bytes(b'fixture artifact')
                release.checksum(path)
            release.verify(args)
            self.assertEqual(len((output / 'SHA256SUMS').read_text().splitlines()), 11)
            path.write_bytes(b'corrupted artifact')
            with self.assertRaises(SystemExit):
                release.verify(args)
            release.checksum(path)
            extra = output / 'unexpected-secret.pem'
            extra.write_text('must never publish this file')
            with self.assertRaises(SystemExit):
                release.verify(args)
            extra.unlink()
            path.unlink()
            with self.assertRaises(SystemExit):
                release.verify(args)

    def test_client_packaging_preserves_executable_and_symlink(self):
        import tarfile
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            bundle = root / 'build/Reign-1.2.3-macos-arm64'
            # NSGo uses an underscore in the target directory name.
            bundle = bundle.with_name('Reign-1.2.3-macos_arm64')
            bundle.mkdir(parents=True)
            executable = bundle / 'Reign'
            executable.write_text('test fixture')
            executable.chmod(0o755)
            (bundle / 'link').symlink_to('Reign')
            release.collect(SimpleNamespace(version='1.2.3', source=str(bundle.parent), output=str(root / 'out'), targets='macos_arm64'))
            archive = root / 'out/reign-1.2.3-macos-arm64.tar.gz'
            with tarfile.open(archive) as tar:
                self.assertEqual(tar.getmember(bundle.name + '/Reign').mode & 0o777, 0o755)
                self.assertTrue(tar.getmember(bundle.name + '/link').issym())


if __name__ == '__main__':
    unittest.main()
