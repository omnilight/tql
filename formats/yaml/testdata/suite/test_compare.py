"""Ensure corpus failures cannot be hidden by the adapter protocol."""

from decimal import Decimal
from pathlib import Path
import tempfile
import unittest

import compare


class CorpusComparison(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="tql-yaml-suite-test-")
        self.addCleanup(self.temporary.cleanup)
        self.folder = Path(self.temporary.name)
        self.events = "+STR\n+DOC\n=VAL :value\n-DOC\n-STR\n"
        (self.folder / "in.yaml").write_text("value\n", encoding="utf-8")
        (self.folder / "test.event").write_text(self.events, encoding="utf-8")

    def answer(self, **changes):
        result = {"ok": True, "partial": False, "source_unchanged": True,
                  "bytes_same": True, "events": self.events,
                  "documents": ["value"], "value_error": ""}
        result.update(changes)
        return result

    def test_events_and_json_are_separate_required_checks(self):
        (self.folder / "in.json").write_text('"value"\n', encoding="utf-8")
        result = compare.compare_answer(self.folder, self.answer())
        self.assertEqual(result["result"], "valid_passed")
        wrong_events = compare.compare_answer(self.folder, self.answer(events="+STR\n-STR\n"))
        self.assertEqual(wrong_events["result"], "valid_mismatch")
        self.assertTrue(wrong_events["json_match"])
        wrong_value = compare.compare_answer(self.folder, self.answer(documents=["other"]))
        self.assertEqual(wrong_value["result"], "valid_mismatch")
        self.assertTrue(wrong_value["events_match"])
        construction_error = compare.compare_answer(self.folder, self.answer(value_error="unknown"))
        self.assertEqual(construction_error["result"], "valid_mismatch")

    def test_invalid_acceptance_and_partial_results_are_failures(self):
        (self.folder / "error").touch()
        self.assertEqual(compare.compare_answer(self.folder, self.answer())["result"],
                         "invalid_accepted")
        rejected = self.answer(ok=False, error="invalid syntax")
        self.assertEqual(compare.compare_answer(self.folder, rejected)["result"],
                         "invalid_rejected")
        rejected["partial"] = True
        self.assertEqual(compare.compare_answer(self.folder, rejected)["result"],
                         "partial_result")
        self.assertEqual(compare.compare_answer(self.folder,
                         self.answer(source_unchanged=False))["result"], "source_changed")

    def test_exact_numbers_are_not_rounded_or_confused_with_booleans(self):
        expected = compare.json_stream("10000000000000000999\nfalse\n")
        self.assertEqual(compare.typed(expected),
                         compare.typed([Decimal("10000000000000000999"), False]))
        self.assertNotEqual(compare.typed(expected),
                            compare.typed([Decimal("10000000000000001000"), False]))
        self.assertNotEqual(compare.typed(False), compare.typed(Decimal(0)))

    def test_empty_or_missing_corpus_is_not_a_success(self):
        (self.folder / "in.yaml").unlink()
        with self.assertRaisesRegex(ValueError, "no in.yaml"):
            compare.load_cases(self.folder)
        with self.assertRaisesRegex(ValueError, "does not exist"):
            compare.load_cases(self.folder / "missing")

    def test_valid_case_requires_event_reference(self):
        (self.folder / "test.event").unlink()
        with self.assertRaises(FileNotFoundError):
            compare.compare_answer(self.folder, self.answer())


if __name__ == "__main__":
    unittest.main()
