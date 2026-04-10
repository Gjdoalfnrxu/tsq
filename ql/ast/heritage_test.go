package ast

import "testing"

func TestMemberDefiningClass_DirectMember(t *testing.T) {
	classes := map[string]*ClassDecl{
		"Animal": {
			Name:    "Animal",
			Members: []MemberDecl{{Name: "speak"}},
		},
	}
	result := MemberDefiningClass(classes["Animal"], "speak", classes)
	if result == nil {
		t.Fatal("expected to find defining class")
	}
	if result.Name != "Animal" {
		t.Errorf("expected Animal, got %s", result.Name)
	}
}

func TestMemberDefiningClass_InheritedMember(t *testing.T) {
	classes := map[string]*ClassDecl{
		"Animal": {
			Name:    "Animal",
			Members: []MemberDecl{{Name: "speak"}},
		},
		"Dog": {
			Name:       "Dog",
			SuperTypes: []TypeRef{{Path: []string{"Animal"}}},
			Members:    []MemberDecl{{Name: "bark"}},
		},
	}
	// Dog.speak should be found in Animal
	result := MemberDefiningClass(classes["Dog"], "speak", classes)
	if result == nil {
		t.Fatal("expected to find defining class for speak")
	}
	if result.Name != "Animal" {
		t.Errorf("expected Animal, got %s", result.Name)
	}

	// Dog.bark should be found in Dog
	result = MemberDefiningClass(classes["Dog"], "bark", classes)
	if result == nil {
		t.Fatal("expected to find defining class for bark")
	}
	if result.Name != "Dog" {
		t.Errorf("expected Dog, got %s", result.Name)
	}
}

func TestMemberDefiningClass_NotFound(t *testing.T) {
	classes := map[string]*ClassDecl{
		"Animal": {
			Name:    "Animal",
			Members: []MemberDecl{{Name: "speak"}},
		},
	}
	result := MemberDefiningClass(classes["Animal"], "nonexistent", classes)
	if result != nil {
		t.Errorf("expected nil for nonexistent member, got %s", result.Name)
	}
}

func TestMemberDefiningClass_Nil(t *testing.T) {
	result := MemberDefiningClass(nil, "foo", nil)
	if result != nil {
		t.Error("expected nil for nil input")
	}
}

func TestMemberDefiningClass_CyclicInheritance(t *testing.T) {
	// A extends B, B extends A — should not infinite loop
	classes := map[string]*ClassDecl{
		"A": {
			Name:       "A",
			SuperTypes: []TypeRef{{Path: []string{"B"}}},
			Members:    []MemberDecl{{Name: "foo"}},
		},
		"B": {
			Name:       "B",
			SuperTypes: []TypeRef{{Path: []string{"A"}}},
		},
	}
	result := MemberDefiningClass(classes["B"], "foo", classes)
	if result == nil {
		t.Fatal("expected to find foo in A despite cycle")
	}
	if result.Name != "A" {
		t.Errorf("expected A, got %s", result.Name)
	}
}
