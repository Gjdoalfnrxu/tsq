// syntax_error.ts — intentional syntax errors
// tree-sitter produces ERROR nodes for unparseable regions but still
// returns a complete (partial) tree.

function valid() {
  return 1 + 2;
}

// Intentional syntax error: unexpected token
const broken = {
  key: ,
};

function alsoValid() {
  return "after error";
}

// Another error: unterminated expression
const x = (1 +;
