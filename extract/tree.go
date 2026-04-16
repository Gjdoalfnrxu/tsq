package extract

// tree.go provides shared ASTNode helpers used by both the scope analyser and
// the fact walker. Keeping them here avoids duplicate definitions and ensures
// any edge-case fixes apply consistently.

// ChildByField returns the first child of n with the given field name,
// or nil if no such child exists.
func ChildByField(n ASTNode, field string) ASTNode {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil && child.FieldName() == field {
			return child
		}
	}
	return nil
}
