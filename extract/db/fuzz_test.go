package db

// fuzz_test.go — Go 1.18+ fuzz tests for the binary fact DB reader.
//
// Run manually with:
//
//	go test -fuzz=FuzzReadDB ./extract/db/ -fuzztime=60s
//
// The seed corpus is seeded from a valid encoded DB so the fuzzer starts from
// real binary data and mutates from there. This is more effective than starting
// from random bytes because it exercises the actual parsing paths.
//
// Safety contract: ReadDB must NEVER panic on arbitrary byte input. It must
// either return a valid *DB or return an error. A crash (nil dereference,
// index out of bounds, integer overflow, etc.) is a bug.
//
// Note: ReadDB already has bounds checks (maxRelations = 1024, maxStrings = 16M)
// that guard against obviously malicious inputs. The fuzz test probes for cases
// where intermediate parsing steps panic before those guards fire.

import (
	"bytes"
	"testing"
)

// FuzzReadDB feeds arbitrary bytes into the binary DB reader and asserts it
// does not panic. ReadDB may return errors; that is expected and correct.
// Any panic is a bug.
func FuzzReadDB(f *testing.F) {
	// Seed corpus: encode a real DB with non-trivial content so the fuzzer
	// has a valid starting point for structural mutations.
	validDB := makeSeededDB(f)
	var buf bytes.Buffer
	if err := validDB.Encode(&buf); err != nil {
		f.Fatalf("seeding: encode DB: %v", err)
	}
	f.Add(buf.Bytes())

	// Additional seeds: truncated header, wrong magic, zero-length.
	f.Add([]byte{})                                  // empty input
	f.Add([]byte("TSQ\x00"))                         // magic only, no version
	f.Add([]byte("TSQ\x00\x01\x00\x00\x00"))         // magic + version, no counts
	f.Add([]byte("XXXX\x00\x00\x00\x00"))            // wrong magic
	f.Add(make([]byte, 16))                          // all-zero header
	f.Add(bytes.Repeat([]byte{0xff}, 32))            // all-0xff
	f.Add(append([]byte("TSQ\x00"), buf.Bytes()...)) // double magic
	f.Add(buf.Bytes()[:len(buf.Bytes())/2])          // truncated mid-stream

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		// The contract is no panic, not no error.
		//nolint:errcheck // intentional: we only care about panics, not errors
		_, _ = ReadDB(r, int64(len(data)))
	})
}

// makeSeededDB returns a *DB populated with representative tuples across
// several schema-registered relations. This gives the fuzzer a realistic
// binary structure to start mutating from.
func makeSeededDB(f *testing.F) *DB {
	f.Helper()
	database := NewDB()

	// File relation: 2 files.
	fr := database.Relation("File")
	if err := fr.AddTuple(database, int32(1001), "/src/main.ts", "sha256:abc"); err != nil {
		f.Fatalf("AddTuple File 1: %v", err)
	}
	if err := fr.AddTuple(database, int32(1002), "/src/utils.ts", "sha256:def"); err != nil {
		f.Fatalf("AddTuple File 2: %v", err)
	}

	// Node relation: several AST nodes.
	nr := database.Relation("Node")
	for i, row := range [][]interface{}{
		{int32(2001), int32(1001), "FunctionDeclaration", int32(1), int32(0), int32(5), int32(0)},
		{int32(2002), int32(1001), "CallExpression", int32(3), int32(4), int32(3), int32(20)},
		{int32(2003), int32(1002), "ArrowFunction", int32(1), int32(10), int32(1), int32(30)},
	} {
		if err := nr.AddTuple(database, row...); err != nil {
			f.Fatalf("AddTuple Node %d: %v", i, err)
		}
	}

	// Function relation.
	fnr := database.Relation("Function")
	if err := fnr.AddTuple(database, int32(2001), "processData", int32(0), int32(0), int32(0), int32(0)); err != nil {
		f.Fatalf("AddTuple Function: %v", err)
	}

	// Call relation.
	cr := database.Relation("Call")
	if err := cr.AddTuple(database, int32(2002), int32(2003), int32(2)); err != nil {
		f.Fatalf("AddTuple Call: %v", err)
	}

	// CallArg relation.
	car := database.Relation("CallArg")
	if err := car.AddTuple(database, int32(2002), int32(0), int32(3001)); err != nil {
		f.Fatalf("AddTuple CallArg: %v", err)
	}

	// SchemaVersion — exactly one.
	sv := database.Relation("SchemaVersion")
	if err := sv.AddTuple(database, int32(SchemaVersion)); err != nil {
		f.Fatalf("AddTuple SchemaVersion: %v", err)
	}

	return database
}
