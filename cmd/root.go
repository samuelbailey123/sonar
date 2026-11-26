// Package cmd implements the sonar CLI entry point. It owns flag parsing,
// output mode selection, and top-level orchestration. All DNS work is
// delegated to the internal packages.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/samuelbailey123/sonar/internal/checker"
	"github.com/samuelbailey123/sonar/internal/query"
	"github.com/samuelbailey123/sonar/internal/resolver"
	"github.com/samuelbailey123/sonar/internal/style"
	"github.com/samuelbailey123/sonar/internal/ui"
	"github.com/samuelbailey123/sonar/internal/version"
)

// styles for the plain-text table rendered in --no-tui mode.
// styleHeader uses ANSI 256-color index 12 (bright blue) with bold.
// All other styles are sourced from pre-defined package-level variables in
// the style package so that color choices remain centralised.
var (
	styleHeader  = style.New().Bold(true).Foreground("12")
	styleSuccess = style.Green
	styleFailure = style.Red
	styleDim     = style.DimGray
	styleBold    = style.BoldStyle
)

// jsonOutput is the top-level envelope written to stdout when --json is set.
// Every field is always present so consumers can rely on a stable schema.
type jsonOutput struct {
	Domain         string       `json:"domain"`
	RecordType     string       `json:"record_type"`
	Timestamp      time.Time    `json:"timestamp"`
	PropagationPct float64      `json:"propagation_pct"`
	Consensus      []string     `json:"consensus"`
	Results        []jsonResult `json:"results"`
}

// jsonResult is the per-resolver entry inside jsonOutput.Results.
type jsonResult struct {
	ResolverName string   `json:"resolver_name"`
	ResolverIP   string   `json:"resolver_ip"`
	Country      string   `json:"country"`
	City         string   `json:"city"`
	Continent    string   `json:"continent"`
	Status       string   `json:"status"`
	Records      []string `json:"records"`
	LatencyMS    int64    `json:"latency_ms"`
	Error        string   `json:"error,omitempty"`
}

// Execute is the sole public entry point for the sonar CLI. It parses flags
// from os.Args, validates inputs, configures logging, runs DNS queries, and
// writes output in the requested format (TUI, plain table, or JSON). It
// returns a non-nil error when the invocation is invalid or a fatal runtime
// error occurs; the caller is expected to exit non-zero on error.
func Execute() error {
	slog.Debug("cmd: Execute() entry")

	fs := flag.NewFlagSet("sonar", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	// --type / -t: DNS record type.
	// flag does not natively support combined short+long aliases; we register
	// both names pointing at the same string variable via a shared pointer.
	recordTypeVal := "A"
	fs.StringVar(&recordTypeVal, "type", recordTypeVal, "DNS record type (A, AAAA, MX, CNAME, NS, TXT, SOA)")
	fs.StringVar(&recordTypeVal, "t", recordTypeVal, "DNS record type (shorthand for --type)")

	// --workers / -w: bounded concurrency for outgoing DNS queries.
	workersVal := 20
	fs.IntVar(&workersVal, "workers", workersVal, "maximum number of concurrent DNS queries")
	fs.IntVar(&workersVal, "w", workersVal, "maximum number of concurrent DNS queries (shorthand for --workers)")

	// --no-tui: disable the interactive terminal UI.
	noTUI := false
	fs.BoolVar(&noTUI, "no-tui", false, "print results as a plain text table instead of the interactive TUI")

	// --json: machine-readable JSON output; implies --no-tui.
	jsonMode := false
	fs.BoolVar(&jsonMode, "json", false, "output all results as a JSON object to stdout")

	// --verbose / -v: enable DEBUG-level structured logging to stderr.
	verbose := false
	fs.BoolVar(&verbose, "verbose", false, "enable debug logging")
	fs.BoolVar(&verbose, "v", false, "enable debug logging (shorthand for --verbose)")

	// --version: print version information and exit.
	showVersion := false
	fs.BoolVar(&showVersion, "version", false, "print version information and exit")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: sonar [flags] <domain>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Check DNS propagation for a domain across public resolvers worldwide.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Arguments:")
		fmt.Fprintln(os.Stderr, "  domain    The fully-qualified domain name to query (e.g. example.com)")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		// flag.ContinueOnError already wrote the error to Stderr.
		return err
	}

	if showVersion {
		fmt.Println(version.Full())
		return nil
	}

	// Configure the default slog handler before any further work so all
	// subsequent log calls — including those in internal packages — use the
	// correct level.
	configureLogging(verbose)

	slog.Debug("cmd: flags parsed",
		slog.String("record_type", recordTypeVal),
		slog.Int("workers", workersVal),
		slog.Bool("no_tui", noTUI),
		slog.Bool("json", jsonMode),
		slog.Bool("verbose", verbose),
	)

	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		return errors.New("domain argument is required")
	}
	if len(args) > 1 {
		fs.Usage()
		return fmt.Errorf("unexpected arguments after domain: %v", args[1:])
	}

	domain := strings.TrimSpace(args[0])
	if domain == "" {
		return errors.New("domain must not be empty")
	}

	slog.Info("cmd: domain validated", slog.String("domain", domain))

	// Validate the record type string before proceeding so the user gets a
	// clear error message rather than a runtime failure deep inside a worker.
	recordType, err := query.ParseRecordType(recordTypeVal)
	if err != nil {
		return fmt.Errorf("invalid --type value: %w", err)
	}

	slog.Info("cmd: record type parsed",
		slog.String("input", recordTypeVal),
		slog.Uint64("type_id", uint64(recordType)),
	)

	resolvers := resolver.All()

	slog.Info("cmd: resolver list loaded", slog.Int("count", len(resolvers)))

	cfg := checker.Config{
		Domain:     domain,
		RecordType: recordType,
		Resolvers:  resolvers,
		MaxWorkers: workersVal,
	}

	slog.Debug("cmd: checker config built", slog.Any("config", cfg))

	switch {
	case jsonMode:
		return runJSON(cfg)
	case noTUI:
		return runPlain(cfg)
	default:
		return runTUI(cfg)
	}
}

// configureLogging replaces the default slog handler with a text handler
// writing to stderr. When verbose is true the minimum level is DEBUG;
// otherwise it is WARN so that only warnings and errors surface to the user.
func configureLogging(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})

	slog.SetDefault(slog.New(handler))

	slog.Debug("cmd: logging configured", slog.String("level", level.String()))
}

// runTUI creates a ui.Model and calls Run() on it to launch the interactive
// terminal UI.
func runTUI(cfg checker.Config) error {
	slog.Info("cmd: starting TUI mode",
		slog.String("domain", cfg.Domain),
		slog.Int("resolvers", len(cfg.Resolvers)),
		slog.Int("workers", cfg.MaxWorkers),
	)

	c := checker.New(cfg)
	model := ui.New(cfg.Domain, recordTypeName(cfg.RecordType), cfg.Resolvers, c)

	slog.Debug("cmd: ui.Model created, invoking Run()")

	if err := model.Run(); err != nil {
		return fmt.Errorf("TUI run failed: %w", err)
	}

	slog.Info("cmd: TUI exited cleanly")

	return nil
}

// runJSON executes all DNS queries, waits for completion, then writes a single
// JSON object to stdout containing the propagation summary and every per-resolver
// result. It returns an error if the checker fails to run or the JSON cannot be
// encoded.
func runJSON(cfg checker.Config) error {
	slog.Info("cmd: starting JSON output mode",
		slog.String("domain", cfg.Domain),
		slog.Int("resolvers", len(cfg.Resolvers)),
		slog.Int("workers", cfg.MaxWorkers),
	)

	c := checker.New(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	results, err := c.Run(ctx)
	if err != nil {
		return fmt.Errorf("checker failed: %w", err)
	}

	slog.Info("cmd: all queries complete", slog.Int("result_count", len(results)))

	pct, consensus, _ := checker.PropagationStats(results)

	out := jsonOutput{
		Domain:         cfg.Domain,
		RecordType:     recordTypeName(cfg.RecordType),
		Timestamp:      time.Now().UTC(),
		PropagationPct: pct,
		Consensus:      consensus,
		Results:        toJSONResults(results),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("JSON encode failed: %w", err)
	}

	slog.Info("cmd: JSON output written successfully")

	return nil
}

// runPlain executes all DNS queries and prints results as a formatted table
// to stdout as they arrive. Progress is streamed via a results channel so the
// user sees each resolver's outcome immediately rather than waiting for all
// queries to complete. A summary line is printed once all queries are done.
func runPlain(cfg checker.Config) error {
	slog.Info("cmd: starting plain-text output mode",
		slog.String("domain", cfg.Domain),
		slog.Int("resolvers", len(cfg.Resolvers)),
		slog.Int("workers", cfg.MaxWorkers),
	)

	printBanner()
	fmt.Printf("Querying %s (%s) across %d resolvers...\n\n",
		styleBold.Render(cfg.Domain),
		recordTypeName(cfg.RecordType),
		len(cfg.Resolvers),
	)

	// Print table header.
	fmt.Printf("%s\n",
		styleHeader.Render(
			fmt.Sprintf("%-28s %-24s %-8s %-28s %s",
				"Resolver", "Location", "Status", "Result", "Latency",
			),
		),
	)
	fmt.Println(styleDim.Render(strings.Repeat("─", 100)))

	c := checker.New(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	resultsCh, errCh := c.Stream(ctx)

	var all []query.Result

	for r := range resultsCh {
		all = append(all, r)
		printRow(r)
	}

	// Drain the error channel; Stream sends at most one error, after results
	// are exhausted.
	if streamErr := <-errCh; streamErr != nil {
		slog.Error("cmd: checker stream returned an error", slog.String("error", streamErr.Error()))
		return fmt.Errorf("checker stream error: %w", streamErr)
	}

	fmt.Println()

	pct, consensus, _ := checker.PropagationStats(all)

	fmt.Printf("Propagation: %s (%d/%d resolvers agree)\n",
		styleBold.Render(fmt.Sprintf("%.0f%%", pct)),
		int(math.Round(pct/100*float64(len(all)))),
		len(all),
	)

	if len(consensus) > 0 {
		fmt.Printf("Consensus:   %s\n", styleSuccess.Render(strings.Join(consensus, ", ")))
	} else {
		fmt.Printf("Consensus:   %s\n", styleDim.Render("none — results are inconsistent"))
	}

	slog.Info("cmd: plain output complete",
		slog.Float64("propagation_pct", pct),
		slog.Any("consensus", consensus),
	)

	return nil
}

// printRow renders a single query.Result as a table row using style package
// colours. Successful rows use green for the tick; errors and timeouts use red.
func printRow(r query.Result) {
	slog.Debug("cmd: printRow called",
		slog.String("resolver_name", r.Resolver.Name),
		slog.String("status", r.Status.String()),
	)

	location := locationString(r.Resolver)

	var statusStyle, resultStyle style.Style
	var statusText, resultText string

	switch r.Status {
	case query.Success:
		statusStyle, statusText = styleSuccess, "✓"
		if len(r.Records) > 0 {
			resultText = shortRecord(r.Records[0])
			if len(r.Records) > 1 {
				resultText += fmt.Sprintf(" +%d", len(r.Records)-1)
			}
		} else {
			resultStyle, resultText = styleDim, "NODATA"
		}
	case query.Timeout:
		statusStyle, statusText = styleFailure, "T"
		resultStyle, resultText = styleFailure, "timeout"
	default:
		statusStyle, statusText = styleFailure, "✗"
		resultStyle, resultText = styleFailure, "error"
		if r.Error != nil {
			resultText = r.Error.Error()
		}
	}

	latencyStr := fmt.Sprintf("%dms", r.Latency.Milliseconds())
	if r.Status != query.Success {
		latencyStr = styleDim.Render(latencyStr)
	}

	fmt.Printf("%-28s %-24s %s %s %s\n",
		truncate(r.Resolver.Name, 27),
		truncate(location, 23),
		statusStyle.Render(fmt.Sprintf("%-8s", statusText)),
		resultStyle.Render(fmt.Sprintf("%-28s", truncate(resultText, 27))),
		latencyStr,
	)
}

// toJSONResults converts a slice of query.Result values into the JSON-friendly
// jsonResult slice, extracting only the fields needed for the output schema.
func toJSONResults(results []query.Result) []jsonResult {
	out := make([]jsonResult, 0, len(results))

	for _, r := range results {
		jr := jsonResult{
			ResolverName: r.Resolver.Name,
			ResolverIP:   r.Resolver.IP,
			Country:      r.Resolver.Country,
			City:         r.Resolver.City,
			Continent:    r.Resolver.Continent,
			Status:       r.Status.String(),
			Records:      r.Records,
			LatencyMS:    r.Latency.Milliseconds(),
		}
		if r.Error != nil {
			jr.Error = r.Error.Error()
		}
		out = append(out, jr)
	}

	return out
}

// recordTypeName returns the string representation of a DNS type constant
// (e.g. TypeA → "A"). It falls back to the numeric value if the type
// is not in the standard mapping.
func recordTypeName(t uint16) string {
	names := map[uint16]string{
		1:  "A",
		28: "AAAA",
		15: "MX",
		5:  "CNAME",
		2:  "NS",
		16: "TXT",
		6:  "SOA",
		12: "PTR",
	}
	if n, ok := names[t]; ok {
		return n
	}
	return fmt.Sprintf("TYPE%d", t)
}

// locationString formats a resolver's geographic metadata into a short
// human-readable string suitable for table display.
func locationString(r resolver.Resolver) string {
	if r.Country == "Global" {
		return "Global, Anycast"
	}
	if r.City != "" && r.Country != "" {
		return r.City + ", " + r.Country
	}
	if r.Country != "" {
		return r.Country
	}
	return r.Continent
}

// shortRecord strips the full DNS wire-format prefix from a record string,
// returning only the data portion for compact table display.
func shortRecord(s string) string {
	// DNS records are formatted as: "name TTL class type data"
	// We want only the data portion — the last non-empty whitespace-delimited
	// field.
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return s
	}
	return parts[len(parts)-1]
}

// truncate returns s with any characters beyond maxLen replaced by a single
// trailing ellipsis character. It is used to keep table columns aligned.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// printBanner renders the ASCII art welcome banner to stdout for the plain-text
// and --no-tui output modes.
func printBanner() {
	banner := `
   ███████  ██████  ███    ██  █████  ██████
   ██      ██    ██ ████   ██ ██   ██ ██   ██
   ███████ ██    ██ ██ ██  ██ ███████ ██████
        ██ ██    ██ ██  ██ ██ ██   ██ ██   ██
   ███████  ██████  ██   ████ ██   ██ ██   ██
`
	fmt.Print(styleHeader.Render(banner))
	fmt.Printf("   %s\n\n", styleDim.Render(fmt.Sprintf("DNS Propagation Checker  %s", version.Short())))
}
