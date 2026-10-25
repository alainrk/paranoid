package prove

import (
	"fmt"
	"strings"

	"github.com/alainrk/paranoid/internal/difftest"
	"github.com/alainrk/paranoid/internal/hotspot"
)

// SystemPrompt is the system message of the draft step.
func SystemPrompt() string {
	return `You translate one function from Go, Python, or TypeScript into a Lean 4 model, and you state a few theorems about the model.
The model is run on thousands of generated inputs and compared with the real function. A model that disagrees with the real code anywhere is wrong, even if it looks nicer.
Rules:
- Output exactly one fenced code block with the language tag lean, and no other text.
- The file has this shape: optional import lines, then "namespace <Identifier>", then "def model ...", then 2 to 5 theorems, then "end <Identifier>". Nothing else may be outside the namespace.
- "def model" must have EXACTLY the signature you are given. Do not rename parameters, change types, add binders, or add implicit arguments. Write the body after ":=".
- The model must be complete, total, and executable. Use plain recursion with a termination proof, "Id.run do" loops, or Array and List functions. Do not use partial, opaque, or noncomputable. Do not use sorry in the model or in any helper.
- Helper definitions are allowed between "def model" and the first theorem only if they are complete too. Prefer no helpers.
- Every theorem must be about the model: its statement must mention "model". State real properties (what the function computes, how it relates to its inputs, bounds, symmetry, monotonicity). Do not write tautologies or statements that ignore the model.
- Before each theorem write a doc comment with exactly one plain-English sentence: /-- ... -/. Say what the theorem means to a reader who does not know Lean.
- Every theorem proof must be exactly "by sorry". A later step fills the proofs in.
- Never use axiom, native_decide, implemented_by, extern, unsafe, ofReduceBool, set_option, macros, notation, syntax, attributes, or any command that starts with #. Never turn off a checker.
- Do not import Mathlib. The core library is enough.
- Use only the types in the signature. Fixed-width integer types (Int8 to Int64, UInt8 to UInt64) are in the Lean core library.`
}

// typeNotes explains how the source language's types and operators map
// to Lean, for the language of h.
func typeNotes(h *hotspot.Hotspot) string {
	var b strings.Builder
	b.WriteString("How the types behave (the model must behave the same way):\n")
	switch h.Lang {
	case hotspot.LangGo:
		b.WriteString("- Go fixed-width integers (int, int8 to int64, uint, uint8 to uint64, rune, byte) wrap around on overflow, like the Lean types Int8 to Int64 and UInt8 to UInt64. Go int and uint are 64 bits. Do not widen to Int unless you convert back with the same wrapping.\n")
		b.WriteString("- Go integer division and remainder round toward zero. For Lean Int64 and the other fixed-width types, / and % do the same. Division by zero is out of scope.\n")
		b.WriteString("- A Go string is a sequence of bytes. len(s) is the byte count, which is s.utf8ByteSize in Lean. Indexing s[i] is a byte, which you get from s.toUTF8. Ranging over a string yields code points (Lean Char). The generated test strings are valid UTF-8.\n")
		b.WriteString("- A Go slice maps to Lean Array. A nil slice and an empty slice are the same.\n")
	case hotspot.LangPython:
		b.WriteString("- A Python int has no size limit. It maps to the Lean Int, which also has no limit. Nothing wraps.\n")
		b.WriteString("- Python // and % round toward negative infinity (the result of % has the sign of the divisor). In Lean use Int.fdiv and Int.fmod for these. Lean's / and % on Int round differently.\n")
		b.WriteString("- A Python str is a sequence of code points. len(s) is s.length in Lean. Indexing and slicing work on code points.\n")
		b.WriteString("- A Python list maps to Lean Array. bool maps to Bool.\n")
	case hotspot.LangTS:
		b.WriteString("- A TypeScript number is treated as an integer in the safe range, -(2^53-1) to 2^53-1. It maps to the Lean Int, which has no limit. The test inputs stay in the safe range.\n")
		b.WriteString("- JavaScript / on numbers is not integer division. Only write a model if the function uses integer operations. For truncating division use Int.tdiv and Int.tmod; for flooring use Int.fdiv and Int.fmod, matching what the code does (for example Math.floor or Math.trunc).\n")
		b.WriteString("- A TypeScript string is a sequence of UTF-16 code units. s.length counts code units, so a character outside the basic plane counts as 2.\n")
		b.WriteString("- A TypeScript array maps to Lean Array.\n")
	}
	b.WriteString("- Lean String is a sequence of Unicode characters. Lean Bool is true or false. Lean Array is a growable array with the same order as the source list.\n")
	return b.String()
}

// DraftPrompt is the first user message for a hotspot.
func DraftPrompt(h *hotspot.Hotspot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Hotspot: %s (%s, lines %d to %d)\n\n", h.Spec, h.Lang, h.StartLine, h.EndLine)
	src := strings.TrimRight(h.Source, "\n")
	f := fence(src)
	fmt.Fprintf(&b, "Function source:\n%s%s\n%s\n%s\n\n", f, string(h.Lang), src, f)
	fmt.Fprintf(&b, "Lean namespace: %s\n", h.Identifier)
	fmt.Fprintf(&b, "Required model signature (exactly, then \":=\" and your body):\n    %s\n\n", h.LeanSignature)
	b.WriteString("Parameters:\n")
	for _, p := range h.Params {
		fmt.Fprintf(&b, "- %s: %s in the source, %s in Lean (binder name %s)\n", p.Name, p.Type.Src, p.Type.Lean(), p.LeanName)
	}
	fmt.Fprintf(&b, "Result: %s in the source, %s in Lean\n\n", h.Result.Src, h.Result.Lean())
	b.WriteString(typeNotes(h))
	fmt.Fprintf(&b, "\nWrite the file now, in this shape:\n\n%slean\nnamespace %s\n\n%s :=\n  -- body\n\n/-- One plain-English sentence. -/\ntheorem some_property (...) : ... model ... := by\n  sorry\n\n/-- Another sentence. -/\ntheorem another_property (...) : ... model ... := by\n  sorry\n\nend %s\n%s\n", "```", h.Identifier, h.LeanSignature, h.Identifier, "```")
	return b.String()
}

// RejectedPrompt is the follow-up message after a rejected attempt.
// problem is the validation error or the build output.
func RejectedPrompt(kind, problem string) string {
	f := fence(problem)
	return fmt.Sprintf("Your file was rejected: %s.\n%s\n%s\n%s\n\nWrite the corrected full file as one fenced lean block and no other text.", kind, f, strings.TrimSpace(problem), f)
}

// MismatchPrompt is the follow-up message when the model and the code
// disagree. It lists the first inputs that differ.
func MismatchPrompt(r difftest.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Your file builds, but the model does not match the real code. %s\n", strings.TrimPrefix(difftest.Summary(r, false), "Not proved. "))
	b.WriteString("Each input below is the JSON array of the function's arguments, in order. The outputs are the values as JSON.\n")
	for i, m := range r.Mismatches {
		fmt.Fprintf(&b, "%d. input %s\n   real code: %s\n   your model: %s\n", i+1, m.Input, m.Code, m.Model)
	}
	for i, m := range r.ModelErrors {
		fmt.Fprintf(&b, "model gave no answer %d. input %s: %s\n", i+1, m.Input, m.Message)
	}
	b.WriteString("\nFix the model so it gives the real code's answer on these inputs and on all others. Keep the same signature. Write the corrected full file as one fenced lean block and no other text.")
	return b.String()
}

// fence returns a backtick fence longer than any run in text.
func fence(text string) string {
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
