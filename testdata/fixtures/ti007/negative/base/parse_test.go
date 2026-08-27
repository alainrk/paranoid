package parser

import "testing"

func TestParse(t *testing.T) {
	if Parse("a=1") == nil {
		t.Error("Parse(a=1) should not be nil")
	}
	if Parse("") != nil {
		t.Error("Parse of empty input should be nil")
	}
}
