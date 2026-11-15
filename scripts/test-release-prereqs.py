#!/usr/bin/env python3
"""Offline tests: no real download, credential, endpoint or Terraform apply."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ReleasePrereqsTests(unittest.TestCase):
    def health(self, statuses='completed', health='[]', second=None):
        with tempfile.TemporaryDirectory() as d:
            mock = Path(d) / 'curl'
            mock.write_text('''#!/bin/bash
last="${!#}"
case "$last" in
  */command) echo '{"id":1}' ;;
  */command/1) printf '{"status":"%s"}' "$STATUS" ;;
  */health)
    if [[ -f "$COUNT" ]]; then printf '%s' "$SECOND"; else touch "$COUNT"; printf '%s' "$HEALTH"; fi ;;
  *) exit 1 ;;
esac
''')
            mock.chmod(0o755)
            sleeper = Path(d) / 'sleep'
            sleeper.write_text('#!/bin/bash\nexit 0\n')
            sleeper.chmod(0o755)
            env = dict(os.environ, PATH=d + ':' + os.environ['PATH'], STATUS=statuses, HEALTH=health,
                       SECOND=second if second is not None else health, COUNT=d + '/count')
            return subprocess.run(['bash', '-c', 'source "$1"; wait_for_replay_health http://127.0.0.1:12345 SYNTHETIC_KEY',
                                   '_', str(ROOT / 'scripts/lib/replay-health.sh')], env=env, text=True, capture_output=True)

    def test_completed_warning_baseline_is_not_required_to_be_empty(self):
        r = self.health(health='[{"type":"Warning","message":"SYNTHETIC_PRIVATE_MESSAGE"}]')
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn('SYNTHETIC_PRIVATE', r.stdout + r.stderr)
        self.assertNotIn('SYNTHETIC_KEY', r.stdout + r.stderr)

    def test_changed_health_summary_fails(self):
        self.assertNotEqual(self.health(health='[]', second='[{"type":"warning"}]').returncode, 0)

    def test_failed_or_timed_out_commands_fail(self):
        for status in ['failed', 'aborted', 'queued', 'started', 'unknown']:
            with self.subTest(status=status):
                self.assertNotEqual(self.health(statuses=status).returncode, 0)

    def test_invalid_health_payload_fails(self):
        self.assertNotEqual(self.health(health='{}').returncode, 0)

    def test_notice_matches_warning_count(self):
        self.assertEqual(self.health(health='[{"type":"notice"}]', second='[{"type":"WARNING"}]').returncode, 0)

    def test_no_non_loopback_endpoint(self):
        r = subprocess.run(['bash', '-c', 'source "$1"; wait_for_replay_health https://example.invalid SYNTHETIC_KEY',
                            '_', str(ROOT / 'scripts/lib/replay-health.sh')], capture_output=True)
        self.assertNotEqual(r.returncode, 0)

    def test_cli_precedes_both_first_tag_lookups(self):
        workflow = (ROOT / '.github/workflows/release.yml').read_text()
        for job in ['consumer-replay-gate', 'goreleaser']:
            body = workflow.split('  ' + job + ':', 1)[1].split('\n  goreleaser:', 1)[0]
            self.assertLess(body.index('name: Checkout release controls'), body.index('name: Ensure GitHub CLI'))
            self.assertLess(body.index('name: Ensure GitHub CLI'), body.index('gh api'))
            self.assertIn('ref: ${{ github.sha }}', body)
        script = (ROOT / '.github/actions/ensure-gh/ensure-gh.sh').read_text()
        self.assertIn('sha256sum -c -', script)
        self.assertIn('version="2.103.3"', script)
        self.assertNotIn('HOME', script)

    def test_gh_cache_works_without_home(self):
        with tempfile.TemporaryDirectory() as d:
            binary = Path(d) / 'runner-tools/gh/2.103.3/arm64/bin/gh'
            binary.parent.mkdir(parents=True)
            binary.write_text('#!/bin/bash\necho gh-version\n')
            binary.chmod(0o755)
            # Control architecture, not the actual host or any installed binary.
            mock = Path(d) / 'uname'
            mock.write_text('#!/bin/bash\necho arm64\n')
            mock.chmod(0o755)
            env = {'PATH': d + ':' + os.environ['PATH'], 'RUNNER_TEMP': d, 'GITHUB_PATH': d + '/path'}
            r = subprocess.run(['bash', str(ROOT / '.github/actions/ensure-gh/ensure-gh.sh')],
                               env=env, text=True, capture_output=True)
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertEqual(Path(d + '/path').read_text().strip(), str(binary.parent))

    def test_drift_gate_remains_strict_and_before_upgrade_apply(self):
        replay = (ROOT / 'scripts/test-consumer-replay.sh').read_text()
        self.assertIn('if [[ ${rc} -ne 0 ]]', replay)
        self.assertLess(replay.index('wait_for_replay_health "${base_url}"'), replay.index('tf apply fresh-apply'))
        self.assertLess(replay.index('assert_clean_plan "after provider upgrade'), replay.index('tf apply upgrade-apply'))
        self.assertNotIn('-refresh-only', replay)
        self.assertIn('source "$(dirname "${BASH_SOURCE[0]}")/lib/replay-health.sh"', replay)

if __name__ == '__main__':
    unittest.main()
