package stats

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"sort"
	"time"
)

// ErrBadMagic is returned when the sidecar header doesn't begin with Magic.
var ErrBadMagic = errors.New("stats: bad magic")

// ErrFormatVersion is returned when FormatVer doesn't equal FormatVersion.
var ErrFormatVersion = errors.New("stats: unsupported format version")

// ErrCRC is returned when the trailer CRC doesn't match the body.
var ErrCRC = errors.New("stats: trailer CRC mismatch")

// ErrHashMismatch is returned by Load when the sidecar's EDBHash does
// not match the EDB hash the caller passed for validation.
var ErrHashMismatch = errors.New("stats: EDB hash mismatch (sidecar is stale)")

var le = binary.LittleEndian

// Encode serialises s to w. The format is documented in
// docs/design/stats-sidecar-format.md.
func Encode(w io.Writer, s *Schema) error {
	if s == nil {
		return fmt.Errorf("stats.Encode: nil schema")
	}
	var body bytes.Buffer

	// Header (after Magic, which goes straight to w too — but we keep
	// it in `body` so the trailer CRC covers everything pre-trailer).
	body.WriteString(Magic)
	writeU32(&body, FormatVersion)
	body.Write(s.EDBHash[:])
	writeI64(&body, s.BuiltAt.Unix())

	// Sort relation names for deterministic output.
	names := make([]string, 0, len(s.Rels))
	for n := range s.Rels {
		names = append(names, n)
	}
	sort.Strings(names)

	writeU32(&body, uint32(len(names)))
	for _, n := range names {
		if err := writeRel(&body, s.Rels[n]); err != nil {
			return err
		}
	}

	writeU32(&body, uint32(len(s.Joins)))
	for i := range s.Joins {
		writeJoin(&body, &s.Joins[i])
	}

	// Trailer CRC over the entire body.
	crc := crc32.ChecksumIEEE(body.Bytes())
	writeU32(&body, crc)

	_, err := w.Write(body.Bytes())
	return err
}

// Decode reads a sidecar from buf. Validates magic, format version,
// and trailer CRC. Does NOT validate EDBHash — that is Load's job
// because it requires comparing against the live EDB.
func Decode(buf []byte) (*Schema, error) {
	if len(buf) < len(Magic)+4+HashSize+8+4+4 {
		return nil, fmt.Errorf("stats: truncated (size %d)", len(buf))
	}
	if string(buf[:len(Magic)]) != Magic {
		return nil, ErrBadMagic
	}
	// Verify CRC first to catch bitrot before we trust any internal
	// length prefix.
	body := buf[:len(buf)-4]
	want := le.Uint32(buf[len(buf)-4:])
	if got := crc32.ChecksumIEEE(body); got != want {
		return nil, ErrCRC
	}

	r := &reader{buf: buf, pos: len(Magic)}
	formatVer := r.u32()
	if formatVer != FormatVersion {
		return nil, fmt.Errorf("%w: file=%d, supported=%d", ErrFormatVersion, formatVer, FormatVersion)
	}
	s := &Schema{FormatVersion: formatVer}
	r.bytesInto(s.EDBHash[:])
	s.BuiltAt = time.Unix(r.i64(), 0).UTC()

	relCount := r.u32()
	s.Rels = make(map[string]*RelStats, relCount)
	for i := uint32(0); i < relCount; i++ {
		rs, err := readRel(r)
		if err != nil {
			return nil, err
		}
		s.Rels[rs.Name] = rs
	}
	joinCount := r.u32()
	s.Joins = make([]JoinStats, 0, joinCount)
	for i := uint32(0); i < joinCount; i++ {
		js, err := readJoin(r)
		if err != nil {
			return nil, err
		}
		s.Joins = append(s.Joins, js)
	}

	// pos should be exactly len(buf)-4 now (trailer)
	if r.err != nil {
		return nil, r.err
	}
	if r.pos != len(buf)-4 {
		return nil, fmt.Errorf("stats: trailing garbage: pos=%d, want=%d", r.pos, len(buf)-4)
	}
	return s, nil
}

// --- write helpers --------------------------------------------------------
//
// All write helpers target *bytes.Buffer and discard the (n, err)
// return tuple. bytes.Buffer.Write never returns a non-nil error — see
// the standard library docs ("the return value n is the length of p;
// err is always nil"). The buffer grows as needed; the only failure
// mode is OOM, which panics. If/when the Encoder is generalised to
// accept any io.Writer (currently it materialises into a buffer for
// CRC), these helpers must be revised to propagate errors.

func writeU32(w *bytes.Buffer, v uint32) {
	var b [4]byte
	le.PutUint32(b[:], v)
	w.Write(b[:])
}

func writeU64(w *bytes.Buffer, v uint64) {
	var b [8]byte
	le.PutUint64(b[:], v)
	w.Write(b[:])
}

func writeI64(w *bytes.Buffer, v int64) {
	writeU64(w, uint64(v))
}

func writeF64(w *bytes.Buffer, v float64) {
	writeU64(w, math.Float64bits(v))
}

func writeStr(w *bytes.Buffer, s string) {
	writeU32(w, uint32(len(s)))
	w.WriteString(s)
}

func writeRel(w *bytes.Buffer, r *RelStats) error {
	writeStr(w, r.Name)
	writeU32(w, uint32(r.Arity))
	writeI64(w, r.RowCount)
	writeU32(w, uint32(len(r.Cols)))
	for _, c := range r.Cols {
		writeCol(w, &c)
	}
	return nil
}

func writeCol(w *bytes.Buffer, c *ColStats) {
	writeU32(w, uint32(c.Pos))
	writeI64(w, c.NDV)
	writeF64(w, c.NullFrac)
	writeU32(w, uint32(len(c.TopK)))
	for _, t := range c.TopK {
		writeU64(w, t.Value)
		writeI64(w, t.Count)
	}
	writeU32(w, uint32(len(c.HistBuckets)))
	for _, b := range c.HistBuckets {
		writeU64(w, b.Lo)
		writeU64(w, b.Hi)
		writeI64(w, b.Count)
	}
}

func writeJoin(w *bytes.Buffer, j *JoinStats) {
	writeStr(w, j.LeftRel)
	writeU32(w, uint32(j.LeftCol))
	writeStr(w, j.RightRel)
	writeU32(w, uint32(j.RightCol))
	writeF64(w, j.LRSelectivity)
	writeF64(w, j.RLSelectivity)
	writeI64(w, j.DistinctMatches)
}

// --- read helpers ---------------------------------------------------------

type reader struct {
	buf []byte
	pos int
	err error
}

func (r *reader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if r.pos+n > len(r.buf) {
		r.err = fmt.Errorf("stats: short read at pos=%d (need %d, have %d)", r.pos, n, len(r.buf)-r.pos)
		return false
	}
	return true
}

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := le.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v
}

func (r *reader) u64() uint64 {
	if !r.need(8) {
		return 0
	}
	v := le.Uint64(r.buf[r.pos:])
	r.pos += 8
	return v
}

func (r *reader) i64() int64 { return int64(r.u64()) }

func (r *reader) f64() float64 { return math.Float64frombits(r.u64()) }

func (r *reader) bytesInto(out []byte) {
	if !r.need(len(out)) {
		return
	}
	copy(out, r.buf[r.pos:r.pos+len(out)])
	r.pos += len(out)
}

// maxStrLen caps the length of a single decoded string field at 64 MB.
// Sidecar string fields hold relation names and similar identifiers —
// realistic sizes are well under 1 KB. This ceiling exists to fail
// loudly on a corrupt length prefix instead of attempting a multi-GB
// allocation.
const maxStrLen = 64 << 20

func (r *reader) str() string {
	n := r.u32()
	if n > maxStrLen {
		r.err = fmt.Errorf("stats: string length %d exceeds cap %d at pos=%d", n, maxStrLen, r.pos)
		return ""
	}
	if !r.need(int(n)) {
		return ""
	}
	s := string(r.buf[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return s
}

func readRel(r *reader) (*RelStats, error) {
	rs := &RelStats{}
	rs.Name = r.str()
	rs.Arity = int(r.u32())
	rs.RowCount = r.i64()
	colCount := r.u32()
	if int(colCount) != rs.Arity {
		return nil, fmt.Errorf("stats: rel %q: arity %d != colcount %d", rs.Name, rs.Arity, colCount)
	}
	rs.Cols = make([]ColStats, colCount)
	for i := uint32(0); i < colCount; i++ {
		c, err := readCol(r)
		if err != nil {
			return nil, err
		}
		rs.Cols[i] = c
	}
	return rs, r.err
}

func readCol(r *reader) (ColStats, error) {
	c := ColStats{}
	c.Pos = int(r.u32())
	c.NDV = r.i64()
	c.NullFrac = r.f64()
	tk := r.u32()
	if tk > TopKLimit {
		return c, fmt.Errorf("stats: TopKCount %d exceeds limit %d", tk, TopKLimit)
	}
	if tk > 0 {
		c.TopK = make([]TopKEntry, tk)
		for i := uint32(0); i < tk; i++ {
			c.TopK[i] = TopKEntry{Value: r.u64(), Count: r.i64()}
		}
	}
	hb := r.u32()
	const maxHist = 4096 // sanity ceiling far above HistogramBuckets
	if hb > maxHist {
		return c, fmt.Errorf("stats: HistBucketCount %d exceeds %d", hb, maxHist)
	}
	if hb > 0 {
		c.HistBuckets = make([]Bucket, hb)
		for i := uint32(0); i < hb; i++ {
			c.HistBuckets[i] = Bucket{Lo: r.u64(), Hi: r.u64(), Count: r.i64()}
		}
	}
	return c, r.err
}

func readJoin(r *reader) (JoinStats, error) {
	j := JoinStats{}
	j.LeftRel = r.str()
	j.LeftCol = int(r.u32())
	j.RightRel = r.str()
	j.RightCol = int(r.u32())
	j.LRSelectivity = r.f64()
	j.RLSelectivity = r.f64()
	j.DistinctMatches = r.i64()
	return j, r.err
}
