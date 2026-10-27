#!/usr/bin/env python3
"""Compare the public YAML graph API with a local yaml-test-suite data export.

The corpus is external: this command never downloads it or adds dependencies.
Events are required for valid cases; JSON companions are compared when provided.
"""

import argparse
from collections import Counter
from decimal import Decimal
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


PASS_RESULTS = {"valid_passed", "invalid_rejected"}
JSON_DECODER = json.JSONDecoder(parse_int=Decimal, parse_float=Decimal)


def json_stream(text):
    documents = []
    while text.strip():
        text = text.lstrip()
        document, end = JSON_DECODER.raw_decode(text)
        documents.append(document)
        text = text[end:]
    return documents


def typed(value):
    if value is None:
        return ("null",)
    if isinstance(value, bool):
        return ("bool", value)
    if isinstance(value, Decimal):
        return ("number", value)
    if isinstance(value, str):
        return ("string", value)
    if isinstance(value, list):
        return ("array", tuple(typed(item) for item in value))
    if isinstance(value, dict):
        return ("object", tuple(sorted((key, typed(item))
                                        for key, item in value.items())))
    raise ValueError(f"unexpected JSON value type: {type(value).__name__}")


def load_cases(suite):
    if not suite.is_dir():
        raise ValueError(f"suite data directory does not exist: {suite}")
    cases = sorted(suite.rglob("in.yaml"))
    if not cases:
        raise ValueError(f"suite data directory contains no in.yaml files: {suite}")
    return cases


def compare_answer(folder, answer):
    if type(answer.get("ok")) is not bool:
        return {"result": "protocol_error", "error": "missing boolean ok"}
    if answer.get("source_unchanged") is not True:
        return {"result": "source_changed"}
    if not answer["ok"]:
        if answer.get("partial") is not False:
            return {"result": "partial_result"}
        return {"result": "invalid_rejected" if (folder / "error").exists()
                else "valid_rejected", "error": answer.get("error", "")}
    if (folder / "error").exists():
        return {"result": "invalid_accepted"}
    if answer.get("bytes_same") is not True:
        return {"result": "source_changed"}

    expected_events = (folder / "test.event").read_text(encoding="utf-8")
    result = {"events_match": answer.get("events") == expected_events}
    if not result["events_match"]:
        result["expected_events"] = expected_events
    if (folder / "in.json").exists():
        expected_json = (folder / "in.json").read_text(encoding="utf-8")
        expected = json_stream(expected_json)
        result["json_match"] = (not answer.get("value_error")
                                and typed(answer.get("documents")) == typed(expected))
        if not result["json_match"]:
            result["expected_json"] = expected_json
    matched = result["events_match"] and result.get("json_match", True)
    result["result"] = "valid_passed" if matched else "valid_mismatch"
    return result


def run_case(binary, source, suite, timeout):
    row = {"id": str(source.parent.relative_to(suite)),
           "invalid": (source.parent / "error").exists()}
    try:
        command = [str(binary)]
        if (source.parent / "in.json").exists():
            command.append("--json-values")
        process = subprocess.run(command, input=source.read_bytes(),
                                 capture_output=True, timeout=timeout)
        if process.returncode:
            row.update(result="crash", exit=process.returncode,
                       stderr=process.stderr.decode("utf-8", errors="replace"))
            return row
        try:
            answer = json.loads(process.stdout, parse_int=Decimal,
                                parse_float=Decimal)
            if not isinstance(answer, dict):
                raise ValueError("helper response is not an object")
        except (ValueError, UnicodeDecodeError) as error:
            row.update(result="protocol_error", error=str(error))
            return row
        row["actual"] = answer
        row.update(compare_answer(source.parent, answer))
    except subprocess.TimeoutExpired:
        row["result"] = "timeout"
    except (OSError, ValueError) as error:
        row.update(result="reference_error", error=str(error))
    return row


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--suite", type=Path, required=True,
                        help="local yaml-test-suite data export")
    parser.add_argument("--timeout", type=float, default=5,
                        help="per-case process timeout in seconds (default: 5)")
    parser.add_argument("--save-results", type=Path,
                        help="optionally save every case and diagnostic as JSON")
    args = parser.parse_args()
    if args.timeout <= 0:
        parser.error("timeout must be positive")
    suite = args.suite.expanduser().resolve()
    try:
        cases = load_cases(suite)
    except ValueError as error:
        parser.error(str(error))

    root = Path(__file__).resolve().parents[4]
    try:
        with tempfile.TemporaryDirectory(prefix="tql-yaml-suite-") as temporary:
            binary = Path(temporary) / ("adapter.exe" if os.name == "nt" else "adapter")
            subprocess.run(["go", "build", "-o", str(binary),
                            "./formats/yaml/testdata/suite"], cwd=root, check=True)
            rows = [run_case(binary, source, suite, args.timeout)
                    for source in cases]
    except (OSError, subprocess.CalledProcessError) as error:
        print(f"Could not build or run the YAML adapter: {error}", file=sys.stderr)
        return 2

    counts = dict(Counter(row["result"] for row in rows))
    failures = [row for row in rows if row["result"] not in PASS_RESULTS]
    if args.save_results:
        report = {"suite": str(suite), "cases": len(rows), "counts": counts,
                  "timeout_seconds": args.timeout,
                  "number_display": "Decimals are strings in this report only",
                  "rows": rows}
        args.save_results.write_text(json.dumps(report, indent=2, ensure_ascii=False,
                                                default=str) + "\n", encoding="utf-8")
    print(f"Compared {len(rows)} cases: "
          f"{counts.get('valid_passed', 0)} valid passed, "
          f"{counts.get('invalid_rejected', 0)} invalid rejected, "
          f"{len(failures)} unexpected results.")
    for row in failures[:20]:
        details = row.get("error") or row.get("actual", {}).get("error", "")
        print(f"{row['id']}: {row['result']} {details}".rstrip())
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
