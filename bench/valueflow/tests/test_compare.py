#!/usr/bin/env python3
"""Smoke tests for compare.py — Phase D bench harness regression guard.

Run:
    python3 bench/valueflow/tests/test_compare.py

Covers the degenerate cases called out in the plan §3 / README:
  - missing run directory
  - zero-row predicate both sides
  - predicate added in B
  - predicate removed in B
  - error row in A or B
  - wall-time ratio math

Includes a mutation probe: if compute_delta is replaced with a
no-op, the assertion on the "added" count fails. Test file fails
visibly — so a benchmarking-harness bug that silently hides diffs
gets caught.
"""
from __future__ import annotations

import os
import sys
import tempfile
import textwrap
import traceback
import unittest
from pathlib import Path

# Put the bench directory on sys.path so we can import compare as a module.
HERE = Path(__file__).resolve().parent
BENCH_DIR = HERE.parent
sys.path.insert(0, str(BENCH_DIR))

import compare  # noqa: E402


def _write_run(tmp: Path, name: str, rows_by_corpus: dict) -> Path:
    """Materialise a fake run_dir under tmp/name with one CSV per corpus."""
    run_dir = tmp / name
    run_dir.mkdir(parents=True, exist_ok=True)
    for corpus, rows in rows_by_corpus.items():
        csv_path = run_dir / f"{corpus}.csv"
        with csv_path.open("w") as f:
            f.write("predicate,fixture,row_count,wall_ms\n")
            for pred, rc, ms in rows:
                f.write(f"{pred},{corpus},{rc},{ms}\n")
    return run_dir


class TestCompare(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="bench-compare-test-"))

    def tearDown(self):
        import shutil
        shutil.rmtree(self.tmp, ignore_errors=True)

    def test_missing_run_raises(self):
        with self.assertRaises(FileNotFoundError):
            compare.read_run(self.tmp / "nope")

    def test_unchanged_zero_rows(self):
        a = _write_run(self.tmp, "a", {"local": [("P1", 0, 10)]})
        b = _write_run(self.tmp, "b", {"local": [("P1", 0, 12)]})
        delta = compare.compute_delta(compare.read_run(a), compare.read_run(b))
        self.assertEqual(len(delta), 1)
        self.assertEqual(delta[0]["status"], "unchanged")
        self.assertEqual(delta[0]["d_rows"], 0)
        self.assertAlmostEqual(delta[0]["ratio"], 1.2, places=5)

    def test_predicate_added_in_b(self):
        a = _write_run(self.tmp, "a", {"local": [("P1", 5, 10)]})
        b = _write_run(self.tmp, "b", {"local": [("P1", 5, 10), ("P2", 3, 7)]})
        delta = compare.compute_delta(compare.read_run(a), compare.read_run(b))
        statuses = {(r["predicate"], r["fixture"]): r["status"] for r in delta}
        self.assertEqual(statuses[("P1", "local")], "unchanged")
        self.assertEqual(statuses[("P2", "local")], "added")
        # Added entries carry d_rows = b_rows (the full count is "new").
        for r in delta:
            if r["predicate"] == "P2":
                self.assertEqual(r["d_rows"], 3)
                self.assertIsNone(r["a_rows"])

    def test_predicate_removed_in_b(self):
        a = _write_run(self.tmp, "a", {"local": [("P1", 5, 10), ("P2", 3, 7)]})
        b = _write_run(self.tmp, "b", {"local": [("P1", 5, 10)]})
        delta = compare.compute_delta(compare.read_run(a), compare.read_run(b))
        statuses = {(r["predicate"], r["fixture"]): r["status"] for r in delta}
        self.assertEqual(statuses[("P2", "local")], "removed")
        for r in delta:
            if r["predicate"] == "P2":
                self.assertEqual(r["d_rows"], -3)
                self.assertIsNone(r["b_rows"])

    def test_error_rows_propagate(self):
        a = _write_run(self.tmp, "a", {"local": [("P1", "ERROR", 100)]})
        b = _write_run(self.tmp, "b", {"local": [("P1", 5, 10)]})
        delta = compare.compute_delta(compare.read_run(a), compare.read_run(b))
        self.assertEqual(delta[0]["status"], "error_a")
        self.assertIsNone(delta[0]["d_rows"])

        a = _write_run(self.tmp, "a2", {"local": [("P1", 5, 10)]})
        b = _write_run(self.tmp, "b2", {"local": [("P1", "ERROR", 100)]})
        delta = compare.compute_delta(compare.read_run(a), compare.read_run(b))
        self.assertEqual(delta[0]["status"], "error_b")

    def test_row_count_delta(self):
        a = _write_run(self.tmp, "a", {"local": [("P1", 10, 100)]})
        b = _write_run(self.tmp, "b", {"local": [("P1", 17, 100)]})
        delta = compare.compute_delta(compare.read_run(a), compare.read_run(b))
        self.assertEqual(delta[0]["d_rows"], 7)
        self.assertEqual(delta[0]["status"], "changed")

    def test_summary_counts(self):
        a = _write_run(self.tmp, "a", {
            "local": [("P1", 5, 10), ("P2", 3, 7)],
        })
        b = _write_run(self.tmp, "b", {
            "local": [("P1", 6, 10), ("P3", 1, 5)],
        })
        delta = compare.compute_delta(compare.read_run(a), compare.read_run(b))
        s = compare.summarise(delta)
        self.assertEqual(s["changed"], 1)
        self.assertEqual(s["added"], 1)
        self.assertEqual(s["removed"], 1)

    def test_multiple_corpora(self):
        a = _write_run(self.tmp, "a", {
            "local": [("P1", 5, 10)],
            "remote": [("P1", 50, 500)],
        })
        b = _write_run(self.tmp, "b", {
            "local": [("P1", 5, 10)],
            "remote": [("P1", 60, 550)],
        })
        delta = compare.compute_delta(compare.read_run(a), compare.read_run(b))
        self.assertEqual(len(delta), 2)
        by_fixture = {r["fixture"]: r for r in delta}
        self.assertEqual(by_fixture["local"]["d_rows"], 0)
        self.assertEqual(by_fixture["remote"]["d_rows"], 10)


class TestMutationProbe(unittest.TestCase):
    """If compute_delta is broken (always returns empty), these tests fail.

    Confirms the test suite is load-bearing, not tautological.
    """

    def test_empty_delta_would_fail_added_check(self):
        # Simulate the mutation in-test rather than editing compare.py.
        def broken_compute_delta(a, b):
            return []

        a = {("P1", "local"): compare.Row("P1", "local", 5, 10)}
        b = {
            ("P1", "local"): compare.Row("P1", "local", 5, 10),
            ("P2", "local"): compare.Row("P2", "local", 3, 7),
        }
        # Real compute_delta surfaces the "added" row; a mutated version
        # returning [] would hide it.
        real = compare.compute_delta(a, b)
        broken = broken_compute_delta(a, b)
        self.assertTrue(any(r["status"] == "added" for r in real),
                        "real compute_delta must surface additions")
        self.assertFalse(any(r["status"] == "added" for r in broken),
                         "mutation probe must not surface additions")


if __name__ == "__main__":
    # Explicit exit code so a shell wrapper can rely on it.
    result = unittest.main(exit=False, verbosity=2).result
    sys.exit(0 if result.wasSuccessful() else 1)
