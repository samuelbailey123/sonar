package version

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// saveVars captures the current values of the three package-level variables and
// returns a restore function. Call it with defer to ensure cleanup even on
// test failure.
func saveVars() (restore func()) {
	origVersion := Version
	origCommit := Commit
	origDate := Date
	return func() {
		Version = origVersion
		Commit = origCommit
		Date = origDate
	}
}

// TestShortDefault verifies that Short() returns the unmodified default value
// when no ldflags override has been applied.
func TestShortDefault(t *testing.T) {
	t.Parallel()

	got := Short()
	if got != Version {
		t.Errorf("Short() = %q, want %q (current Version)", got, Version)
	}
}

// TestShortReturnsVersion verifies that Short() always mirrors the Version
// variable, even after a programmatic override.
func TestShortReturnsVersion(t *testing.T) {
	restore := saveVars()
	defer restore()

	Version = "1.0.0"

	got := Short()
	if got != "1.0.0" {
		t.Errorf("Short() = %q, want %q", got, "1.0.0")
	}
}

// TestShortDefaultValue asserts the compile-time default for Version is "dev".
func TestShortDefaultValue(t *testing.T) {
	t.Parallel()

	// Version is never overridden in the test binary (no ldflags), so the
	// initialised value must equal the literal "dev".
	if Version != "dev" {
		t.Skipf("Version has been overridden via ldflags (%q); skipping default check", Version)
	}

	got := Short()
	if got != "dev" {
		t.Errorf("Short() = %q, want \"dev\"", got)
	}
}

// TestFullContainsRequiredSegments verifies that Full() contains all
// mandatory substrings regardless of the current variable values.
func TestFullContainsRequiredSegments(t *testing.T) {
	t.Parallel()

	got := Full()

	required := []string{
		"sonar",
		"commit",
		"built",
		runtime.GOOS,
		runtime.GOARCH,
	}

	for _, substr := range required {
		if !strings.Contains(got, substr) {
			t.Errorf("Full() = %q does not contain %q", got, substr)
		}
	}
}

// TestFullFormat verifies the exact shape of the Full() output:
//
//	sonar VERSION (commit COMMIT, built DATE, GOOS/GOARCH)
func TestFullFormat(t *testing.T) {
	restore := saveVars()
	defer restore()

	Version = "1.2.3"
	Commit = "abc1234"
	Date = "2024-01-01T00:00:00Z"

	want := fmt.Sprintf("sonar 1.2.3 (commit abc1234, built 2024-01-01T00:00:00Z, %s/%s)",
		runtime.GOOS, runtime.GOARCH)

	got := Full()
	if got != want {
		t.Errorf("Full() format mismatch\n got:  %q\n want: %q", got, want)
	}
}

// TestFullParenthesesStructure verifies that the parenthesised section is
// properly opened and closed.
func TestFullParenthesesStructure(t *testing.T) {
	t.Parallel()

	got := Full()

	openIdx := strings.Index(got, "(")
	closeIdx := strings.LastIndex(got, ")")

	if openIdx == -1 {
		t.Fatalf("Full() = %q: missing opening parenthesis", got)
	}
	if closeIdx == -1 {
		t.Fatalf("Full() = %q: missing closing parenthesis", got)
	}
	if closeIdx != len(got)-1 {
		t.Errorf("Full() = %q: closing parenthesis is not the last character", got)
	}
	if openIdx >= closeIdx {
		t.Errorf("Full() = %q: opening parenthesis appears after closing parenthesis", got)
	}
}

// TestFullCommaDelimitedFields verifies that the three fields inside the
// parentheses are separated by ", ".
func TestFullCommaDelimitedFields(t *testing.T) {
	restore := saveVars()
	defer restore()

	Version = "0.1.0"
	Commit = "deadbeef"
	Date = "2024-06-01"

	got := Full()

	openIdx := strings.Index(got, "(")
	closeIdx := strings.LastIndex(got, ")")
	inner := got[openIdx+1 : closeIdx]

	// Expect exactly two ", " separators for three fields.
	parts := strings.Split(inner, ", ")
	if len(parts) != 3 {
		t.Errorf("inner section %q: expected 3 comma-separated fields, got %d: %v", inner, len(parts), parts)
	}
}

// TestFullPlatformSlash verifies that GOOS and GOARCH are joined with "/" inside
// the Full() output.
func TestFullPlatformSlash(t *testing.T) {
	t.Parallel()

	want := runtime.GOOS + "/" + runtime.GOARCH
	got := Full()

	if !strings.Contains(got, want) {
		t.Errorf("Full() = %q does not contain platform %q", got, want)
	}
}

// TestFullTableDriven exercises Full() and Short() across a range of
// representative input combinations, including edge cases such as empty strings
// and strings with spaces.
func TestFullTableDriven(t *testing.T) {
	platform := runtime.GOOS + "/" + runtime.GOARCH

	cases := []struct {
		name      string
		version   string
		commit    string
		date      string
		wantFull  string
		wantShort string
	}{
		{
			name:      "default values",
			version:   "dev",
			commit:    "unknown",
			date:      "unknown",
			wantFull:  fmt.Sprintf("sonar dev (commit unknown, built unknown, %s)", platform),
			wantShort: "dev",
		},
		{
			name:      "release build",
			version:   "1.0.0",
			commit:    "abc123",
			date:      "2024-01-01T00:00:00Z",
			wantFull:  fmt.Sprintf("sonar 1.0.0 (commit abc123, built 2024-01-01T00:00:00Z, %s)", platform),
			wantShort: "1.0.0",
		},
		{
			name:      "pre-release version",
			version:   "2.0.0-beta.1",
			commit:    "ff00ff",
			date:      "2025-12-31",
			wantFull:  fmt.Sprintf("sonar 2.0.0-beta.1 (commit ff00ff, built 2025-12-31, %s)", platform),
			wantShort: "2.0.0-beta.1",
		},
		{
			name:      "empty strings (pathological)",
			version:   "",
			commit:    "",
			date:      "",
			wantFull:  fmt.Sprintf("sonar  (commit , built , %s)", platform),
			wantShort: "",
		},
		{
			name:      "long commit hash",
			version:   "0.9.1",
			commit:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			date:      "2024-03-15",
			wantFull:  fmt.Sprintf("sonar 0.9.1 (commit a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2, built 2024-03-15, %s)", platform),
			wantShort: "0.9.1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each subtest mutates package state, so they must not run in
			// parallel with each other.
			restore := saveVars()
			defer restore()

			Version = tc.version
			Commit = tc.commit
			Date = tc.date

			if got := Full(); got != tc.wantFull {
				t.Errorf("Full():\n got:  %q\n want: %q", got, tc.wantFull)
			}

			if got := Short(); got != tc.wantShort {
				t.Errorf("Short():\n got:  %q\n want: %q", got, tc.wantShort)
			}
		})
	}
}

// TestFullStartsWithSonar verifies the binary name prefix is always present.
func TestFullStartsWithSonar(t *testing.T) {
	t.Parallel()

	got := Full()
	if !strings.HasPrefix(got, "sonar ") {
		t.Errorf("Full() = %q: expected prefix \"sonar \", not found", got)
	}
}

// TestFullVersionEmbedded verifies that the current value of Version appears
// verbatim inside the Full() output.
func TestFullVersionEmbedded(t *testing.T) {
	restore := saveVars()
	defer restore()

	Version = "3.14.159"

	got := Full()
	if !strings.Contains(got, "3.14.159") {
		t.Errorf("Full() = %q: Version %q not found", got, Version)
	}
}

// TestFullCommitEmbedded verifies that the current value of Commit appears
// verbatim inside the Full() output.
func TestFullCommitEmbedded(t *testing.T) {
	restore := saveVars()
	defer restore()

	Commit = "cafebabe"

	got := Full()
	if !strings.Contains(got, "cafebabe") {
		t.Errorf("Full() = %q: Commit %q not found", got, Commit)
	}
}

// TestFullDateEmbedded verifies that the current value of Date appears verbatim
// inside the Full() output.
func TestFullDateEmbedded(t *testing.T) {
	restore := saveVars()
	defer restore()

	Date = "2099-12-31"

	got := Full()
	if !strings.Contains(got, "2099-12-31") {
		t.Errorf("Full() = %q: Date %q not found", got, Date)
	}
}

// TestVarRestoreAfterMutation is a meta-test that confirms saveVars/restore
// correctly resets all three variables, preventing test pollution.
func TestVarRestoreAfterMutation(t *testing.T) {
	before := [3]string{Version, Commit, Date}

	func() {
		restore := saveVars()
		defer restore()
		Version = "mutated"
		Commit = "mutated"
		Date = "mutated"
	}()

	after := [3]string{Version, Commit, Date}
	if before != after {
		t.Errorf("saveVars restore failed: before=%v after=%v", before, after)
	}
}
