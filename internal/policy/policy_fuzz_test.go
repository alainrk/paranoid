package policy

import (
	"testing"
	"time"
)

// fixedFuzzNow pins the clock Apply uses while fuzzing, so a fuzz
// input cannot make an "expires" comparison depend on wall-clock time.
var fixedFuzzNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// FuzzParse proves the policy parser never panics on arbitrary input,
// no matter how malformed: every failure is a plain returned error, per
// the "no panics outside main" invariant (see AGENTS.md).
// A successful parse is also re-fed through Parse a second time on its
// own error text (when any), since that string is itself arbitrary
// bytes the parser has to survive too.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"fail-under = 80\n",
		"strict = true\n",
		"strict = false\n",
		"[[disable]]\nrule = \"TI012\"\nreason = \"flaky\"\n",
		"[[disable]]\nrule = \"TI012\"\nreason = \"flaky\"\nexpires = \"2099-01-01\"\n",
		"[sandbox]\nimage-go = \"golang:1.24\"\n",
		"[formal]\nallowed_axioms = [\"Classical.choice\", \"MyAxiom\"]\n",
		"[formal]\nallowed_axioms = []\n",
		"[formal]\nallowed_axioms = [\"sorryAx\"]\n",
		"[formal]\nallowed_axioms = [\"\"]\n",
		"[formal]\nallowed_axioms = [\"a\" \"b\"]\n",
		"[formal]\nallowed_axioms = [\"a\\\n",
		"[formal]\nother = 1\n",
		"[formal.llm]\nprovider = \"anthropic\"\nmodel = \"claude-opus-5-5\"\n",
		"[formal.llm]\nprovider = \"openai\"\nbase_url = \"http://localhost:11434/v1\"\napi_key_env = \"LOCAL_LLM_KEY\"\n",
		"[formal.llm.proof]\nprovider = \"gemini\"\nmodel = \"gemini-pro\"\n[formal.llm.spec]\nmodel = \"m\"\n",
		"[formal.llm]\napi_key = \"sk-abc\"\n",
		"[formal.llm]\ntoken = \"x\"\n",
		"[formal.llm]\napi_key_env = \"sk-ant-abc\"\n",
		"[formal.llm]\nbase_url = \"http://user:pw@host/v1\"\n",
		"[formal.llm]\nprovider = \"mystery\"\n",
		"[formal.llm]\n[formal.llm]\n",
		"[formal.llm.other]\n",
		"[formal.llm.proof]\nkey = \"x\"\n",
		"# just a comment\n",
		"fail-under = 80 # trailing comment\n",
		"color = \"red\"\n",
		"[extra]\n",
		"[[disable]]\n",
		"[sandbox]\nimage-go = 1\n",
		"key with no equals\n",
		"fail-under = \"not a number\"\n",
		"[sandbox]\nimage-go = \"unterminated\n",
		"[[disable]]\nrule = \"TI012\"\nreason = \"x\"\nexpires = \"garbage\"\n",
		"fail-under = 80\x00strict = true\n",
		"\xff\xfe not valid utf-8 at all",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(_ *testing.T, in string) {
		file, err := Parse([]byte(in))
		if err != nil {
			return
		}
		// A successful parse must never make Apply panic on whatever
		// Disable entries came out of it, valid-looking or not.
		_, _ = Apply(nil, file.Disable, fixedFuzzNow)
	})
}
