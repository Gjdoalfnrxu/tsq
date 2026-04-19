package db

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/Gjdoalfnrxu/tsq/extract/schema"
)

// schemaWarnOnce ensures the older-schema-version warning is only printed
// once per process, no matter how many DBs get loaded.
var schemaWarnOnce sync.Once

// ReadDB reads a binary columnar fact database from r.
func ReadDB(r io.ReaderAt, size int64) (*DB, error) {
	le := binary.LittleEndian

	// Read header (16 bytes)
	hdr := make([]byte, 16)
	if _, err := r.ReadAt(hdr, 0); err != nil {
		return nil, fmt.Errorf("db: read header: %w", err)
	}
	if string(hdr[0:4]) != Magic {
		return nil, fmt.Errorf("db: invalid magic %q", hdr[0:4])
	}
	schemaVer := le.Uint32(hdr[4:8])
	// Forward-incompat (file is newer than this binary) is hard-fail: the
	// reader can't know which new relations were added or whether existing
	// ones changed shape. Backward-compat (file is older) is accepted with a
	// one-time stderr warning recommending re-extract — the schema is
	// additive, so missing relations come up empty when queried.
	if schemaVer > SchemaVersion {
		return nil, fmt.Errorf("db: schema version too new: file has %d, reader supports up to %d (re-build tsq)", schemaVer, SchemaVersion)
	}
	if schemaVer < SchemaVersion {
		schemaWarnOnce.Do(func() {
			fmt.Fprintf(os.Stderr,
				"warning: db schema version %d is older than reader version %d; "+
					"new relations will be empty. Re-extract for full coverage.\n",
				schemaVer, SchemaVersion)
		})
	}
	relCount := le.Uint32(hdr[8:12])
	strCount := le.Uint32(hdr[12:16])

	const (
		maxRelations = 1024
		maxStrings   = 1 << 24 // 16M strings
	)
	if relCount > maxRelations {
		return nil, fmt.Errorf("db: relation count %d exceeds maximum %d", relCount, maxRelations)
	}
	if strCount > maxStrings {
		return nil, fmt.Errorf("db: string count %d exceeds maximum %d", strCount, maxStrings)
	}

	// Read directory
	dirBuf := make([]byte, relCount*32)
	if _, err := r.ReadAt(dirBuf, 16); err != nil {
		return nil, fmt.Errorf("db: read directory: %w", err)
	}

	type dirEntry struct {
		nameOffset uint32
		tupleCount uint32
		colCount   uint32
		dataOffset uint64
	}
	entries := make([]dirEntry, relCount)
	for i := range entries {
		base := i * 32
		entries[i] = dirEntry{
			nameOffset: le.Uint32(dirBuf[base : base+4]),
			tupleCount: le.Uint32(dirBuf[base+4 : base+8]),
			colCount:   le.Uint32(dirBuf[base+8 : base+12]),
			dataOffset: le.Uint64(dirBuf[base+12 : base+20]),
		}
	}

	// Find string table offset: it follows all relation data.
	// Compute by finding the max (dataOffset + tupleCount * colCount * 4).
	var strTableStart int64
	for _, e := range entries {
		dataEnd := int64(e.dataOffset) + int64(e.tupleCount)*int64(e.colCount)*4
		if dataEnd > strTableStart {
			strTableStart = dataEnd
		}
	}
	// If no relations, string table starts right after directory
	if relCount == 0 {
		strTableStart = 16
	}

	// Read string table
	strTable, err := readStringTable(r, size, strCount, strTableStart, le)
	if err != nil {
		return nil, fmt.Errorf("db: read string table: %w", err)
	}

	db := &DB{
		relations: make(map[string]*Relation),
		strings:   strTable,
		stringIdx: make(map[string]uint32, len(strTable)),
	}
	for i, s := range strTable {
		db.stringIdx[s] = uint32(i)
	}

	// Now read each relation
	for _, entry := range entries {
		if entry.nameOffset >= uint32(len(strTable)) {
			return nil, fmt.Errorf("db: relation name offset %d out of range", entry.nameOffset)
		}
		name := strTable[entry.nameOffset]
		def, ok := schema.Lookup(name)
		if !ok {
			// Unknown relation — skip gracefully (forward compat)
			continue
		}
		if uint32(len(def.Columns)) != entry.colCount {
			return nil, fmt.Errorf("db: relation %q: expected %d columns, got %d", name, len(def.Columns), entry.colCount)
		}

		rel := &Relation{
			Def:     def,
			columns: make([]column, len(def.Columns)),
			size:    int(entry.tupleCount),
		}

		offset := int64(entry.dataOffset)
		for i, colDef := range def.Columns {
			data := make([]byte, entry.tupleCount*4)
			if _, err := r.ReadAt(data, offset); err != nil {
				return nil, fmt.Errorf("db: relation %q column %q: %w", name, colDef.Name, err)
			}
			switch colDef.Type {
			case schema.TypeInt32, schema.TypeEntityRef:
				rel.columns[i].ints = make([]int32, entry.tupleCount)
				for j := range rel.columns[i].ints {
					rel.columns[i].ints[j] = int32(le.Uint32(data[j*4 : j*4+4]))
				}
			case schema.TypeString:
				rel.columns[i].strIdxs = make([]uint32, entry.tupleCount)
				for j := range rel.columns[i].strIdxs {
					rel.columns[i].strIdxs[j] = le.Uint32(data[j*4 : j*4+4])
				}
			}
			offset += int64(entry.tupleCount * 4)
		}
		db.relations[name] = rel
	}
	return db, nil
}

func readStringTable(r io.ReaderAt, fileSize int64, strCount uint32, strTableStart int64, le binary.ByteOrder) ([]string, error) {
	if strTableStart < 0 || strTableStart > fileSize {
		return nil, fmt.Errorf("string table offset %d out of file bounds (size %d)", strTableStart, fileSize)
	}
	strData := make([]byte, fileSize-strTableStart)
	if _, err := r.ReadAt(strData, strTableStart); err != nil {
		return nil, err
	}

	if len(strData) < 4 {
		return nil, fmt.Errorf("string table too short")
	}
	count := le.Uint32(strData[0:4])
	if count != strCount {
		return nil, fmt.Errorf("string count mismatch: header says %d, table says %d", strCount, count)
	}

	strs := make([]string, 0, count)
	pos := 4
	for i := uint32(0); i < count; i++ {
		if pos+4 > len(strData) {
			return nil, fmt.Errorf("string table truncated at string %d", i)
		}
		slen := int(le.Uint32(strData[pos : pos+4]))
		pos += 4
		if slen > MaxStringLen {
			return nil, fmt.Errorf("string %d length %d exceeds limit", i, slen)
		}
		if pos+slen > len(strData) {
			return nil, fmt.Errorf("string %d data truncated", i)
		}
		strs = append(strs, string(strData[pos:pos+slen]))
		pos += slen
	}
	return strs, nil
}
