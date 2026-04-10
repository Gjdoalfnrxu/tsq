package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

func TestWriteJSONLines_Empty(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"name"},
		Rows:    nil,
	}
	var buf bytes.Buffer
	if err := WriteJSONLines(&buf, rs); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got %q", buf.String())
	}
}

func TestWriteJSONLines_SingleRow(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"name", "file"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "hello"}, eval.StrVal{V: "main.ts"}},
		},
	}
	var buf bytes.Buffer
	if err := WriteJSONLines(&buf, rs); err != nil {
		t.Fatal(err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &obj); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
	}
	if obj["name"] != "hello" {
		t.Errorf("name = %v, want hello", obj["name"])
	}
	if obj["file"] != "main.ts" {
		t.Errorf("file = %v, want main.ts", obj["file"])
	}
}

func TestWriteJSONLines_SpecialCharacters(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"msg"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "hello \"world\" \n\ttab"}},
		},
	}
	var buf bytes.Buffer
	if err := WriteJSONLines(&buf, rs); err != nil {
		t.Fatal(err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &obj); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
	}
	if obj["msg"] != "hello \"world\" \n\ttab" {
		t.Errorf("msg = %v", obj["msg"])
	}
}

func TestWriteJSONLines_IntegerValues(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"id", "line"},
		Rows: [][]eval.Value{
			{eval.IntVal{V: 42}, eval.IntVal{V: 100}},
		},
	}
	var buf bytes.Buffer
	if err := WriteJSONLines(&buf, rs); err != nil {
		t.Fatal(err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &obj); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// JSON numbers decode as float64.
	if obj["id"] != float64(42) {
		t.Errorf("id = %v, want 42", obj["id"])
	}
	if obj["line"] != float64(100) {
		t.Errorf("line = %v, want 100", obj["line"])
	}
}

func TestWriteJSONLines_MultipleRows(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"x"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "a"}},
			{eval.StrVal{V: "b"}},
		},
	}
	var buf bytes.Buffer
	if err := WriteJSONLines(&buf, rs); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	// Each line must be valid JSON.
	for i, line := range lines {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d invalid JSON: %v", i, err)
		}
	}
}
