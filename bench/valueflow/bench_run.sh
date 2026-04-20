#!/usr/bin/env bash
# bench_run.sh — Phase D value-flow bench driver.
#
# Usage:
#   ./bench_run.sh <run_id> <corpus_name>
#   ./bench_run.sh <run_id> --all
#
# Writes per-corpus CSVs + manifest under results/<run_id>/.
# See README.md for the full contract.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${HERE}/../.." && pwd)"
RESULTS_DIR="${HERE}/results"
SCRATCH="${TSQ_BENCH_SCRATCH:-/tmp/tsq-bench-corpora}"
CORPORA_YAML="${HERE}/corpora.yaml"

# Queries run against every corpus. Each row:
#   <predicate_label>:<query_file_relative_to_repo_root>
# Labels are used as the `predicate` column in the emitted CSV.
QUERIES=(
  "mayResolveToRec:testdata/queries/v2/valueflow/all_mayResolveToRec_located.ql"
  "mayResolveTo_all:testdata/queries/v2/valueflow/all_mayResolveTo.ql"
  "mayResolveTo_dataflow:testdata/queries/v2/valueflow/all_mayResolveTo_dataflow_located.ql"
  "resolvesToFunctionDirect:testdata/queries/v2/valueflow/resolves_to_function_direct.ql"
)

die() { echo "bench_run.sh: $*" >&2; exit 1; }
log() { echo "[bench] $*" >&2; }

if [[ $# -lt 2 ]]; then
  die "usage: $0 <run_id> <corpus_name|--all>"
fi

RUN_ID="$1"
CORPUS_ARG="$2"

[[ "${RUN_ID}" =~ ^[A-Za-z0-9_.-]+$ ]] || die "run_id must match [A-Za-z0-9_.-]+: ${RUN_ID}"

RUN_DIR="${RESULTS_DIR}/${RUN_ID}"
mkdir -p "${RUN_DIR}"

# Read corpora.yaml. Minimal parser — expects the exact shape we write.
# Emits lines of the form: <name>\t<path>
read_corpora() {
  python3 - "${CORPORA_YAML}" <<'PY'
import re, sys
path = sys.argv[1]
with open(path) as f:
    text = f.read()
# Match blocks:
#   - name: X
#     path: Y
blocks = re.findall(r'-\s+name:\s*(\S+)\s*\n\s+path:\s*(\S+)', text)
for name, p in blocks:
    print(f"{name}\t{p}")
PY
}

mapfile -t CORPORA_LINES < <(read_corpora)
[[ ${#CORPORA_LINES[@]} -gt 0 ]] || die "no corpora parsed from ${CORPORA_YAML}"

select_corpora() {
  local want="$1"
  for line in "${CORPORA_LINES[@]}"; do
    local name path
    name="${line%%$'\t'*}"
    path="${line#*$'\t'}"
    if [[ "${want}" == "--all" || "${want}" == "${name}" ]]; then
      echo "${name}"$'\t'"${path}"
    fi
  done
}

mapfile -t SELECTED < <(select_corpora "${CORPUS_ARG}")
[[ ${#SELECTED[@]} -gt 0 ]] || die "no corpus named '${CORPUS_ARG}' in ${CORPORA_YAML}"

# Resolve a corpus path to a local directory. SSH remotes get rsync'd
# to scratch on first use.
resolve_path() {
  local name="$1"
  local path="$2"
  if [[ "${path}" == *:* ]]; then
    local local_dir="${SCRATCH}/${name}"
    if [[ -d "${local_dir}" && "${TSQ_BENCH_REFRESH:-0}" != "1" ]]; then
      log "reusing cached corpus at ${local_dir} (set TSQ_BENCH_REFRESH=1 to force rsync)"
    else
      mkdir -p "${local_dir}"
      log "rsyncing ${path} -> ${local_dir}"
      rsync -a --delete "${path}/" "${local_dir}/" \
        || die "rsync failed for ${name}"
    fi
    echo "${local_dir}"
  else
    if [[ "${path}" != /* ]]; then
      echo "${REPO_ROOT}/${path}"
    else
      echo "${path}"
    fi
  fi
}

# Build tsq once per run.
TSQ_BIN="${RUN_DIR}/tsq"
log "building tsq at ${TSQ_BIN}"
(cd "${REPO_ROOT}" && CGO_ENABLED=1 go build -o "${TSQ_BIN}" ./cmd/tsq) \
  || die "tsq build failed"

GIT_SHA="$(cd "${REPO_ROOT}" && git rev-parse HEAD 2>/dev/null || echo unknown)"
GIT_DESC="$(cd "${REPO_ROOT}" && git describe --always --dirty 2>/dev/null || echo unknown)"
RUN_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

MANIFEST="${RUN_DIR}/manifest.yaml"
{
  echo "run_id: ${RUN_ID}"
  echo "timestamp_utc: ${RUN_TS}"
  echo "git_sha: ${GIT_SHA}"
  echo "git_describe: ${GIT_DESC}"
  echo "corpora:"
} > "${MANIFEST}"

# Run per corpus.
overall_nonzero=0
for line in "${SELECTED[@]}"; do
  name="${line%%$'\t'*}"
  path="${line#*$'\t'}"
  log "=== corpus: ${name} (${path}) ==="

  local_path="$(resolve_path "${name}" "${path}")"
  [[ -d "${local_path}" ]] || { log "WARN: skipping ${name}: ${local_path} not a directory"; continue; }

  csv="${RUN_DIR}/${name}.csv"
  logfile="${RUN_DIR}/${name}.log"
  db="${RUN_DIR}/${name}.db"

  # Extract.
  extract_start=$(date +%s%3N)
  if ! "${TSQ_BIN}" extract -dir "${local_path}" -output "${db}" >> "${logfile}" 2>&1; then
    log "WARN: extract failed for ${name}; see ${logfile}"
    echo "  - name: ${name}" >> "${MANIFEST}"
    echo "    status: extract_failed" >> "${MANIFEST}"
    continue
  fi
  extract_end=$(date +%s%3N)
  extract_ms=$((extract_end - extract_start))
  log "extract ${name}: ${extract_ms}ms"

  # CSV header.
  echo "predicate,fixture,row_count,wall_ms" > "${csv}"

  corpus_nonzero=0
  for q in "${QUERIES[@]}"; do
    label="${q%%:*}"
    qfile="${REPO_ROOT}/${q#*:}"
    if [[ ! -f "${qfile}" ]]; then
      log "WARN: query file missing: ${qfile}"
      continue
    fi
    raw="${RUN_DIR}/${name}.${label}.raw.csv"
    q_start=$(date +%s%3N)
    if "${TSQ_BIN}" query --db "${db}" --format csv "${qfile}" > "${raw}" 2>> "${logfile}"; then
      q_end=$(date +%s%3N)
      wall_ms=$((q_end - q_start))
      # Row count: total lines minus header.
      total=$(wc -l < "${raw}" | tr -d ' ')
      if [[ "${total}" -gt 0 ]]; then
        rows=$((total - 1))
      else
        rows=0
      fi
      if [[ "${rows}" -lt 0 ]]; then rows=0; fi
      echo "${label},${name},${rows},${wall_ms}" >> "${csv}"
      [[ "${rows}" -gt 0 ]] && corpus_nonzero=1
      log "  ${label}: ${rows} rows, ${wall_ms}ms"
    else
      q_end=$(date +%s%3N)
      wall_ms=$((q_end - q_start))
      echo "${label},${name},ERROR,${wall_ms}" >> "${csv}"
      log "  ${label}: query FAILED after ${wall_ms}ms (see ${logfile})"
    fi
  done

  # Sort the rows (skip header).
  {
    head -n 1 "${csv}"
    tail -n +2 "${csv}" | LC_ALL=C sort -t, -k1,1 -k2,2
  } > "${csv}.sorted" && mv "${csv}.sorted" "${csv}"

  # Emit a repo-relative path when the resolved location is under
  # REPO_ROOT, so manifests committed from different worktrees /
  # CI checkouts compare cleanly. path_resolved is kept for debug
  # (it tells you the exact bytes that were scanned), but
  # path_repo_relative is the stable one.
  path_repo_relative=""
  case "${local_path}" in
    "${REPO_ROOT}"/*) path_repo_relative="${local_path#"${REPO_ROOT}"/}" ;;
    "${REPO_ROOT}")   path_repo_relative="." ;;
  esac

  {
    echo "  - name: ${name}"
    echo "    path_resolved: ${local_path}"
    if [[ -n "${path_repo_relative}" ]]; then
      echo "    path_repo_relative: ${path_repo_relative}"
    fi
    echo "    extract_ms: ${extract_ms}"
    echo "    csv: ${name}.csv"
    echo "    nonzero: ${corpus_nonzero}"
  } >> "${MANIFEST}"

  [[ "${corpus_nonzero}" -eq 1 ]] && overall_nonzero=1
done

# Sanity: at least one predicate across all corpora returned non-zero
# rows. Total-collapse check — does NOT catch partial regressions.
if [[ "${overall_nonzero}" -ne 1 ]]; then
  log "ERROR: every predicate across every corpus returned zero rows — likely broken extraction or query wiring"
  echo "sanity: FAIL_ALL_ZERO" >> "${MANIFEST}"
  exit 3
fi
echo "sanity: OK" >> "${MANIFEST}"

log "done. results: ${RUN_DIR}"
log "manifest: ${MANIFEST}"
