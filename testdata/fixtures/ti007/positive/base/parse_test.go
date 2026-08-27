package parser

import "testing"

func TestParse(t *testing.T) {
	if Parse("a=1") == nil {
		t.Error("Parse(a=1) should not be nil")
	}
}

func TestFields(t *testing.T) {
	if len(Fields("a b")) != 2 {
		t.Error("two fields expected")
	}
	if len(Fields("")) != 0 {
		t.Error("no fields expected")
	}
	if len(Fields("a")) != 1 {
		t.Error("one field expected")
	}
	if len(Fields("a b c")) != 3 {
		t.Error("three fields expected")
	}
}
