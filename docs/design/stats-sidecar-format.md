# EDB statistics sidecar — file format

**Status:** implemented in PR `feat(stats): EDB statistics sidecar — schema, compute, persist` (Phase B PR1).
**Plan source:** `docs/design/valueflow-phase-b-plan.md` §1, §2.

This document is the on-disk source of truth for the `*.stats` sidecar
file written next to a tsq EDB. It is intended to be small, version-gated,
self-validating against the EDB it describes, and decodable without any
external schema definition (no protobuf, no flatbuffers — pure Go binary).

## 1. File location

```
<edb-path>             # e.g. tsq.db
<edb-path>.stats       # NEW: sidecar
<edb-path>.stats.lock  # advisory lock during write (created+removed by writer)
```

The sidecar always sits beside the EDB it describes. Move/rename together.

## 2. Top-level layout

All multi-byte integers are little-endian. All strings are length-prefixed
(uint32 length, then bytes; no NUL terminator).

```
+---------------------------+
| Magic        "TSQS\0"  4B |   # "tsq stats"
| FormatVer    uint32    4B |   # currently 1; bump on incompatible change
| EDBHash      [32]byte 32B |   # SHA-256 of the EDB bytes
| BuiltAtUnix  int64     8B |   # seconds since epoch
| RelCount     uint32    4B |
+---------------------------+
| Rel[0]                    |   # see §3
| Rel[1]                    |
| ...                       |
| Rel[RelCount-1]           |
+---------------------------+
| JoinCount    uint32    4B |
+---------------------------+
| Join[0]                   |   # see §4
| ...                       |
| Join[JoinCount-1]         |
+---------------------------+
| TrailerCRC   uint32    4B |   # IEEE CRC32 over everything before this
+---------------------------+
```

## 3. Per-relation block (`RelStats`)

```
NameLen   uint32                 # length in bytes of relation name
Name      [NameLen]byte
Arity     uint32
RowCount  int64
ColCount  uint32                 # equals Arity (defensive duplicate)
Col[0..ColCount-1]               # see §3.1
```

### 3.1 Per-column block (`ColStats`)

```
Pos          uint32
NDV          int64                # HyperLogLog estimate, ≥0
NullFrac     float64              # null-fraction in [0.0, 1.0]; only
                                  # incremented for columns whose
                                  # ColumnDef.Nullable is true. Non-
                                  # nullable columns always emit 0.
TopKCount    uint32               # ≤ 32
TopK[0..TopKCount-1]:
    Value uint64                  # raw id (int32/entity-ref) or 64-bit
                                  # FNV-1a surrogate id (string column)
    Count int64                   # SpaceSaving lower bound (count - err)
HistBucketCount uint32            # 0 if NDV ≤ NDVHistogramThreshold
                                  # OR column is TypeString (string
                                  # histograms are out of scope for v1)
Hist[0..count-1]:
    Lo    uint64
    Hi    uint64
    Count int64
```

**Per-column-type bucket-shape limitation:** all histogram buckets use
`uint64` Lo/Hi regardless of the underlying column type. For
int32/entity-ref columns this is a faithful representation. For string
columns no bucket is emitted (HistBucketCount = 0); per-string-id
ordering is meaningless to the planner. Future column types
(time-of-day, decimal) will need a per-type bucket shape — that's a
format-version bump.

### 3.2 Sentinels

- `TopKCount = 0` means "no observed top values" (e.g. empty relation).
- `HistBucketCount = 0` means "no histogram emitted" — either because
  `NDV ≤ NDVHistogramThreshold` (currently 256) or because the column
  is `TypeString`. String columns currently use a 64-bit FNV-1a
  surrogate id for TopK/reservoir tracking; that id has no useful
  ordering, so a numeric equi-depth histogram on it would produce
  meaningless boundaries. The planner's consumer falls back to default
  selectivity for string columns. See §6 for the v1 limitations list.

## 4. Per-pair join block (`JoinStats`)

```
LeftRelLen      uint32
LeftRel         [..]byte
LeftCol         uint32
RightRelLen     uint32
RightRel        [..]byte
RightCol        uint32
LRSelectivity   float64           # P(random L row matches ≥1 R row)
RLSelectivity   float64           # P(random R row matches ≥1 L row)
DistinctMatches int64             # |πLeftCol(L) ∩ πRightCol(R)| (HLL intersect estimate)
```

**Selectivity is asymmetric.** A symmetric scalar collapses two very
different planner answers into one. Example: on `Contains(parent,
child)` joined to itself, a child has exactly one parent (LR sel
near 1) but a parent has many children (RL sel ≪ 1). The split is
introduced now while no consumer reads JoinStats; bumping it later
would require a format-version migration.

`JoinStats` are emitted only for relation/column pairs declared in
`extract/schema/joinpaired.go` (added by this PR). Initial set: empty.
Wiring in `extract/schema/joinpaired.go` is a stub for PR2/PR3 to
populate as `mayResolveTo` shapes need them. The plan calls out
`CallArg.call → Call.call` etc. as candidates; we don't have a
`CallArg`/`Call` schema yet, so the v1 list is conservative.

## 5. Hashing and invalidation

- `EDBHash` is SHA-256 of the entire EDB file bytes.
  - The plan §2.3 calls for BLAKE2b. SHA-256 is substituted to avoid
    pulling `golang.org/x/crypto` (which would force a Go toolchain
    bump on the current `go 1.23.8` module). The hash's only role is
    invalidation: detecting when the EDB has been rewritten so the
    sidecar must be rejected. SHA-256 satisfies that role identically.
    The on-disk field is exactly 32 bytes either way; switching to
    BLAKE2b later is a bump of `FormatVer` and a single function call
    in `hash.go`.
- On `Load`, the loader recomputes the EDB hash and compares.
  Mismatch → return `nil, ErrHashMismatch` and emit a stderr warning.
  The caller must treat `nil` stats as "default-stats mode" (planner
  consumer arrives in PR2).
- `FormatVer` mismatch is also `nil + warning`. Same fallback.
- `TrailerCRC` mismatch → `nil + warning`. Detects truncation/bitrot.

## 6. Known limitations (v1)

- **No string-column histograms.** Histograms are skipped entirely for
  `TypeString` columns: their per-row id is a 64-bit FNV-1a hash of
  the string contents, with no useful ordering. The TopK list still
  surfaces frequent strings (skew detection works). Range predicates
  on strings fall back to default selectivity in the planner.
- **No streaming write.** The sidecar is built in memory then flushed
  in one shot. Mastodon-scale (~30 MB EDB) sidecar is ~1.5 MB; well
  within budget.
- **No compression.** Plan §2.2 mentions gzip; we omit it for v1
  because the wire size is already small and uncompressed access
  simplifies inspection and partial-read patterns the planner may
  want later. Revisit if sidecar > 10% of EDB on any real corpus.

## 7. CLI surface

```
tsq extract <project>          # writes .stats alongside .db unless --no-stats
tsq extract --no-stats <project>
tsq stats compute <db>         # rebuild sidecar for an existing EDB
tsq stats inspect <db> [rel]   # human-readable dump
```
