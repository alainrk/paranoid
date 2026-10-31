package fill

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	fparse "github.com/alainrk/paranoid/internal/formal"
	"github.com/alainrk/paranoid/internal/llm"
	"github.com/alainrk/paranoid/internal/sandbox"
)

// scriptedProvider answers from a list and records every request.
type scriptedProvider struct {
	replies []string
	err     error // returned when the replies run out
	reqs    []llm.Request
	stops   []string // optional stop reason per reply
}

func (p *scriptedProvider) Name() string { return "fake" }

func (p *scriptedProvider) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	p.reqs = append(p.reqs, req)
	if len(p.reqs) > len(p.replies) {
		if p.err != nil {
			return llm.Response{}, p.err
		}
		return llm.Response{}, errors.New("the script ran out of replies")
	}
	i := len(p.reqs) - 1
	resp := llm.Response{Text: p.replies[i]}
	if i < len(p.stops) {
		resp.StopReason = p.stops[i]
	}
	return resp, nil
}

// fakeChecker stands in for the toolchain. fn sees the file content.
type fakeChecker struct {
	fn    func(content string) (string, int, error)
	files []string
}

func (c *fakeChecker) Available() error { return nil }

func (c *fakeChecker) Check(_ context.Context, file string) (string, int, error) {
	raw, err := os.ReadFile(file) // #nosec G304 -- test temp path
	if err != nil {
		return "", -1, err
	}
	c.files = append(c.files, string(raw))
	return c.fn(string(raw))
}

// cleanLean passes every file and reports the audited names as closed.
func cleanLean(content string) (string, int, error) {
	return auditAnswer(content, "does not depend on any axioms"), 0, nil
}

// auditAnswer prints an answer line for each "#print axioms NAME" in
// the file. tail is the text after the quoted name.
func auditAnswer(content, tail string) string {
	var out strings.Builder
	for _, l := range strings.Split(content, "\n") {
		if name, ok := strings.CutPrefix(l, "#print axioms "); ok {
			out.WriteString("'" + name + "' " + tail + "\n")
		}
	}
	return out.String()
}

const leanSrc = `namespace MyList

theorem rev_rev (l : List Nat) : l.reverse.reverse = l := by
  simp

theorem helper (n : Nat) : n = n := by
  sorry

theorem other (n : Nat) : n + 0 = n := by
  simp

end MyList
`

func fence(body string) string { return "```lean\n" + body + "\n```" }

func newRunner(p llm.Provider, c Checker) *Runner {
	return &Runner{
		Project:  sandbox.ProofProject{Kind: sandbox.KindLean},
		Provider: p, Checker: c, Attempts: 1, TempDir: os.TempDir(),
	}
}

func TestFillAccepts(t *testing.T) {
	prov := &scriptedProvider{replies: []string{fence("by\n  rfl")}}
	chk := &fakeChecker{fn: cleanLean}
	r := newRunner(prov, chk)
	var written string
	r.Write = func(_ string, content []byte) error { written = string(content); return nil }

	got, results, err := r.Fill(context.Background(), "Proofs/Rev.lean", []byte(leanSrc), nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, Filled, results[0].Status, results[0].Detail)
	require.Equal(t, "MyList.helper", results[0].Name)
	require.Equal(t, 1, results[0].Attempts)
	want := strings.Replace(leanSrc, " by\n  sorry", " by\n  rfl", 1)
	require.Equal(t, want, string(got))
	require.Equal(t, want, written)
	// One run for the file, one for the file with the audit lines.
	require.Len(t, chk.files, 2)
	require.Contains(t, chk.files[1], "#print axioms MyList.helper")
	// The scratch copy is not the real file.
	require.Contains(t, prov.reqs[0].System, "Never change the statement")
	require.Contains(t, prov.reqs[0].Messages[0].Text, "Whole file:")
	require.Contains(t, prov.reqs[0].Messages[0].Text, "MyList.helper")
}

func TestFillGateRejections(t *testing.T) {
	macroSrc := strings.Replace(leanSrc, "namespace MyList\n", "macro \"cheat\" : tactic => `(tactic| exact sorryAx _)\n\nnamespace MyList\n", 1)
	tests := []struct {
		name   string
		src    string
		reply  string
		fn     func(string) (string, int, error)
		extra  []string
		status Status
		detail string
	}{
		{
			name: "checker fails", src: leanSrc, reply: fence("by\n  omega"),
			fn: func(string) (string, int, error) {
				return "Proofs/Rev.lean:6:2: error: omega could not prove the goal", 1, nil
			},
			status: Failed, detail: "omega could not prove the goal",
		},
		{
			name: "not a fenced block", src: leanSrc, reply: "by\n  rfl",
			fn: cleanLean, status: Failed, detail: "fence",
		},
		{
			name: "text around the block", src: leanSrc, reply: "Here you go:\n" + fence("by\n  rfl"),
			fn: cleanLean, status: Failed, detail: "fence",
		},
		{
			name: "hole left in the proof", src: leanSrc, reply: fence("by\n  sorry"),
			fn: cleanLean, status: Failed, detail: "still has a hole",
		},
		{
			name: "admit tactic", src: leanSrc, reply: fence("by\n  admit"),
			fn: cleanLean, status: Failed, detail: "still has a hole",
		},
		{
			name: "adds an axiom in the answer", src: leanSrc, reply: fence("by\n  exact magic\n\naxiom magic : 1 = 2"),
			fn: cleanLean, status: Failed, detail: "extra commands",
		},
		{
			name: "native_decide is an escape", src: leanSrc, reply: fence("by\n  native_decide"),
			fn: cleanLean, status: Failed, detail: "native_decide",
		},
		{
			name: "turns the kernel check off", src: leanSrc, reply: fence("by\n  set_option debug.skipKernelTC true in\n  rfl"),
			fn: cleanLean, status: Failed, detail: "FV006",
		},
		{
			name: "adds a declaration", src: leanSrc, reply: fence("by\n  rfl\n\ntheorem extra : True := trivial"),
			fn: cleanLean, status: Failed, detail: "number of declarations",
		},
		{
			name: "swallows the neighbour in a comment", src: leanSrc, reply: fence("by\n  rfl\n/-"),
			fn: cleanLean, status: Failed, detail: "number of declarations",
		},
		{
			name: "sorry hidden in a macro, caught by the audit", src: macroSrc, reply: fence("by\n  cheat"),
			fn: func(c string) (string, int, error) {
				return auditAnswer(c, "depends on axioms: [sorryAx]"), 0, nil
			},
			status: Failed, detail: "sorryAx",
		},
		{
			name: "axiom outside the allowed list", src: leanSrc, reply: fence("by\n  rfl"),
			fn: func(c string) (string, int, error) {
				return auditAnswer(c, "depends on axioms: [propext, Mine.axiom]"), 0, nil
			},
			status: Failed, detail: "Mine.axiom",
		},
		{
			name: "axiom allowed by the policy list", src: leanSrc, reply: fence("by\n  rfl"),
			fn: func(c string) (string, int, error) {
				return auditAnswer(c, "depends on axioms: [propext, Mine.axiom]"), 0, nil
			},
			extra: []string{"Mine.axiom"}, status: Filled,
		},
		{
			name: "sorryAx is never allowed, even when listed", src: leanSrc, reply: fence("by\n  rfl"),
			fn: func(c string) (string, int, error) {
				return auditAnswer(c, "depends on axioms: [sorryAx]"), 0, nil
			},
			extra: []string{"sorryAx"}, status: Failed, detail: "sorryAx",
		},
		{
			name: "kernel gives no answer", src: leanSrc, reply: fence("by\n  rfl"),
			fn: func(c string) (string, int, error) {
				if strings.Contains(c, "#print axioms") {
					return "error: unknown constant 'MyList.helper'", 0, nil
				}
				return "", 0, nil
			},
			status: Failed, detail: "no axiom answer",
		},
		{
			name: "audit run fails", src: leanSrc, reply: fence("by\n  rfl"),
			fn: func(c string) (string, int, error) {
				if strings.Contains(c, "#print axioms") {
					return "error: boom", 1, nil
				}
				return "", 0, nil
			},
			status: Failed, detail: "did not run",
		},
		{
			name: "a leading := is tolerated", src: leanSrc, reply: fence(":= by\n  rfl"),
			fn: cleanLean, status: Filled,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunner(&scriptedProvider{replies: []string{tc.reply}}, &fakeChecker{fn: tc.fn})
			r.ExtraAxioms = tc.extra
			got, results, err := r.Fill(context.Background(), "A.lean", []byte(tc.src), nil)
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, tc.status, results[0].Status, results[0].Detail)
			if tc.status == Failed {
				require.Contains(t, results[0].Detail, tc.detail)
				require.Equal(t, tc.src, string(got), "a rejected answer must not change the file")
			}
		})
	}
}

func TestFillRetryCarriesCheckerErrors(t *testing.T) {
	prov := &scriptedProvider{replies: []string{fence("by\n  omega"), fence("by\n  rfl")}}
	calls := 0
	chk := &fakeChecker{fn: func(c string) (string, int, error) {
		calls++
		if calls == 1 {
			return "A.lean:6:2: error: omega failed", 1, nil
		}
		return cleanLean(c)
	}}
	r := newRunner(prov, chk)
	r.Attempts = 3
	_, results, err := r.Fill(context.Background(), "A.lean", []byte(leanSrc), nil)
	require.NoError(t, err)
	require.Equal(t, Filled, results[0].Status)
	require.Equal(t, 2, results[0].Attempts)
	second := prov.reqs[1].Messages[0].Text
	require.Contains(t, second, "omega failed")
	require.Contains(t, second, "Your previous attempt")
	require.Contains(t, second, "Whole file:")
	require.NotContains(t, prov.reqs[0].Messages[0].Text, "Your previous attempt")
}

// An empty answer cut off at the output cap must not wipe the checker
// output from the attempt before it.
func TestFillKeepsHistoryAfterTruncatedAnswer(t *testing.T) {
	prov := &scriptedProvider{
		replies: []string{fence("by\n  omega"), "", fence("by\n  rfl")},
		stops:   []string{"stop", "length", "stop"},
	}
	calls := 0
	chk := &fakeChecker{fn: func(c string) (string, int, error) {
		calls++
		if calls == 1 {
			return "A.lean:6:2: error: omega failed", 1, nil
		}
		return cleanLean(c)
	}}
	r := newRunner(prov, chk)
	r.Attempts = 3
	_, results, err := r.Fill(context.Background(), "A.lean", []byte(leanSrc), nil)
	require.NoError(t, err)
	require.Equal(t, Filled, results[0].Status)
	third := prov.reqs[2].Messages[0].Text
	require.Contains(t, third, "reached its output limit")
	require.Contains(t, third, "omega failed")
	require.Contains(t, third, "Your previous attempt")
}

func TestFillExhaustsAttempts(t *testing.T) {
	prov := &scriptedProvider{replies: []string{fence("by\n  a"), fence("by\n  b"), fence("by\n  c")}}
	chk := &fakeChecker{fn: func(string) (string, int, error) { return "error: nope", 1, nil }}
	r := newRunner(prov, chk)
	r.Attempts = 3
	_, results, err := r.Fill(context.Background(), "A.lean", []byte(leanSrc), nil)
	require.NoError(t, err)
	require.Equal(t, Failed, results[0].Status)
	require.Equal(t, 3, results[0].Attempts)
	require.Len(t, prov.reqs, 3)
}

func TestFillTwoHolesUseTheUpdatedFile(t *testing.T) {
	src := "theorem a : 1 = 1 := by\n  sorry\n\ntheorem b : 2 = 2 := by\n  sorry\n"
	prov := &scriptedProvider{replies: []string{fence("rfl"), fence("by\n  rfl")}}
	r := newRunner(prov, &fakeChecker{fn: cleanLean})
	var writes int
	r.Write = func(string, []byte) error { writes++; return nil }
	got, results, err := r.Fill(context.Background(), "A.lean", []byte(src), nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, Filled, results[0].Status, results[0].Detail)
	require.Equal(t, Filled, results[1].Status, results[1].Detail)
	require.Equal(t, "theorem a : 1 = 1 := rfl\n\ntheorem b : 2 = 2 := by\n  rfl\n", string(got))
	require.Contains(t, prov.reqs[1].Messages[0].Text, "theorem a : 1 = 1 := rfl")
	require.Equal(t, 2, writes)
}

func TestFillOnlyNamedDeclaration(t *testing.T) {
	src := "theorem a : 1 = 1 := by\n  sorry\n\ntheorem b : 2 = 2 := by\n  sorry\n"
	prov := &scriptedProvider{replies: []string{fence("rfl")}}
	r := newRunner(prov, &fakeChecker{fn: cleanLean})
	got, results, err := r.Fill(context.Background(), "A.lean", []byte(src), []string{"b"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "b", results[0].Name)
	require.Equal(t, "theorem a : 1 = 1 := by\n  sorry\n\ntheorem b : 2 = 2 := rfl\n", string(got))
}

func TestFillCapSkipsTheRest(t *testing.T) {
	src := "theorem a : 1 = 1 := by\n  sorry\n\ntheorem b : 2 = 2 := by\n  sorry\n"
	capErr := &llm.CapError{Flag: "--max-requests", Limit: 1, Used: 1}
	prov := &scriptedProvider{err: capErr}
	r := newRunner(prov, &fakeChecker{fn: cleanLean})
	_, results, err := r.Fill(context.Background(), "A.lean", []byte(src), nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, res := range results {
		require.Equal(t, Skipped, res.Status)
		require.Contains(t, res.Detail, "--max-requests")
	}
	require.Len(t, prov.reqs, 1, "no more calls after a cap")
}

func TestFillProviderErrorIsAnError(t *testing.T) {
	prov := &scriptedProvider{err: &llm.APIError{Provider: "fake", Status: 401, Message: "bad key"}}
	r := newRunner(prov, &fakeChecker{fn: cleanLean})
	_, _, err := r.Fill(context.Background(), "A.lean", []byte(leanSrc), nil)
	var api *llm.APIError
	require.ErrorAs(t, err, &api)
}

func TestFillCheckerTimeout(t *testing.T) {
	prov := &scriptedProvider{replies: []string{fence("by\n  rfl")}}
	chk := &fakeChecker{fn: func(string) (string, int, error) {
		time.Sleep(200 * time.Millisecond)
		return "", 0, context.DeadlineExceeded
	}}
	r := newRunner(prov, chk)
	r.CheckTimeout = 20 * time.Millisecond
	// The fake ignores its context, so give it one that fires.
	r.Checker = ctxChecker{chk}
	_, results, err := r.Fill(context.Background(), "A.lean", []byte(leanSrc), nil)
	require.NoError(t, err)
	require.Equal(t, Failed, results[0].Status)
	require.Contains(t, results[0].Detail, "timed out")
}

// ctxChecker returns as soon as its context is done.
type ctxChecker struct{ *fakeChecker }

func (c ctxChecker) Check(ctx context.Context, _ string) (string, int, error) {
	<-ctx.Done()
	return "", -1, ctx.Err()
}

func TestFillRunTimeoutSkips(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)
	r := newRunner(&scriptedProvider{}, &fakeChecker{fn: cleanLean})
	_, results, err := r.Fill(ctx, "A.lean", []byte(leanSrc), nil)
	require.NoError(t, err)
	require.Equal(t, Skipped, results[0].Status)
	require.Contains(t, results[0].Detail, "--timeout")
}

func TestFillCheckerCannotRun(t *testing.T) {
	prov := &scriptedProvider{replies: []string{fence("by\n  rfl")}}
	r := newRunner(prov, &fakeChecker{fn: func(string) (string, int, error) { return "", -1, errors.New("lake vanished") }})
	_, _, err := r.Fill(context.Background(), "A.lean", []byte(leanSrc), nil)
	require.ErrorContains(t, err, "lake vanished")
}

func TestFillPrivateAndDuplicateNames(t *testing.T) {
	tests := []struct {
		name, src, detail string
	}{
		{"private", "private theorem a : 1 = 1 := by\n  sorry\n", "private"},
		{"declared twice", "theorem a : 1 = 1 := by\n  sorry\n\ntheorem a : 1 = 1 := by\n  sorry\n", "declared 2 times"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prov := &scriptedProvider{}
			r := newRunner(prov, &fakeChecker{fn: cleanLean})
			_, results, err := r.Fill(context.Background(), "A.lean", []byte(tc.src), nil)
			require.NoError(t, err)
			require.NotEmpty(t, results)
			require.Equal(t, Failed, results[0].Status)
			require.Contains(t, results[0].Detail, tc.detail)
			require.Empty(t, prov.reqs, "nothing is sent for a hole that cannot be audited")
		})
	}
}

const rocqSrc = `Lemma a : True.
Proof.
  admit.
Admitted.

Lemma b : 1 = 1.
Proof. reflexivity. Qed.
`

func rocqRunner(p llm.Provider, fn func(string) (string, int, error)) *Runner {
	r := newRunner(p, &fakeChecker{fn: fn})
	r.Project = sandbox.ProofProject{Kind: sandbox.KindRocq}
	return r
}

func rocqClean(c string) (string, int, error) {
	var out strings.Builder
	for _, l := range strings.Split(c, "\n") {
		if _, ok := strings.CutPrefix(l, "Goal True. idtac \"PARANOID-BEGIN "); ok {
			name := strings.TrimSuffix(strings.TrimPrefix(l, "Goal True. idtac \"PARANOID-BEGIN "), "\". Abort.")
			out.WriteString("PARANOID-BEGIN " + name + "\nClosed under the global context\n")
		}
	}
	return out.String(), 0, nil
}

func TestFillRocq(t *testing.T) {
	good := "```coq\nProof.\n  exact I.\nQed.\n```"
	t.Run("accepts", func(t *testing.T) {
		r := rocqRunner(&scriptedProvider{replies: []string{good}}, rocqClean)
		got, results, err := r.Fill(context.Background(), "A.v", []byte(rocqSrc), nil)
		require.NoError(t, err)
		require.Equal(t, Filled, results[0].Status, results[0].Detail)
		require.Equal(t, strings.Replace(rocqSrc, "Proof.\n  admit.\nAdmitted.", "Proof.\n  exact I.\nQed.", 1), string(got))
	})
	rejections := []struct{ name, reply, detail string }{
		{"Admitted", "```coq\nProof.\n  admit.\nAdmitted.\n```", "hole"},
		{"no Qed", "```coq\nProof.\n  exact I.\n```", "complete proof"},
		{"Axiom", "```coq\nProof.\n  exact I.\nQed.\nAxiom bad : False.\n```", "extra commands"},
		{"Unset Guard Checking", "```coq\nProof.\n  Unset Guard Checking.\n  exact I.\nQed.\n```", "FV006"},
	}
	for _, tc := range rejections {
		t.Run(tc.name, func(t *testing.T) {
			r := rocqRunner(&scriptedProvider{replies: []string{tc.reply}}, rocqClean)
			got, results, err := r.Fill(context.Background(), "A.v", []byte(rocqSrc), nil)
			require.NoError(t, err)
			require.Equal(t, Failed, results[0].Status)
			require.Contains(t, results[0].Detail, tc.detail)
			require.Equal(t, rocqSrc, string(got))
		})
	}
	t.Run("axioms reported by Print Assumptions", func(t *testing.T) {
		fn := func(c string) (string, int, error) {
			if !strings.Contains(c, "Print Assumptions") {
				return "", 0, nil
			}
			return "PARANOID-BEGIN a\nAxioms:\nbad : False\n", 0, nil
		}
		r := rocqRunner(&scriptedProvider{replies: []string{good}}, fn)
		_, results, err := r.Fill(context.Background(), "A.v", []byte(rocqSrc), nil)
		require.NoError(t, err)
		require.Equal(t, Failed, results[0].Status)
		require.Contains(t, results[0].Detail, "bad")
	})
}

func TestCheckStructure(t *testing.T) {
	lang := fparse.LangLean
	orig := []byte(leanSrc)
	var target fparse.Decl
	for _, d := range fparse.Decls(lang, orig) {
		if d.Name == "MyList.helper" {
			target = d
		}
	}
	require.True(t, target.HasProofSpan())

	require.Nil(t, CheckStructure(lang, orig, Splice(orig, target, " by\n  rfl"), target))

	tests := []struct {
		name, cand, detail string
	}{
		{
			name:   "statement edited",
			cand:   strings.Replace(leanSrc, "theorem helper (n : Nat) : n = n := by\n  sorry", "theorem helper (n : Nat) : True := by\n  trivial", 1),
			detail: "statement of MyList.helper changed",
		},
		{
			name:   "neighbour proof edited",
			cand:   strings.Replace(strings.Replace(leanSrc, "sorry", "rfl", 1), "n + 0 = n := by\n  simp", "n + 0 = n := by\n  omega", 1),
			detail: "declaration MyList.other changed",
		},
		{
			name:   "earlier neighbour edited",
			cand:   strings.Replace(strings.Replace(leanSrc, "sorry", "rfl", 1), "l.reverse.reverse = l := by\n  simp", "l.reverse.reverse = l := by\n  decide", 1),
			detail: "declaration MyList.rev_rev changed",
		},
		{
			name:   "declaration renamed",
			cand:   strings.Replace(strings.Replace(leanSrc, "sorry", "rfl", 1), "theorem helper", "theorem helper2", 1),
			detail: "became",
		},
		{
			name:   "text before the file changed",
			cand:   "-- new header\n" + strings.Replace(leanSrc, "sorry", "rfl", 1),
			detail: "before the proof",
		},
		{
			name:   "text after the last declaration changed (it belongs to that declaration)",
			cand:   strings.Replace(leanSrc, "sorry", "rfl", 1) + "axiom evil : False\n",
			detail: "declaration MyList.other changed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rej := CheckStructure(lang, orig, []byte(tc.cand), target)
			require.NotNil(t, rej)
			require.Equal(t, gateStructure, rej.Gate)
			require.Contains(t, rej.Detail, tc.detail)
		})
	}
}

func TestCheckStatic(t *testing.T) {
	orig := []byte(leanSrc)
	tests := []struct {
		name, cand, rule string
	}{
		{"hole added elsewhere", strings.Replace(strings.Replace(leanSrc, "sorry", "rfl", 1), "n + 0 = n := by\n  simp", "n + 0 = n := by\n  sorry", 1) + "theorem z : True := by\n  sorry\n", "FV001"},
		{"escape added", strings.Replace(leanSrc, "sorry", "native_decide", 1), "FV002"},
		{"kernel check off", strings.Replace(leanSrc, "sorry", "set_option debug.skipKernelTC true in\n  rfl", 1), "FV006"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rej, err := CheckStatic("A.lean", orig, []byte(tc.cand))
			require.NoError(t, err)
			require.NotNil(t, rej)
			require.Contains(t, rej.Detail, tc.rule)
		})
	}
	rej, err := CheckStatic("A.lean", orig, []byte(strings.Replace(leanSrc, "sorry", "rfl", 1)))
	require.NoError(t, err)
	require.Nil(t, rej)
}

func TestCheckAxioms(t *testing.T) {
	allowed := map[string]bool{"propext": true}
	require.Nil(t, CheckAxioms("a", map[string][]string{"a": {"propext"}}, allowed))
	require.Nil(t, CheckAxioms("a", map[string][]string{"a": {}}, allowed))
	require.Contains(t, CheckAxioms("a", map[string][]string{}, allowed).Detail, "no axiom answer")
	require.Contains(t, CheckAxioms("a", map[string][]string{"a": {"z", "y"}}, allowed).Detail, "y, z")
}

func TestFindTargets(t *testing.T) {
	src := []byte("def f : Nat := sorry\n\ntheorem a : 1 = 1 := by\n  sorry\n\ntheorem b : 1 = 1 := rfl\n")
	ts, outside, err := FindTargets(fparse.LangLean, "A.lean", src, nil)
	require.NoError(t, err)
	require.Equal(t, []Target{{File: "A.lean", Name: "a", Line: 3, Holes: 1}}, ts)
	require.Equal(t, 1, outside)

	_, _, err = FindTargets(fparse.LangLean, "A.lean", src, []string{"zzz"})
	require.ErrorContains(t, err, "no theorem or lemma named")
	ts, _, err = FindTargets(fparse.LangLean, "A.lean", src, []string{"b"})
	require.NoError(t, err)
	require.Empty(t, ts, "a named declaration without a hole has nothing to fill")
}
