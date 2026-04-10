package ast

// MemberDefiningClass walks the supertype chain from cd to find the ClassDecl
// that directly declares a member named name. The classes map provides the
// class hierarchy (typically resolve.Environment.Classes).
//
// This is a shared utility extracted from both resolve.go and desugar.go,
// which previously had duplicate implementations (connascence of algorithm).
func MemberDefiningClass(cd *ClassDecl, name string, classes map[string]*ClassDecl) *ClassDecl {
	if cd == nil {
		return nil
	}
	visited := make(map[string]bool)
	return memberDefiningClassRec(cd, name, classes, visited)
}

func memberDefiningClassRec(cd *ClassDecl, name string, classes map[string]*ClassDecl, visited map[string]bool) *ClassDecl {
	if cd == nil || visited[cd.Name] {
		return nil
	}
	visited[cd.Name] = true
	for i := range cd.Members {
		if cd.Members[i].Name == name {
			return cd
		}
	}
	for _, st := range cd.SuperTypes {
		if superCD, ok := classes[st.String()]; ok {
			if found := memberDefiningClassRec(superCD, name, classes, visited); found != nil {
				return found
			}
		}
	}
	return nil
}
