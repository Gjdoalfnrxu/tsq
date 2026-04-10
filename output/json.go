package output

import (
	"encoding/json"
	"io"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// WriteJSONLines writes one JSON object per result row to w.
// Each object maps column names to values.
func WriteJSONLines(w io.Writer, rs *eval.ResultSet) error {
	enc := json.NewEncoder(w)
	// Do not escape HTML entities — plain JSON.
	enc.SetEscapeHTML(false)

	for _, row := range rs.Rows {
		obj := make(map[string]interface{}, len(rs.Columns))
		for i, col := range rs.Columns {
			if i >= len(row) {
				obj[col] = nil
				continue
			}
			switch v := row[i].(type) {
			case eval.IntVal:
				obj[col] = v.V
			case eval.StrVal:
				obj[col] = v.V
			default:
				obj[col] = eval.ValueToString(row[i])
			}
		}
		if err := enc.Encode(obj); err != nil {
			return err
		}
	}
	return nil
}
