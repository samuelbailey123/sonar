// Package checker orchestrates concurrent DNS propagation checks across
// multiple resolvers and aggregates the results for analysis.
package checker

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/samuelbailey123/sonar/internal/query"
	"github.com/samuelbailey123/sonar/internal/resolver"
)

const defaultMaxWorkers = 20

// Config holds the parameters for a propagation check run.
type Config struct {
	// Domain is the fully-qualified domain name to query.
	Domain string

	// RecordType is the DNS record type to query (e.g. dns.TypeA, dns.TypeMX).
	RecordType uint16

	// Resolvers is the set of DNS resolvers to query concurrently.
	Resolvers []resolver.Resolver

	// MaxWorkers caps the number of concurrent resolver queries.
	// Defaults to 20 when left as zero.
	MaxWorkers int
}

// Checker performs concurrent DNS propagation checks and collects results.
type Checker struct {
	config  Config
	results []query.Result
	mu      sync.Mutex
}

// New creates a Checker from the provided Config.
// If MaxWorkers is not set (zero), it defaults to 20.
// The internal results slice is pre-allocated to the number of resolvers to
// avoid reallocations during concurrent appends.
func New(cfg Config) *Checker {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = defaultMaxWorkers
		slog.Debug("checker: MaxWorkers not set, applying default",
			slog.Int("default", defaultMaxWorkers),
		)
	}

	slog.Info("checker: initialised",
		slog.String("domain", cfg.Domain),
		slog.Uint64("record_type", uint64(cfg.RecordType)),
		slog.Int("resolvers", len(cfg.Resolvers)),
		slog.Int("max_workers", cfg.MaxWorkers),
	)

	return &Checker{
		config:  cfg,
		results: make([]query.Result, 0, len(cfg.Resolvers)),
	}
}

// Run executes all DNS queries, waits for completion, and returns the
// collected results. This is the synchronous interface — use Stream for
// real-time result delivery.
func (c *Checker) Run(ctx context.Context) ([]query.Result, error) {
	resultCh := make(chan query.Result, len(c.config.Resolvers))
	c.run(ctx, resultCh)
	return c.Results(), nil
}

// Stream executes DNS queries and returns channels for streaming results as
// they arrive. The result channel is closed when all queries complete. The
// error channel receives at most one value after the result channel closes.
func (c *Checker) Stream(ctx context.Context) (<-chan query.Result, <-chan error) {
	resultCh := make(chan query.Result, len(c.config.Resolvers))
	errCh := make(chan error, 1)
	go func() {
		c.run(ctx, resultCh)
		errCh <- nil
		close(errCh)
	}()
	return resultCh, errCh
}

// run fans out DNS queries to every configured resolver using a bounded worker
// pool. Each result is stored internally and forwarded to resultCh as it
// arrives. resultCh is closed once all queries have completed or the context
// is cancelled.
func (c *Checker) run(ctx context.Context, resultCh chan<- query.Result) {
	resolvers := c.config.Resolvers
	workers := c.config.MaxWorkers

	slog.Info("checker: starting worker pool",
		slog.Int("total_resolvers", len(resolvers)),
		slog.Int("max_workers", workers),
		slog.String("domain", c.config.Domain),
	)

	// semaphore is a buffered channel used as a counting semaphore to bound
	// the number of goroutines that can perform a DNS query simultaneously.
	semaphore := make(chan struct{}, workers)

	var wg sync.WaitGroup

	for i, r := range resolvers {
		// Honour context cancellation before dispatching each new query.
		if ctx.Err() != nil {
			slog.Warn("checker: context cancelled before dispatching all queries",
				slog.Int("dispatched", i),
				slog.Int("total", len(resolvers)),
				slog.String("reason", ctx.Err().Error()),
			)
			break
		}

		wg.Add(1)
		go func(res resolver.Resolver, idx int) {
			defer wg.Done()

			// Acquire a slot in the semaphore, or bail on cancellation.
			select {
			case semaphore <- struct{}{}:
				slog.Debug("checker: worker slot acquired",
					slog.Int("worker_index", idx),
					slog.String("resolver", res.IP),
				)
			case <-ctx.Done():
				slog.Warn("checker: context cancelled while waiting for worker slot",
					slog.Int("worker_index", idx),
					slog.String("resolver", res.IP),
					slog.String("reason", ctx.Err().Error()),
				)
				return
			}
			defer func() {
				<-semaphore
				slog.Debug("checker: worker slot released",
					slog.Int("worker_index", idx),
					slog.String("resolver", res.IP),
				)
			}()

			slog.Debug("checker: dispatching query",
				slog.Int("worker_index", idx),
				slog.String("resolver_ip", res.IP),
				slog.String("resolver_name", res.Name),
				slog.String("domain", c.config.Domain),
				slog.Uint64("record_type", uint64(c.config.RecordType)),
			)

			result := query.Execute(ctx, res, c.config.Domain, c.config.RecordType)

			slog.Debug("checker: query completed",
				slog.Int("worker_index", idx),
				slog.String("resolver_ip", res.IP),
				slog.Bool("success", result.Error == nil),
				slog.Int("record_count", len(result.Records)),
			)

			// Store the result thread-safely.
			c.mu.Lock()
			c.results = append(c.results, result)
			c.mu.Unlock()

			// Forward the result to the caller's channel, or drop on cancellation
			// to prevent blocking indefinitely.
			select {
			case resultCh <- result:
			case <-ctx.Done():
				slog.Warn("checker: dropped result because context was cancelled",
					slog.String("resolver", res.IP),
				)
			}
		}(r, i)
	}

	// Wait for all goroutines to finish before closing the channel so the
	// caller's range loop terminates cleanly.
	wg.Wait()
	close(resultCh)

	slog.Info("checker: all queries finished",
		slog.Int("total_results", len(c.results)),
	)
}

// Results returns a copy of the collected query results. It is safe to call
// after Run has returned. The returned slice does not share backing memory
// with the Checker's internal state.
func (c *Checker) Results() []query.Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]query.Result, len(c.results))
	copy(out, c.results)

	slog.Debug("checker: results snapshot taken", slog.Int("count", len(out)))

	return out
}

// PropagationPercentage returns the proportion of successful results that
// agree with the consensus answer, expressed as a value between 0 and 100.
//
// The consensus answer is the most commonly observed set of records across all
// successful results (see ConsensusRecords). Resolvers that returned an error
// are excluded from both the numerator and denominator so that network
// failures do not artificially deflate the propagation percentage.
//
// Returns 0 if there are no successful results.
func PropagationPercentage(results []query.Result) float64 {
	consensus := ConsensusRecords(results)
	consensusKey := joinRecords(consensus)

	var successCount, matchCount int

	for _, r := range results {
		if r.Error != nil {
			slog.Debug("checker: skipping errored result in propagation calculation",
				slog.String("resolver", r.Resolver.IP),
				slog.String("error", r.Error.Error()),
			)
			continue
		}

		successCount++

		sorted := sortedCopy(r.Records)
		if joinRecords(sorted) == consensusKey {
			matchCount++
		}
	}

	if successCount == 0 {
		slog.Debug("checker: no successful results, propagation is 0%")
		return 0
	}

	pct := float64(matchCount) / float64(successCount) * 100

	slog.Info("checker: propagation percentage calculated",
		slog.Int("successful_resolvers", successCount),
		slog.Int("matching_resolvers", matchCount),
		slog.Float64("percentage", pct),
	)

	return pct
}

// ConsensusRecords returns the most commonly observed set of DNS records
// across all successful results. Records within each result are sorted before
// comparison so that ordering differences between resolvers do not create
// false disagreement.
//
// When there is a tie between two equally common answers, the lexicographically
// smaller joined string is chosen for determinism. Returns nil when there are
// no successful results.
func ConsensusRecords(results []query.Result) []string {
	// Tally occurrences of each unique sorted answer.
	tally := make(map[string]int)
	canonical := make(map[string][]string) // key → original sorted records

	for _, r := range results {
		if r.Error != nil {
			continue
		}

		sorted := sortedCopy(r.Records)
		key := joinRecords(sorted)
		tally[key]++
		canonical[key] = sorted
	}

	if len(tally) == 0 {
		slog.Debug("checker: no successful results for consensus calculation")
		return nil
	}

	var bestKey string
	var bestCount int

	for key, count := range tally {
		switch {
		case count > bestCount:
			bestCount = count
			bestKey = key
		case count == bestCount && key < bestKey:
			// Tie-break lexicographically for determinism.
			bestKey = key
		}
	}

	slog.Info("checker: consensus records determined",
		slog.Int("unique_answers", len(tally)),
		slog.Int("consensus_count", bestCount),
		slog.Int("consensus_records", len(canonical[bestKey])),
	)

	return canonical[bestKey]
}

// sortedCopy returns a sorted copy of records so the original slice is not
// mutated.
func sortedCopy(records []string) []string {
	if len(records) == 0 {
		return nil
	}

	out := make([]string, len(records))
	copy(out, records)
	slices.Sort(out)

	return out
}

// joinRecords produces a single comparable string from a sorted record slice.
// The pipe character is used as the separator because it does not appear in
// standard DNS record data.
func joinRecords(records []string) string {
	return strings.Join(records, "|")
}
