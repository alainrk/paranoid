package difftest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// Output kinds in the JSON-lines the harnesses print.
const (
	outOK    = "ok"
	outPanic = "panic"
	outError = "error"
)

// harnessLine is one parsed output line: {"ok":v}, {"panic":msg}, or
// {"error":msg}.
type harnessLine struct {
	Kind    string
	Value   string // canonical JSON of the value, for ok
	Message string // for panic and error
}

// parseLine reads one output line of a harness.
func parseLine(raw string) (harnessLine, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil {
		return harnessLine{}, fmt.Errorf("output line %q is not a JSON object: %w", clip(raw), err)
	}
	if len(m) != 1 {
		return harnessLine{}, fmt.Errorf("output line %q must have exactly one key", clip(raw))
	}
	for k, v := range m {
		switch k {
		case outOK:
			c, err := canonical(v)
			if err != nil {
				return harnessLine{}, fmt.Errorf("output value %q: %w", clip(string(v)), err)
			}
			return harnessLine{Kind: outOK, Value: c}, nil
		case outPanic, outError:
			var msg string
			if err := json.Unmarshal(v, &msg); err != nil {
				msg = string(v)
			}
			return harnessLine{Kind: k, Message: msg}, nil
		}
		return harnessLine{}, fmt.Errorf("output line %q has unknown key %q", clip(raw), k)
	}
	return harnessLine{}, fmt.Errorf("empty output line")
}

// canonical re-encodes JSON so that equal values give equal text:
// integers as plain digits, sorted object keys, no extra spaces.
func canonical(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	var b strings.Builder
	if err := writeCanonical(&b, v); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writeCanonical(b *strings.Builder, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		fmt.Fprint(b, x)
	case string:
		enc, err := json.Marshal(x)
		if err != nil {
			return fmt.Errorf("encoding a string: %w", err)
		}
		b.Write(enc)
	case json.Number:
		if n, ok := new(big.Int).SetString(string(x), 10); ok {
			b.WriteString(n.String())
		} else {
			b.WriteString(string(x))
		}
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			enc, err := json.Marshal(k)
			if err != nil {
				return fmt.Errorf("encoding a key: %w", err)
			}
			b.Write(enc)
			b.WriteByte(':')
			if err := writeCanonical(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("unexpected JSON value of type %T", v)
	}
	return nil
}

// clip shortens long text for messages.
func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// Compare lines up the inputs with the code and model outputs and
// builds the result. The slices must have the same length.
func Compare(inputs []string, codeOut, modelOut []string) (Result, error) {
	if len(inputs) != len(codeOut) || len(inputs) != len(modelOut) {
		return Result{}, fmt.Errorf("got %d inputs, %d code outputs, %d model outputs", len(inputs), len(codeOut), len(modelOut))
	}
	res := Result{Cases: len(inputs)}
	for i, in := range inputs {
		c, err := parseLine(codeOut[i])
		if err != nil {
			return Result{}, fmt.Errorf("code output for input %s: %w", clip(in), err)
		}
		m, err := parseLine(modelOut[i])
		if err != nil {
			return Result{}, fmt.Errorf("model output for input %s: %w", clip(in), err)
		}
		switch {
		case c.Kind != outOK:
			res.CodeErrors = append(res.CodeErrors, Failure{Input: in, Message: c.Kind + ": " + c.Message})
		case m.Kind != outOK:
			res.ModelErrors = append(res.ModelErrors, Failure{Input: in, Message: m.Kind + ": " + m.Message})
		case c.Value != m.Value:
			res.MismatchCount++
			if len(res.Mismatches) < MaxReported {
				res.Mismatches = append(res.Mismatches, Mismatch{Input: in, Code: c.Value, Model: m.Value})
			}
		}
	}
	res.CodeErrors = capFailures(res.CodeErrors, &res.CodeErrorCount)
	res.ModelErrors = capFailures(res.ModelErrors, &res.ModelErrorCount)
	return res, nil
}

func capFailures(f []Failure, count *int) []Failure {
	*count = len(f)
	if len(f) > MaxReported {
		return f[:MaxReported]
	}
	return f
}
