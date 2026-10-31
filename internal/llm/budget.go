package llm

import (
	"errors"
	"fmt"
	"sync"
)

// Default caps for one run.
const (
	DefaultMaxRequests    = 100
	DefaultMaxTotalTokens = 2_000_000
)

// ErrCapReached is matched by errors.Is for any cap error.
var ErrCapReached = errors.New("run cap reached")

// Caps are the limits for one run. A field that is zero or less uses
// its default.
type Caps struct {
	// MaxRequests is the most provider calls in the run.
	MaxRequests int
	// MaxTokens is the most input plus output tokens in the run.
	MaxTokens int
}

// CapError says which cap stopped the run.
type CapError struct {
	// Flag is the command-line flag name of the cap, for example
	// "--max-requests".
	Flag  string
	Limit int
	Used  int
}

// Error names the cap, the limit, and the use so far.
func (e *CapError) Error() string {
	return fmt.Sprintf("stopped at the %s cap (%d used, limit %d); raise %s to continue", e.Flag, e.Used, e.Limit, e.Flag)
}

// Is makes errors.Is(err, ErrCapReached) true.
func (e *CapError) Is(target error) bool { return target == ErrCapReached }

// Budget counts requests and tokens for one run and refuses a new call
// once a cap is reached. The token cap is checked before a call, so the
// last call may go a little over it. It is safe for concurrent use.
type Budget struct {
	mu       sync.Mutex
	caps     Caps
	requests int
	tokens   int
}

// NewBudget builds a Budget, filling in defaults for unset caps.
func NewBudget(c Caps) *Budget {
	if c.MaxRequests <= 0 {
		c.MaxRequests = DefaultMaxRequests
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultMaxTotalTokens
	}
	return &Budget{caps: c}
}

// Admit reserves one request. It returns a *CapError when a cap is
// already reached.
func (b *Budget) Admit() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requests >= b.caps.MaxRequests {
		return &CapError{Flag: "--max-requests", Limit: b.caps.MaxRequests, Used: b.requests}
	}
	if b.tokens >= b.caps.MaxTokens {
		return &CapError{Flag: "--max-tokens", Limit: b.caps.MaxTokens, Used: b.tokens}
	}
	b.requests++
	return nil
}

// Record adds the tokens one call used.
func (b *Budget) Record(inputTokens, outputTokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens += max(inputTokens, 0) + max(outputTokens, 0)
}

// Usage returns the requests and tokens used so far.
func (b *Budget) Usage() (requests, tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requests, b.tokens
}
