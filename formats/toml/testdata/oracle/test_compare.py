"""Checks that newer TOML syntax never bypasses the independent value comparison."""

from pathlib import Path
import unittest

import compare


class VersionDifferences(unittest.TestCase):
    def test_independent_reference_checks_acceptance_and_values(self):
        root = Path(__file__).resolve().parents[4]
        cases = [
            {"source": "a={b=1,}", "python_ok": False,
             "go": {"ok": True, "value": {"a": {"b": 1}}}},
            {"source": "a=00:32", "python_ok": False,
             "go": {"ok": True, "value": {"a": "00:32"}}},
            {"source": "a={b=1,}", "python_ok": False,
             "go": {"ok": True, "value": {"a": {"b": 2}}}},
            {"source": "a={b=1,,}", "python_ok": False,
             "go": {"ok": True, "value": {"a": {"b": 1}}}},
        ]
        unexpected, confirmed = compare.check_version_differences(root, cases)
        self.assertEqual(confirmed, 2)
        self.assertEqual(unexpected, cases[2:])
        self.assertIn("toml11_reference", unexpected[0])
        self.assertIn("toml11_error", unexpected[1])


if __name__ == "__main__":
    unittest.main()
