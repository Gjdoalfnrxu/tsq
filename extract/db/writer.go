// Package db implements the tsq binary columnar fact database format.
package db

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/Gjdoalfnrxu/tsq/extract/schema"
)

const (
	Magic         = "TSQ\x00"
	SchemaVersion = 1
	MaxTuples     = math.MaxUint32
	MaxStringLen  = 1 << 20 // 1MB per string — sanity limit
)

// Relation holds in-memory columnar data for a single fact relation.
type Relation struct {
	Def     schema.RelationDef
	columns []column
	size    int // number of tuples
}

type column struct {
	ints    []int32  // for TypeInt32, TypeEntityRef
	strIdxs []uint32 // for TypeString (indexes into writer's string table)
}

// DB holds all relations for writing.
type DB struct {
	relations map[string]*Relation
	strings   []string
	stringIdx map[string]uint32
}

// NewDB creates an empty fact database.
func NewDB() *DB {
	return &DB{
		relations: make(map[string]*Relation),
		stringIdx: make(map[string]uint32),
		strings:   []string{""}, // index 0 = empty string
	}
}

// Relation returns or creates the named relation. Panics if the name is not in the registry.
func (db *DB) Relation(name string) *Relation {
	if r, ok := db.relations[name]; ok {
		return r
	}
	def, ok := schema.Lookup(name)
	if !ok {
		panic(fmt.Sprintf("db.Relation: unknown relation %q", name))
	}
	r := &Relation{
		Def:     def,
		columns: make([]column, len(def.Columns)),
	}
	db.relations[name] = r
	return r
}

// intern returns the string table index for s, adding it if necessary.
func (db *DB) intern(s string) uint32 {
	if idx, ok := db.stringIdx[s]; ok {
		return idx
	}
	if len(db.strings) > int(MaxTuples) {
		panic("string table overflow")
	}
	idx := uint32(len(db.strings))
	db.strings = append(db.strings, s)
	db.stringIdx[s] = idx
	return idx
}

// Tuples returns the number of tuples in the relation.
func (r *Relation) Tuples() int { return r.size }

// GetInt returns the int32 value at (tuple, col) for an Int32/EntityRef column.
func (r *Relation) GetInt(tuple, col int) (int32, error) {
	if col >= len(r.columns) {
		return 0, fmt.Errorf("col %d out of range", col)
	}
	if tuple >= r.size {
		return 0, fmt.Errorf("tuple %d out of range", tuple)
	}
	return r.columns[col].ints[tuple], nil
}

// GetString returns the string value at (tuple, col) for a String column.
func (r *Relation) GetString(db *DB, tuple, col int) (string, error) {
	if col >= len(r.columns) {
		return "", fmt.Errorf("col %d out of range", col)
	}
	if tuple >= r.size {
		return "", fmt.Errorf("tuple %d out of range", tuple)
	}
	idx := r.columns[col].strIdxs[tuple]
	if int(idx) >= len(db.strings) {
		return "", fmt.Errorf("string index %d out of range", idx)
	}
	return db.strings[idx], nil
}

// AddTuple appends a tuple to the relation. Values must match the column types.
// Int32/EntityRef columns take int32. String columns take string.
func (r *Relation) AddTuple(db *DB, vals ...interface{}) error {
	if len(vals) != len(r.Def.Columns) {
		return fmt.Errorf("relation %q: expected %d values, got %d", r.Def.Name, len(r.Def.Columns), len(vals))
	}
	for i, val := range vals {
		col := &r.columns[i]
		switch r.Def.Columns[i].Type {
		case schema.TypeInt32, schema.TypeEntityRef:
			v, ok := toInt32(val)
			if !ok {
				return fmt.Errorf("relation %q column %q: expected int-like, got %T", r.Def.Name, r.Def.Columns[i].Name, val)
			}
			col.ints = append(col.ints, v)
		case schema.TypeString:
			s, ok := val.(string)
			if !ok {
				return fmt.Errorf("relation %q column %q: expected string, got %T", r.Def.Name, r.Def.Columns[i].Name, val)
			}
			if len(s) > MaxStringLen {
				return fmt.Errorf("relation %q column %q: string length %d exceeds limit", r.Def.Name, r.Def.Columns[i].Name, len(s))
			}
			col.strIdxs = append(col.strIdxs, db.intern(s))
		}
	}
	r.size++
	return nil
}

func toInt32(v interface{}) (int32, bool) {
	switch x := v.(type) {
	case int32:
		return x, true
	case int:
		return int32(x), true
	case uint32:
		return int32(x), true
	case int64:
		return int32(x), true
	}
	return 0, false
}

// Encode serialises the DB in binary columnar format to w.
func (db *DB) Encode(w io.Writer) error {
	// build ordered relation list from registry (consistent ordering)
	var rels []*Relation
	for _, def := range schema.Registry {
		if r, ok := db.relations[def.Name]; ok {
			rels = append(rels, r)
		}
	}

	le := binary.LittleEndian

	// Pre-intern all relation names so they are in the string table
	// before we build it or write the header.
	type relMeta struct {
		nameOffset uint32
		dataOffset uint64
	}
	metas := make([]relMeta, len(rels))
	for i, r := range rels {
		metas[i].nameOffset = db.intern(r.Def.Name)
	}

	// Header
	if _, err := io.WriteString(w, Magic); err != nil {
		return err
	}
	hdr := make([]byte, 12)
	le.PutUint32(hdr[0:4], SchemaVersion)
	le.PutUint32(hdr[4:8], uint32(len(rels)))
	le.PutUint32(hdr[8:12], uint32(len(db.strings)))
	if _, err := w.Write(hdr); err != nil {
		return err
	}

	// Compute data offsets
	dirSize := uint64(len(rels)) * 32
	baseOffset := uint64(16) + dirSize

	// Build string table bytes
	strTable := buildStringTable(db.strings)

	// Compute per-relation data offsets
	offset := baseOffset
	for i, r := range rels {
		metas[i].dataOffset = offset
		offset += relDataSize(r)
	}

	// Write directory
	dir := make([]byte, 32)
	for i, r := range rels {
		le.PutUint32(dir[0:4], metas[i].nameOffset)
		le.PutUint32(dir[4:8], uint32(r.size))
		le.PutUint32(dir[8:12], uint32(len(r.Def.Columns)))
		le.PutUint64(dir[12:20], metas[i].dataOffset)
		// padding [20:32] = zero
		for j := 20; j < 32; j++ {
			dir[j] = 0
		}
		if _, err := w.Write(dir); err != nil {
			return err
		}
	}

	// Write relation data
	for _, r := range rels {
		if err := writeRelationData(w, r, le); err != nil {
			return err
		}
	}

	// Write string table
	_, err := w.Write(strTable)
	return err
}

func relDataSize(r *Relation) uint64 {
	sz := uint64(0)
	for i, col := range r.Def.Columns {
		switch col.Type {
		case schema.TypeInt32, schema.TypeEntityRef:
			sz += uint64(len(r.columns[i].ints)) * 4
		case schema.TypeString:
			sz += uint64(len(r.columns[i].strIdxs)) * 4
		}
	}
	return sz
}

func writeRelationData(w io.Writer, r *Relation, le binary.ByteOrder) error {
	buf := make([]byte, 4)
	for i, col := range r.Def.Columns {
		switch col.Type {
		case schema.TypeInt32, schema.TypeEntityRef:
			for _, v := range r.columns[i].ints {
				le.PutUint32(buf, uint32(v))
				if _, err := w.Write(buf); err != nil {
					return err
				}
			}
		case schema.TypeString:
			for _, idx := range r.columns[i].strIdxs {
				le.PutUint32(buf, idx)
				if _, err := w.Write(buf); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func buildStringTable(strings []string) []byte {
	var out []byte
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(len(strings)))
	out = append(out, buf...)
	for _, s := range strings {
		binary.LittleEndian.PutUint32(buf, uint32(len(s)))
		out = append(out, buf...)
		out = append(out, s...)
	}
	return out
}
