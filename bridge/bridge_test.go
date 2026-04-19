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
	classRe := regexp.MustCompile(`(?m)^\s*(?:abstract\s+)?class\s+(\w+)\s+extends\s+`)
	predicateRe := regexp.MustCompile(`(?m)^\s+(?:override\s+)?(string|int|predicate|ASTNode|File|Call|JsxElement|Function|Parameter|CallArg|ParameterRest|ParameterOptional|ParameterDestructured|ParamIsFunctionType|CallArgSpread|VarDecl|Assign|ExprMayRef|ExprIsCall|FieldRead|FieldWrite|Await|Cast|DestructureField|ArrayDestructure|DestructureRest|JsxAttribute|ImportBinding|ExportBinding|ExtractError|SchemaVersion|Contains)\s+\w+\(`)

	// Predicate-only bridge files: pure rule libraries with no class wrappers.
	// tsq_valueflow.qll (Phase A) is the first of these — it exposes a set of
	// named predicates (`mayResolveTo`, `mayResolveToBase`, ...) consumed by
	// other bridge files and queries directly. Adding a vacuous class wrapper
	// would buy nothing and obscure intent. Skip the class-presence assertion
	// for these files but still require they parse and contain top-level
	// `predicate` declarations.
	predicateOnlyFiles := map[string]bool{
		"tsq_valueflow.qll": true,
	}
	topLevelPredRe := regexp.MustCompile(`(?m)^predicate\s+\w+\(`)

	files := LoadBridge()
	for name, data := range files {
		src := string(data)
		if predicateOnlyFiles[name] {
			if !topLevelPredRe.MatchString(src) {
				t.Errorf("predicate-only bridge file %q contains no top-level predicate declarations", name)
			}
			continue
		}
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
// (PascalCase matching the schema registry).
func TestBridgeClassesReferenceValidRelations(t *testing.T) {
	// Build a set of valid relation names in PascalCase.
	validRelations := make(map[string]bool)
	for _, rel := range schema.Registry {
		validRelations[rel.Name] = true
	}

	// Regex to find characteristic predicate calls like: Node(this, _, _, ...)
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
	// Build arity map from schema (PascalCase keys).
	arities := make(map[string]int)
	for _, rel := range schema.Registry {
		arities[rel.Name] = rel.Arity()
	}

	// Regex to find characteristic predicate bodies: RelationName(this, _, _, ...)
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

// TestBridgeNoTaintStepClass ensures we fail-closed: no TaintStep
// classes in the bridge (TaintTracking is now allowed via compat_tainttracking.qll).
func TestBridgeNoTaintStepClass(t *testing.T) {
	forbidden := []string{"TaintStep"}
	files := LoadBridge()
	for name, data := range files {
		src := string(data)
		for _, kw := range forbidden {
			if strings.Contains(src, "class "+kw) {
				t.Errorf("bridge file %q contains forbidden class %q — fail-closed: not yet implemented", name, kw)
			}
		}
	}
}
