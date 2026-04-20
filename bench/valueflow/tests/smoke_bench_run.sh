#!/usr/bin/env bash
# Smoke test for bench_run.sh. Confirms:
#   - script exists, is executable
#   - corpora.yaml parser round-trips
#   - running against a non-existent corpus exits non-zero
#   - --help-ish bad invocation is rejected
#
# Does NOT run the full extraction — that's the MVP's job, not the
# smoke test's. Keep this fast so it can live in CI.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BENCH="${HERE}/.."
SCRIPT="${BENCH}/bench_run.sh"

fail=0
note() { echo "[smoke] $*"; }

[[ -x "${SCRIPT}" ]] || { note "FAIL: ${SCRIPT} not executable"; fail=1; }

# No args → non-zero
if "${SCRIPT}" 2>/dev/null; then
  note "FAIL: bench_run.sh with no args returned 0"
  fail=1
fi

# Unknown corpus → exit non-zero without creating a run dir
tmp="$(mktemp -d)"
if TSQ_BENCH_SCRATCH="${tmp}" "${SCRIPT}" "smoke_$$_nope" "no_such_corpus" 2>/dev/null; then
  note "FAIL: unknown corpus returned 0"
  fail=1
fi

# corpora.yaml parses into at least one line
pyout="$(python3 - "${BENCH}/corpora.yaml" <<'PY'
import re, sys
with open(sys.argv[1]) as f:
    text = f.read()
blocks = re.findall(r'-\s+name:\s*(\S+)\s*\n\s+path:\s*(\S+)', text)
print(len(blocks))
PY
)"
if [[ "${pyout}" -lt 1 ]]; then
  note "FAIL: corpora.yaml parsed to ${pyout} entries"
  fail=1
fi

if [[ "${fail}" -eq 0 ]]; then
  note "OK"
  exit 0
else
  exit 1
fi
