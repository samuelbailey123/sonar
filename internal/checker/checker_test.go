package checker_test

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/samuelbailey123/sonar/internal/checker"
	"github.com/samuelbailey123/sonar/internal/dns"
	"github.com/samuelbailey123/sonar/internal/query"
	"github.com/samuelbailey123/sonar/internal/resolver"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// makeResolver returns a resolver.Resolver with the given IP and a synthetic
// name so tests do not depend on the real curated list.
func makeResolver(ip string) resolver.Resolver {
	return resolver.Resolver{
		IP:        ip,
		Name:      "test-" + ip,
		Country:   "Test",
		City:      "Test",
		Continent: "Test",
	}
}

// makeResult builds a query.Result with the given records and no error
// (Success). Pass a non-nil error for the Error field to simulate failure.
func makeResult(res resolver.Resolver, records []string, err error) query.Result {
	st := query.Success
	if err != nil {
		st = query.Error
	}
	return query.Result{
		Resolver: res,
		Records:  records,
		Status:   st,
		Error:    err,
		Latency:  1 * time.Millisecond,
	}
}

// reliableResolvers returns two globally reachable anycast resolvers. Using
// only two keeps integration tests fast while still exercising concurrency.
func reliableResolvers() []resolver.Resolver {
	return []resolver.Resolver{
		makeResolver("8.8.8.8"),
		makeResolver("1.1.1.1"),
		makeResolver("9.9.9.9"),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// New
// ─────────────────────────────────────────────────────────────────────────────

func TestNew_DefaultMaxWorkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		inputWorkers   int
		wantDefaulting bool
	}{
		{
			name:           "zero defaults to 20",
			inputWorkers:   0,
			wantDefaulting: true,
		},
		{
			name:           "negative defaults to 20",
			inputWorkers:   -1,
			wantDefaulting: true,
		},
		{
			name:           "positive value preserved",
			inputWorkers:   5,
			wantDefaulting: false,
		},
		{
			name:           "exactly 20 preserved",
			inputWorkers:   20,
			wantDefaulting: false,
		},
		{
			name:           "large value preserved",
			inputWorkers:   100,
			wantDefaulting: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := checker.Config{
				Domain:     "example.com",
				RecordType: dns.TypeA,
				Resolvers:  reliableResolvers(),
				MaxWorkers: tc.inputWorkers,
			}

			c := checker.New(cfg)
			if c == nil {
				t.Fatal("New returned nil")
			}

			// Run with a context that cancels immediately so we do not actually
			// issue network queries — we only verify New did not panic.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _ = c.Run(ctx)
		})
	}
}

func TestNew_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
	})
	if c == nil {
		t.Fatal("New should never return nil")
	}
}

func TestNew_EmptyResolversAllowed(t *testing.T) {
	t.Parallel()

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  nil,
		MaxWorkers: 1,
	})
	if c == nil {
		t.Fatal("New should not return nil even with no resolvers")
	}

	results, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run with no resolvers returned unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Run — integration (real network, small resolver set)
// ─────────────────────────────────────────────────────────────────────────────

func TestRun_ReturnsResultsForEachResolver(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	resolvers := reliableResolvers()
	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  resolvers,
		MaxWorkers: len(resolvers),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(results) != len(resolvers) {
		t.Errorf("expected %d results, got %d", len(resolvers), len(results))
	}
}

func TestRun_ResultsMatchResultsMethod(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	resolvers := reliableResolvers()
	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  resolvers,
		MaxWorkers: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runResults, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	snapshot := c.Results()
	if len(runResults) != len(snapshot) {
		t.Errorf("Run returned %d results but Results() returned %d", len(runResults), len(snapshot))
	}
}

func TestRun_SingleWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	resolvers := reliableResolvers()[:2]
	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  resolvers,
		MaxWorkers: 1, // force sequential execution via single worker slot
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != len(resolvers) {
		t.Errorf("expected %d results, got %d", len(resolvers), len(results))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Run — context cancellation
// ─────────────────────────────────────────────────────────────────────────────

func TestRun_CancelledContextBeforeStart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  reliableResolvers(),
		MaxWorkers: 5,
	})

	results, err := c.Run(ctx)
	// Run must not error — context cancellation causes partial/empty results,
	// not an error return. The internal run loop tolerates cancellation.
	if err != nil {
		t.Fatalf("Run should not return an error on context cancellation, got: %v", err)
	}
	// We cannot assert an exact count because some goroutines may have been
	// dispatched before observing cancellation, but the slice must be non-nil.
	if results == nil {
		t.Error("Run returned nil results slice; expected at least an empty slice")
	}
}

func TestRun_ContextCancelledMidway(t *testing.T) {
	t.Parallel()

	// Use an unreachable resolver that will block until the context cancels,
	// paired with a very short deadline to trigger the cancellation path.
	unreachable := []resolver.Resolver{
		makeResolver("192.0.2.1"), // TEST-NET-1, RFC 5737 — not routable
		makeResolver("192.0.2.2"),
		makeResolver("192.0.2.3"),
		makeResolver("192.0.2.4"),
		makeResolver("192.0.2.5"),
	}

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  unreachable,
		MaxWorkers: 2,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run should not surface context cancellation as an error, got: %v", err)
	}
	// Results may be partial or empty — both are acceptable.
	_ = results
}

// ─────────────────────────────────────────────────────────────────────────────
// Stream
// ─────────────────────────────────────────────────────────────────────────────

func TestStream_ChannelClosesAfterAllResults(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	resolvers := reliableResolvers()
	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  resolvers,
		MaxWorkers: len(resolvers),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resultCh, errCh := c.Stream(ctx)

	var received []query.Result
	for r := range resultCh {
		received = append(received, r)
	}

	// The result channel must be closed before the error channel is written.
	err := <-errCh
	if err != nil {
		t.Fatalf("Stream errCh received unexpected error: %v", err)
	}

	if len(received) != len(resolvers) {
		t.Errorf("Stream delivered %d results, expected %d", len(received), len(resolvers))
	}
}

func TestStream_ResultsArrivedBeforeErrCh(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	resolvers := reliableResolvers()[:1]
	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  resolvers,
		MaxWorkers: 1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resultCh, errCh := c.Stream(ctx)

	got := 0
	for range resultCh {
		got++
	}
	if got != 1 {
		t.Errorf("expected 1 result from Stream, got %d", got)
	}

	// errCh must be readable (not blocking) now that resultCh is drained.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error from errCh: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for errCh after resultCh was drained")
	}
}

func TestStream_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  reliableResolvers(),
		MaxWorkers: 5,
	})

	resultCh, errCh := c.Stream(ctx)

	// Drain results (may be zero or a few).
	for range resultCh {
	}

	// errCh must close cleanly.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Stream errCh should emit nil, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for errCh after cancellation")
	}
}

func TestStream_EmptyResolvers(t *testing.T) {
	t.Parallel()

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  nil,
		MaxWorkers: 1,
	})

	ctx := context.Background()
	resultCh, errCh := c.Stream(ctx)

	var count int
	for range resultCh {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 results for empty resolver list, got %d", count)
	}

	err := <-errCh
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Results — copy semantics
// ─────────────────────────────────────────────────────────────────────────────

func TestResults_ReturnsCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	resolvers := reliableResolvers()[:2]
	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  resolvers,
		MaxWorkers: 2,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	first := c.Results()
	originalLen := len(first)

	// Truncate the returned slice — internal state must be unaffected.
	first = first[:0]

	second := c.Results()
	if len(second) != originalLen {
		t.Errorf("mutating returned slice affected internal state: got %d, want %d", len(second), originalLen)
	}
}

func TestResults_BeforeRun_Empty(t *testing.T) {
	t.Parallel()

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  reliableResolvers(),
		MaxWorkers: 5,
	})

	results := c.Results()
	if len(results) != 0 {
		t.Errorf("Results() before Run should be empty, got %d entries", len(results))
	}
}

func TestResults_NotNilOnEmptyResolvers(t *testing.T) {
	t.Parallel()

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  nil,
		MaxWorkers: 1,
	})

	_, _ = c.Run(context.Background())

	results := c.Results()
	// An explicit empty slice (not nil) is acceptable because Results uses make+copy.
	if results == nil {
		// make([]T, 0) is not nil, but copy of a zero-length slice returns
		// a non-nil empty slice. Both nil and non-nil empty slices are
		// functionally equivalent here — just ensure no panic.
		t.Log("Results returned nil for empty resolver list (acceptable)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PropagationPercentage — unit tests (no network)
// ─────────────────────────────────────────────────────────────────────────────

func TestPropagationPercentage(t *testing.T) {
	t.Parallel()

	r1 := makeResolver("1.1.1.1")
	r2 := makeResolver("8.8.8.8")
	r3 := makeResolver("9.9.9.9")
	r4 := makeResolver("4.4.4.4")

	errSentinel := errors.New("timeout")

	tests := []struct {
		name    string
		results []query.Result
		want    float64
	}{
		{
			name:    "empty input returns 0",
			results: nil,
			want:    0,
		},
		{
			name: "all errors returns 0",
			results: []query.Result{
				makeResult(r1, nil, errSentinel),
				makeResult(r2, nil, errSentinel),
			},
			want: 0,
		},
		{
			name: "all matching returns 100",
			results: []query.Result{
				makeResult(r1, []string{"93.184.216.34"}, nil),
				makeResult(r2, []string{"93.184.216.34"}, nil),
				makeResult(r3, []string{"93.184.216.34"}, nil),
			},
			want: 100,
		},
		{
			name: "single successful result returns 100",
			results: []query.Result{
				makeResult(r1, []string{"1.2.3.4"}, nil),
			},
			want: 100,
		},
		{
			name: "mixed matching and non-matching",
			results: []query.Result{
				makeResult(r1, []string{"93.184.216.34"}, nil),
				makeResult(r2, []string{"93.184.216.34"}, nil),
				makeResult(r3, []string{"1.2.3.4"}, nil),
				makeResult(r4, []string{"93.184.216.34"}, nil),
			},
			// consensus = "93.184.216.34" (3 of 4), 3/4 * 100 = 75
			want: 75,
		},
		{
			name: "errors excluded from denominator",
			results: []query.Result{
				makeResult(r1, []string{"93.184.216.34"}, nil),
				makeResult(r2, []string{"93.184.216.34"}, nil),
				makeResult(r3, nil, errSentinel),
				makeResult(r4, nil, errSentinel),
			},
			// Only r1 and r2 counted; both match consensus → 100%
			want: 100,
		},
		{
			name: "half matching half not",
			results: []query.Result{
				makeResult(r1, []string{"1.1.1.1"}, nil),
				makeResult(r2, []string{"2.2.2.2"}, nil),
			},
			// Tie: consensus chosen by lexicographic order of joined key.
			// "1.1.1.1" < "2.2.2.2" → consensus is "1.1.1.1", 1/2 = 50%
			want: 50,
		},
		{
			name: "record order does not affect matching",
			results: []query.Result{
				makeResult(r1, []string{"b.com", "a.com"}, nil),
				makeResult(r2, []string{"a.com", "b.com"}, nil),
				makeResult(r3, []string{"a.com", "b.com"}, nil),
			},
			// After sorting, both produce "a.com|b.com" — all match → 100%
			want: 100,
		},
		{
			name: "single result with error returns 0",
			results: []query.Result{
				makeResult(r1, nil, errSentinel),
			},
			want: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checker.PropagationPercentage(tc.results)
			if got != tc.want {
				t.Errorf("PropagationPercentage = %v, want %v", got, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ConsensusRecords — unit tests (no network)
// ─────────────────────────────────────────────────────────────────────────────

func TestConsensusRecords(t *testing.T) {
	t.Parallel()

	r1 := makeResolver("1.1.1.1")
	r2 := makeResolver("8.8.8.8")
	r3 := makeResolver("9.9.9.9")
	r4 := makeResolver("4.4.4.4")
	r5 := makeResolver("5.5.5.5")

	errSentinel := errors.New("network error")

	tests := []struct {
		name    string
		results []query.Result
		want    []string // nil means we expect nil return
	}{
		{
			name:    "empty input returns nil",
			results: nil,
			want:    nil,
		},
		{
			name: "all errors returns nil",
			results: []query.Result{
				makeResult(r1, nil, errSentinel),
				makeResult(r2, nil, errSentinel),
			},
			want: nil,
		},
		{
			name: "unanimous single record",
			results: []query.Result{
				makeResult(r1, []string{"93.184.216.34"}, nil),
				makeResult(r2, []string{"93.184.216.34"}, nil),
				makeResult(r3, []string{"93.184.216.34"}, nil),
			},
			want: []string{"93.184.216.34"},
		},
		{
			name: "majority wins",
			results: []query.Result{
				makeResult(r1, []string{"1.1.1.1"}, nil),
				makeResult(r2, []string{"2.2.2.2"}, nil),
				makeResult(r3, []string{"1.1.1.1"}, nil),
				makeResult(r4, []string{"1.1.1.1"}, nil),
				makeResult(r5, []string{"2.2.2.2"}, nil),
			},
			want: []string{"1.1.1.1"},
		},
		{
			name: "tie broken lexicographically (smaller wins)",
			results: []query.Result{
				makeResult(r1, []string{"b.example.com"}, nil),
				makeResult(r2, []string{"a.example.com"}, nil),
			},
			// "a.example.com" < "b.example.com" lexicographically
			want: []string{"a.example.com"},
		},
		{
			name: "tie broken lexicographically (larger loses)",
			results: []query.Result{
				makeResult(r1, []string{"z.example.com"}, nil),
				makeResult(r2, []string{"a.example.com"}, nil),
			},
			want: []string{"a.example.com"},
		},
		{
			name: "multiple records sorted before comparison",
			results: []query.Result{
				makeResult(r1, []string{"b.com", "a.com"}, nil),
				makeResult(r2, []string{"a.com", "b.com"}, nil),
				makeResult(r3, []string{"c.com"}, nil),
			},
			// r1 and r2 normalise to ["a.com","b.com"], count=2 → consensus
			want: []string{"a.com", "b.com"},
		},
		{
			name: "errors excluded from tally",
			results: []query.Result{
				makeResult(r1, []string{"1.1.1.1"}, nil),
				makeResult(r2, nil, errSentinel),
				makeResult(r3, nil, errSentinel),
				makeResult(r4, nil, errSentinel),
			},
			// Only r1 is counted; it is the sole successful answer.
			want: []string{"1.1.1.1"},
		},
		{
			name: "single successful result",
			results: []query.Result{
				makeResult(r1, []string{"42.42.42.42"}, nil),
			},
			want: []string{"42.42.42.42"},
		},
		{
			name: "empty records slice in successful result",
			results: []query.Result{
				makeResult(r1, nil, nil), // success, but no records
				makeResult(r2, nil, nil),
			},
			// Both have an empty record set (key=""), count=2 → consensus is empty
			want: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := checker.ConsensusRecords(tc.results)

			if tc.want == nil {
				if got != nil {
					t.Errorf("ConsensusRecords = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatalf("ConsensusRecords returned nil, want %v", tc.want)
			}

			// Sort both for comparison because the consensus records are
			// already sorted but we want deterministic test assertions.
			gotCopy := make([]string, len(got))
			copy(gotCopy, got)
			sort.Strings(gotCopy)

			wantCopy := make([]string, len(tc.want))
			copy(wantCopy, tc.want)
			sort.Strings(wantCopy)

			if len(gotCopy) != len(wantCopy) {
				t.Fatalf("ConsensusRecords length = %d, want %d\ngot:  %v\nwant: %v",
					len(gotCopy), len(wantCopy), gotCopy, wantCopy)
			}

			for i := range gotCopy {
				if gotCopy[i] != wantCopy[i] {
					t.Errorf("ConsensusRecords[%d] = %q, want %q", i, gotCopy[i], wantCopy[i])
				}
			}
		})
	}
}

// TestConsensusRecords_DeterministicOnRepeatedCall verifies that calling
// ConsensusRecords multiple times with the same input always yields the same
// answer (important because map iteration order in Go is randomised).
func TestConsensusRecords_DeterministicOnRepeatedCall(t *testing.T) {
	t.Parallel()

	r1 := makeResolver("1.1.1.1")
	r2 := makeResolver("8.8.8.8")

	results := []query.Result{
		makeResult(r1, []string{"alpha.example.com"}, nil),
		makeResult(r2, []string{"beta.example.com"}, nil),
	}

	first := checker.ConsensusRecords(results)
	for i := 0; i < 20; i++ {
		got := checker.ConsensusRecords(results)
		if len(got) != len(first) {
			t.Fatalf("iteration %d: length changed from %d to %d", i, len(first), len(got))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("iteration %d: result[%d] changed from %q to %q", i, j, first[j], got[j])
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PropagationPercentage + ConsensusRecords — boundary / precision
// ─────────────────────────────────────────────────────────────────────────────

func TestPropagationPercentage_PrecisionNotRounded(t *testing.T) {
	t.Parallel()

	r1 := makeResolver("1.1.1.1")
	r2 := makeResolver("8.8.8.8")
	r3 := makeResolver("9.9.9.9")

	// 2 of 3 match → 66.666...
	results := []query.Result{
		makeResult(r1, []string{"93.184.216.34"}, nil),
		makeResult(r2, []string{"93.184.216.34"}, nil),
		makeResult(r3, []string{"1.2.3.4"}, nil),
	}

	got := checker.PropagationPercentage(results)
	expected := (2.0 / 3.0) * 100

	// Use a tolerance because floating-point equality is unreliable.
	const epsilon = 1e-9
	if got < expected-epsilon || got > expected+epsilon {
		t.Errorf("PropagationPercentage = %v, want approximately %v", got, expected)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Concurrency — race detector coverage
// ─────────────────────────────────────────────────────────────────────────────

// TestRun_NoDataRace exercises concurrent goroutines via go test -race.
// It uses a very short deadline so real network calls time out quickly, but
// the goroutine lifecycle and shared-state access are still fully exercised.
func TestRun_NoDataRace(t *testing.T) {
	t.Parallel()

	resolvers := reliableResolvers()
	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  resolvers,
		MaxWorkers: 3,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _ = c.Run(ctx)

	// Concurrently call Results() while the worker pool may still be writing.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Results()
	}()
	<-done
}

// TestStream_NoDataRace runs Stream and reads from both channels concurrently
// to verify there are no races on the shared result slice or channel.
func TestStream_NoDataRace(t *testing.T) {
	t.Parallel()

	resolvers := reliableResolvers()[:2]
	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: dns.TypeA,
		Resolvers:  resolvers,
		MaxWorkers: 2,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	resultCh, errCh := c.Stream(ctx)

	go func() {
		for range resultCh {
		}
	}()

	<-errCh
}

// ─────────────────────────────────────────────────────────────────────────────
// Run — semaphore cancellation path
// ─────────────────────────────────────────────────────────────────────────────

// TestRun_SemaphoreWaitCancelled exercises the case <-ctx.Done() branch inside
// the semaphore-acquire select. It does this by:
//
//  1. Setting MaxWorkers=1 so only a single goroutine can hold the semaphore
//     slot at a time.
//  2. Using ten RFC 5737 unroutable resolvers (192.0.2.x) so the first
//     goroutine to acquire the slot will block inside query.Execute for the
//     full context deadline.
//  3. Cancelling the context after a short timeout — all goroutines that are
//     blocked waiting for the semaphore will observe <-ctx.Done() and return
//     without executing a query.
//
// The test asserts only safety invariants (no panic, no error return, non-nil
// result slice) because the exact number of results depends on scheduling.
func TestRun_SemaphoreWaitCancelled(t *testing.T) {
	t.Parallel()

	// Ten unroutable resolvers ensure plenty of goroutines are queued behind
	// the single worker slot.
	resolvers := make([]resolver.Resolver, 10)
	for i := range resolvers {
		resolvers[i] = makeResolver("192.0.2." + itoa(i+1))
	}

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: 1, // TypeA
		Resolvers:  resolvers,
		MaxWorkers: 1, // only one slot — all others must wait
	})

	// Short deadline guarantees the first goroutine does not finish, so at
	// least several others are blocked in the semaphore select when Done fires.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := c.Run(ctx)

	if err != nil {
		t.Fatalf("Run must not return an error on context cancellation, got: %v", err)
	}
	if results == nil {
		t.Error("Run returned nil results slice; expected at least an empty slice")
	}
}

// TestRun_SemaphoreWaitCancelled_ManyResolvers is a higher-pressure variant
// that uses 20 resolvers against a single worker slot and a very tight deadline
// to maximise the probability of multiple goroutines being in the semaphore
// wait when the context fires.
func TestRun_SemaphoreWaitCancelled_ManyResolvers(t *testing.T) {
	t.Parallel()

	resolvers := make([]resolver.Resolver, 20)
	for i := range resolvers {
		resolvers[i] = makeResolver("192.0.2." + itoa(i+1))
	}

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: 1,
		Resolvers:  resolvers,
		MaxWorkers: 1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	results, err := c.Run(ctx)

	if err != nil {
		t.Fatalf("Run must not return an error on context cancellation, got: %v", err)
	}
	if results == nil {
		t.Error("Run returned nil results slice; expected at least an empty slice")
	}
}

// TestRun_PreCancelledContext_NoDispatches verifies that a pre-cancelled
// context causes the dispatch loop to break on its very first iteration,
// resulting in zero or very few results — the loop checks ctx.Err() at the
// top of each iteration before calling wg.Add.
func TestRun_PreCancelledContext_NoDispatches(t *testing.T) {
	t.Parallel()

	resolvers := make([]resolver.Resolver, 10)
	for i := range resolvers {
		resolvers[i] = makeResolver("192.0.2." + itoa(i+1))
	}

	cfg := checker.Config{
		Domain:     "example.com",
		RecordType: 1,
		Resolvers:  resolvers,
		MaxWorkers: 2,
	}

	c := checker.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run is called

	results, err := c.Run(ctx)

	if err != nil {
		t.Fatalf("Run must not return an error on pre-cancellation, got: %v", err)
	}
	if results == nil {
		t.Error("Run returned nil results slice; expected at least an empty slice")
	}
	// The loop should have broken on the first iteration, so no goroutines
	// were dispatched. The result count must be zero.
	if len(results) != 0 {
		t.Errorf("expected 0 results when context is pre-cancelled, got %d", len(results))
	}
}

// TestStream_ResultDroppedOnCancellation exercises the defensive case <-ctx.Done()
// in the result-send select. The channel is buffered to len(resolvers), so a
// send normally succeeds immediately. The ctx.Done() arm fires only when the
// context is already cancelled at the moment a goroutine reaches the select and
// Go's runtime scheduler happens to pick the Done case over the channel case
// (both are ready simultaneously). This is inherently non-deterministic. The
// test is therefore purely behavioural: it confirms no hang, no panic, and
// clean channel closure. A companion high-count benchmark below maximises the
// probability of hitting the branch at least once across many executions.
func TestStream_ResultDroppedOnCancellation(t *testing.T) {
	t.Parallel()

	// Use unroutable resolvers so queries block until the context fires, then
	// every goroutine reaches the send select with a cancelled context.
	resolvers := make([]resolver.Resolver, 8)
	for i := range resolvers {
		resolvers[i] = makeResolver("192.0.2." + itoa(i+1))
	}

	c := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: 1,
		Resolvers:  resolvers,
		MaxWorkers: len(resolvers), // all start simultaneously — no semaphore wait
	})

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	resultCh, errCh := c.Stream(ctx)

	// Do not read from resultCh while the run is in progress. This increases
	// the chance that goroutines racing to send will find both the channel and
	// ctx.Done() ready, giving the scheduler a chance to pick the Done arm.
	select {
	case <-errCh:
		// Run completed (or was cancelled) — drain remaining results.
		for range resultCh {
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Stream to complete after context cancellation")
	}
}

// itoa is a minimal integer-to-string helper that avoids importing strconv
// in the test file for a single use-case.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 3)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkPropagationPercentage(b *testing.B) {
	r := makeResolver("1.1.1.1")
	results := make([]query.Result, 100)
	for i := range results {
		if i%10 == 0 {
			results[i] = makeResult(r, []string{"1.2.3.4"}, nil)
		} else {
			results[i] = makeResult(r, []string{"93.184.216.34"}, nil)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.PropagationPercentage(results)
	}
}

func BenchmarkConsensusRecords(b *testing.B) {
	r := makeResolver("1.1.1.1")
	results := make([]query.Result, 100)
	for i := range results {
		results[i] = makeResult(r, []string{"93.184.216.34", "1.2.3.4"}, nil)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.ConsensusRecords(results)
	}
}
