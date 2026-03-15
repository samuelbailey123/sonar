package style

import (
	"strings"
	"testing"
)

// ─── hexToANSI ────────────────────────────────────────────────────────────────

func TestHexToANSI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "hex green",
			input: "#22c55e",
			want:  "38;2;34;197;94",
		},
		{
			name:  "hex red",
			input: "#ef4444",
			want:  "38;2;239;68;68",
		},
		{
			name:  "hex cyan",
			input: "#06b6d4",
			want:  "38;2;6;182;212",
		},
		{
			name:  "hex yellow",
			input: "#eab308",
			want:  "38;2;234;179;8",
		},
		{
			name:  "hex white",
			input: "#ffffff",
			want:  "38;2;255;255;255",
		},
		{
			name:  "hex black",
			input: "#000000",
			want:  "38;2;0;0;0",
		},
		{
			name:  "ansi 256 index single digit",
			input: "9",
			want:  "38;5;9",
		},
		{
			name:  "ansi 256 index double digit",
			input: "12",
			want:  "38;5;12",
		},
		{
			name:  "ansi 256 index zero",
			input: "0",
			want:  "38;5;0",
		},
		{
			name:  "ansi 256 max index",
			input: "255",
			want:  "38;5;255",
		},
		{
			name:  "raw sgr fragment passthrough",
			input: "38;2;100;200;50",
			want:  "38;2;100;200;50",
		},
		{
			name:  "invalid hex length returns as-is",
			input: "#fff",
			want:  "#fff",
		},
		{
			name:  "invalid hex chars returns as-is",
			input: "#zzzzzz",
			want:  "#zzzzzz",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hexToANSI(tc.input)
			if got != tc.want {
				t.Errorf("hexToANSI(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ─── Style.Render ─────────────────────────────────────────────────────────────

func TestStyleRender_Empty(t *testing.T) {
	t.Parallel()
	// A zero-value Style with no attributes must not emit any escape codes.
	s := New()
	got := s.Render("hello")
	if got != "hello" {
		t.Errorf("empty Style.Render produced unexpected output: %q", got)
	}
}

func TestStyleRender_Bold(t *testing.T) {
	t.Parallel()
	got := New().Bold(true).Render("hello")
	if !strings.Contains(got, "\x1b[1m") {
		t.Errorf("bold Render missing bold escape, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("bold Render missing reset suffix, got %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("bold Render does not contain source text, got %q", got)
	}
}

func TestStyleRender_BoldFalse(t *testing.T) {
	t.Parallel()
	// Explicitly disabled bold should produce no output (zero attributes).
	got := New().Bold(false).Render("hello")
	if got != "hello" {
		t.Errorf("Bold(false) Style should not emit escapes, got %q", got)
	}
}

func TestStyleRender_Dim(t *testing.T) {
	t.Parallel()
	got := New().Dim(true).Render("text")
	if !strings.Contains(got, "\x1b[2m") {
		t.Errorf("dim Render missing dim escape, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("dim Render missing reset suffix, got %q", got)
	}
}

func TestStyleRender_Underline(t *testing.T) {
	t.Parallel()
	got := New().Underline(true).Render("text")
	if !strings.Contains(got, "\x1b[4m") {
		t.Errorf("underline Render missing underline escape, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("underline Render missing reset suffix, got %q", got)
	}
}

func TestStyleRender_Foreground(t *testing.T) {
	t.Parallel()
	got := New().Foreground("#22c55e").Render("text")
	// Truecolor: ESC[38;2;34;197;94m
	if !strings.Contains(got, "38;2;34;197;94") {
		t.Errorf("foreground Render missing truecolor sequence, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("foreground Render missing reset suffix, got %q", got)
	}
}

func TestStyleRender_BoldAndForeground(t *testing.T) {
	t.Parallel()
	got := New().Bold(true).Foreground("#ef4444").Render("err")
	// Both bold (1) and truecolor red must appear in the opening sequence.
	if !strings.Contains(got, "1") {
		t.Errorf("bold+fg Render missing bold code, got %q", got)
	}
	if !strings.Contains(got, "38;2;239;68;68") {
		t.Errorf("bold+fg Render missing red truecolor, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("bold+fg Render missing reset suffix, got %q", got)
	}
}

func TestStyleRender_ContainsText(t *testing.T) {
	t.Parallel()
	input := "DNS propagation check"
	got := New().Foreground("#06b6d4").Bold(true).Render(input)
	if !strings.Contains(got, input) {
		t.Errorf("Render did not preserve input text; got %q", got)
	}
}

func TestStyleRender_ResetPresent(t *testing.T) {
	t.Parallel()
	// Every styled string must end with a reset so subsequent output is not
	// affected.
	styles := []Style{
		New().Bold(true),
		New().Dim(true),
		New().Underline(true),
		New().Foreground("#9ca3af"),
		New().Bold(true).Foreground("#a78bfa"),
	}
	for _, s := range styles {
		got := s.Render("x")
		if !strings.HasSuffix(got, "\x1b[0m") {
			t.Errorf("Render %+v missing reset at end, got %q", s, got)
		}
	}
}

// ─── Immutability ─────────────────────────────────────────────────────────────

func TestStyleImmutability(t *testing.T) {
	t.Parallel()
	base := New().Foreground("#22c55e")
	bold := base.Bold(true)
	dim := base.Dim(true)

	// base must not gain bold or dim from derived styles.
	baseOut := base.Render("a")
	boldOut := bold.Render("a")
	dimOut := dim.Render("a")

	if baseOut == boldOut {
		t.Errorf("base and bold produced identical output %q; Bold() mutated base", baseOut)
	}
	if baseOut == dimOut {
		t.Errorf("base and dim produced identical output %q; Dim() mutated base", baseOut)
	}
	if boldOut == dimOut {
		t.Errorf("bold and dim produced identical output %q", boldOut)
	}
}

// ─── Width ────────────────────────────────────────────────────────────────────

func TestWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "plain ASCII",
			input: "hello",
			want:  5,
		},
		{
			name:  "empty string",
			input: "",
			want:  0,
		},
		{
			name:  "multibyte runes",
			input: "héllo",
			want:  5,
		},
		{
			name:  "ansi bold sequence stripped",
			input: New().Bold(true).Render("hello"),
			want:  5,
		},
		{
			name:  "ansi foreground sequence stripped",
			input: New().Foreground("#22c55e").Render("world"),
			want:  5,
		},
		{
			name:  "bold + foreground stripped",
			input: New().Bold(true).Foreground("#ef4444").Render("error"),
			want:  5,
		},
		{
			name:  "multiple escapes stripped",
			input: "\x1b[1m" + "ab" + "\x1b[0m" + "\x1b[32m" + "cd" + "\x1b[0m",
			want:  4,
		},
		{
			name:  "no escapes unchanged",
			input: "sonar DNS",
			want:  9,
		},
		{
			name:  "unicode runes counted correctly",
			input: "✓✗⏱",
			want:  3,
		},
		{
			name:  "styled unicode",
			input: New().Foreground("#22c55e").Render("✓"),
			want:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Width(tc.input)
			if got != tc.want {
				t.Errorf("Width(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// ─── Pre-defined variables ────────────────────────────────────────────────────

func TestPredefinedStyles_Render(t *testing.T) {
	t.Parallel()

	// Each pre-defined variable must render without panicking and must include
	// a reset sequence (i.e. attributes are set).
	vars := []struct {
		name  string
		style Style
	}{
		{"Green", Green},
		{"Yellow", Yellow},
		{"Red", Red},
		{"Cyan", Cyan},
		{"DimGray", DimGray},
		{"DimYellow", DimYellow},
		{"Purple", Purple},
		{"Gray", Gray},
		{"BoldStyle", BoldStyle},
	}

	for _, v := range vars {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			got := v.style.Render("test")
			if !strings.Contains(got, "test") {
				t.Errorf("%s.Render missing source text, got %q", v.name, got)
			}
			if !strings.HasSuffix(got, "\x1b[0m") {
				t.Errorf("%s.Render missing reset suffix, got %q", v.name, got)
			}
			// Visible width must equal the length of the input text.
			if w := Width(got); w != 4 {
				t.Errorf("%s.Render Width = %d, want 4", v.name, w)
			}
		})
	}
}

func TestPredefinedStyles_ChainBold(t *testing.T) {
	t.Parallel()
	// Chaining Bold(true) onto a color variable must not mutate the original.
	base := Green.Render("a")
	bolded := Green.Bold(true).Render("a")
	if base == bolded {
		t.Errorf("Green.Bold(true) produced same output as Green; original mutated or chaining broken")
	}
}

// ─── Foreground with 256-color index ─────────────────────────────────────────

func TestForeground_256Color(t *testing.T) {
	t.Parallel()
	got := New().Foreground("196").Render("x")
	if !strings.Contains(got, "38;5;196") {
		t.Errorf("256-color foreground missing expected sequence, got %q", got)
	}
}
