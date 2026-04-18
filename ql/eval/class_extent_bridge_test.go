package eval_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// TestClassExtent_BridgeTaintSink_EndToEnd parses the actual bridge
// `TaintSink` class shape (mirrored from bridge/tsq_taint.qll), runs it
// through the parse → resolve → desugar pipeline, then materialises
// against a populated `TaintSink/2` base relation. Asserts the head
// `TaintSink/1` ends up materialised with the correct projection.
//
// This is the end-to-end regression for the arity-shadow blocker:
// a name-keyed shadow check would silently skip materialisation here
// (head TaintSink/1 same name as base TaintSink/2). The desugar-side
// tag check at ql/desugar/class_extent_test.go only proves the rule
// is tagged; this test proves the eval side actually materialises it
// in the bridge shape.
func TestClassExtent_BridgeTaintSink_EndToEnd(t *testing.T) {
	src := `
class TaintSink extends @taint_sink {
    TaintSink() { TaintSink(this, _) }
    string toString() { result = "TaintSink" }
}
`
	p := parse.NewParser(src, "<test>")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	prog, errs := desugar.Desugar(rm)
	if len(errs) > 0 {
		t.Fatalf("desugar: %v", errs)
	}

	// Populate base TaintSink/2 (the schema-side relation backing
	// @taint_sink) with concrete tuples — same shape as the real
	// bridge fact loader produces.
	base2 := eval.NewRelation("TaintSink", 2)
	base2.Add(eval.Tuple{eval.IntVal{V: 1001}, eval.IntVal{V: 1}})
	base2.Add(eval.Tuple{eval.IntVal{V: 1002}, eval.IntVal{V: 2}})
	base2.Add(eval.Tuple{eval.IntVal{V: 1003}, eval.IntVal{V: 3}})
	base := map[string]*eval.Relation{"TaintSink": base2}

	mats, updates := eval.MaterialiseClassExtents(prog, base, nil, 0)

	got, ok := mats["TaintSink/1"]
	if !ok {
		t.Fatalf("bridge TaintSink class extent did NOT materialise; mats=%v", mats)
	}
	if got.Len() != 3 {
		t.Errorf("materialised TaintSink/1 len: want 3, got %d", got.Len())
	}
	if updates["TaintSink"] != 3 {
		t.Errorf("updates[TaintSink]: want 3, got %d", updates["TaintSink"])
	}
}

// TestClassExtent_BridgeSymbol_EndToEnd is the arity-4 sibling: the
// CodeQL `class Symbol extends @symbol { Symbol() { Symbol(this,_,_,_) } }`
// pattern. Same end-to-end shape as the TaintSink test.
func TestClassExtent_BridgeSymbol_EndToEnd(t *testing.T) {
	src := `
class Symbol extends @symbol {
    Symbol() { Symbol(this, _, _, _) }
    string toString() { result = "sym" }
}
`
	p := parse.NewParser(src, "<test>")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	prog, errs := desugar.Desugar(rm)
	if len(errs) > 0 {
		t.Fatalf("desugar: %v", errs)
	}

	base4 := eval.NewRelation("Symbol", 4)
	base4.Add(eval.Tuple{eval.IntVal{V: 1}, eval.IntVal{V: 0}, eval.IntVal{V: 0}, eval.IntVal{V: 0}})
	base4.Add(eval.Tuple{eval.IntVal{V: 2}, eval.IntVal{V: 0}, eval.IntVal{V: 0}, eval.IntVal{V: 0}})
	base := map[string]*eval.Relation{"Symbol": base4}

	mats, _ := eval.MaterialiseClassExtents(prog, base, nil, 0)

	got, ok := mats["Symbol/1"]
	if !ok {
		t.Fatalf("bridge Symbol class extent did NOT materialise; mats=%v", mats)
	}
	if got.Len() != 2 {
		t.Errorf("materialised Symbol/1 len: want 2, got %d", got.Len())
	}
}
