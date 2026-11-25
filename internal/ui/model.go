package ui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samuelbailey123/sonar/internal/checker"
	"github.com/samuelbailey123/sonar/internal/query"
	"github.com/samuelbailey123/sonar/internal/resolver"
	"github.com/samuelbailey123/sonar/internal/style"
	"github.com/samuelbailey123/sonar/internal/version"
)

// output is the writer used for all terminal output. Tests may redirect this
// to a bytes.Buffer to capture rendered content without touching a real
// terminal.
var output io.Writer = os.Stdout

// ─── ANSI escape sequences ────────────────────────────────────────────────────

const escClearLine = "\x1b[2K"

// ─── Spinner frames ───────────────────────────────────────────────────────────

// spinnerFrames cycles through braille dot patterns to indicate pending work.
// The ticker advances through these at 80 ms per frame.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ─── Fixed column widths (targeting 120-char terminals) ───────────────────────

const (
	colWidthName     = 24
	colWidthLocation = 22
	colWidthStatus   = 6
	colWidthAnswer   = 42
	colWidthLatency  = 10

	// colSep is the inter-column spacing string.
	colSep = "  "
	// rowIndent is the left-margin applied to every rendered row.
	rowIndent = "  "
)

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	styleGreen     = style.Green
	styleYellow    = style.Yellow
	styleRed       = style.Red
	styleDimGray   = style.DimGray
	styleDimYellow = style.DimYellow
	styleCyan      = style.Cyan
	styleBold      = style.BoldStyle
	styleBanner    = style.Cyan.Bold(true)

	styleColHeader = style.Gray.Bold(true)
	styleContinent = style.Purple.Bold(true)
)

// ─── Banner ───────────────────────────────────────────────────────────────────

// banner returns the ASCII art welcome banner displayed at the top of the
// output.
func banner() string {
	art := `
   ███████  ██████  ███    ██  █████  ██████
   ██      ██    ██ ████   ██ ██   ██ ██   ██
   ███████ ██    ██ ██ ██  ██ ███████ ██████
        ██ ██    ██ ██  ██ ██ ██   ██ ██   ██
   ███████  ██████  ██   ████ ██   ██ ██   ██`

	return styleBanner.Render(art)
}

// ─── Model ────────────────────────────────────────────────────────────────────

type Model struct {
	mu         sync.Mutex
	outputMu   sync.Mutex
	domain     string
	recordType string
	resolvers  []resolver.Resolver
	results    map[string]query.Result
	startTime  time.Time
	checker    *checker.Checker
}

// New constructs an initialised Model ready to pass to Run. The checker is
// stored for later use; no DNS queries are initiated until Run is called.
func New(domain string, recordType string, resolvers []resolver.Resolver, chk *checker.Checker) *Model {
	slog.Info("ui.Model created",
		slog.String("domain", domain),
		slog.String("record_type", recordType),
		slog.Int("resolver_count", len(resolvers)),
	)
	return &Model{
		domain:     domain,
		recordType: recordType,
		resolvers:  resolvers,
		results:    make(map[string]query.Result, len(resolvers)),
		checker:    chk,
		startTime:  time.Now(),
	}
}

// Run collects all DNS results first, then presents them continent-by-continent
// with a progressive reveal. Each continent group is printed with a small delay
// between rows (15ms) so the output cascades down the screen rather than
// appearing all at once. A spinner status line at the bottom animates while
// queries are in flight.
//
// Flow:
//  1. Print banner, header, column headers.
//  2. Show a spinner while queries run in the background.
//  3. Once all results are in, reveal them grouped by continent with pacing.
//  4. Print the footer summary.
func (m *Model) Run() error {
	slog.Info("ui.Model.Run: starting")

	m.printBanner()

	// Hold the banner briefly so the user can see it before results start.
	time.Sleep(800 * time.Millisecond)

	m.printHeader()
	m.printColumnHeaders()

	// Show spinner while waiting for results.
	m.writeStatusLine(spinnerFrames[0])

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultCh, _ := m.checker.Stream(ctx)

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		for result := range resultCh {
			m.mu.Lock()
			m.results[result.Resolver.IP] = result
			m.mu.Unlock()
		}
	}()

	// Animate the spinner while waiting.
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	frame := 0

	for {
		select {
		case <-doneCh:
			goto reveal
		case <-tick.C:
			frame = (frame + 1) % len(spinnerFrames)
			m.writeStatusLine(spinnerFrames[frame])
		}
	}

reveal:
	// Clear the spinner line.
	m.clearStatusLine()

	m.mu.Lock()
	results := make(map[string]query.Result, len(m.results))
	for k, v := range m.results {
		results[k] = v
	}
	m.mu.Unlock()
	_, consensus, majority := checker.PropagationStats(resultsSlice(results))

	grouped := groupByContinent(m.resolvers)
	continents := orderedContinents(grouped)

	const rowDelay = 15 * time.Millisecond
	const continentDelay = 40 * time.Millisecond

	for _, continent := range continents {
		// Print continent header.
		fmt.Fprintf(output, "%s%s\n", rowIndent, styleContinent.Render(continent))
		time.Sleep(continentDelay)

		// Print each resolver row with a small delay.
		for _, r := range grouped[continent] {
			result, hasResult := results[r.IP]
			row := renderRow(r, result, hasResult, consensus, majority)
			fmt.Fprintln(output, row)
			time.Sleep(rowDelay)
		}
	}

	// Footer.
	m.printFooter()

	slog.Info("ui.Model.Run: output complete")
	return nil
}

// ─── Inline output helpers (append-only) ─────────────────────────────────────

// printBanner writes the ASCII art banner and subtitle line to output.
func (m *Model) printBanner() {
	slog.Debug("ui.Model.printBanner: writing banner")
	fmt.Fprintln(output, banner())
	fmt.Fprintf(output, "%s%s\n\n",
		rowIndent,
		styleDimGray.Render(fmt.Sprintf("DNS Propagation Checker  %s", version.Short())),
	)
}

// printHeader writes the domain/type info line.
func (m *Model) printHeader() {
	slog.Debug("ui.Model.printHeader: writing header")
	parts := []string{
		styleBold.Render("Domain:") + " " + styleCyan.Bold(true).Render(m.domain),
		styleBold.Render("Type:") + " " + styleBold.Render(m.recordType),
	}
	fmt.Fprintf(output, "%s%s\n\n", rowIndent, strings.Join(parts, "   "))
}

// printColumnHeaders writes the table column header row and divider.
func (m *Model) printColumnHeaders() {
	fmt.Fprintln(output, renderColumnHeaders())
	fmt.Fprintf(output, "%s%s\n",
		rowIndent,
		styleDimGray.Render(strings.Repeat("─", totalTableWidth())),
	)
}

// writeStatusLine clears the current line and writes the spinner + progress.
// This is the ONLY line that gets updated in-place (always at cursor position).
func (m *Model) writeStatusLine(spinnerFrame string) {
	m.mu.Lock()
	checked := len(m.results)
	total := len(m.resolvers)
	elapsed := time.Since(m.startTime).Seconds()
	m.mu.Unlock()

	line := fmt.Sprintf("%s%s %s",
		rowIndent,
		styleCyan.Render(spinnerFrame),
		styleDimGray.Render(fmt.Sprintf("Checking %d/%d resolvers... (%.1fs)", checked, total, elapsed)),
	)

	m.outputMu.Lock()
	fmt.Fprintf(output, "\r"+escClearLine+"%s", line)
	m.outputMu.Unlock()
}

// clearStatusLine removes the status line so the footer can be printed below.
func (m *Model) clearStatusLine() {
	m.outputMu.Lock()
	fmt.Fprintf(output, "\r"+escClearLine)
	m.outputMu.Unlock()
}

// printFooter appends the divider + summary + elapsed-time line below the
// table after all results have arrived.
func (m *Model) printFooter() {
	slog.Debug("ui.Model.printFooter: writing footer")

	m.mu.Lock()
	v := &viewState{
		resolvers: m.resolvers,
		results:   m.results,
		startTime: m.startTime,
	}
	m.mu.Unlock()

	fmt.Fprint(output, v.renderFooter())

	elapsed := time.Since(v.startTime).Round(time.Millisecond)
	fmt.Fprintf(output, "%s%s\n",
		rowIndent,
		styleDimGray.Render(fmt.Sprintf("completed in %s", elapsed)),
	)
}

type viewState struct {
	resolvers []resolver.Resolver
	results   map[string]query.Result
	startTime time.Time
}

// renderColumnHeaders returns the formatted column header row string.
func renderColumnHeaders() string {
	cols := []string{
		padVisible(styleColHeader.Render("Resolver"), colWidthName),
		padVisible(styleColHeader.Render("Location"), colWidthLocation),
		padVisible(styleColHeader.Render("St"), colWidthStatus),
		padVisible(styleColHeader.Render("Answer"), colWidthAnswer),
		styleColHeader.Render("Latency"),
	}
	return rowIndent + strings.Join(cols, colSep)
}

// renderRow produces a single formatted table row for one resolver. It uses
// the live result if one has arrived, or a pending placeholder otherwise.
func renderRow(r resolver.Resolver, result query.Result, hasResult bool, consensus []string, majority bool) string {
	nameCell := padVisible(truncate(r.Name, colWidthName), colWidthName)
	locCell := padVisible(truncate(formatLocation(r), colWidthLocation), colWidthLocation)

	var statusCell, answerCell, latencyCell string

	switch {
	case !hasResult:
		statusCell = padVisible(styleDimGray.Render("..."), colWidthStatus)
		answerCell = padVisible(styleDimGray.Render("—"), colWidthAnswer)
		latencyCell = styleDimGray.Render("—")

	case result.Status == query.Timeout:
		statusCell = padVisible(styleDimYellow.Render("⏱"), colWidthStatus)
		answerCell = padVisible(styleDimYellow.Render("timeout"), colWidthAnswer)
		latencyCell = styleDimGray.Render("—")

	case result.Status == query.Error || result.Error != nil:
		errMsg := "error"
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		statusCell = padVisible(styleRed.Render("✗"), colWidthStatus)
		answerCell = padVisible(styleRed.Render(truncate(errMsg, colWidthAnswer)), colWidthAnswer)
		latencyCell = styleDimGray.Render("—")

	default:
		answer := formatAnswer(result.Records)
		if majority && matchesConsensus(result.Records, consensus) {
			statusCell = padVisible(styleGreen.Render("✓"), colWidthStatus)
			answerCell = padVisible(styleGreen.Render(truncate(answer, colWidthAnswer)), colWidthAnswer)
		} else {
			statusCell = padVisible(styleYellow.Render("⚠"), colWidthStatus)
			answerCell = padVisible(styleYellow.Render(truncate(answer, colWidthAnswer)), colWidthAnswer)
		}
		latencyCell = colorForLatency(result.Latency).Render(formatLatency(result.Latency))
	}

	return rowIndent + strings.Join([]string{nameCell, locCell, statusCell, answerCell, latencyCell}, colSep)
}

// ─── Footer ───────────────────────────────────────────────────────────────────

func (v *viewState) renderFooter() string {
	total := len(v.resolvers)
	checked := len(v.results)
	pct, consensus, _ := checker.PropagationStats(resultsSlice(v.results))

	consensusStr := "—"
	if len(consensus) > 0 {
		consensusStr = truncate(formatAnswer(consensus), 48)
	}

	divider := rowIndent + styleDimGray.Render(strings.Repeat("─", totalTableWidth())) + "\n"
	summary := fmt.Sprintf(
		"%sChecked: %s/%s   Consensus: %s   Propagated: %s\n",
		rowIndent,
		styleBold.Render(fmt.Sprintf("%d", checked)),
		styleBold.Render(fmt.Sprintf("%d", total)),
		styleCyan.Render(consensusStr),
		colorForPct(pct).Bold(true).Render(fmt.Sprintf("%.0f%%", pct)),
	)
	return divider + summary
}

// ─── Propagation helpers ──────────────────────────────────────────────────────

func matchesConsensus(records, consensus []string) bool {
	if len(records) != len(consensus) {
		return false
	}
	sorted := sortedCopy(records)
	for i, v := range sorted {
		if v != consensus[i] {
			return false
		}
	}
	return true
}

// resultsSlice converts the results map into a []query.Result slice for use
// with package-level functions in checker that operate on slices.
func resultsSlice(m map[string]query.Result) []query.Result {
	out := make([]query.Result, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	return out
}

// ─── Grouping helpers ─────────────────────────────────────────────────────────

// groupByContinent organises resolvers into a map keyed by continent name.
// Resolvers with an empty Continent field are grouped under "Unknown".
func groupByContinent(resolvers []resolver.Resolver) map[string][]resolver.Resolver {
	out := make(map[string][]resolver.Resolver)
	for _, r := range resolvers {
		c := r.Continent
		if c == "" {
			c = "Unknown"
		}
		out[c] = append(out[c], r)
	}
	return out
}

// orderedContinents returns continent keys from grouped in a stable,
// geographically intuitive order. Any unrecognised continent is appended at
// the end, sorted alphabetically.
func orderedContinents(grouped map[string][]resolver.Resolver) []string {
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

	seen := make(map[string]bool)
	var out []string

	for _, c := range preferred {
		if _, ok := grouped[c]; ok {
			out = append(out, c)
			seen[c] = true
		}
	}

	var extras []string
	for c := range grouped {
		if !seen[c] {
			extras = append(extras, c)
		}
	}
	slices.Sort(extras)
	return append(out, extras...)
}

// ─── Formatting helpers ───────────────────────────────────────────────────────

// formatLocation returns "City, Country" when City is set and differs from
// Country, or just "Country" otherwise.
func formatLocation(r resolver.Resolver) string {
	if r.City != "" && r.City != r.Country {
		return r.City + ", " + r.Country
	}
	return r.Country
}

// formatAnswer joins a slice of DNS record strings with a comma separator.
func formatAnswer(records []string) string {
	return strings.Join(records, ", ")
}

// formatLatency renders a duration as a compact human-readable string,
// using microseconds for sub-millisecond values.
func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dus", d.Microseconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// colorForLatency returns the appropriate style for a latency value:
//   - green:  < 50 ms  (fast)
//   - yellow: < 200 ms (moderate)
//   - red:    >= 200 ms (slow)
func colorForLatency(d time.Duration) style.Style {
	switch {
	case d < 50*time.Millisecond:
		return styleGreen
	case d < 200*time.Millisecond:
		return styleYellow
	default:
		return styleRed
	}
}

// colorForPct returns the appropriate style for a propagation percentage:
//   - green:  100 %
//   - yellow: >= 50 %
//   - red:    < 50 %
func colorForPct(pct float64) style.Style {
	switch {
	case pct >= 100:
		return styleGreen
	case pct >= 50:
		return styleYellow
	default:
		return styleRed
	}
}

// ─── String utilities ─────────────────────────────────────────────────────────

// padVisible right-pads s with spaces so its visible (ANSI-stripped) width
// equals exactly width. If s is already wider, it is returned unchanged.
func padVisible(s string, width int) string {
	visible := style.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

// truncate shortens s to at most maxLen visible runes. When truncation occurs
// the string ends with "..." so the visible length is still maxLen.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// totalTableWidth returns the total visible character width of a data row,
// matching the fixed-column layout defined by the width constants.
func totalTableWidth() int {
	return len(rowIndent) +
		colWidthName + len(colSep) +
		colWidthLocation + len(colSep) +
		colWidthStatus + len(colSep) +
		colWidthAnswer + len(colSep) +
		colWidthLatency
}

// sortedCopy returns a sorted copy of ss without mutating the input.
func sortedCopy(ss []string) []string {
	cp := make([]string, len(ss))
	copy(cp, ss)
	slices.Sort(cp)
	return cp
}
