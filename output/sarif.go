// Package output implements result formatters (SARIF, JSON, CSV).
package output

import (
	"encoding/json"
	"io"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// SARIF 2.1.0 types — minimal subset needed for output.

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation *sarifPhysicalLocation `json:"physicalLocation,omitempty"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine,omitempty"`
	StartColumn int `json:"startColumn,omitempty"`
}

// SARIFOptions controls SARIF output.
type SARIFOptions struct {
	QueryName   string // used as ruleId
	ToolVersion string // tool version string
}

// WriteSARIF writes the ResultSet as SARIF 2.1.0 JSON to w.
// Column heuristics for location: columns named "file"/"path" are used for URI,
// columns named "line"/"startLine" for line, "col"/"column"/"startCol" for column.
func WriteSARIF(w io.Writer, rs *eval.ResultSet, opts SARIFOptions) error {
	if opts.QueryName == "" {
		opts.QueryName = "tsq-query"
	}
	if opts.ToolVersion == "" {
		opts.ToolVersion = "0.0.1-dev"
	}

	// Build column index for location heuristics.
	colIdx := make(map[string]int, len(rs.Columns))
	for i, c := range rs.Columns {
		colIdx[c] = i
	}

	var results []sarifResult
	for _, row := range rs.Rows {
		msg := buildRowMessage(rs.Columns, row)
		r := sarifResult{
			RuleID:  opts.QueryName,
			Level:   "warning",
			Message: sarifMessage{Text: msg},
		}

		// Try to build a location from well-known column names.
		loc := tryBuildLocation(colIdx, row)
		if loc != nil {
			r.Locations = []sarifLocation{*loc}
		}

		results = append(results, r)
	}

	log := sarifLog{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:    "tsq",
						Version: opts.ToolVersion,
					},
				},
				Results: results,
			},
		},
	}

	// Ensure Results is always an array, not null.
	if log.Runs[0].Results == nil {
		log.Runs[0].Results = []sarifResult{}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

// buildRowMessage creates a text summary of a result row.
func buildRowMessage(columns []string, row []eval.Value) string {
	if len(columns) == 0 || len(row) == 0 {
		return "query result"
	}
	// Use first column value as the message.
	return eval.ValueToString(row[0])
}

// tryBuildLocation attempts to extract a SARIF location from well-known column names.
func tryBuildLocation(colIdx map[string]int, row []eval.Value) *sarifLocation {
	fileCol := -1
	for _, name := range []string{"file", "path", "filepath", "uri"} {
		if idx, ok := colIdx[name]; ok {
			fileCol = idx
			break
		}
	}
	if fileCol < 0 || fileCol >= len(row) {
		return nil
	}

	uri := eval.ValueToString(row[fileCol])
	loc := &sarifLocation{
		PhysicalLocation: &sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: uri},
		},
	}

	// Try to find line.
	lineCol := -1
	for _, name := range []string{"line", "startLine", "start_line"} {
		if idx, ok := colIdx[name]; ok {
			lineCol = idx
			break
		}
	}

	colCol := -1
	for _, name := range []string{"col", "column", "startCol", "start_col"} {
		if idx, ok := colIdx[name]; ok {
			colCol = idx
			break
		}
	}

	if lineCol >= 0 && lineCol < len(row) {
		region := &sarifRegion{}
		if iv, ok := row[lineCol].(eval.IntVal); ok {
			region.StartLine = int(iv.V)
		}
		if colCol >= 0 && colCol < len(row) {
			if iv, ok := row[colCol].(eval.IntVal); ok {
				// SARIF 2.1.0 requires 1-based column numbers; schema stores 0-based.
				region.StartColumn = int(iv.V) + 1
			}
		}
		if region.StartLine > 0 || region.StartColumn > 0 {
			loc.PhysicalLocation.Region = region
		}
	}

	return loc
}
