package parser

import "testing"

func TestParse(t *testing.T) {
	if testParseInput(t, "a=1") == nil {
		t.Error("Parse(a=1) should not be nil")
	}
	if Parse("") != nil {
		t.Error("Parse of empty input should be nil")
	}
}

func testParseInput(t *testing.T, s string) []string {
	t.Helper()
	return Parse(s)
}
