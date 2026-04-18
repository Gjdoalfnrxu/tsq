package bridge

import "embed"

//go:embed tsq_base.qll tsq_functions.qll tsq_calls.qll tsq_variables.qll tsq_expressions.qll tsq_jsx.qll tsq_imports.qll tsq_errors.qll tsq_types.qll tsq_symbols.qll tsq_callgraph.qll tsq_dataflow.qll tsq_dataflow_track.qll tsq_summaries.qll tsq_composition.qll tsq_taint.qll tsq_express.qll tsq_react.qll tsq_node.qll compat_javascript.qll compat_dataflow.qll compat_tainttracking.qll compat_security_xss.qll compat_security_cmdi.qll compat_security_sqli.qll compat_security_pathtraversal.qll compat_dom.qll compat_crypto.qll compat_http.qll compat_io.qll compat_regexp.qll
var bridgeFS embed.FS

// LoadBridge returns all embedded .qll files as a map from filename to contents.
func LoadBridge() map[string][]byte {
	files := []string{
		"tsq_base.qll",
		"tsq_functions.qll",
		"tsq_calls.qll",
		"tsq_variables.qll",
		"tsq_expressions.qll",
		"tsq_jsx.qll",
		"tsq_imports.qll",
		"tsq_errors.qll",
		"tsq_types.qll",
		"tsq_symbols.qll",
		"tsq_callgraph.qll",
		"tsq_dataflow.qll",
		"tsq_dataflow_track.qll",
		"tsq_summaries.qll",
		"tsq_composition.qll",
		"tsq_taint.qll",
		"tsq_express.qll",
		"tsq_react.qll",
		"tsq_node.qll",
		"compat_javascript.qll",
		"compat_dataflow.qll",
		"compat_tainttracking.qll",
		"compat_security_xss.qll",
		"compat_security_cmdi.qll",
		"compat_security_sqli.qll",
		"compat_security_pathtraversal.qll",
		"compat_dom.qll",
		"compat_crypto.qll",
		"compat_http.qll",
		"compat_io.qll",
		"compat_regexp.qll",
	}
	result := make(map[string][]byte, len(files))
	for _, name := range files {
		data, err := bridgeFS.ReadFile(name)
		if err != nil {
			// Should never happen — embedded at compile time.
			panic("bridge: missing embedded file: " + name)
		}
		result[name] = data
	}
	return result
}

// ImportPathToFile maps QL import paths (e.g. "tsq::base") to their embedded
// .qll filenames. This is the single source of truth; cmd/tsq/main.go consumes it
// rather than maintaining a duplicate map.
var ImportPathToFile = map[string]string{
	"tsq::base":           "tsq_base.qll",
	"tsq::functions":      "tsq_functions.qll",
	"tsq::calls":          "tsq_calls.qll",
	"tsq::variables":      "tsq_variables.qll",
	"tsq::expressions":    "tsq_expressions.qll",
	"tsq::jsx":            "tsq_jsx.qll",
	"tsq::imports":        "tsq_imports.qll",
	"tsq::errors":         "tsq_errors.qll",
	"tsq::types":          "tsq_types.qll",
	"tsq::symbols":        "tsq_symbols.qll",
	"tsq::callgraph":      "tsq_callgraph.qll",
	"tsq::dataflow":       "tsq_dataflow.qll",
	"tsq::dataflow_track": "tsq_dataflow_track.qll",
	"tsq::summaries":      "tsq_summaries.qll",
	"tsq::composition":    "tsq_composition.qll",
	"tsq::taint":          "tsq_taint.qll",
	"tsq::express":        "tsq_express.qll",
	"tsq::react":          "tsq_react.qll",
	"tsq::node":           "tsq_node.qll",
	"javascript":          "compat_javascript.qll",
	"DataFlow::PathGraph": "compat_dataflow.qll",
	"TaintTracking":       "compat_tainttracking.qll",
	"semmle.javascript.security.dataflow.XssQuery":              "compat_security_xss.qll",
	"semmle.javascript.security.dataflow.CommandInjectionQuery": "compat_security_cmdi.qll",
	"semmle.javascript.security.dataflow.SqlInjectionQuery":     "compat_security_sqli.qll",
	"semmle.javascript.security.dataflow.PathTraversalQuery":    "compat_security_pathtraversal.qll",
	"semmle.javascript.security.dataflow.DomBasedXssQuery":      "compat_dom.qll",
	"semmle.javascript.security.CryptoLibraries":                "compat_crypto.qll",
	"semmle.javascript.frameworks.HTTP":                         "compat_http.qll",
	"semmle.javascript.security.dataflow.DatabaseAccess":        "compat_io.qll",
	"semmle.javascript.security.dataflow.FileSystemAccess":      "compat_io.qll",
	"semmle.javascript.security.dataflow.RegExpInjectionQuery":  "compat_regexp.qll",
}

// ImportLoader returns a function suitable for use as the importLoader
// parameter to resolve.Resolve. It checks the bridge embed first, returning
// the .qll source for known bridge paths. For unknown paths it returns nil.
//
// Usage in the pipeline:
//
//	bridgeFiles := bridge.LoadBridge()
//	loader := bridge.ImportLoader(bridgeFiles, parseFunc)
//	resolved, err := resolve.Resolve(mod, loader)
func ImportLoader(bridgeFiles map[string][]byte, parseFn func(src, file string) interface{}) func(path string) (interface{}, bool) {
	return func(path string) (interface{}, bool) {
		filename, ok := ImportPathToFile[path]
		if !ok {
			return nil, false
		}
		data, ok := bridgeFiles[filename]
		if !ok {
			return nil, false
		}
		return parseFn(string(data), filename), true
	}
}
