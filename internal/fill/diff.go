package fill

import (
	"fmt"
	"strings"
)

// diffContext is how many unchanged lines surround each change.
const diffContext = 3

// maxDiffCells caps the table size of the line diff. Past it, the whole
// changed middle is shown as one replacement.
const maxDiffCells = 25_000_000

type diffOp struct {
	kind byte // ' ', '-', '+'
	text string
}

// UnifiedDiff returns a unified diff of two texts, or "" when they are
// equal. name is used for the "---" and "+++" lines.
func UnifiedDiff(name string, a, b []byte) string {
	if string(a) == string(b) {
		return ""
	}
	al, bl := splitLines(string(a)), splitLines(string(b))
	ops := diffLines(al, bl)

	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", name, name)
	n := len(ops)
	i := 0
	aLine, bLine := 1, 1 // line numbers of ops[i] in a and b
	for i < n {
		if ops[i].kind == ' ' {
			i++
			aLine++
			bLine++
			continue
		}
		start, back := i, 0
		for start > 0 && back < diffContext && ops[start-1].kind == ' ' {
			start--
			back++
		}
		hunkA, hunkB := aLine-back, bLine-back
		last := i
		for j := i; j < n; j++ {
			if ops[j].kind != ' ' {
				last = j
			} else if j-last > 2*diffContext {
				break
			}
		}
		end := min(last+diffContext, n-1)
		var body strings.Builder
		na, nb := 0, 0
		for j := start; j <= end; j++ {
			if ops[j].kind != '+' {
				na++
			}
			if ops[j].kind != '-' {
				nb++
			}
			body.WriteByte(ops[j].kind)
			body.WriteString(ops[j].text)
			body.WriteByte('\n')
		}
		fmt.Fprintf(&out, "@@ -%s +%s @@\n%s", hunkRange(hunkA, na), hunkRange(hunkB, nb), body.String())
		for j := i; j <= end; j++ {
			if ops[j].kind != '+' {
				aLine++
			}
			if ops[j].kind != '-' {
				bLine++
			}
		}
		i = end + 1
	}
	return out.String()
}

func hunkRange(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	if count == 0 {
		start--
	}
	return fmt.Sprintf("%d,%d", start, count)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// diffLines builds an edit script with a longest-common-subsequence table
// over the lines between the common head and tail.
func diffLines(a, b []string) []diffOp {
	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	am, bm := a[head:len(a)-tail], b[head:len(b)-tail]
	var ops []diffOp
	for _, l := range a[:head] {
		ops = append(ops, diffOp{' ', l})
	}
	ops = append(ops, lcsOps(am, bm)...)
	for _, l := range a[len(a)-tail:] {
		ops = append(ops, diffOp{' ', l})
	}
	return ops
}

func lcsOps(a, b []string) []diffOp {
	if len(a) == 0 || len(b) == 0 || len(a)*len(b) > maxDiffCells {
		var ops []diffOp
		for _, l := range a {
			ops = append(ops, diffOp{'-', l})
		}
		for _, l := range b {
			ops = append(ops, diffOp{'+', l})
		}
		return ops
	}
	w := len(b) + 1
	t := make([]int32, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i*w+j] = t[(i+1)*w+j+1] + 1
			} else {
				t[i*w+j] = max(t[(i+1)*w+j], t[i*w+j+1])
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case t[(i+1)*w+j] >= t[i*w+j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}
