"""Provenance validation for the owned Windows console experiment.

The runner records the base tip from the triggering event, while the merge
commit is rebuilt whenever the base branch advances. These tests pin the
resulting contract: the recorded head must be the merge's second parent and the
recorded base must be an ancestor of its first parent.
"""
import importlib.util
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parent))

SPEC = importlib.util.spec_from_file_location(
    'owned_console_experiment',
    Path(__file__).with_name('run_owned_windows_console_experiment.py'))
m = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(m)

BASE = 'a' * 40
NEWER_BASE = 'b' * 40
HEAD = 'c' * 40
CHECKOUT = 'd' * 40


def info(parents, base=BASE):
    return {'checkout': CHECKOUT, 'tree': 'e' * 40, 'parents': parents, 'event': 'pull_request', 'head': HEAD, 'base': base}


class ProvenanceTests(unittest.TestCase):
    def validate(self, parents, is_ancestor=False, base=BASE):
        calls = []

        def probe(ancestor, descendant):
            calls.append((ancestor, descendant))
            return is_ancestor

        m.validate_provenance(info(parents, base), CHECKOUT, 'pull_request', HEAD, base, probe)
        return calls

    def test_exact_base_parent_accepted(self):
        calls = self.validate([BASE, HEAD])
        self.assertEqual(calls, [], 'an exact base parent needs no ancestry probe')

    def test_advanced_base_descendant_accepted(self):
        calls = self.validate([NEWER_BASE, HEAD], is_ancestor=True)
        self.assertEqual(calls, [(BASE, NEWER_BASE)], 'the probe compares the declared base with the merge parent')

    def test_unrelated_base_rejected(self):
        with self.assertRaisesRegex(ValueError, 'must descend from declared base'):
            self.validate([NEWER_BASE, HEAD], is_ancestor=False)

    def test_head_must_be_second_parent(self):
        with self.assertRaisesRegex(ValueError, 'declared head as second parent'):
            self.validate([HEAD, BASE])

    def test_single_parent_merge_rejected(self):
        with self.assertRaisesRegex(ValueError, 'declared head as second parent'):
            self.validate([BASE])

    def test_malformed_parent_rejected(self):
        with self.assertRaisesRegex(ValueError, 'malformed raw commit parents'):
            self.validate(['not-a-sha', HEAD])

    def test_push_event_unchanged(self):
        push = {'checkout': HEAD, 'tree': 'e' * 40, 'parents': [], 'event': 'push', 'head': HEAD, 'base': ''}
        m.validate_provenance(push, HEAD, 'push', HEAD, '', lambda *_: False)
        with self.assertRaisesRegex(ValueError, 'only bound push or pull_request contexts are admitted'):
            m.validate_provenance(dict(push, base=BASE), HEAD, 'push', HEAD, BASE, lambda *_: False)


if __name__ == '__main__':
    unittest.main()
