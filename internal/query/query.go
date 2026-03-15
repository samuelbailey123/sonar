// Package query handles executing individual DNS queries against resolvers
// and parsing record type strings into their corresponding DNS type constants.
package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/samuelbailey123/sonar/internal/dns"
	"github.com/samuelbailey123/sonar/internal/resolver"
)

// Status represents the outcome of a DNS query.
type Status int

const (
	// Pending indicates the query has not yet been executed.
	Pending Status = iota

	// Success indicates the query completed and returned a valid response.
	Success

	// Error indicates the query failed due to a network or protocol error.
	Error

	// Timeout indicates the query did not complete within the allowed duration.
	Timeout
)

// String returns a human-readable representation of the Status value.
func (s Status) String() string {
	switch s {
	case Pending:
		return "pending"
	case Success:
		return "success"
	case Error:
		return "error"
	case Timeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// Result holds the outcome of a single DNS query against one resolver.
type Result struct {
	// Resolver is the upstream DNS server that was queried.
	Resolver resolver.Resolver

	// Records contains the answer records returned as formatted strings.
	// It is nil when Status is not Success.
	Records []string

	// Status is the final state of the query.
	Status Status

	// Error holds any error returned during execution.
	// It is nil when Status is Success.
	Error error

	// Latency is the round-trip time for the DNS exchange.
	Latency time.Duration
}

// Execute sends a DNS query for the given domain and record type to the
// specified resolver. It respects context cancellation and deadlines via the
// internal dns package. The returned Result always has its Status, Latency,
// and Resolver fields populated.
func Execute(ctx context.Context, r resolver.Resolver, domain string, recordType uint16) Result {
	log := slog.With(
		slog.String("resolver_name", r.Name),
		slog.String("resolver_ip", r.IP),
		slog.String("domain", domain),
		slog.String("record_type", dns.TypeToString[recordType]),
	)

	log.Debug("dns query starting")

	result := Result{
		Resolver: r,
		Status:   Pending,
	}

	records, latency, err := dns.Query(ctx, r.IP, domain, recordType)
	result.Latency = latency

	if err != nil {
		// Distinguish timeout conditions from other network errors so the
		// caller can render them differently in the UI.
		if isTimeout(ctx, err) {
			log.Warn("dns query timed out",
				slog.Duration("latency", result.Latency),
				slog.String("error", err.Error()),
			)
			result.Status = Timeout
			result.Error = err
			return result
		}

		log.Error("dns query failed",
			slog.Duration("latency", result.Latency),
			slog.String("error", err.Error()),
		)
		result.Status = Error
		result.Error = err
		return result
	}

	result.Records = recordsToStrings(records)
	result.Status = Success

	log.Info("dns query succeeded",
		slog.Duration("latency", result.Latency),
		slog.Int("answer_count", len(result.Records)),
		slog.Any("records", result.Records),
	)

	return result
}

// ParseRecordType converts a DNS record type string (case-insensitive) into
// the corresponding dns package type constant. Supported types are: A, AAAA,
// MX, CNAME, NS, TXT, SOA, PTR.
func ParseRecordType(s string) (uint16, error) {
	slog.Debug("parsing record type", slog.String("input", s))

	upper := strings.ToUpper(strings.TrimSpace(s))

	supported := map[string]uint16{
		"A":     dns.TypeA,
		"AAAA":  dns.TypeAAAA,
		"MX":    dns.TypeMX,
		"CNAME": dns.TypeCNAME,
		"NS":    dns.TypeNS,
		"TXT":   dns.TypeTXT,
		"SOA":   dns.TypeSOA,
		"PTR":   dns.TypePTR,
	}

	t, ok := supported[upper]
	if !ok {
		slog.Warn("unknown record type requested", slog.String("input", s))
		return 0, fmt.Errorf("unsupported record type %q: must be one of A, AAAA, MX, CNAME, NS, TXT, SOA, PTR", s)
	}

	slog.Debug("record type parsed", slog.String("input", s), slog.Uint64("type_id", uint64(t)))
	return t, nil
}

// recordsToStrings converts a slice of dns.Record values into their canonical
// string representations using each record's String method.
func recordsToStrings(records []dns.Record) []string {
	if len(records) == 0 {
		return nil
	}

	out := make([]string, len(records))
	for i, rec := range records {
		out[i] = rec.String()
	}
	return out
}

// isTimeout reports whether the error represents a query timeout, either
// because the context was cancelled/exceeded or because the DNS client
// returned a network timeout error.
func isTimeout(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}

	var netErr net.Error
	if ok := isNetError(err, &netErr); ok && netErr.Timeout() {
		return true
	}

	// The internal dns client wraps errors; check the message as a last resort.
	return strings.Contains(err.Error(), "i/o timeout") ||
		strings.Contains(err.Error(), "deadline exceeded")
}

// isNetError unwraps err into a net.Error if possible.
func isNetError(err error, target *net.Error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		*target = ne
		return true
	}
	return false
}
