package stats

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

func sampleSchema() *Schema {
	s := &Schema{
		FormatVersion: FormatVersion,
		BuiltAt:       time.Unix(1700000000, 0).UTC(),
		Rels: map[string]*RelStats{
			"R": {
				Name:     "R",
				Arity:    2,
				RowCount: 12345,
				Cols: []ColStats{
					{
						Pos: 0, NDV: 999, NullFrac: 0.01,
						TopK: []TopKEntry{{Value: 7, Count: 100}, {Value: 8, Count: 50}},
					},
					{
						Pos: 1, NDV: 500, NullFrac: 0.0,
						HistBuckets: []Bucket{{Lo: 0, Hi: 10, Count: 1000}, {Lo: 11, Hi: 99, Count: 11345}},
					},
				},
			},
			"S": {
				Name: "S", Arity: 1, RowCount: 0,
				Cols: []ColStats{{Pos: 0}},
			},
		},
		Joins: []JoinStats{
			{LeftRel: "R", LeftCol: 0, RightRel: "S", RightCol: 0, LRSelectivity: 0.0123, RLSelectivity: 0.987, DistinctMatches: 42},
		},
	}
	for i := range s.EDBHash {
		s.EDBHash[i] = byte(i)
	}
	return s
}

func TestCodec_RoundTrip(t *testing.T) {
	s := sampleSchema()
	var buf bytes.Buffer
	if err := Encode(&buf, s); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.FormatVersion != s.FormatVersion {
		t.Errorf("FormatVersion: got %d, want %d", got.FormatVersion, s.FormatVersion)
	}
	if got.EDBHash != s.EDBHash {
		t.Errorf("EDBHash: got %x, want %x", got.EDBHash, s.EDBHash)
	}
	if !got.BuiltAt.Equal(s.BuiltAt) {
		t.Errorf("BuiltAt: got %s, want %s", got.BuiltAt, s.BuiltAt)
	}
	if !reflect.DeepEqual(got.Rels, s.Rels) {
		t.Errorf("Rels mismatch:\n got=%+v\nwant=%+v", got.Rels, s.Rels)
	}
	if !reflect.DeepEqual(got.Joins, s.Joins) {
		t.Errorf("Joins mismatch:\n got=%+v\nwant=%+v", got.Joins, s.Joins)
	}
}

// Determinism: encoding the same schema twice must produce the same
// bytes (sorted relation iteration). This underpins reproducible plans
// across runs (plan §8.3).
func TestCodec_Deterministic(t *testing.T) {
	s := sampleSchema()
	var a, b bytes.Buffer
	if err := Encode(&a, s); err != nil {
		t.Fatal(err)
	}
	if err := Encode(&b, s); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatalf("non-deterministic: %d vs %d bytes", a.Len(), b.Len())
	}
}

func TestCodec_BadMagic(t *testing.T) {
	// Buffer must be ≥ minimum sidecar size so we hit the magic check
	// rather than the truncation check. Pad with zeroes.
	buf := make([]byte, 200)
	copy(buf, "XXXX\x00")
	_, err := Decode(buf)
	if !errors.Is(err, ErrBadMagic) {
		t.Fatalf("expected ErrBadMagic, got %v", err)
	}
}

func TestCodec_Truncated(t *testing.T) {
	_, err := Decode([]byte("TSQS\x00"))
	if err == nil {
		t.Fatal("expected error on truncated buffer")
	}
}

func TestCodec_CRCMismatch(t *testing.T) {
	s := sampleSchema()
	var buf bytes.Buffer
	if err := Encode(&buf, s); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// Flip a byte in the middle (within the body, not the trailer).
	b[len(b)/2] ^= 0xff
	_, err := Decode(b)
	if !errors.Is(err, ErrCRC) {
		t.Fatalf("expected ErrCRC, got %v", err)
	}
}

func TestCodec_FormatVersionMismatch(t *testing.T) {
	s := sampleSchema()
	s.FormatVersion = 999 // doesn't matter — Encode writes the current FormatVersion constant
	var buf bytes.Buffer
	if err := Encode(&buf, s); err != nil {
		t.Fatal(err)
	}
	// Manually corrupt the version field in the encoded bytes.
	b := buf.Bytes()
	// Layout: Magic(5) | FormatVer(4) | EDBHash(32) | ...
	b[5] = 99
	b[6] = 0
	b[7] = 0
	b[8] = 0
	// Recompute trailer CRC so we don't get ErrCRC instead.
	// (For this test we'd rather see ErrFormatVersion; recomputing the
	// CRC keeps the test focused.)
	body := b[:len(b)-4]
	rebuildCRC(b, body)
	_, err := Decode(b)
	if !errors.Is(err, ErrFormatVersion) {
		t.Fatalf("expected ErrFormatVersion, got %v", err)
	}
}

func rebuildCRC(full, body []byte) {
	crc := crc32IEEE(body)
	full[len(full)-4] = byte(crc)
	full[len(full)-3] = byte(crc >> 8)
	full[len(full)-2] = byte(crc >> 16)
	full[len(full)-1] = byte(crc >> 24)
}

// Local CRC helper to avoid importing hash/crc32 in test (we already
// use it in production, but isolating the test keeps it easy to read).
func crc32IEEE(b []byte) uint32 {
	// IEEE polynomial: identical to hash/crc32.ChecksumIEEE.
	var tab [256]uint32
	for i := 0; i < 256; i++ {
		c := uint32(i)
		for j := 0; j < 8; j++ {
			if c&1 != 0 {
				c = 0xedb88320 ^ (c >> 1)
			} else {
				c >>= 1
			}
		}
		tab[i] = c
	}
	c := ^uint32(0)
	for _, x := range b {
		c = tab[byte(c)^x] ^ (c >> 8)
	}
	return ^c
}
