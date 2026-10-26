#!/usr/bin/env python3
"""Optional, deterministic differential check against Python 3.11+ tomllib.

Production and ordinary Go tests do not require Python. This harness builds its
Go helper in a temporary directory and leaves the repository unchanged. Expected
numeric range-policy differences are counted separately from actual failures.
"""

import argparse
import datetime
import itertools
import json
import math
import os
from pathlib import Path
import random
import subprocess
import tempfile

try:
    import tomllib
except ImportError:
    raise SystemExit("This optional check requires Python >= 3.11 (stdlib tomllib).")

VALUES = ['0',
 '-0',
 '+0',
 '1',
 '-9223372036854775808',
 '9223372036854775807',
 '0x0',
 '0xF_F',
 '0o777',
 '0b0_1',
 '0o0_1',
 '0x00_1',
 '0b000_1',
 '0e00',
 '+0e-001',
 '-0.00',
 '1e-9999',
 '1e9999',
 '1.0',
 '1_2.3_4e+0_1',
 'true',
 'false',
 'inf',
 '+inf',
 '-inf',
 'nan',
 '+nan',
 '-nan',
 "''",
 '""',
 "'a\\b'",
 '"\\U00010000"',
 '"\\u0000"',
 '"\\u007f"',
 '"\\uFEFF"',
 '"""\nabc\n"""',
 "'''\nabc\n'''",
 '"""abc\\\n \nxyz"""',
 '"""abc\\ \n xyz"""',
 '"""a""""',
 '"""a"""""',
 "'''a''''",
 "'''a'''''",
 '1979-05-27',
 '1979-05-27T00:32:00Z',
 '1979-05-27t00:32:00z',
 '1979-05-27 00:32:00-00:00',
 '1979-05-27T00:32:00.123456789999+23:59',
 '00:32:00',
 '00:32:00.999999999999',
 '[1,2,]',
 '[true, "s", 3]',
 '[\n 1 # yes\n, 2\n]',
 '{}',
 '{a.b=1, a.c=2}',
 '{a=[\n1,\n]}',
 '{a="""\nabc\n"""}']

KEYS = ['a',
 'a.b',
 'a.b.c',
 'a . b',
 '""',
 "''",
 '"a.b"',
 '"\\u0061"',
 "'x'",
 'a.""',
 '0.1',
 'a-b',
 '日本語',
 '"日本語"']

FRAGMENTS = ['a.b=1',
 'a.c=2',
 'a.b.c=3',
 '[a]',
 '[a.b]',
 '[a.b.c]',
 '[[a]]',
 '[[a.b]]',
 '[[a.b.c]]',
 'a={b=1}',
 'b=1',
 'a=[]',
 'a=1',
 'a.b={c=1}',
 'b.d=2',
 'b.d.x=2',
 'b.c.d=2',
 'c.x=2',
 '[[b]]',
 '[b]',
 '[b.c]',
 'b.c=2',
 'b={c=2}',
 'c={x=1}',
 '[a.c.d]',
 '[b.c.d]']

ALPHABET = 'abc0129_+-.,=[]{}\'"# \\tnrfuU\n\r\t\x00\x01\x7f'

def documents(mutations, combinations):
    """Keep seed and generation order stable so default results are reproducible."""
    rng = random.Random(541)
    docs = [f"{key} = {value}\n" for key, value in itertools.product(KEYS, VALUES)]
    for _ in range(combinations):
        docs.append("\n".join(rng.choices(FRAGMENTS, k=rng.randint(2, 10))))
    valid = []
    for source in docs:
        try:
            tomllib.loads(source)
            valid.append(source)
        except (ValueError, OverflowError):
            pass
    for _ in range(mutations):
        source = rng.choice(valid)
        for _ in range(rng.randint(1, 4)):
            pos = rng.randrange(len(source) + 1)
            kind = rng.randrange(3)
            if kind == 0:
                source = source[:pos] + rng.choice(ALPHABET) + source[pos:]
            elif kind == 1:
                source = source[:pos] + source[pos + 1:]
            else:
                source = source[:pos] + rng.choice(ALPHABET) + source[pos + 1:]
        docs.append(source)
    return docs, len(valid)


def equivalent(reference, actual):
    """Compare meaning, accounting for TOML temporal types and special floats."""
    if isinstance(reference, dict):
        return (isinstance(actual, dict) and reference.keys() == actual.keys()
                and all(equivalent(value, actual[key])
                        for key, value in reference.items()))
    if isinstance(reference, list):
        return (isinstance(actual, list) and len(reference) == len(actual)
                and all(equivalent(a, b) for a, b in zip(reference, actual)))
    if isinstance(reference, float):
        if math.isnan(reference):
            return actual in ["nan", "+nan", "-nan"]
        if math.isinf(reference):
            return actual in (["inf", "+inf"] if reference > 0 else ["-inf"])
        return isinstance(actual, (int, float)) and actual == reference
    if isinstance(reference, datetime.datetime):
        try:
            return datetime.datetime.fromisoformat(
                actual.replace("t", "T").replace("z", "Z")) == reference
        except (ValueError, TypeError, AttributeError):
            return False
    if isinstance(reference, datetime.date):
        return reference.isoformat() == actual
    if isinstance(reference, datetime.time):
        try:
            return datetime.time.fromisoformat(actual) == reference
        except (ValueError, TypeError):
            return False
    return type(reference) is type(actual) and reference == actual


def contains(value, predicate):
    if isinstance(value, dict):
        return any(contains(item, predicate) for item in value.values())
    if isinstance(value, list):
        return any(contains(item, predicate) for item in value)
    return predicate(value)


def known_range_difference(reference, answer):
    """Python integers are unbounded, and Python converts exponent overflow to inf.

    TQL deliberately rejects integers outside signed 64-bit and float overflow.
    Only count a mismatch as expected when both error text and Python value
    demonstrate this specific policy difference.
    """
    if answer["ok"]:
        return False
    error = answer.get("error", "")
    if "integer out of range" in error:
        return contains(reference, lambda v: type(v) is int
                        and not -(2**63) <= v < 2**63)
    if "float out of range" in error:
        return contains(reference, lambda v: isinstance(v, float) and math.isinf(v))
    return False


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cases", type=int, default=40000,
                        help="number of mutated documents (default: 40000)")
    parser.add_argument("--combinations", type=int, default=15000,
                        help="number of declaration combinations (default: 15000)")
    parser.add_argument("--save-mismatches", type=Path,
                        help="optionally write unexpected cases as JSON")
    args = parser.parse_args()
    if args.cases < 0 or args.combinations < 0:
        parser.error("case counts must be nonnegative")
    docs, valid_count = documents(args.cases, args.combinations)
    root = Path(__file__).resolve().parents[4]
    with tempfile.TemporaryDirectory(prefix="tql-toml-oracle-") as temp:
        binary = Path(temp) / ("oracle.exe" if os.name == "nt" else "oracle")
        subprocess.run(["go", "build", "-o", str(binary),
                        "./formats/toml/testdata/oracle"], cwd=root, check=True)
        result = subprocess.run(
            [str(binary)], input="".join(json.dumps(s) + "\n" for s in docs),
            text=True, capture_output=True, check=True, timeout=60)
    answers = [json.loads(line) for line in result.stdout.splitlines()]
    if len(answers) != len(docs):
        raise SystemExit("Go helper returned an unexpected number of answers")
    expected = 0
    unexpected = []
    for source, answer in zip(docs, answers):
        try:
            reference = tomllib.loads(source)
            reference_ok = True
        except (ValueError, OverflowError):
            reference, reference_ok = None, False
        mismatch = (reference_ok != answer["ok"] or reference_ok
                    and not equivalent(reference, answer.get("value")))
        if not mismatch:
            continue
        if reference_ok and known_range_difference(reference, answer):
            expected += 1
            continue
        unexpected.append({"source": source, "python_ok": reference_ok,
                           "python": repr(reference), "go": answer})
    if args.save_mismatches:
        args.save_mismatches.write_text(
            json.dumps(unexpected, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8")
    print(f"Compared {len(docs):,} documents; {valid_count:,} valid seeds; "
          f"{expected} expected numeric-range differences; "
          f"{len(unexpected)} unexpected differences.")
    for mismatch in unexpected[:20]:
        print(json.dumps(mismatch, ensure_ascii=False))
    raise SystemExit(1 if unexpected else 0)


if __name__ == "__main__":
    main()
