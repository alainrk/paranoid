package parser

import "testing"

func TestFieldsBasic(t *testing.T) {
	got := Fields("a b c")
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	if got[0] != "a" {
		t.Errorf("first = %q, want a", got[0])
	}
}

func TestFieldsEmpty(t *testing.T) {
	if len(Fields("")) != 0 {
		t.Errorf("empty line should have no fields")
	}
	if len(Fields("   ")) != 0 {
		t.Errorf("blank line should have no fields")
	}
}

func TestStripComment(t *testing.T) {
	if StripComment("a # b") != "a " {
		t.Errorf("comment not stripped")
	}
}
