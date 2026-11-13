import pathlib
import subprocess
import sys
import tempfile
import unittest


class NativeBrowserCoverageProofTests(unittest.TestCase):
    def verify(self, omit_test=None, skip_test=None, miss_line=None):
        tests = [
            "TestNativeBrowserScrapeURLCookieAndRenderingContract",
            "TestNativeBrowserCancellationAfterNavigation",
            "TestNativeBrowserCancellationAfterNavigation/fetch",
            "TestNativeBrowserCancellationAfterNavigation/scrape_url",
        ]
        lines = {"browser.go": [162, 165, 166, 181, 182, 183, 184, 187, 188, 189, 192], "search.go": [456]}
        with tempfile.TemporaryDirectory() as root:
            root = pathlib.Path(root)
            log = root / "test.log"
            log.write_text("\n".join(f"--- PASS: {test} (0.01s)" for test in tests if test != omit_test)
                           + (f"\n--- SKIP: {skip_test} (0.00s)" if skip_test else ""))
            profile = root / "coverage.out"
            rows = ["mode: atomic"]
            for filename, numbers in lines.items():
                for number in numbers:
                    count = 0 if (filename, number) == miss_line else 1
                    rows.append(f"github.com/javinizer/javinizer-go/internal/scraper/dmm/{filename}:{number}.1,{number}.2 1 {count}")
            profile.write_text("\n".join(rows) + "\n")
            return subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name("verify_dmm_browser_coverage.py")),
                                   str(profile), str(log)], text=True, capture_output=True, timeout=10)

    def test_complete_rendering_and_cancellation_proof_passes(self):
        result = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_every_native_contract_must_explicitly_pass(self):
        for test in ["TestNativeBrowserScrapeURLCookieAndRenderingContract",
                     "TestNativeBrowserCancellationAfterNavigation",
                     "TestNativeBrowserCancellationAfterNavigation/fetch",
                     "TestNativeBrowserCancellationAfterNavigation/scrape_url"]:
            with self.subTest(test=test):
                self.assertNotEqual(self.verify(omit_test=test).returncode, 0)
                self.assertNotEqual(self.verify(skip_test=test).returncode, 0)

    def test_cancellation_error_paths_must_be_covered(self):
        for missing in [("browser.go", 162), ("search.go", 456)]:
            with self.subTest(missing=missing):
                result = self.verify(miss_line=missing)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("native browser executable targets not hit", result.stderr)


if __name__ == "__main__":
    unittest.main()
