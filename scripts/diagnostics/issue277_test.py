"""Adversarial tests for diagnostic acceptance, without external dependencies."""
import copy
import json
from pathlib import Path
import tempfile
import unittest

import issue277


class EvidenceTests(unittest.TestCase):
    def records(self, version='main-17092b8', native_os='linux'):
        rows = []
        scenarios = issue277.SCENARIOS + (('case-alias',) if native_os == 'windows' else ())
        for mode in issue277.MODES:
            for move in (False, True):
                for scenario in scenarios:
                    alias = scenario in ('hardlink-video', 'case-alias') or scenario.startswith('resident')
                    row = dict(subject_version=version, subject_revision=issue277.REVISIONS[version],
                               mode=mode, move=move, scenario=scenario, goos=native_os, goarch='amd64',
                               fenced=version != 'v1.5.2', force_update=False, overwrite_existing_media=False,
                               protected_bytes_preserved=True, protected_identities_preserved=True,
                               source_target_same_object_before=alias, original_source_present=True,
                               original_source_bytes_preserved=True, original_source_identity_preserved=True,
                               error='', failed_step='', files_sha256={'fixture': 'a' * 64})
                    if mode == 'organize' and scenario in ('foreign-video', 'equal-video'):
                        row['error'] = ('organization validation failed' if version == 'v1.5.2' else
                                        'staged publication destination exists and overwrite is disabled')
                        row['failed_step'] = 'organize' if version == 'v1.5.2' else 'artifact_publication'
                    if mode == 'organize' and scenario in ('hardlink-video', 'resident', 'case-alias') and version in ('v1.6.0', 'v1.6.1'):
                        row['error'] = 'staged publication destination exists and overwrite is disabled'
                    rows.append(row)
        return rows

    def validate(self, rows, version='main-17092b8', native_os='linux'):
        issue277.validate_records(rows, version, issue277.REVISIONS[version], native_os)

    def test_positive_exact_linux_and_windows(self):
        for version in issue277.REVISIONS:
            for native_os in ('linux', 'windows'):
                self.validate(self.records(version, native_os), version, native_os)

    def test_historical_unsafe_outcomes_are_observations(self):
        rows = self.records('v1.6.1')
        row = next(r for r in rows if r['mode'] == 'in-place-norenamefolder' and r['scenario'] == 'foreign-video')
        row['protected_bytes_preserved'] = False
        row['protected_identities_preserved'] = False
        self.validate(rows, 'v1.6.1')

    def test_successful_move_may_retire_source(self):
        rows = self.records()
        row = next(r for r in rows if r['mode'] == 'organize' and r['move'] and r['scenario'] == 'fresh')
        row.update(original_source_present=False, original_source_bytes_preserved=False, original_source_identity_preserved=False)
        self.validate(rows)

    def test_native_case_alias_requires_identity_proof(self):
        rows = self.records(native_os='windows')
        next(r for r in rows if r['scenario'] == 'case-alias')['source_target_same_object_before'] = False
        with self.assertRaises(RuntimeError):
            self.validate(rows, native_os='windows')

    def test_adversarial_records_rejected(self):
        mutations = {
            'wrong-case': lambda rows: rows[0].update(scenario='invented'),
            'missing-case': lambda rows: rows.pop(),
            'duplicate-case': lambda rows: rows.__setitem__(0, copy.deepcopy(rows[1])),
            'wrong-version': lambda rows: rows[0].update(subject_version='v1.6.1'),
            'wrong-revision': lambda rows: rows[0].update(subject_revision='0' * 40),
            'wrong-os': lambda rows: rows[0].update(goos='windows'),
            'false-protected-bytes': lambda rows: rows[0].update(protected_bytes_preserved=False),
            'false-protected-identity': lambda rows: rows[0].update(protected_identities_preserved=False),
            'false-failed-source-bytes': lambda rows: rows[1].update(original_source_bytes_preserved=False),
            'false-failed-source-identity': lambda rows: rows[1].update(original_source_identity_preserved=False),
            'false-alias': lambda rows: rows[3].update(source_target_same_object_before=False),
            'false-distinct': lambda rows: rows[1].update(source_target_same_object_before=True),
            'silent-duplicate-success': lambda rows: rows[1].update(error=''),
            'alias-failure': lambda rows: rows[3].update(error='unexpected failure'),
            'wrong-permission': lambda rows: rows[0].update(force_update=True),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                rows = self.records()
                mutate(rows)
                with self.assertRaises(RuntimeError):
                    self.validate(rows)
        with self.assertRaises(RuntimeError):
            issue277.validate_records(self.records(), 'v1.6.1', issue277.REVISIONS['main-17092b8'], 'linux')

    def events(self):
        package = 'github.com/javinizer/javinizer-go/internal/workflow'
        events = []
        for row in self.records():
            test = f"TestIssue277NativeDiagnostic/{row['mode']}/move={str(row['move']).lower()}/{row['scenario']}"
            # Deliberately split the JSON like test2json does for long lines.
            output = 'ISSUE277_RESULT ' + json.dumps(row) + '\n'
            events += [dict(Action='output', Package=package, Test=test, Output=part) for part in (output[:100], output[100:])]
            events.append(dict(Action='pass', Package=package, Test=test))
        events += [dict(Action='pass', Package=package, Test=name) for name in ('TestIssue277NativeDiagnostic', 'TestIssue277NativeDiagnosticIdentitySnapshot')]
        events.append(dict(Action='pass', Package=package))
        return events

    def collect(self, events):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / 'go-test.jsonl'
            log.write_text('\n'.join(json.dumps(e) for e in events))
            issue277.collect(log, Path(directory) / 'out', 'main-17092b8', issue277.REVISIONS['main-17092b8'], 'linux')

    def test_split_stream_accepted(self):
        self.collect(self.events())

    def test_event_stream_corruption_rejected(self):
        for action in ('fail', 'skip'):
            with self.subTest(action=action), self.assertRaises(RuntimeError):
                self.collect(self.events() + [dict(Action=action)])
        for name in ('', 'TestIssue277NativeDiagnostic', 'TestIssue277NativeDiagnosticIdentitySnapshot', 'TestIssue277NativeDiagnostic/organize/move=false/fresh'):
            with self.subTest(missing_pass=name), self.assertRaises(RuntimeError):
                self.collect([e for e in self.events() if not (e.get('Action') == 'pass' and e.get('Test', '') == name)])
        with self.assertRaises(RuntimeError):
            self.collect([dict(e, Package='wrong/package') if e.get('Action') == 'pass' else e for e in self.events()])


if __name__ == '__main__':
    unittest.main()
