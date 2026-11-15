#!/usr/bin/env python3
"""Offline regression coverage for headless self-hosted runner Go setup."""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
ACTION = ROOT / '.github/actions/ensure-go-cache/action.yml'


class GoCacheTests(unittest.TestCase):
    def run_action(self, overrides=None, runner_temp=True):
        with tempfile.TemporaryDirectory(prefix='go cache test ') as directory:
            env_file = Path(directory) / 'env'
            env = {'PATH': os.environ['PATH'], 'GITHUB_ENV': str(env_file)}
            if runner_temp:
                env['RUNNER_TEMP'] = directory
            env.update(overrides or {})
            script = ACTION.read_text().split('      run: |\n', 1)[1]
            script = '\n'.join(line[8:] for line in script.splitlines())
            result = subprocess.run(['bash', '-c', script], env=env, capture_output=True, text=True)
            values = dict(line.split('=', 1) for line in env_file.read_text().splitlines()) if env_file.exists() else {}
            return result, values, directory

    def test_no_home_or_xdg_cache(self):
        result, values, directory = self.run_action()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(values, {'GOCACHE': directory + '/chaptarr-go-build', 'GOMODCACHE': directory + '/chaptarr-go-mod'})

    def test_preserves_explicit_caches(self):
        expected = {'GOCACHE': '/existing/build cache', 'GOMODCACHE': '/existing/module cache'}
        result, values, _ = self.run_action(expected)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(values, expected)

    def test_empty_caches_get_fallbacks(self):
        result, values, directory = self.run_action({'GOCACHE': '', 'GOMODCACHE': ''})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(values['GOCACHE'], directory + '/chaptarr-go-build')
        self.assertEqual(values['GOMODCACHE'], directory + '/chaptarr-go-mod')

    def test_missing_runner_temp_fails(self):
        result, values, _ = self.run_action(runner_temp=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('RUNNER_TEMP is required', result.stderr)
        self.assertEqual(values, {})

    def test_each_go_setup_has_matching_prior_bootstrap(self):
        count = 0
        for path in (ROOT / '.github/workflows').glob('*.yml'):
            # Steps are consistently indented at six spaces in these workflows.
            steps = re.split(r'(?m)^      - ', path.read_text())
            for index, step in enumerate(steps):
                if 'uses: actions/setup-go@' not in step:
                    continue
                count += 1
                self.assertGreater(index, 0)
                prior = steps[index - 1]
                expected = './release-control/.github/actions/ensure-go-cache' if path.name == 'release.yml' else './.github/actions/ensure-go-cache'
                self.assertIn('uses: ' + expected, prior, str(path))
                conditions = lambda block: re.findall(r'(?m)^        if: (.+)$', block)
                self.assertEqual(conditions(prior), conditions(step), str(path))
        self.assertGreaterEqual(count, 14)


if __name__ == '__main__':
    unittest.main()
