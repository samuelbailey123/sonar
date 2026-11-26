package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samuelbailey123/sonar/internal/checker"
	"github.com/samuelbailey123/sonar/internal/query"
	"github.com/samuelbailey123/sonar/internal/resolver"
)

// ─────────────────────────────────────────────────────────────────────────────
// truncate
// ─────────────────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "shorter than limit",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exactly at limit",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "one over limit",
			input:  "hello!",
			maxLen: 5,
			want:   "hell…",
		},
		{
			name:   "well over limit",
			input:  "this is a very long string that should be truncated",
			maxLen: 10,
			want:   "this is a…",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 5,
			want:   "",
		},
		{
			name:   "maxLen of 1",
			input:  "ab",
			maxLen: 1,
			want:   "…",
		},
		{
			name:   "unicode characters within limit",
			input:  "héllo",
			maxLen: 10,
			want:   "héllo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncate(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q; want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// shortRecord
// ─────────────────────────────────────────────────────────────────────────────

func TestShortRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "A record wire format",
			input: "example.com. 300 IN A 93.184.216.34",
			want:  "93.184.216.34",
		},
		{
			name:  "AAAA record wire format",
			input: "example.com. 3600 IN AAAA 2001:db8::1",
			want:  "2001:db8::1",
		},
		{
			name:  "MX record wire format",
			input: "example.com. 900 IN MX 10 mail.example.com.",
			want:  "mail.example.com.",
		},
		{
			name:  "CNAME record wire format",
			input: "www.example.com. 300 IN CNAME example.com.",
			want:  "example.com.",
		},
		{
			name:  "NS record wire format",
			input: "example.com. 86400 IN NS ns1.example.com.",
			want:  "ns1.example.com.",
		},
		{
			name:  "single token — no leading fields",
			input: "onlythis",
			want:  "onlythis",
		},
		{
			name:  "empty string returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "only whitespace — fields is empty so original is returned",
			input: "   ",
			want:  "   ",
		},
		{
			name:  "two-field record",
			input: "example.com. 93.184.216.34",
			want:  "93.184.216.34",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shortRecord(tc.input)
			if got != tc.want {
				t.Errorf("shortRecord(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestShortRecord_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	// strings.Fields on whitespace-only input returns an empty slice, so the
	// guard `if len(parts) == 0 { return s }` returns the original string
	// unchanged rather than an empty string.
	input := "   \t  "
	got := shortRecord(input)
	if got != input {
		t.Errorf("shortRecord(%q) = %q; want original (unchanged) string %q", input, got, input)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// recordTypeName
// ─────────────────────────────────────────────────────────────────────────────

func TestRecordTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typeID uint16
		want   string
	}{
		{typeID: 1, want: "A"},
		{typeID: 28, want: "AAAA"},
		{typeID: 15, want: "MX"},
		{typeID: 5, want: "CNAME"},
		{typeID: 2, want: "NS"},
		{typeID: 16, want: "TXT"},
		{typeID: 6, want: "SOA"},
		{typeID: 12, want: "PTR"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got := recordTypeName(tc.typeID)
			if got != tc.want {
				t.Errorf("recordTypeName(%d) = %q; want %q", tc.typeID, got, tc.want)
			}
		})
	}
}

func TestRecordTypeName_UnknownFallback(t *testing.T) {
	t.Parallel()

	unknowns := []struct {
		typeID uint16
		want   string
	}{
		{typeID: 0, want: "TYPE0"},
		{typeID: 99, want: "TYPE99"},
		{typeID: 255, want: "TYPE255"},
		{typeID: 65535, want: "TYPE65535"},
	}

	for _, tc := range unknowns {
		t.Run(fmt.Sprintf("TYPE%d", tc.typeID), func(t *testing.T) {
			t.Parallel()
			got := recordTypeName(tc.typeID)
			if got != tc.want {
				t.Errorf("recordTypeName(%d) = %q; want %q", tc.typeID, got, tc.want)
			}
		})
	}
}

func TestRecordTypeName_AllKnownTypes_Count(t *testing.T) {
	t.Parallel()

	// The internal map must have exactly 8 entries. Verify by checking each
	// known type ID does not fall back to the "TYPE%d" format.
	known := []uint16{1, 28, 15, 5, 2, 16, 6, 12}
	for _, id := range known {
		name := recordTypeName(id)
		if strings.HasPrefix(name, "TYPE") {
			t.Errorf("recordTypeName(%d) = %q; expected a named type, not a fallback", id, name)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// locationString
// ─────────────────────────────────────────────────────────────────────────────

func TestLocationString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input resolver.Resolver
		want  string
	}{
		{
			name: "global anycast resolver",
			input: resolver.Resolver{
				Country:   "Global",
				City:      "Anycast",
				Continent: "Global",
			},
			want: "Global, Anycast",
		},
		{
			name: "city and country present",
			input: resolver.Resolver{
				Country:   "Germany",
				City:      "Berlin",
				Continent: "Europe",
			},
			want: "Berlin, Germany",
		},
		{
			name: "country only, no city",
			input: resolver.Resolver{
				Country:   "Japan",
				City:      "",
				Continent: "Asia",
			},
			want: "Japan",
		},
		{
			name: "continent fallback when no country",
			input: resolver.Resolver{
				Country:   "",
				City:      "",
				Continent: "Oceania",
			},
			want: "Oceania",
		},
		{
			name: "all fields empty returns empty continent",
			input: resolver.Resolver{
				Country:   "",
				City:      "",
				Continent: "",
			},
			want: "",
		},
		{
			name: "country equals Global triggers anycast path",
			input: resolver.Resolver{
				Country:   "Global",
				City:      "",
				Continent: "Global",
			},
			want: "Global, Anycast",
		},
		{
			name: "city set but country empty falls through to country branch (empty country skips city+country)",
			input: resolver.Resolver{
				Country:   "",
				City:      "Mystery City",
				Continent: "Unknown",
			},
			want: "Unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := locationString(tc.input)
			if got != tc.want {
				t.Errorf("locationString(%+v) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// checker.PropagationStats
// ─────────────────────────────────────────────────────────────────────────────

// makeResult is a helper that constructs a query.Result for propagation tests.
func makeResult(status query.Status, records ...string) query.Result {
	return query.Result{
		Resolver: resolver.Resolver{IP: "1.2.3.4", Name: "test"},
		Status:   status,
		Records:  records,
	}
}

func TestPropagationStats_EmptyResults(t *testing.T) {
	t.Parallel()

	pct, consensus, _ := checker.PropagationStats(nil)
	if pct != 0 {
		t.Errorf("checker.PropagationStats(nil) pct = %f; want 0", pct)
	}
	if consensus != nil {
		t.Errorf("checker.PropagationStats(nil) consensus = %v; want nil", consensus)
	}

	pct, consensus, _ = checker.PropagationStats([]query.Result{})
	if pct != 0 {
		t.Errorf("checker.PropagationStats([]) pct = %f; want 0", pct)
	}
	if consensus != nil {
		t.Errorf("checker.PropagationStats([]) consensus = %v; want nil", consensus)
	}
}

func TestPropagationStats_AllErrors_ZeroPercent(t *testing.T) {
	t.Parallel()

	results := []query.Result{
		makeResult(query.Error),
		makeResult(query.Error),
		makeResult(query.Timeout),
	}

	pct, consensus, _ := checker.PropagationStats(results)
	if pct != 0 {
		t.Errorf("checker.PropagationStats(all failures) pct = %f; want 0", pct)
	}
	if consensus != nil {
		t.Errorf("checker.PropagationStats(all failures) consensus = %v; want nil", consensus)
	}
}

func TestPropagationStats_AllSuccessAllAgree_100Percent(t *testing.T) {
	t.Parallel()

	results := []query.Result{
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "1.2.3.4"),
	}

	pct, consensus, _ := checker.PropagationStats(results)
	if pct != 100.0 {
		t.Errorf("checker.PropagationStats(all agree) pct = %f; want 100.0", pct)
	}
	if len(consensus) != 1 || consensus[0] != "1.2.3.4" {
		t.Errorf("checker.PropagationStats(all agree) consensus = %v; want [1.2.3.4]", consensus)
	}
}

func TestPropagationStats_MajorityConsensus(t *testing.T) {
	t.Parallel()

	// 3 out of 4 resolvers agree — majority exists.
	results := []query.Result{
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "5.6.7.8"),
	}

	pct, consensus, _ := checker.PropagationStats(results)
	wantPct := 75.0
	if pct != wantPct {
		t.Errorf("checker.PropagationStats pct = %f; want %f", pct, wantPct)
	}
	if len(consensus) != 1 || consensus[0] != "1.2.3.4" {
		t.Errorf("checker.PropagationStats consensus = %v; want [1.2.3.4]", consensus)
	}
}

func TestPropagationStats_NoMajority_NilConsensus(t *testing.T) {
	t.Parallel()

	// Split evenly — no strict majority.
	results := []query.Result{
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "5.6.7.8"),
	}

	_, consensus, _ := checker.PropagationStats(results)
	if consensus != nil {
		t.Errorf("checker.PropagationStats(50/50 split) consensus = %v; want nil", consensus)
	}
}

func TestPropagationStats_MixedSuccessAndFailure(t *testing.T) {
	t.Parallel()

	// 2 succeed with same record, 2 fail — 50% propagation. No majority
	// because 2 > 4/2 is false (not strictly greater than 2).
	results := []query.Result{
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Error),
		makeResult(query.Timeout),
	}

	pct, consensus, _ := checker.PropagationStats(results)
	if pct != 50.0 {
		t.Errorf("checker.PropagationStats pct = %f; want 50.0", pct)
	}
	// 2 successes out of 4 total — 2 is not > 4/2 (integer division: 2 > 2 is false).
	if consensus != nil {
		t.Errorf("checker.PropagationStats(2/4 agree) consensus should be nil (not strict majority); got %v", consensus)
	}
}

func TestPropagationStats_StrictMajorityThreshold(t *testing.T) {
	t.Parallel()

	// 3 agree out of 5 total: 3 > 5/2 (integer: 2) — strict majority, consensus returned.
	results := []query.Result{
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "1.2.3.4"),
		makeResult(query.Success, "9.9.9.9"),
		makeResult(query.Error),
	}

	pct, consensus, _ := checker.PropagationStats(results)
	wantPct := 60.0
	if pct != wantPct {
		t.Errorf("checker.PropagationStats pct = %f; want %f", pct, wantPct)
	}
	if len(consensus) == 0 {
		t.Error("checker.PropagationStats: expected consensus for strict majority (3 > 5/2=2)")
	}
}

func TestPropagationStats_SingleResult_Success(t *testing.T) {
	t.Parallel()

	results := []query.Result{
		makeResult(query.Success, "10.0.0.1"),
	}

	pct, consensus, _ := checker.PropagationStats(results)
	if pct != 100.0 {
		t.Errorf("checker.PropagationStats(single success) pct = %f; want 100.0", pct)
	}
	// 1 > 1/2 (int: 0) — strict majority.
	if len(consensus) != 1 {
		t.Errorf("checker.PropagationStats(single success) consensus = %v; want [10.0.0.1]", consensus)
	}
}

func TestPropagationStats_SingleResult_Error(t *testing.T) {
	t.Parallel()

	results := []query.Result{
		makeResult(query.Error),
	}

	pct, consensus, _ := checker.PropagationStats(results)
	if pct != 0 {
		t.Errorf("checker.PropagationStats(single error) pct = %f; want 0", pct)
	}
	if consensus != nil {
		t.Errorf("checker.PropagationStats(single error) consensus = %v; want nil", consensus)
	}
}

func TestPropagationStats_MultipleRecordsPerResult(t *testing.T) {
	t.Parallel()

	// Results with multi-record sets: different orderings must map to the same key.
	results := []query.Result{
		makeResult(query.Success, "1.2.3.4", "5.6.7.8"),
		makeResult(query.Success, "5.6.7.8", "1.2.3.4"),
		makeResult(query.Success, "1.2.3.4", "5.6.7.8"),
	}

	pct, consensus, _ := checker.PropagationStats(results)
	if pct != 100.0 {
		t.Errorf("checker.PropagationStats(multi-record sets, same content) pct = %f; want 100.0", pct)
	}
	if len(consensus) != 2 {
		t.Errorf("checker.PropagationStats consensus length = %d; want 2 records", len(consensus))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// toJSONResults
// ─────────────────────────────────────────────────────────────────────────────

func TestToJSONResults_Empty(t *testing.T) {
	t.Parallel()

	got := toJSONResults(nil)
	if len(got) != 0 {
		t.Errorf("toJSONResults(nil) len = %d; want 0", len(got))
	}

	got = toJSONResults([]query.Result{})
	if len(got) != 0 {
		t.Errorf("toJSONResults([]) len = %d; want 0", len(got))
	}
}

func TestToJSONResults_SuccessResult(t *testing.T) {
	t.Parallel()

	r := resolver.Resolver{
		IP:        "8.8.8.8",
		Name:      "Google Primary",
		Country:   "Global",
		City:      "Anycast",
		Continent: "Global",
	}
	result := query.Result{
		Resolver: r,
		Status:   query.Success,
		Records:  []string{"93.184.216.34"},
		Latency:  42 * time.Millisecond,
		Error:    nil,
	}

	got := toJSONResults([]query.Result{result})

	if len(got) != 1 {
		t.Fatalf("toJSONResults len = %d; want 1", len(got))
	}
	jr := got[0]

	if jr.ResolverName != r.Name {
		t.Errorf("ResolverName = %q; want %q", jr.ResolverName, r.Name)
	}
	if jr.ResolverIP != r.IP {
		t.Errorf("ResolverIP = %q; want %q", jr.ResolverIP, r.IP)
	}
	if jr.Country != r.Country {
		t.Errorf("Country = %q; want %q", jr.Country, r.Country)
	}
	if jr.City != r.City {
		t.Errorf("City = %q; want %q", jr.City, r.City)
	}
	if jr.Continent != r.Continent {
		t.Errorf("Continent = %q; want %q", jr.Continent, r.Continent)
	}
	if jr.Status != "success" {
		t.Errorf("Status = %q; want %q", jr.Status, "success")
	}
	if len(jr.Records) != 1 || jr.Records[0] != "93.184.216.34" {
		t.Errorf("Records = %v; want [93.184.216.34]", jr.Records)
	}
	if jr.LatencyMS != 42 {
		t.Errorf("LatencyMS = %d; want 42", jr.LatencyMS)
	}
	if jr.Error != "" {
		t.Errorf("Error = %q; want empty", jr.Error)
	}
}

func TestToJSONResults_ErrorResult(t *testing.T) {
	t.Parallel()

	result := query.Result{
		Resolver: resolver.Resolver{IP: "1.1.1.1", Name: "CF"},
		Status:   query.Error,
		Records:  nil,
		Latency:  5 * time.Millisecond,
		Error:    errors.New("connection refused"),
	}

	got := toJSONResults([]query.Result{result})

	if len(got) != 1 {
		t.Fatalf("toJSONResults len = %d; want 1", len(got))
	}
	if got[0].Error != "connection refused" {
		t.Errorf("Error = %q; want %q", got[0].Error, "connection refused")
	}
	if got[0].Status != "error" {
		t.Errorf("Status = %q; want error", got[0].Status)
	}
}

func TestToJSONResults_TimeoutResult(t *testing.T) {
	t.Parallel()

	result := query.Result{
		Resolver: resolver.Resolver{IP: "9.9.9.9", Name: "Quad9"},
		Status:   query.Timeout,
		Records:  nil,
		Latency:  5000 * time.Millisecond,
		Error:    errors.New("context deadline exceeded"),
	}

	got := toJSONResults([]query.Result{result})

	if len(got) != 1 {
		t.Fatalf("toJSONResults len = %d; want 1", len(got))
	}
	if got[0].Status != "timeout" {
		t.Errorf("Status = %q; want timeout", got[0].Status)
	}
	if got[0].LatencyMS != 5000 {
		t.Errorf("LatencyMS = %d; want 5000", got[0].LatencyMS)
	}
}

func TestToJSONResults_PreservesOrder(t *testing.T) {
	t.Parallel()

	results := make([]query.Result, 10)
	for i := range results {
		results[i] = query.Result{
			Resolver: resolver.Resolver{
				IP:   fmt.Sprintf("10.0.0.%d", i),
				Name: fmt.Sprintf("Resolver-%d", i),
			},
			Status:  query.Success,
			Records: []string{fmt.Sprintf("192.168.1.%d", i)},
			Latency: time.Duration(i) * time.Millisecond,
		}
	}

	got := toJSONResults(results)

	if len(got) != len(results) {
		t.Fatalf("toJSONResults len = %d; want %d", len(got), len(results))
	}
	for i := range results {
		if got[i].ResolverIP != results[i].Resolver.IP {
			t.Errorf("index %d: ResolverIP = %q; want %q", i, got[i].ResolverIP, results[i].Resolver.IP)
		}
	}
}

func TestToJSONResults_NilRecordsPreserved(t *testing.T) {
	t.Parallel()

	result := query.Result{
		Resolver: resolver.Resolver{IP: "1.2.3.4", Name: "test"},
		Status:   query.Success,
		Records:  nil,
	}

	got := toJSONResults([]query.Result{result})
	if got[0].Records != nil {
		t.Errorf("Records = %v; want nil", got[0].Records)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// configureLogging
// ─────────────────────────────────────────────────────────────────────────────

func TestConfigureLogging_VerboseTrue_SetsDebugLevel(t *testing.T) {
	// Not parallel: mutates the global slog default handler.
	configureLogging(true)

	logger := slog.Default()
	if !logger.Enabled(nil, slog.LevelDebug) {
		t.Error("configureLogging(true) should enable DEBUG level")
	}
	if !logger.Enabled(nil, slog.LevelWarn) {
		t.Error("configureLogging(true) should still enable WARN level")
	}
}

func TestConfigureLogging_VerboseFalse_SetsWarnLevel(t *testing.T) {
	// Not parallel: mutates the global slog default handler.
	configureLogging(false)

	logger := slog.Default()
	if logger.Enabled(nil, slog.LevelDebug) {
		t.Error("configureLogging(false) should NOT enable DEBUG level")
	}
	if logger.Enabled(nil, slog.LevelInfo) {
		t.Error("configureLogging(false) should NOT enable INFO level")
	}
	if !logger.Enabled(nil, slog.LevelWarn) {
		t.Error("configureLogging(false) should enable WARN level")
	}
	if !logger.Enabled(nil, slog.LevelError) {
		t.Error("configureLogging(false) should enable ERROR level")
	}
}

func TestConfigureLogging_Toggle(t *testing.T) {
	// Not parallel: mutates the global slog default handler.

	configureLogging(true)
	if !slog.Default().Enabled(nil, slog.LevelDebug) {
		t.Fatal("expected DEBUG after configureLogging(true)")
	}

	configureLogging(false)
	if slog.Default().Enabled(nil, slog.LevelDebug) {
		t.Fatal("expected DEBUG suppressed after configureLogging(false)")
	}

	configureLogging(true)
	if !slog.Default().Enabled(nil, slog.LevelDebug) {
		t.Fatal("expected DEBUG re-enabled after second configureLogging(true)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// printBanner
// ─────────────────────────────────────────────────────────────────────────────

func TestPrintBanner_DoesNotPanic(t *testing.T) {
	// Not parallel — mutates os.Stdout.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	printBanner()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck
	os.Stdout = old
	r.Close()

	if buf.Len() == 0 {
		t.Error("printBanner() produced no output on stdout")
	}

	// The ASCII art contains block characters.
	output := buf.String()
	if !strings.Contains(output, "█") {
		t.Error("printBanner() output does not contain expected ASCII art block characters")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Execute — flag parsing (os.Args override)
// ─────────────────────────────────────────────────────────────────────────────

func TestExecute_Version_ReturnsNil(t *testing.T) {
	// Not parallel: temporarily overrides os.Args and os.Stdout.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	os.Args = []string{"sonar", "--version"}
	err = Execute()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck
	r.Close()

	if err != nil {
		t.Errorf("Execute() with --version returned error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("Execute() with --version produced no output")
	}
}

func TestExecute_NoArgs_ReturnsError(t *testing.T) {
	// Not parallel: temporarily overrides os.Args.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Suppress stderr output from Usage().
	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	defer func() {
		os.Stderr = oldStderr
		devNull.Close()
	}()

	os.Args = []string{"sonar"}
	err := Execute()

	if err == nil {
		t.Error("Execute() with no domain argument should return an error")
	}
	if !strings.Contains(err.Error(), "domain") {
		t.Errorf("Execute() error should mention 'domain'; got: %v", err)
	}
}

func TestExecute_TooManyArgs_ReturnsError(t *testing.T) {
	// Not parallel: temporarily overrides os.Args.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	defer func() {
		os.Stderr = oldStderr
		devNull.Close()
	}()

	os.Args = []string{"sonar", "example.com", "extra-arg"}
	err := Execute()

	if err == nil {
		t.Error("Execute() with too many arguments should return an error")
	}
	if !strings.Contains(err.Error(), "unexpected arguments") {
		t.Errorf("Execute() error should mention 'unexpected arguments'; got: %v", err)
	}
}

func TestExecute_InvalidType_ReturnsError(t *testing.T) {
	// Not parallel: temporarily overrides os.Args.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	defer func() {
		os.Stderr = oldStderr
		devNull.Close()
	}()

	// Flags must precede the domain positional argument because the flag set
	// stops parsing at the first non-flag token.
	os.Args = []string{"sonar", "--type", "BOGUS", "example.com"}
	err := Execute()

	if err == nil {
		t.Error("Execute() with invalid --type should return an error")
	}
	if !strings.Contains(err.Error(), "invalid --type value") {
		t.Errorf("Execute() error should mention 'invalid --type value'; got: %v", err)
	}
}

func TestExecute_InvalidFlag_ReturnsError(t *testing.T) {
	// Not parallel: temporarily overrides os.Args.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	defer func() {
		os.Stderr = oldStderr
		devNull.Close()
	}()

	os.Args = []string{"sonar", "--nonexistent-flag"}
	err := Execute()

	if err == nil {
		t.Error("Execute() with an unknown flag should return an error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkTruncate(b *testing.B) {
	b.ReportAllocs()
	s := "this is a string that needs to be truncated for display"
	b.ResetTimer()
	for range b.N {
		truncate(s, 27)
	}
}

func BenchmarkShortRecord(b *testing.B) {
	b.ReportAllocs()
	s := "example.com. 300 IN A 93.184.216.34"
	b.ResetTimer()
	for range b.N {
		shortRecord(s)
	}
}

func BenchmarkRecordTypeName(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		recordTypeName(1)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// printRow
// ─────────────────────────────────────────────────────────────────────────────

// captureStdout redirects os.Stdout to a pipe for the duration of fn, then
// returns all bytes written to stdout. It is NOT safe to call from parallel
// subtests because it mutates the global os.Stdout pointer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureStdout: os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("captureStdout: io.Copy: %v", err)
	}
	os.Stdout = old
	r.Close()

	return buf.String()
}

func TestPrintRow_Success_SingleRecord(t *testing.T) {
	// Not parallel — mutates os.Stdout.
	r := query.Result{
		Resolver: resolver.Resolver{
			Name:      "Google Primary",
			IP:        "8.8.8.8",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},
		Records: []string{"example.com. 300 IN A 93.184.216.34"},
		Status:  query.Success,
		Latency: 25 * time.Millisecond,
	}

	out := captureStdout(t, func() { printRow(r) })

	if !strings.Contains(out, "Google Primary") {
		t.Errorf("printRow(Success) output missing resolver name; got: %q", out)
	}
	if !strings.Contains(out, "Global") {
		t.Errorf("printRow(Success) output missing location; got: %q", out)
	}
	if !strings.Contains(out, "25ms") {
		t.Errorf("printRow(Success) output missing latency; got: %q", out)
	}
}

func TestPrintRow_Success_MultipleRecords(t *testing.T) {
	// Not parallel — mutates os.Stdout.
	r := query.Result{
		Resolver: resolver.Resolver{
			Name:      "Cloudflare",
			IP:        "1.1.1.1",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},
		Records: []string{
			"example.com. 300 IN A 1.2.3.4",
			"example.com. 300 IN A 5.6.7.8",
			"example.com. 300 IN A 9.10.11.12",
		},
		Status:  query.Success,
		Latency: 10 * time.Millisecond,
	}

	out := captureStdout(t, func() { printRow(r) })

	// The row must mention the extra-record count: "+2".
	if !strings.Contains(out, "+2") {
		t.Errorf("printRow(Success, 3 records) output missing '+2' suffix; got: %q", out)
	}
}

func TestPrintRow_Success_NoRecords(t *testing.T) {
	// Not parallel — mutates os.Stdout.
	r := query.Result{
		Resolver: resolver.Resolver{
			Name:      "Quad9",
			IP:        "9.9.9.9",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},
		Records: nil,
		Status:  query.Success,
		Latency: 5 * time.Millisecond,
	}

	out := captureStdout(t, func() { printRow(r) })

	if !strings.Contains(out, "NODATA") {
		t.Errorf("printRow(Success, no records) output missing NODATA; got: %q", out)
	}
}

func TestPrintRow_Timeout(t *testing.T) {
	// Not parallel — mutates os.Stdout.
	r := query.Result{
		Resolver: resolver.Resolver{
			Name:      "Slow Resolver",
			IP:        "1.2.3.4",
			Country:   "United States",
			City:      "New York",
			Continent: "North America",
		},
		Records: nil,
		Status:  query.Timeout,
		Latency: 5000 * time.Millisecond,
	}

	out := captureStdout(t, func() { printRow(r) })

	if !strings.Contains(out, "timeout") {
		t.Errorf("printRow(Timeout) output missing 'timeout'; got: %q", out)
	}
	if !strings.Contains(out, "5000ms") {
		t.Errorf("printRow(Timeout) output missing latency; got: %q", out)
	}
}

func TestPrintRow_Error_WithMessage(t *testing.T) {
	// Not parallel — mutates os.Stdout.
	r := query.Result{
		Resolver: resolver.Resolver{
			Name:      "Broken DNS",
			IP:        "10.0.0.1",
			Country:   "Germany",
			City:      "Berlin",
			Continent: "Europe",
		},
		Records: nil,
		Status:  query.Error,
		Error:   fmt.Errorf("connection refused"),
		Latency: 1 * time.Millisecond,
	}

	out := captureStdout(t, func() { printRow(r) })

	if !strings.Contains(out, "connectio") {
		t.Errorf("printRow(Error) output missing error message prefix; got: %q", out)
	}
	if !strings.Contains(out, "Berlin, Germany") {
		t.Errorf("printRow(Error) output missing location; got: %q", out)
	}
}

func TestPrintRow_Error_NilError(t *testing.T) {
	// Not parallel — mutates os.Stdout.
	r := query.Result{
		Resolver: resolver.Resolver{
			Name:      "Mystery Resolver",
			IP:        "10.0.0.2",
			Country:   "Japan",
			City:      "Tokyo",
			Continent: "Asia",
		},
		Records: nil,
		Status:  query.Error,
		Error:   nil, // explicitly nil
		Latency: 2 * time.Millisecond,
	}

	out := captureStdout(t, func() { printRow(r) })

	// Falls into the default nil-error branch — should render generic "error".
	if !strings.Contains(out, "error") {
		t.Errorf("printRow(Error, nil error) output missing 'error'; got: %q", out)
	}
}

func TestPrintRow_ProducesNewline(t *testing.T) {
	// Not parallel — mutates os.Stdout.
	r := query.Result{
		Resolver: resolver.Resolver{Name: "Test", IP: "1.2.3.4"},
		Status:   query.Success,
		Records:  []string{"example.com. 60 IN A 1.2.3.4"},
		Latency:  1 * time.Millisecond,
	}

	out := captureStdout(t, func() { printRow(r) })

	if !strings.HasSuffix(out, "\n") {
		t.Errorf("printRow output should end with newline; got: %q", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runJSON
// ─────────────────────────────────────────────────────────────────────────────

// minimalConfig returns a checker.Config with a small, fast subset of global
// anycast resolvers and a single worker so integration tests complete quickly.
func minimalConfig(domain string) checker.Config {
	return checker.Config{
		Domain:     domain,
		RecordType: 1, // TypeA
		Resolvers: []resolver.Resolver{
			{
				IP:        "8.8.8.8",
				Name:      "Google Primary",
				Country:   "Global",
				City:      "Anycast",
				Continent: "Global",
			},
			{
				IP:        "1.1.1.1",
				Name:      "Cloudflare Primary",
				Country:   "Global",
				City:      "Anycast",
				Continent: "Global",
			},
		},
		MaxWorkers: 2,
	}
}

func TestRunJSON_WritesValidJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	cfg := minimalConfig("example.com")

	out := captureStdout(t, func() {
		if err := runJSON(cfg); err != nil {
			t.Errorf("runJSON returned unexpected error: %v", err)
		}
	})

	if len(out) == 0 {
		t.Fatal("runJSON produced no output on stdout")
	}

	var got jsonOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("runJSON output is not valid JSON: %v\noutput: %s", err, out)
	}

	if got.Domain != "example.com" {
		t.Errorf("runJSON domain = %q; want %q", got.Domain, "example.com")
	}
	if got.RecordType != "A" {
		t.Errorf("runJSON record_type = %q; want %q", got.RecordType, "A")
	}
	if got.Timestamp.IsZero() {
		t.Error("runJSON timestamp should not be zero")
	}
	if len(got.Results) != len(cfg.Resolvers) {
		t.Errorf("runJSON result count = %d; want %d", len(got.Results), len(cfg.Resolvers))
	}
	if got.PropagationPct < 0 || got.PropagationPct > 100 {
		t.Errorf("runJSON propagation_pct = %f; want value in [0, 100]", got.PropagationPct)
	}
}

func TestRunJSON_ResultsContainResolverInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	cfg := minimalConfig("example.com")

	out := captureStdout(t, func() {
		if err := runJSON(cfg); err != nil {
			t.Errorf("runJSON returned unexpected error: %v", err)
		}
	})

	var got jsonOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("runJSON output is not valid JSON: %v", err)
	}

	// Verify each result carries resolver metadata.
	for i, jr := range got.Results {
		if jr.ResolverName == "" {
			t.Errorf("result[%d].resolver_name is empty", i)
		}
		if jr.ResolverIP == "" {
			t.Errorf("result[%d].resolver_ip is empty", i)
		}
		if jr.Status == "" {
			t.Errorf("result[%d].status is empty", i)
		}
		if jr.LatencyMS < 0 {
			t.Errorf("result[%d].latency_ms = %d; must be non-negative", i, jr.LatencyMS)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runPlain
// ─────────────────────────────────────────────────────────────────────────────

func TestRunPlain_PrintsTableHeaderAndSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	cfg := minimalConfig("example.com")

	out := captureStdout(t, func() {
		if err := runPlain(cfg); err != nil {
			t.Errorf("runPlain returned unexpected error: %v", err)
		}
	})

	if len(out) == 0 {
		t.Fatal("runPlain produced no output on stdout")
	}

	// The table header must name all columns.
	if !strings.Contains(out, "Resolver") {
		t.Errorf("runPlain output missing 'Resolver' column header; got: %q", out)
	}
	if !strings.Contains(out, "Status") {
		t.Errorf("runPlain output missing 'Status' column header; got: %q", out)
	}
	if !strings.Contains(out, "Latency") {
		t.Errorf("runPlain output missing 'Latency' column header; got: %q", out)
	}

	// The propagation summary line must be present.
	if !strings.Contains(out, "Propagation") {
		t.Errorf("runPlain output missing 'Propagation' summary line; got: %q", out)
	}

	// At least one resolver row must appear.
	if !strings.Contains(out, "Google Primary") && !strings.Contains(out, "Cloudflare Primary") {
		t.Errorf("runPlain output missing resolver rows; got: %q", out)
	}
}

func TestRunPlain_IncludesResolverNames(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	cfg := minimalConfig("example.com")

	out := captureStdout(t, func() {
		if err := runPlain(cfg); err != nil {
			t.Errorf("runPlain returned unexpected error: %v", err)
		}
	})

	// Both configured resolver names must appear somewhere in the output.
	for _, res := range cfg.Resolvers {
		if !strings.Contains(out, res.Name) {
			t.Errorf("runPlain output missing resolver %q; output:\n%s", res.Name, out)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Execute — json and no-tui dispatch paths
// ─────────────────────────────────────────────────────────────────────────────

func TestExecute_JSONMode_ProducesJSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	// Not parallel — mutates os.Args and os.Stdout.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Suppress stderr (usage/logging output).
	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	defer func() {
		os.Stderr = oldStderr
		devNull.Close()
	}()

	os.Args = []string{"sonar", "--json", "--workers", "2", "example.com"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute(--json) returned error: %v", err)
		}
	})

	if len(out) == 0 {
		t.Fatal("Execute(--json) produced no stdout output")
	}

	var got jsonOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("Execute(--json) output is not valid JSON: %v\noutput: %s", err, out)
	}

	if got.Domain != "example.com" {
		t.Errorf("Execute(--json) domain = %q; want %q", got.Domain, "example.com")
	}
}

func TestExecute_NoTUIMode_PrintsTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in short mode")
	}

	// Not parallel — mutates os.Args and os.Stdout.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	defer func() {
		os.Stderr = oldStderr
		devNull.Close()
	}()

	os.Args = []string{"sonar", "--no-tui", "--workers", "2", "example.com"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute(--no-tui) returned error: %v", err)
		}
	})

	if len(out) == 0 {
		t.Fatal("Execute(--no-tui) produced no stdout output")
	}

	if !strings.Contains(out, "Propagation") {
		t.Errorf("Execute(--no-tui) output missing 'Propagation' summary; got: %q", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkPropagationStats(b *testing.B) {
	b.ReportAllocs()
	results := make([]query.Result, 50)
	for i := range results {
		results[i] = makeResult(query.Success, fmt.Sprintf("10.0.0.%d", i%5))
	}
	b.ResetTimer()
	for range b.N {
		checker.PropagationStats(results)
	}
}

func BenchmarkToJSONResults(b *testing.B) {
	b.ReportAllocs()
	results := make([]query.Result, 50)
	for i := range results {
		results[i] = query.Result{
			Resolver: resolver.Resolver{
				IP:        fmt.Sprintf("10.0.0.%d", i),
				Name:      fmt.Sprintf("Resolver-%d", i),
				Country:   "United States",
				City:      "New York",
				Continent: "North America",
			},
			Status:  query.Success,
			Records: []string{"93.184.216.34"},
			Latency: 50 * time.Millisecond,
		}
	}
	b.ResetTimer()
	for range b.N {
		toJSONResults(results)
	}
}
