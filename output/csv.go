package output

import (
	"encoding/csv"
	"io"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// WriteCSV writes the ResultSet as CSV with a header row to w.
// Uses standard CSV encoding (RFC 4180): fields containing commas, quotes,
// or newlines are quoted, and embedded quotes are doubled.
func WriteCSV(w io.Writer, rs *eval.ResultSet) error {
	cw := csv.NewWriter(w)

	// Write header.
	if err := cw.Write(rs.Columns); err != nil {
		return err
	}

	// Write data rows.
	record := make([]string, len(rs.Columns))
	for _, row := range rs.Rows {
		for i := range rs.Columns {
			if i < len(row) {
				record[i] = eval.ValueToString(row[i])
			} else {
				record[i] = ""
			}
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}

	cw.Flush()
	return cw.Error()
}
