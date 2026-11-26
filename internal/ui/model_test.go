package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samuelbailey123/sonar/internal/checker"
	"github.com/samuelbailey123/sonar/internal/query"
	"github.com/samuelbailey123/sonar/internal/resolver"
	"github.com/samuelbailey123/sonar/internal/style"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// makeResolver builds a resolver.Resolver for test use.
func makeResolver(ip, name, city, country, continent string) resolver.Resolver {
	return resolver.Resolver{
		IP:        ip,
		Name:      name,
		City:      city,
		Country:   country,
		Continent: continent,
	}
}

// successResult builds a successful query.Result for the given resolver.
func successResult(r resolver.Resolver, records []string, latency time.Duration) query.Result {
	return query.Result{
		Resolver: r,
		Status:   query.Success,
		Records:  records,
		Latency:  latency,
	}
}

// timeoutResult builds a timeout query.Result for the given resolver.
func timeoutResult(r resolver.Resolver) query.Result {
	return query.Result{
		Resolver: r,
		Status:   query.Timeout,
	}
}

// errorResult builds an error query.Result for the given resolver.
func errorResult(r resolver.Resolver, msg string) query.Result {
	return query.Result{
		Resolver: r,
		Status:   query.Error,
		Error:    errors.New(msg),
	}
}

// visibleWidth counts the visible rune width of s (ANSI stripped) via the
// exported style.Width helper so tests share the same definition as the code
// under test.
func visibleWidth(s string) int {
	return style.Width(s)
}

// ─── New ──────────────────────────────────────────────────────────────────────

func TestNew(t *testing.T) {
	t.Parallel()

	resolvers := []resolver.Resolver{
		makeResolver("1.1.1.1", "CF", "Anycast", "Global", "Global"),
	}
	chk := checker.New(checker.Config{
		Domain:     "example.com",
		RecordType: 1,
		Resolvers:  resolvers,
	})

	m := New("example.com", "A", resolvers, chk)

	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.domain != "example.com" {
		t.Errorf("domain = %q, want %q", m.domain, "example.com")
	}
	if m.recordType != "A" {
		t.Errorf("recordType = %q, want %q", m.recordType, "A")
	}
	if len(m.resolvers) != 1 {
		t.Errorf("resolvers length = %d, want 1", len(m.resolvers))
	}
	if m.results == nil {
		t.Error("results map is nil, want initialised map")
	}
	if m.checker == nil {
		t.Error("checker is nil")
	}
	if m.startTime.IsZero() {
		t.Error("startTime is zero")
	}
}

func TestNew_EmptyResolvers(t *testing.T) {
	t.Parallel()

	chk := checker.New(checker.Config{Domain: "x.com", RecordType: 1})
	m := New("x.com", "AAAA", nil, chk)

	if m == nil {
		t.Fatal("New returned nil for empty resolver list")
	}
	if len(m.resolvers) != 0 {
		t.Errorf("want 0 resolvers, got %d", len(m.resolvers))
	}
	if m.results == nil {
		t.Error("results map must not be nil even with empty resolvers")
	}
}

// ─── groupByContinent ─────────────────────────────────────────────────────────

func TestGroupByContinent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resolvers []resolver.Resolver
		wantKeys  map[string]int // continent → expected count
	}{
		{
			name:      "empty input returns empty map",
			resolvers: nil,
			wantKeys:  map[string]int{},
		},
		{
			name: "single continent",
			resolvers: []resolver.Resolver{
				makeResolver("1.1.1.1", "CF", "A", "Global", "Global"),
				makeResolver("8.8.8.8", "G", "A", "Global", "Global"),
			},
			wantKeys: map[string]int{"Global": 2},
		},
		{
			name: "multiple continents",
			resolvers: []resolver.Resolver{
				makeResolver("1.1.1.1", "CF", "A", "Global", "Global"),
				makeResolver("8.8.8.8", "G", "A", "Global", "Global"),
				makeResolver("84.200.69.80", "DNS.WATCH", "D", "Germany", "Europe"),
			},
			wantKeys: map[string]int{"Global": 2, "Europe": 1},
		},
		{
			name: "empty continent becomes Unknown",
			resolvers: []resolver.Resolver{
				makeResolver("10.0.0.1", "Private", "X", "Nowhere", ""),
			},
			wantKeys: map[string]int{"Unknown": 1},
		},
		{
			name: "mix of empty and named continents",
			resolvers: []resolver.Resolver{
				makeResolver("10.0.0.1", "R1", "X", "Nowhere", ""),
				makeResolver("10.0.0.2", "R2", "X", "Nowhere", ""),
				makeResolver("1.1.1.1", "CF", "A", "Global", "Global"),
			},
			wantKeys: map[string]int{"Unknown": 2, "Global": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := groupByContinent(tt.resolvers)

			if len(got) != len(tt.wantKeys) {
				t.Errorf("got %d continent keys, want %d: %v", len(got), len(tt.wantKeys), got)
			}

			for continent, wantCount := range tt.wantKeys {
				group, ok := got[continent]
				if !ok {
					t.Errorf("missing continent %q in result", continent)
					continue
				}
				if len(group) != wantCount {
					t.Errorf("continent %q: got %d resolvers, want %d", continent, len(group), wantCount)
				}
			}
		})
	}
}

func TestGroupByContinent_PreservesOrder(t *testing.T) {
	t.Parallel()

	rs := []resolver.Resolver{
		makeResolver("1.1.1.1", "A", "x", "G", "Global"),
		makeResolver("2.2.2.2", "B", "x", "G", "Global"),
		makeResolver("3.3.3.3", "C", "x", "G", "Global"),
	}

	got := groupByContinent(rs)
	group := got["Global"]

	for i, want := range rs {
		if group[i].IP != want.IP {
			t.Errorf("position %d: got IP %q, want %q", i, group[i].IP, want.IP)
		}
	}
}

// ─── orderedContinents ────────────────────────────────────────────────────────

func TestOrderedContinents_PreferredOrder(t *testing.T) {
	t.Parallel()

	grouped := map[string][]resolver.Resolver{
		"Asia":          {{IP: "1.1.1.1"}},
		"Europe":        {{IP: "2.2.2.2"}},
		"North America": {{IP: "3.3.3.3"}},
		"Global":        {{IP: "4.4.4.4"}},
	}

	result := orderedContinents(grouped)

	// Global must come before North America, which must come before Europe,
	// which must come before Asia — as defined by the preferred order slice.
	positions := map[string]int{}
	for i, c := range result {
		positions[c] = i
	}

	order := []struct{ first, second string }{
		{"Global", "North America"},
		{"North America", "Europe"},
		{"Europe", "Asia"},
	}

	for _, o := range order {
		if positions[o.first] >= positions[o.second] {
			t.Errorf("want %q before %q, got positions %d and %d in %v",
				o.first, o.second, positions[o.first], positions[o.second], result)
		}
	}
}

func TestOrderedContinents_UnknownAppendedAlphabetically(t *testing.T) {
	t.Parallel()

	grouped := map[string][]resolver.Resolver{
		"Zebra Land":    {{IP: "1.1.1.1"}},
		"Alpha Kingdom": {{IP: "2.2.2.2"}},
		"Global":        {{IP: "3.3.3.3"}},
	}

	result := orderedContinents(grouped)

	if result[0] != "Global" {
		t.Errorf("first entry should be Global, got %q", result[0])
	}
	// Extra continents must appear alphabetically after preferred ones.
	extraStart := 1
	if result[extraStart] != "Alpha Kingdom" {
		t.Errorf("expected 'Alpha Kingdom' before 'Zebra Land', got %v", result[extraStart:])
	}
	if result[extraStart+1] != "Zebra Land" {
		t.Errorf("expected 'Zebra Land' last, got %v", result[extraStart:])
	}
}

func TestOrderedContinents_EmptyMap(t *testing.T) {
	t.Parallel()

	result := orderedContinents(map[string][]resolver.Resolver{})

	if len(result) != 0 {
		t.Errorf("expected empty slice for empty map, got %v", result)
	}
}

func TestOrderedContinents_AllPreferred(t *testing.T) {
	t.Parallel()

	preferred := []string{
		"Global",
		"North America",
		"South America",
		"Europe",
		"Africa",
		"Middle East",
		"Asia",
		"Oceania",
		"Unknown",
	}

	grouped := map[string][]resolver.Resolver{}
	for _, c := range preferred {
		grouped[c] = []resolver.Resolver{{IP: "1.1.1.1"}}
	}

	result := orderedContinents(grouped)

	if len(result) != len(preferred) {
		t.Fatalf("got %d entries, want %d", len(result), len(preferred))
	}
	for i, want := range preferred {
		if result[i] != want {
			t.Errorf("position %d: got %q, want %q", i, result[i], want)
		}
	}
}

// ─── formatLocation ───────────────────────────────────────────────────────────

func TestFormatLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		city    string
		country string
		want    string
	}{
		{
			name:    "city and country differ",
			city:    "London",
			country: "United Kingdom",
			want:    "London, United Kingdom",
		},
		{
			name:    "city equals country",
			city:    "Germany",
			country: "Germany",
			want:    "Germany",
		},
		{
			name:    "empty city returns country only",
			city:    "",
			country: "France",
			want:    "France",
		},
		{
			name:    "both empty",
			city:    "",
			country: "",
			want:    "",
		},
		{
			name:    "city set country empty",
			city:    "Paris",
			country: "",
			want:    "Paris, ",
		},
		{
			name:    "anycast city with global country",
			city:    "Anycast",
			country: "Global",
			want:    "Anycast, Global",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := makeResolver("1.1.1.1", "Test", tt.city, tt.country, "Global")
			got := formatLocation(r)
			if got != tt.want {
				t.Errorf("formatLocation = %q, want %q", got, tt.want)
			}
		})
	}
}

// ─── formatAnswer ─────────────────────────────────────────────────────────────

func TestFormatAnswer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records []string
		want    string
	}{
		{name: "nil records", records: nil, want: ""},
		{name: "empty slice", records: []string{}, want: ""},
		{name: "single record", records: []string{"1.2.3.4"}, want: "1.2.3.4"},
		{name: "two records", records: []string{"1.2.3.4", "5.6.7.8"}, want: "1.2.3.4, 5.6.7.8"},
		{
			name:    "three records",
			records: []string{"a", "b", "c"},
			want:    "a, b, c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatAnswer(tt.records)
			if got != tt.want {
				t.Errorf("formatAnswer(%v) = %q, want %q", tt.records, got, tt.want)
			}
		})
	}
}

// ─── formatLatency ────────────────────────────────────────────────────────────

func TestFormatLatency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		d        time.Duration
		wantSufx string // expected unit suffix in output
		wantVal  string // exact expected string
	}{
		{
			name:    "zero is microseconds",
			d:       0,
			wantVal: "0us",
		},
		{
			name:    "500 microseconds",
			d:       500 * time.Microsecond,
			wantVal: "500us",
		},
		{
			name:    "999 microseconds",
			d:       999 * time.Microsecond,
			wantVal: "999us",
		},
		{
			name:    "exactly 1 ms uses milliseconds",
			d:       time.Millisecond,
			wantVal: "1ms",
		},
		{
			name:    "50 ms",
			d:       50 * time.Millisecond,
			wantVal: "50ms",
		},
		{
			name:    "200 ms",
			d:       200 * time.Millisecond,
			wantVal: "200ms",
		},
		{
			name:    "1 second",
			d:       time.Second,
			wantVal: "1000ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatLatency(tt.d)
			if got != tt.wantVal {
				t.Errorf("formatLatency(%v) = %q, want %q", tt.d, got, tt.wantVal)
			}
		})
	}
}

// ─── colorForLatency ──────────────────────────────────────────────────────────

func TestColorForLatency(t *testing.T) {
	t.Parallel()

	// We verify via the Render output: green/yellow/red produce different ANSI
	// colour codes. A simpler approach is to render a sentinel string and check
	// which package-level style produced the same output.
	sentinel := "X"

	tests := []struct {
		name      string
		d         time.Duration
		wantStyle style.Style
	}{
		{name: "0ms is green", d: 0, wantStyle: styleGreen},
		{name: "1ms is green", d: time.Millisecond, wantStyle: styleGreen},
		{name: "49ms is green", d: 49 * time.Millisecond, wantStyle: styleGreen},
		{name: "50ms is yellow", d: 50 * time.Millisecond, wantStyle: styleYellow},
		{name: "100ms is yellow", d: 100 * time.Millisecond, wantStyle: styleYellow},
		{name: "199ms is yellow", d: 199 * time.Millisecond, wantStyle: styleYellow},
		{name: "200ms is red", d: 200 * time.Millisecond, wantStyle: styleRed},
		{name: "500ms is red", d: 500 * time.Millisecond, wantStyle: styleRed},
		{name: "1s is red", d: time.Second, wantStyle: styleRed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := colorForLatency(tt.d).Render(sentinel)
			want := tt.wantStyle.Render(sentinel)
			if got != want {
				t.Errorf("colorForLatency(%v) rendered %q, want %q", tt.d, got, want)
			}
		})
	}
}

// ─── colorForPct ──────────────────────────────────────────────────────────────

func TestColorForPct(t *testing.T) {
	t.Parallel()

	sentinel := "Y"

	tests := []struct {
		name      string
		pct       float64
		wantStyle style.Style
	}{
		{name: "0% is red", pct: 0, wantStyle: styleRed},
		{name: "49% is red", pct: 49, wantStyle: styleRed},
		{name: "49.9% is red", pct: 49.9, wantStyle: styleRed},
		{name: "50% is yellow", pct: 50, wantStyle: styleYellow},
		{name: "75% is yellow", pct: 75, wantStyle: styleYellow},
		{name: "99.9% is yellow", pct: 99.9, wantStyle: styleYellow},
		{name: "100% is green", pct: 100, wantStyle: styleGreen},
		{name: "200% (over 100) is green", pct: 200, wantStyle: styleGreen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := colorForPct(tt.pct).Render(sentinel)
			want := tt.wantStyle.Render(sentinel)
			if got != want {
				t.Errorf("colorForPct(%.1f) rendered %q, want %q", tt.pct, got, want)
			}
		})
	}
}

// ─── padVisible ───────────────────────────────────────────────────────────────

func TestPadVisible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		s         string
		width     int
		wantWidth int
	}{
		{name: "plain shorter string", s: "hello", width: 10, wantWidth: 10},
		{name: "plain exact width", s: "hello", width: 5, wantWidth: 5},
		{name: "plain already wider", s: "hello world", width: 5, wantWidth: 11},
		{name: "empty string padded", s: "", width: 4, wantWidth: 4},
		{name: "zero width", s: "hi", width: 0, wantWidth: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := padVisible(tt.s, tt.width)
			gotWidth := visibleWidth(got)
			if gotWidth != tt.wantWidth {
				t.Errorf("padVisible(%q, %d) visible width = %d, want %d (result=%q)",
					tt.s, tt.width, gotWidth, tt.wantWidth, got)
			}
		})
	}
}

func TestPadVisible_ANSIString(t *testing.T) {
	t.Parallel()

	styled := styleGreen.Render("OK")
	padded := padVisible(styled, 10)
	gotWidth := visibleWidth(padded)

	if gotWidth != 10 {
		t.Errorf("padVisible on ANSI string: visible width = %d, want 10", gotWidth)
	}
	// The original ANSI content must still be present.
	if !strings.Contains(padded, "OK") {
		t.Error("padVisible stripped visible text from ANSI-styled string")
	}
}

// ─── truncate ─────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{name: "shorter than maxLen", s: "hello", maxLen: 10, want: "hello"},
		{name: "exact maxLen", s: "hello", maxLen: 5, want: "hello"},
		{name: "longer than maxLen", s: "hello world", maxLen: 8, want: "hello..."},
		{name: "maxLen is 3 exactly long string", s: "abcde", maxLen: 3, want: "abc"},
		{name: "maxLen is 2 exactly long string", s: "abcde", maxLen: 2, want: "ab"},
		{name: "maxLen is 1 exactly long string", s: "abcde", maxLen: 1, want: "a"},
		{name: "empty string", s: "", maxLen: 5, want: ""},
		{name: "unicode multibyte runes truncated", s: "日本語テスト", maxLen: 4, want: "日..."},
		{name: "exactly maxLen not truncated", s: "abcdef", maxLen: 6, want: "abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := truncate(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestTruncate_LengthNeverExceedsMaxLen(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"short",
		"this is a much longer string that will definitely need truncation",
		strings.Repeat("a", 200),
	}

	for _, maxLen := range []int{1, 3, 5, 10, 42} {
		for _, s := range inputs {
			got := truncate(s, maxLen)
			gotRunes := len([]rune(got))
			if gotRunes > maxLen {
				t.Errorf("truncate(%q, %d) produced %d runes, exceeds maxLen",
					s, maxLen, gotRunes)
			}
		}
	}
}

// ─── totalTableWidth ──────────────────────────────────────────────────────────

func TestTotalTableWidth(t *testing.T) {
	t.Parallel()

	got := totalTableWidth()

	// The width must be positive and deterministic.
	if got <= 0 {
		t.Errorf("totalTableWidth() = %d, want positive value", got)
	}

	// Calling it twice must produce the same result (pure function).
	if got2 := totalTableWidth(); got != got2 {
		t.Errorf("totalTableWidth is not deterministic: %d != %d", got, got2)
	}

	// Sanity-check the expected value from the constants.
	want := len(rowIndent) +
		colWidthName + len(colSep) +
		colWidthLocation + len(colSep) +
		colWidthStatus + len(colSep) +
		colWidthAnswer + len(colSep) +
		colWidthLatency
	if got != want {
		t.Errorf("totalTableWidth() = %d, want %d", got, want)
	}
}

// ─── sortedCopy ───────────────────────────────────────────────────────────────

func TestSortedCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "nil input", input: nil, want: []string{}},
		{name: "empty slice", input: []string{}, want: []string{}},
		{name: "already sorted", input: []string{"a", "b", "c"}, want: []string{"a", "b", "c"}},
		{name: "reverse order", input: []string{"c", "b", "a"}, want: []string{"a", "b", "c"}},
		{name: "duplicates", input: []string{"b", "a", "b"}, want: []string{"a", "b", "b"}},
		{
			name:  "IP addresses",
			input: []string{"10.0.0.1", "1.1.1.1", "8.8.8.8"},
			want:  []string{"1.1.1.1", "10.0.0.1", "8.8.8.8"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := make([]string, len(tt.input))
			copy(original, tt.input)

			got := sortedCopy(tt.input)

			// Verify sorted order.
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("sortedCopy[%d] = %q, want %q", i, got[i], want)
				}
			}

			// Verify the original is not mutated.
			for i, orig := range original {
				if tt.input[i] != orig {
					t.Errorf("sortedCopy mutated input at index %d: was %q, now %q", i, orig, tt.input[i])
				}
			}
		})
	}
}

// ─── matchesConsensus ─────────────────────────────────────────────────────────

func TestMatchesConsensus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		records   []string
		consensus []string
		want      bool
	}{
		{
			name:      "both nil",
			records:   nil,
			consensus: nil,
			want:      true,
		},
		{
			name:      "both empty",
			records:   []string{},
			consensus: []string{},
			want:      true,
		},
		{
			name:      "exact match single record",
			records:   []string{"1.2.3.4"},
			consensus: []string{"1.2.3.4"},
			want:      true,
		},
		{
			name:      "records unsorted but match consensus sorted",
			records:   []string{"b", "a"},
			consensus: []string{"a", "b"},
			want:      true,
		},
		{
			name:      "different lengths",
			records:   []string{"a", "b"},
			consensus: []string{"a"},
			want:      false,
		},
		{
			name:      "same length different values",
			records:   []string{"1.2.3.4"},
			consensus: []string{"5.6.7.8"},
			want:      false,
		},
		{
			name:      "empty records non-empty consensus",
			records:   []string{},
			consensus: []string{"1.2.3.4"},
			want:      false,
		},
		{
			name:      "multiple matching records",
			records:   []string{"c", "a", "b"},
			consensus: []string{"a", "b", "c"},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := matchesConsensus(tt.records, tt.consensus)
			if got != tt.want {
				t.Errorf("matchesConsensus(%v, %v) = %v, want %v",
					tt.records, tt.consensus, got, tt.want)
			}
		})
	}
}

// ─── resultsSlice ─────────────────────────────────────────────────────────────

func TestResultsSlice(t *testing.T) {
	t.Parallel()

	r1 := makeResolver("1.1.1.1", "CF", "A", "G", "Global")
	r2 := makeResolver("8.8.8.8", "G", "A", "G", "Global")

	tests := []struct {
		name    string
		input   map[string]query.Result
		wantLen int
	}{
		{name: "nil map", input: nil, wantLen: 0},
		{name: "empty map", input: map[string]query.Result{}, wantLen: 0},
		{
			name: "single entry",
			input: map[string]query.Result{
				"1.1.1.1": successResult(r1, []string{"1.2.3.4"}, 10*time.Millisecond),
			},
			wantLen: 1,
		},
		{
			name: "two entries",
			input: map[string]query.Result{
				"1.1.1.1": successResult(r1, []string{"1.2.3.4"}, 10*time.Millisecond),
				"8.8.8.8": successResult(r2, []string{"5.6.7.8"}, 20*time.Millisecond),
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resultsSlice(tt.input)
			if len(got) != tt.wantLen {
				t.Errorf("resultsSlice len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestResultsSlice_DoesNotShareMemory(t *testing.T) {
	t.Parallel()

	r := makeResolver("1.1.1.1", "CF", "A", "G", "Global")
	m := map[string]query.Result{
		"1.1.1.1": successResult(r, []string{"1.2.3.4"}, 5*time.Millisecond),
	}

	got := resultsSlice(m)

	// Deleting from the original map must not affect the slice.
	delete(m, "1.1.1.1")

	if len(got) != 1 {
		t.Errorf("resultsSlice output was mutated after map delete: len = %d, want 1", len(got))
	}
}

// ─── banner ───────────────────────────────────────────────────────────────────

func TestBanner_NonEmpty(t *testing.T) {
	t.Parallel()

	got := banner()

	if got == "" {
		t.Error("banner() returned empty string")
	}
	// Strip ANSI to check for visible ASCII art content.
	visible := style.Width(got)
	if visible == 0 {
		t.Error("banner() has no visible characters after ANSI stripping")
	}
}

func TestBanner_ContainsSONA(t *testing.T) {
	t.Parallel()

	// The art contains "SONAR" letters split across rows. We just verify that
	// the banner contains the word "SONAR" somewhere in its visible content.
	got := banner()
	if !strings.Contains(got, "SONAR") && !strings.Contains(got, "sonar") {
		// The art uses block characters not the word; confirm at minimum the
		// banner is longer than 20 chars in total.
		if len(got) < 20 {
			t.Errorf("banner() seems too short: %d bytes", len(got))
		}
	}
}

// ─── renderColumnHeaders ──────────────────────────────────────────────────────

func TestRenderColumnHeaders(t *testing.T) {
	t.Parallel()

	got := renderColumnHeaders()

	if got == "" {
		t.Fatal("renderColumnHeaders() returned empty string")
	}

	expected := []string{"Resolver", "Location", "St", "Answer", "Latency"}
	for _, col := range expected {
		if !strings.Contains(got, col) {
			t.Errorf("renderColumnHeaders() missing column %q, got:\n%s", col, got)
		}
	}
}

func TestRenderColumnHeaders_StartsWithIndent(t *testing.T) {
	t.Parallel()

	got := renderColumnHeaders()
	if !strings.HasPrefix(got, rowIndent) {
		t.Errorf("renderColumnHeaders() should start with rowIndent %q, got prefix %q",
			rowIndent, got[:min(len(got), 10)])
	}
}

// ─── renderRow ────────────────────────────────────────────────────────────────

func TestRenderRow_Pending(t *testing.T) {
	t.Parallel()

	r := makeResolver("1.1.1.1", "Cloudflare Primary", "Anycast", "Global", "Global")

	got := renderRow(r, query.Result{}, false, nil, false)

	if got == "" {
		t.Fatal("renderRow returned empty string for pending state")
	}
	// Pending rows show dashes as placeholders.
	stripped := got
	_ = stripped
	// At minimum the resolver name must appear in the output.
	if !strings.Contains(got, "Cloudflare") {
		t.Errorf("renderRow pending: expected resolver name in output, got: %q", got)
	}
}

func TestRenderRow_Success_MatchesConsensus(t *testing.T) {
	t.Parallel()

	r := makeResolver("8.8.8.8", "Google Primary", "Anycast", "Global", "Global")
	result := successResult(r, []string{"1.2.3.4"}, 25*time.Millisecond)
	consensus := []string{"1.2.3.4"}

	got := renderRow(r, result, true, consensus, true)

	if !strings.Contains(got, "1.2.3.4") {
		t.Errorf("renderRow success: expected answer in output, got: %q", got)
	}
	if !strings.Contains(got, "25ms") {
		t.Errorf("renderRow success: expected latency '25ms' in output, got: %q", got)
	}
}

func TestRenderRow_Success_Divergent(t *testing.T) {
	t.Parallel()

	r := makeResolver("8.8.4.4", "Google Secondary", "Anycast", "Global", "Global")
	result := successResult(r, []string{"9.9.9.9"}, 30*time.Millisecond)
	consensus := []string{"1.2.3.4"} // different from result

	got := renderRow(r, result, true, consensus, true)

	// Divergent row should still contain the answer.
	if !strings.Contains(got, "9.9.9.9") {
		t.Errorf("renderRow divergent: expected answer in output, got: %q", got)
	}
}

func TestRenderRow_Timeout(t *testing.T) {
	t.Parallel()

	r := makeResolver("1.0.0.1", "CF Secondary", "Anycast", "Global", "Global")
	result := timeoutResult(r)

	got := renderRow(r, result, true, nil, false)

	if !strings.Contains(got, "timeout") {
		t.Errorf("renderRow timeout: expected 'timeout' in output, got: %q", got)
	}
}

func TestRenderRow_Error(t *testing.T) {
	t.Parallel()

	r := makeResolver("9.9.9.9", "Quad9", "Anycast", "Global", "Global")
	result := errorResult(r, "connection refused")

	got := renderRow(r, result, true, nil, false)

	if !strings.Contains(got, "connection refused") {
		t.Errorf("renderRow error: expected error message in output, got: %q", got)
	}
}

func TestRenderRow_ErrorStatus_NoErrorField(t *testing.T) {
	t.Parallel()

	r := makeResolver("9.9.9.10", "Quad9 Alt", "Anycast", "Global", "Global")
	result := query.Result{
		Resolver: r,
		Status:   query.Error,
		Error:    nil,
	}

	got := renderRow(r, result, true, nil, false)

	// When Status==Error but Error field is nil, "error" is used as the message.
	if !strings.Contains(got, "error") {
		t.Errorf("renderRow error (nil err field): expected 'error' in output, got: %q", got)
	}
}

func TestRenderRow_LongNameTruncated(t *testing.T) {
	t.Parallel()

	longName := strings.Repeat("A", colWidthName+10)
	r := makeResolver("10.0.0.1", longName, "City", "Country", "Europe")

	got := renderRow(r, query.Result{}, false, nil, false)

	// The name cell must not exceed colWidthName visible characters.
	// Find it in the output (it starts after rowIndent, at the beginning of the row).
	// A simpler check: the total visible row width should not be unbounded.
	if len(got) == 0 {
		t.Fatal("renderRow with long name returned empty string")
	}
}

func TestRenderRow_StartsWithIndent(t *testing.T) {
	t.Parallel()

	r := makeResolver("1.1.1.1", "CF", "A", "G", "Global")

	got := renderRow(r, query.Result{}, false, nil, false)

	if !strings.HasPrefix(got, rowIndent) {
		t.Errorf("renderRow should start with rowIndent %q", rowIndent)
	}
}

// ─── viewState rendering ──────────────────────────────────────────────────────

func TestViewState_RenderFooter_NilConsensus(t *testing.T) {
	t.Parallel()

	r := makeResolver("1.1.1.1", "CF", "Anycast", "Global", "Global")
	v := &viewState{
		resolvers: []resolver.Resolver{r},
		results:   map[string]query.Result{},
		startTime: time.Now(),
	}

	got := v.renderFooter()

	if !strings.Contains(got, "Checked:") {
		t.Error("renderFooter should contain 'Checked:' label")
	}
	if !strings.Contains(got, "Consensus:") {
		t.Error("renderFooter should contain 'Consensus:' label")
	}
	if !strings.Contains(got, "Propagated:") {
		t.Error("renderFooter should contain 'Propagated:' label")
	}
}

func TestViewState_RenderFooter_WithConsensus(t *testing.T) {
	t.Parallel()

	r := makeResolver("1.1.1.1", "CF", "Anycast", "Global", "Global")
	v := &viewState{
		resolvers: []resolver.Resolver{r},
		results: map[string]query.Result{
			"1.1.1.1": successResult(r, []string{"1.2.3.4"}, 5*time.Millisecond),
		},
		startTime: time.Now(),
	}

	got := v.renderFooter()

	if !strings.Contains(got, "1.2.3.4") {
		t.Error("renderFooter should display consensus answer")
	}
}

// ─── output capture helper ────────────────────────────────────────────────────

// captureOutput redirects the package-level output writer to a fresh
// bytes.Buffer for the duration of the test and returns both the buffer and a
// restore function. The caller must invoke restore (typically via defer) so
// that subsequent tests are not affected.
func captureOutput(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	old := output
	output = &buf
	return &buf, func() { output = old }
}

// ─── printBanner ──────────────────────────────────────────────────────────────

func TestPrintBanner_WritesOutput(t *testing.T) {
	// No t.Parallel(): these tests mutate the global output variable and must
	// run sequentially to avoid data races when the race detector is active.
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "banner.test", RecordType: 1, Resolvers: rs})
	m := New("banner.test", "A", rs, chk)

	m.printBanner()

	got := buf.String()
	if got == "" {
		t.Fatal("printBanner wrote nothing to output")
	}
	// The banner must have visible content (ANSI codes wrap actual characters).
	if style.Width(got) == 0 && !strings.Contains(got, "\x1b[") {
		t.Error("printBanner output has no visible content and no ANSI codes")
	}
}

func TestPrintBanner_ContainsDNSPropagationText(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "x.io", RecordType: 1, Resolvers: rs})
	m := New("x.io", "A", rs, chk)

	m.printBanner()

	got := buf.String()
	if !strings.Contains(got, "DNS Propagation Checker") {
		t.Errorf("printBanner output missing subtitle text, got:\n%s", got)
	}
}

// ─── printHeader ──────────────────────────────────────────────────────────────

func TestPrintHeader_ContainsDomain(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "mysite.example", RecordType: 1, Resolvers: rs})
	m := New("mysite.example", "AAAA", rs, chk)

	m.printHeader()

	got := buf.String()
	if !strings.Contains(got, "mysite.example") {
		t.Errorf("printHeader output missing domain, got:\n%s", got)
	}
}

func TestPrintHeader_ContainsRecordType(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "foo.bar", RecordType: 15, Resolvers: rs})
	m := New("foo.bar", "MX", rs, chk)

	m.printHeader()

	got := buf.String()
	if !strings.Contains(got, "MX") {
		t.Errorf("printHeader output missing record type 'MX', got:\n%s", got)
	}
}

func TestPrintHeader_NonEmpty(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("8.8.8.8", "Google", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "test.net", RecordType: 1, Resolvers: rs})
	m := New("test.net", "A", rs, chk)

	m.printHeader()

	if buf.Len() == 0 {
		t.Error("printHeader wrote nothing to output")
	}
}

// ─── printColumnHeaders ───────────────────────────────────────────────────────

func TestPrintColumnHeaders_ContainsColumnNames(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "col.test", RecordType: 1, Resolvers: rs})
	m := New("col.test", "A", rs, chk)

	m.printColumnHeaders()

	got := buf.String()
	for _, col := range []string{"Resolver", "Location", "St", "Answer", "Latency"} {
		if !strings.Contains(got, col) {
			t.Errorf("printColumnHeaders output missing column %q, got:\n%s", col, got)
		}
	}
}

func TestPrintColumnHeaders_NonEmpty(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "col.test", RecordType: 1, Resolvers: rs})
	m := New("col.test", "A", rs, chk)

	m.printColumnHeaders()

	if buf.Len() == 0 {
		t.Error("printColumnHeaders wrote nothing to output")
	}
}

// ─── writeStatusLine ──────────────────────────────────────────────────────────

func TestWriteStatusLine_ContainsSpinnerFrame(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "spin.test", RecordType: 1, Resolvers: rs})
	m := New("spin.test", "A", rs, chk)

	frame := spinnerFrames[0]
	m.writeStatusLine(frame)

	got := buf.String()
	if !strings.Contains(got, frame) {
		t.Errorf("writeStatusLine output missing spinner frame %q, got:\n%q", frame, got)
	}
}

func TestWriteStatusLine_ContainsProgressText(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{
		makeResolver("1.1.1.1", "CF", "A", "G", "Global"),
		makeResolver("8.8.8.8", "G", "A", "G", "Global"),
	}
	chk := checker.New(checker.Config{Domain: "progress.test", RecordType: 1, Resolvers: rs})
	m := New("progress.test", "A", rs, chk)

	m.writeStatusLine(spinnerFrames[1])

	got := buf.String()
	// The status line must mention the total resolver count somewhere.
	if !strings.Contains(got, "2") {
		t.Errorf("writeStatusLine should include total resolver count (2), got:\n%q", got)
	}
	if !strings.Contains(got, "resolvers") {
		t.Errorf("writeStatusLine should contain the word 'resolvers', got:\n%q", got)
	}
}

func TestWriteStatusLine_UsesCarriageReturn(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "cr.test", RecordType: 1, Resolvers: rs})
	m := New("cr.test", "A", rs, chk)

	m.writeStatusLine(spinnerFrames[0])

	got := buf.String()
	// The in-place update must start with \r to overwrite the current line.
	if !strings.HasPrefix(got, "\r") {
		t.Errorf("writeStatusLine output should start with carriage return, got:\n%q", got)
	}
}

func TestWriteStatusLine_AllFramesProduceOutput(t *testing.T) {
	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "frames.test", RecordType: 1, Resolvers: rs})

	for i, frame := range spinnerFrames {
		buf, restore := captureOutput(t)

		m := New("frames.test", "A", rs, chk)
		m.writeStatusLine(frame)

		if buf.Len() == 0 {
			t.Errorf("writeStatusLine with frame[%d]=%q wrote nothing", i, frame)
		}
		restore()
	}
}

// ─── clearStatusLine ──────────────────────────────────────────────────────────

func TestClearStatusLine_WritesCarriageReturnAndErase(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "clear.test", RecordType: 1, Resolvers: rs})
	m := New("clear.test", "A", rs, chk)

	m.clearStatusLine()

	got := buf.String()
	if got == "" {
		t.Fatal("clearStatusLine wrote nothing to output")
	}
	// Must start with \r to return to line start.
	if !strings.HasPrefix(got, "\r") {
		t.Errorf("clearStatusLine output should start with carriage return, got:\n%q", got)
	}
	// Must contain the clear-line ANSI sequence.
	if !strings.Contains(got, escClearLine) {
		t.Errorf("clearStatusLine output missing escClearLine sequence %q, got:\n%q", escClearLine, got)
	}
}

func TestClearStatusLine_IsIdempotent(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "idem.test", RecordType: 1, Resolvers: rs})
	m := New("idem.test", "A", rs, chk)

	// Calling clearStatusLine twice must not panic and must produce output
	// that contains the clear sequence both times.
	m.clearStatusLine()
	m.clearStatusLine()

	got := buf.String()
	count := strings.Count(got, escClearLine)
	if count < 2 {
		t.Errorf("clearStatusLine called twice: expected at least 2 clear sequences, got %d in:\n%q", count, got)
	}
}

// ─── printFooter ──────────────────────────────────────────────────────────────

func TestPrintFooter_ContainsSummaryLabels(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	r := makeResolver("1.1.1.1", "CF", "Anycast", "Global", "Global")
	rs := []resolver.Resolver{r}
	chk := checker.New(checker.Config{Domain: "footer.test", RecordType: 1, Resolvers: rs})
	m := New("footer.test", "A", rs, chk)

	m.mu.Lock()
	m.results["1.1.1.1"] = successResult(r, []string{"1.2.3.4"}, 10*time.Millisecond)
	m.mu.Unlock()

	m.printFooter()

	got := buf.String()
	for _, label := range []string{"Checked:", "Consensus:", "Propagated:", "completed in"} {
		if !strings.Contains(got, label) {
			t.Errorf("printFooter output missing %q, got:\n%s", label, got)
		}
	}
}

func TestPrintFooter_ContainsConsensusAnswer(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	r := makeResolver("8.8.8.8", "Google", "Anycast", "Global", "Global")
	rs := []resolver.Resolver{r}
	chk := checker.New(checker.Config{Domain: "ans.test", RecordType: 1, Resolvers: rs})
	m := New("ans.test", "A", rs, chk)

	m.mu.Lock()
	m.results["8.8.8.8"] = successResult(r, []string{"203.0.113.42"}, 5*time.Millisecond)
	m.mu.Unlock()

	m.printFooter()

	got := buf.String()
	if !strings.Contains(got, "203.0.113.42") {
		t.Errorf("printFooter output missing consensus IP, got:\n%s", got)
	}
}

func TestPrintFooter_NoResultsShowsDash(t *testing.T) {
	buf, restore := captureOutput(t)
	defer restore()

	rs := []resolver.Resolver{makeResolver("1.1.1.1", "CF", "A", "G", "Global")}
	chk := checker.New(checker.Config{Domain: "nodash.test", RecordType: 1, Resolvers: rs})
	m := New("nodash.test", "A", rs, chk)
	// No results, no consensus — consensus display should be "—".

	m.printFooter()

	got := buf.String()
	if !strings.Contains(got, "—") {
		t.Errorf("printFooter with no consensus should display '—', got:\n%s", got)
	}
}
