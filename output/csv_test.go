package output

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

func TestWriteCSV_Empty(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"name", "file"},
		Rows:    nil,
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, rs); err != nil {
		t.Fatal(err)
	}

	// Should have header only.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Errorf("lines = %d, want 1 (header only)", len(lines))
	}
	if lines[0] != "name,file" {
		t.Errorf("header = %q, want name,file", lines[0])
	}
}

func TestWriteCSV_SingleRow(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"name", "value"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "foo"}, eval.IntVal{V: 42}},
		},
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, rs); err != nil {
		t.Fatal(err)
	}

	r := csv.NewReader(strings.NewReader(buf.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2 (header + 1 row)", len(records))
	}
	if records[1][0] != "foo" {
		t.Errorf("col0 = %q, want foo", records[1][0])
	}
	if records[1][1] != "42" {
		t.Errorf("col1 = %q, want 42", records[1][1])
	}
}

func TestWriteCSV_CommasInValues(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"msg"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "hello, world"}},
		},
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, rs); err != nil {
		t.Fatal(err)
	}

	r := csv.NewReader(strings.NewReader(buf.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if records[1][0] != "hello, world" {
		t.Errorf("msg = %q, want 'hello, world'", records[1][0])
	}
}

func TestWriteCSV_NewlinesInValues(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"msg"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "line1\nline2"}},
		},
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, rs); err != nil {
		t.Fatal(err)
	}

	r := csv.NewReader(strings.NewReader(buf.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if records[1][0] != "line1\nline2" {
		t.Errorf("msg = %q, want 'line1\\nline2'", records[1][0])
	}
}

func TestWriteCSV_QuotesInValues(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"msg"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: `say "hi"`}},
		},
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, rs); err != nil {
		t.Fatal(err)
	}

	r := csv.NewReader(strings.NewReader(buf.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if records[1][0] != `say "hi"` {
		t.Errorf("msg = %q, want 'say \"hi\"'", records[1][0])
	}
}
