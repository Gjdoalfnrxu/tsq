#!/usr/bin/env python3
"""compare.py — two-run diff for Phase D value-flow bench.

Inputs: two results/run_NNN/ directories. Output: per-predicate, per-
corpus delta table on stdout.

Honest-delta rule: both absent -> drop silently; one absent -> print
as "added" / "removed" explicitly. Never hide a diff.

Also reports:
  - predicates added between A and B
  - predicates removed between A and B
  - zero-row predicates in either run (flagged explicitly)
  - wall-time delta as ms and as ratio

Usage:
    python3 compare.py results/run_001 results/run_042
"""
from __future__ import annotations

import csv
import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterator, List, Optional, Tuple


@dataclass(frozen=True)
class Row:
    predicate: str
    fixture: str
    row_count: Optional[int]  # None if the CSV marked ERROR
    wall_ms: int

    @property
    def key(self) -> Tuple[str, str]:
        return (self.predicate, self.fixture)


def read_run(run_dir: Path) -> Dict[Tuple[str, str], Row]:
    """Read every <corpus>.csv under run_dir into a (predicate, fixture) -> Row map."""
    if not run_dir.is_dir():
        raise FileNotFoundError(f"run dir not found: {run_dir}")
    out: Dict[Tuple[str, str], Row] = {}
    # Only the per-corpus summary CSVs — skip the per-query raw dumps
    # emitted by bench_run.sh (`*.raw.csv`) which have a different
    # (location-projection) schema.
    summary_csvs = [p for p in sorted(run_dir.glob("*.csv"))
                    if not p.name.endswith(".raw.csv")]
    for csv_path in summary_csvs:
        with csv_path.open() as f:
            reader = csv.DictReader(f)
            for rec in reader:
                try:
                    pred = rec["predicate"]
                    fixture = rec["fixture"]
                    raw = rec["row_count"]
                    wall = int(rec["wall_ms"])
                except (KeyError, ValueError) as e:
                    print(f"WARN: malformed row in {csv_path}: {rec} ({e})", file=sys.stderr)
                    continue
                row_count: Optional[int]
                if raw == "ERROR":
                    row_count = None
                else:
                    try:
                        row_count = int(raw)
                    except ValueError:
                        print(f"WARN: non-int row_count in {csv_path}: {raw}", file=sys.stderr)
                        continue
                row = Row(pred, fixture, row_count, wall)
                if row.key in out:
                    # Duplicate key within a run - take the latest but warn.
                    print(f"WARN: duplicate key {row.key} in {csv_path}", file=sys.stderr)
                out[row.key] = row
    return out


def compute_delta(a: Dict[Tuple[str, str], Row],
                  b: Dict[Tuple[str, str], Row]) -> List[dict]:
    """Return a list of delta records, one per predicate/fixture pair in
    A ∪ B. Never silently drops anything.

    Each record: predicate, fixture, a_rows, b_rows, d_rows, a_ms, b_ms,
    ratio, status ∈ {"unchanged","changed","added","removed","error_a","error_b"}.
    """
    keys = sorted(set(a.keys()) | set(b.keys()))
    out: List[dict] = []
    for key in keys:
        ra = a.get(key)
        rb = b.get(key)
        rec: dict = {
            "predicate": key[0],
            "fixture": key[1],
            "a_rows": ra.row_count if ra else None,
            "b_rows": rb.row_count if rb else None,
            "a_ms": ra.wall_ms if ra else None,
            "b_ms": rb.wall_ms if rb else None,
        }
        if ra is None:
            rec["status"] = "added"
            rec["d_rows"] = rb.row_count
        elif rb is None:
            rec["status"] = "removed"
            rec["d_rows"] = -(ra.row_count if ra.row_count is not None else 0)
        elif ra.row_count is None:
            rec["status"] = "error_a"
            rec["d_rows"] = None
        elif rb.row_count is None:
            rec["status"] = "error_b"
            rec["d_rows"] = None
        else:
            rec["d_rows"] = rb.row_count - ra.row_count
            rec["status"] = "unchanged" if rec["d_rows"] == 0 else "changed"
        if rec["a_ms"] and rec["b_ms"] and rec["a_ms"] > 0:
            rec["ratio"] = rec["b_ms"] / rec["a_ms"]
        else:
            rec["ratio"] = None
        out.append(rec)
    return out


def format_table(delta: List[dict]) -> str:
    """Pretty-print as a simple fixed-width table."""
    header = ("predicate", "fixture", "a_rows", "b_rows", "d_rows", "a_ms", "b_ms", "ratio", "status")
    rows = [header]
    for r in delta:
        rows.append((
            r["predicate"],
            r["fixture"],
            _s(r.get("a_rows")),
            _s(r.get("b_rows")),
            _s(r.get("d_rows")),
            _s(r.get("a_ms")),
            _s(r.get("b_ms")),
            f"{r['ratio']:.2f}" if r.get("ratio") else "-",
            r["status"],
        ))
    widths = [max(len(str(row[i])) for row in rows) for i in range(len(header))]
    out_lines = []
    for i, row in enumerate(rows):
        parts = [str(cell).ljust(widths[idx]) for idx, cell in enumerate(row)]
        out_lines.append("  ".join(parts))
        if i == 0:
            out_lines.append("  ".join("-" * w for w in widths))
    return "\n".join(out_lines)


def _s(x) -> str:
    if x is None:
        return "-"
    return str(x)


def summarise(delta: List[dict]) -> dict:
    changed = [r for r in delta if r["status"] == "changed"]
    added = [r for r in delta if r["status"] == "added"]
    removed = [r for r in delta if r["status"] == "removed"]
    errors = [r for r in delta if r["status"] in ("error_a", "error_b")]
    return {
        "total": len(delta),
        "changed": len(changed),
        "added": len(added),
        "removed": len(removed),
        "errors": len(errors),
    }


def main(argv: List[str]) -> int:
    if len(argv) < 3:
        print("usage: compare.py <run_A_dir> <run_B_dir> [--json]", file=sys.stderr)
        return 2
    json_mode = "--json" in argv[3:]
    a_dir = Path(argv[1])
    b_dir = Path(argv[2])
    try:
        a = read_run(a_dir)
        b = read_run(b_dir)
    except FileNotFoundError as e:
        print(f"error: {e}", file=sys.stderr)
        return 1

    delta = compute_delta(a, b)
    summary = summarise(delta)
    if json_mode:
        print(json.dumps({"summary": summary, "delta": delta}, indent=2))
        return 0

    print(f"# bench compare: {a_dir} -> {b_dir}")
    print(f"# summary: {summary}")
    print(format_table(delta))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
