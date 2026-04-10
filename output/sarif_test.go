package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

func TestWriteSARIF_Empty(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"name"},
		Rows:    nil,
	}
	var buf bytes.Buffer
	if err := WriteSARIF(&buf, rs, SARIFOptions{}); err != nil {
		t.Fatal(err)
	}

	// Must be valid JSON.
	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	if len(log.Runs[0].Results) != 0 {
		t.Errorf("results = %d, want 0", len(log.Runs[0].Results))
	}
	// Results should be [] not null.
	if strings.Contains(buf.String(), `"results": null`) {
		t.Error("results should be empty array, not null")
	}
}

func TestWriteSARIF_SingleResultWithLocation(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"name", "file", "line", "col"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "unusedVar"}, eval.StrVal{V: "src/main.ts"}, eval.IntVal{V: 42}, eval.IntVal{V: 10}},
		},
	}
	var buf bytes.Buffer
	if err := WriteSARIF(&buf, rs, SARIFOptions{QueryName: "unused-vars"}); err != nil {
		t.Fatal(err)
	}

	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(log.Runs[0].Results) != 1 {
		t.Fatalf("results = %d, want 1", len(log.Runs[0].Results))
	}
	r := log.Runs[0].Results[0]
	if r.RuleID != "unused-vars" {
		t.Errorf("ruleId = %q, want unused-vars", r.RuleID)
	}
	if r.Message.Text != "unusedVar" {
		t.Errorf("message = %q, want unusedVar", r.Message.Text)
	}
	if len(r.Locations) != 1 {
		t.Fatalf("locations = %d, want 1", len(r.Locations))
	}
	pl := r.Locations[0].PhysicalLocation
	if pl == nil {
		t.Fatal("physicalLocation is nil")
	}
	if pl.ArtifactLocation.URI != "src/main.ts" {
		t.Errorf("uri = %q, want src/main.ts", pl.ArtifactLocation.URI)
	}
	if pl.Region == nil {
		t.Fatal("region is nil")
	}
	if pl.Region.StartLine != 42 {
		t.Errorf("startLine = %d, want 42", pl.Region.StartLine)
	}
	if pl.Region.StartColumn != 10 {
		t.Errorf("startColumn = %d, want 10", pl.Region.StartColumn)
	}
}

func TestWriteSARIF_MultipleResults(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"msg"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "first"}},
			{eval.StrVal{V: "second"}},
			{eval.StrVal{V: "third"}},
		},
	}
	var buf bytes.Buffer
	if err := WriteSARIF(&buf, rs, SARIFOptions{}); err != nil {
		t.Fatal(err)
	}

	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(log.Runs[0].Results) != 3 {
		t.Errorf("results = %d, want 3", len(log.Runs[0].Results))
	}
}

func TestWriteSARIF_ValidStructure(t *testing.T) {
	rs := &eval.ResultSet{
		Columns: []string{"file", "line"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "a.ts"}, eval.IntVal{V: 1}},
		},
	}
	var buf bytes.Buffer
	if err := WriteSARIF(&buf, rs, SARIFOptions{ToolVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}

	// Verify required SARIF fields.
	var raw map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["$schema"]; !ok {
		t.Error("missing $schema field")
	}
	if _, ok := raw["version"]; !ok {
		t.Error("missing version field")
	}
	if _, ok := raw["runs"]; !ok {
		t.Error("missing runs field")
	}
}
