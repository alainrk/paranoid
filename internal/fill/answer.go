package fill

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/llm"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// ParseReply is ParseAnswer on a provider response. An empty answer cut
// off at the output cap gets an error that says so.
func ParseReply(resp llm.Response) (string, error) {
	if strings.TrimSpace(resp.Text) == "" && resp.HitOutputLimit() {
		return "", fmt.Errorf("the answer was empty: the model reached its output limit (%d tokens) before answering, probably while thinking", resp.OutputTokens)
	}
	return ParseAnswer(resp.Text)
}

// ParseAnswer reads the model's reply. The whole reply must be one fenced
// code block and nothing else. The fence is three or more backticks with
// an optional one-word language tag, and the closing fence is a line of
// at least as many backticks. It returns the text inside the fence.
func ParseAnswer(reply string) (string, error) {
	text := strings.TrimSpace(strings.ReplaceAll(reply, "\r\n", "\n"))
	if text == "" {
		return "", errors.New("the answer was empty")
	}
	lines := strings.Split(text, "\n")
	open := strings.TrimRight(lines[0], " \t")
	ticks := 0
	for ticks < len(open) && open[ticks] == '`' {
		ticks++
	}
	if ticks < 3 {
		return "", errors.New("the answer must be exactly one fenced code block, but it does not start with a fence")
	}
	info := strings.TrimSpace(open[ticks:])
	if strings.ContainsAny(info, "` \t") {
		return "", errors.New("the opening fence has more than a language tag after the backticks")
	}
	closeAt := -1
	for i := 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if len(l) >= ticks && strings.Trim(l, "`") == "" {
			closeAt = i
			break
		}
	}
	if closeAt < 0 {
		return "", errors.New("the code block is not closed (the answer may have been cut off)")
	}
	if closeAt != len(lines)-1 {
		return "", errors.New("the answer has text after the code block; it must be exactly one fenced code block")
	}
	body := strings.Join(lines[1:closeAt], "\n")
	if strings.TrimSpace(body) == "" {
		return "", errors.New("the code block is empty")
	}
	return body, nil
}

// normalizeProof turns the code block into the text that replaces the
// proof span. Lean: the term or "by" block, with a leading ":=" dropped
// and one space in front, so it follows the existing ":=". Rocq: the
// block as written, which starts at "Proof.".
func normalizeProof(lang fparse.Lang, block string) (string, error) {
	s := strings.TrimSpace(block)
	if lang == fparse.LangLean {
		s = strings.TrimSpace(strings.TrimPrefix(s, ":="))
	}
	if s == "" {
		return "", errors.New("the proof is empty")
	}
	if lang == fparse.LangLean {
		return " " + s, nil
	}
	return s, nil
}

// fenceFor returns a backtick fence longer than any run of backticks in
// text, so the text can sit inside it.
func fenceFor(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// langTag is the code fence tag for a language.
func langTag(lang fparse.Lang) string {
	if lang == fparse.LangLean {
		return "lean"
	}
	return "coq"
}

// ProjectNote tells the model which Lean toolchain the project uses and
// whether Mathlib is there. Without it, models often reach for Mathlib
// lemma names in a project that has only core Lean. It is empty for
// Rocq and when the toolchain is unknown.
func ProjectNote(p sandbox.ProofProject) string {
	if p.Kind != sandbox.KindLean || p.Toolchain == "" {
		return ""
	}
	note := "\nThe project uses the Lean toolchain " + p.Toolchain + "."
	if p.Mathlib {
		return note + " Mathlib is available.\n"
	}
	return note + " Mathlib is not available: use only names from core Lean and its standard library, and check that each lemma exists in that version.\n"
}

// SystemPrompt is the system prompt for a language.
func SystemPrompt(lang fparse.Lang) string {
	var shape string
	if lang == fparse.LangLean {
		shape = "the Lean 4 term or `by` block that goes right after the `:=` of the theorem"
	} else {
		shape = "the Rocq proof from `Proof.` through the closing `Qed.`"
	}
	name := "Rocq"
	if lang == fparse.LangLean {
		name = "Lean 4"
	}
	return "You write proofs for the " + name + " proof assistant. " +
		"You are given one file and one theorem in it whose proof is a hole.\n" +
		"Rules:\n" +
		"- Never change the statement of any theorem or lemma.\n" +
		"- Never use sorry, admit, Admitted, give_up, or any axiom. Never use native_decide, implemented_by, unsafe, ofReduceBool, or any other escape hatch. Never turn off a checker.\n" +
		"- Do not change anything outside the proof of the named theorem.\n" +
		"- Answer with the full replacement proof: " + shape + ".\n" +
		"- Output exactly one fenced code block and no other text."
}

// fixedWidth matches Lean's fixed-width integer types.
var fixedWidth = regexp.MustCompile(`\bU?Int(8|16|32|64)\b`)

// fixedWidthTip is added for Lean files that use fixed-width integers.
// Models get stuck on these types without it. The lemma names were
// checked on Lean v4.24.0.
const fixedWidthTip = "\nTip: for Int64 and the other signed fixed-width types, rewrite comparisons with " +
	"Int64.lt_iff_toInt_lt and Int64.le_iff_toInt_le (the same names exist for Int8 to Int32), then use omega. " +
	"For UInt64 and the other unsigned types, use UInt64.lt_iff_toNat_lt and UInt64.le_iff_toNat_le. " +
	"Unfold the definition and split each if first.\n"

// Prompt builds the user message for one attempt. prevProof and
// prevError are empty on the first attempt.
func Prompt(lang fparse.Lang, file string, src []byte, d fparse.Decl, prevProof, prevError string) string {
	var b strings.Builder
	tag := langTag(lang)
	f := fenceFor(string(src))
	fmt.Fprintf(&b, "File: %s\n\nWhole file:\n%s%s\n%s\n%s\n\n", file, f, tag, strings.TrimRight(string(src), "\n"), f)
	fmt.Fprintf(&b, "Theorem to prove: %s (%s, line %d)\n", d.Name, d.Kind, d.Line)
	fmt.Fprintf(&b, "Statement (do not change it): %s %s %s\n\n", d.Kind, d.Name, d.Statement)
	cur := string(src[d.ProofStart:d.ProofEnd])
	cf := fenceFor(cur)
	fmt.Fprintf(&b, "Current proof (it has a hole):\n%s%s\n%s\n%s\n", cf, tag, strings.TrimSpace(cur), cf)
	if lang == fparse.LangLean && fixedWidth.Match(src) {
		b.WriteString(fixedWidthTip)
	}
	if prevError != "" {
		if prevProof != "" {
			pf := fenceFor(prevProof)
			fmt.Fprintf(&b, "\nYour previous attempt:\n%s%s\n%s\n%s\n", pf, tag, strings.TrimSpace(prevProof), pf)
		}
		ef := fenceFor(prevError)
		fmt.Fprintf(&b, "\nThe attempt was rejected. Latest checker output:\n%s\n%s\n%s\n", ef, strings.TrimSpace(prevError), ef)
		b.WriteString("\nWrite a corrected full replacement proof.\n")
	} else {
		b.WriteString("\nWrite the full replacement proof.\n")
	}
	return b.String()
}
