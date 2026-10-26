import copy
import unittest
from verify_offline_sync_browser import REQUIRED, verify


def reports():
    specs = [{'title': title, 'file': 'offline-name-sync.spec.ts', 'tests': [{
        'projectName': 'sync-chromium', 'expectedStatus': 'passed', 'status': 'expected',
        'results': [{'status': 'passed', 'retry': 0}]}]} for title in sorted(REQUIRED)]
    report = {'suites': [{'title': 'offline-name-sync.spec.ts', 'specs': specs}], 'errors': []}
    inventory = copy.deepcopy(report)
    for spec in inventory['suites'][0]['specs']:
        spec['tests'][0]['results'] = []
    return inventory, report


class BrowserProofTests(unittest.TestCase):
    def test_complete_actual_passes(self):
        self.assertEqual(verify(*reports()), len(REQUIRED))

    def test_rejects_false_green_outcomes(self):
        for field, value in [('status', 'flaky'), ('expectedStatus', 'skipped')]:
            with self.subTest(field=field):
                inventory, report = reports()
                report['suites'][0]['specs'][0]['tests'][0][field] = value
                with self.assertRaises(ValueError):
                    verify(inventory, report)
        for outcomes in [[], [{'status': 'skipped'}], [{'status': 'failed'}],
                         [{'status': 'passed', 'retry': 1}], [{'status': 'passed'}] * 2]:
            with self.subTest(outcomes=outcomes):
                inventory, report = reports()
                report['suites'][0]['specs'][0]['tests'][0]['results'] = outcomes
                with self.assertRaises(ValueError):
                    verify(inventory, report)

    def test_rejects_missing_extra_duplicate_and_top_level_errors(self):
        for mode in ('missing', 'extra', 'duplicate', 'error', 'empty'):
            with self.subTest(mode=mode):
                inventory, report = reports()
                specs = report['suites'][0]['specs']
                if mode == 'missing':
                    specs.pop()
                elif mode == 'extra':
                    specs.append({**copy.deepcopy(specs[0]), 'title': 'unexpected'})
                elif mode == 'duplicate':
                    specs.append(copy.deepcopy(specs[0]))
                elif mode == 'error':
                    report['errors'] = [{'message': 'browser launch failed'}]
                else:
                    inventory['suites'] = report['suites'] = []
                with self.assertRaises(ValueError):
                    verify(inventory, report)


if __name__ == '__main__':
    unittest.main()
