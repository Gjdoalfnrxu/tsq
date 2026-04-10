package bridge

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/schema"
)

// TestBridgeFilesNotEmpty verifies all .qll files contain content.
func TestBridgeFilesNotEmpty(t *testing.T) {
	files := LoadBridge()
	for name, data := range files {
		if len(data) == 0 {
			t.Errorf("bridge file %q is empty", name)
		}
	}
}

// TestBridgeFilesParseBasicStructure does a lightweight structural parse
// of each .qll file to verify they contain valid-looking QL class declarations.
func TestBridgeFilesParseBasicStructure(t *testing.T) {
	classRe := regexp.MustCompile(`(?m)^class\s+(\w+)\s+extends\s+`)
	predicateRe := regexp.MustCompile(`(?m)^\s+(string|int|predicate|ASTNode|File|Call|JsxElement|Function|Parameter|CallArg|ParameterRest|ParameterOptional|ParamIsFunctionType|CallArgSpread|VarDecl|Assign|ExprMayRef|ExprIsCall|FieldRead|FieldWrite|Await|Cast|DestructureField|ArrayDestructure|DestructureRest|JsxAttribute|ImportBinding|ExportBinding|ExtractError|SchemaVersion|Contains)\s+\w+\(`)

	files := LoadBridge()
	for name, data := range files {
		src := string(data)
		classes := classRe.FindAllStringSubmatch(src, -1)
		if len(classes) == 0 {
			t.Errorf("bridge file %q contains no class declarations", name)
		}
		predicates := predicateRe.FindAllString(src, -1)
		if len(predicates) == 0 {
			t.Errorf("bridge file %q contains no member declarations", name)
		}
	}
}

// TestBridgeClassesReferenceValidRelations verifies that the relation names
// used in characteristic predicates correspond to registered schema relations
// (lowercased, with underscores matching the snake_case convention).
func TestBridgeClassesReferenceValidRelations(t *testing.T) {
	// Build a set of valid relation names in lowercase/snake_case.
	validRelations := make(map[string]bool)
	for _, rel := range schema.Registry {
		validRelations[toSnakeCase(rel.Name)] = true
	}

	// Regex to find characteristic predicate calls like: node(this, _, _, ...)
	charPredRe := regexp.MustCompile(`(?m)^\s+\w+\(\)\s*\{\s*(\w+)\(this`)

	files := LoadBridge()
	for name, data := range files {
		src := string(data)
		matches := charPredRe.FindAllStringSubmatch(src, -1)
		for _, m := range matches {
			relName := m[1]
			if !validRelations[relName] {
				t.Errorf("bridge file %q references unknown relation %q in characteristic predicate", name, relName)
			}
		}
	}
}

// TestBridgeRelationArities checks that the number of underscore/variable
// arguments in characteristic predicates matches the schema relation arity.
func TestBridgeRelationArities(t *testing.T) {
	// Build arity map from schema.
	arities := make(map[string]int)
	for _, rel := range schema.Registry {
		arities[toSnakeCase(rel.Name)] = rel.Arity()
	}

	// Regex to find characteristic predicate bodies: relation_name(this, _, _, ...)
	charPredRe := regexp.MustCompile(`(?m)^\s+\w+\(\)\s*\{\s*(\w+)\(([^)]+)\)`)

	files := LoadBridge()
	for name, data := range files {
		src := string(data)
		matches := charPredRe.FindAllStringSubmatch(src, -1)
		for _, m := range matches {
			relName := m[1]
			args := m[2]
			expectedArity, ok := arities[relName]
			if !ok {
				continue // reported by TestBridgeClassesReferenceValidRelations
			}
			actualArity := len(strings.Split(args, ","))
			if actualArity != expectedArity {
				t.Errorf("bridge file %q: relation %q has arity %d in schema but %d args in characteristic predicate",
					name, relName, expectedArity, actualArity)
			}
		}
	}
}

// TestBridgeNoDataFlowClasses ensures we fail-closed: no DataFlow or
// TaintTracking classes in the bridge.
func TestBridgeNoDataFlowClasses(t *testing.T) {
	forbidden := []string{"DataFlow", "TaintTracking", "TaintStep", "PathGraph"}
	files := LoadBridge()
	for name, data := range files {
		src := string(data)
		for _, kw := range forbidden {
			if strings.Contains(src, "class "+kw) {
				t.Errorf("bridge file %q contains forbidden class %q — fail-closed: no data flow in v1", name, kw)
			}
		}
	}
}

// toSnakeCase converts PascalCase to snake_case.
func toSnakeCase(s string) string {
	var result []byte
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, byte(c+'a'-'A'))
		} else {
			result = append(result, byte(c))
		}
	}
	return string(result)
}
