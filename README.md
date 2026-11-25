# sonar

DNS propagation checker with live, global resolver coverage. Query your domain across 54 resolvers in 8 regions to see how DNS records propagate worldwide.

## Overview

Sonar queries a domain across a curated list of public DNS resolvers distributed globally, then reports back:
- Overall propagation percentage
- Per-resolver query results with latency and geographic location
- Consensus record set (the records agreed upon by the majority)

Three output modes suit different workflows: an animated table in the terminal, a plain-text table for CI/CD, and JSON for programmatic integration.

## Features

- **54 Global Resolvers** — coverage across North America, South America, Europe, Africa, Asia, Middle East, and Oceania
- **Zero Dependencies** — custom RFC 1035 DNS client, no external packages
- **Three Output Modes** — animated table, plain-text table, or JSON for scripting
- **Concurrent Queries** — configurable worker pool with smart timeouts
- **Consensus Detection** — automatic majority-vote analysis of record sets
- **Structured Logging** — debug mode for troubleshooting propagation issues
- **All Record Types** — A, AAAA, MX, CNAME, NS, TXT, SOA, PTR

## Installation

### Using `go install`

```bash
go install github.com/samuelbailey123/sonar@latest
sonar --version
```

### From Source

```bash
git clone https://github.com/samuelbailey123/sonar.git
cd sonar
make build
./sonar example.com
```

## Usage

Flags go before the domain: `sonar [flags] <domain>`.

### Basic Query

```bash
sonar example.com
```

The default mode shows a banner, animates a spinner while queries run, then reveals results continent-by-continent with a cascading effect. Output is inline and scrollable — no alternate screen buffer.

### Plain-Text Output

```bash
sonar --no-tui example.com
```

Output, after the banner:
```
Querying example.com (A) across 54 resolvers...

Resolver                     Location                 Status   Result                       Latency
────────────────────────────────────────────────────────────────────────────────────────────────────
FDN                          Paris, France            ✓ 172.66.147.243 +1            25ms
Quad9 Primary                Global, Anycast          ✓ 172.66.147.243 +1            26ms
Cloudflare Primary           Global, Anycast          ✓ 172.66.147.243 +1            27ms
[... 51 more rows ...]

Propagation: 100% (54/54 resolvers agree)
Consensus:   example.com. 212 IN A 104.20.23.154, example.com. 212 IN A 172.66.147.243
```

### JSON Output

```bash
sonar --json example.com | jq .
```

Output, trimmed to two results:
```json
{
  "domain": "example.com",
  "record_type": "A",
  "timestamp": "2026-11-25T21:40:13.643284Z",
  "propagation_pct": 100,
  "consensus": [
    "example.com. 212 IN A 104.20.23.154",
    "example.com. 212 IN A 172.66.147.243"
  ],
  "results": [
    {
      "resolver_name": "CleanBrowsing Primary",
      "resolver_ip": "185.228.168.168",
      "country": "United States",
      "city": "Anycast",
      "continent": "North America",
      "status": "success",
      "records": [
        "example.com. 212 IN A 172.66.147.243",
        "example.com. 212 IN A 104.20.23.154"
      ],
      "latency_ms": 3
    },
    {
      "resolver_name": "Comodo Secure DNS Primary",
      "resolver_ip": "8.26.56.26",
      "country": "United States",
      "city": "Clifton",
      "continent": "North America",
      "status": "success",
      "records": [
        "example.com. 212 IN A 172.66.147.243",
        "example.com. 212 IN A 104.20.23.154"
      ],
      "latency_ms": 3
    }
  ]
}
```

### Query Specific Record Types

```bash
sonar -t AAAA example.com      # Query for IPv6 records
sonar -t MX example.com        # Query for mail exchanger records
sonar -t NS example.com        # Query for nameserver records
sonar -t TXT example.com       # Query for TXT records
sonar -t CNAME example.com     # Query for CNAME records
sonar -t SOA example.com       # Query for SOA records
sonar -t PTR example.com       # Query for PTR records
```

### Control Concurrency

```bash
sonar -w 10 example.com    # Use 10 concurrent workers (default: 20)
sonar --workers 50 example.com
```

### Enable Debug Logging

```bash
sonar -v example.com
sonar --verbose example.com
```

Outputs structured logs to stderr:
```
time=2026-11-25T15:43:34.355-06:00 level=DEBUG msg="cmd: logging configured" level=DEBUG
time=2026-11-25T15:43:34.355-06:00 level=DEBUG msg="cmd: flags parsed" record_type=A workers=20 no_tui=false json=false verbose=true
time=2026-11-25T15:43:34.355-06:00 level=INFO msg="cmd: domain validated" domain=example.com
```

### Print Version

```bash
sonar --version
# Output: sonar <version> (commit <short sha>, built <UTC build time>, <os>/<arch>)
```

`make build` stamps the version, commit and build time from git. A plain `go build` or `go install` prints `dev` and `unknown` instead.

## Flag Reference

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--type` | `-t` | string | `A` | DNS record type (A, AAAA, MX, CNAME, NS, TXT, SOA, PTR) |
| `--workers` | `-w` | int | `20` | Maximum number of concurrent DNS queries |
| `--no-tui` | — | bool | `false` | Print a plain-text table instead of the animated default view |
| `--json` | — | bool | `false` | Output results as JSON (implies `--no-tui`) |
| `--verbose` | `-v` | bool | `false` | Enable debug-level logging to stderr |
| `--version` | — | bool | `false` | Print version information and exit |

## Supported Record Types

| Type | Example | Usage |
|------|---------|-------|
| **A** | `93.184.216.34` | Query IPv4 address records |
| **AAAA** | `2606:2800:220:1:248:1893:25c8:1946` | Query IPv6 address records |
| **MX** | `10 mail.example.com.` | Query mail exchanger records |
| **CNAME** | `www.example.com.` | Query canonical name records |
| **NS** | `ns1.example.com.` | Query nameserver records |
| **TXT** | `v=spf1 include:_spf.example.com ~all` | Query text records (DNS verification, SPF, DKIM) |
| **SOA** | `ns1.example.com. hostmaster.example.com. ...` | Query start of authority records |
| **PTR** | `example.com.` | Query reverse DNS records |

## Resolver Coverage

Sonar queries 54 public DNS resolvers across 8 geographic regions:

| Continent | Region | Resolvers | Examples |
|-----------|--------|-----------|----------|
| **Global** | Anycast | 6 | Google Public DNS, Cloudflare, Quad9 |
| **North America** | US + Canada | 12 | OpenDNS, Level3, Comodo, Neustar, CIRA |
| **South America** | Brazil | 3 | Claro, CPS, EMBRATEL |
| **Europe** | Germany, France, Denmark, Sweden, Luxembourg, Estonia | 12 | DNS.WATCH, Freifunk, FDN, UncensoredDNS, LibreDNS, Mullvad, dns0.eu |
| **Africa** | Kenya, South Africa, Nigeria | 3 | Liquid Telecom, Rain, MTN |
| **Asia** | China, South Korea, Japan, Taiwan, Russia | 11 | AliDNS, 114DNS, KT, SK Broadband, IIJ, SoftBank, TWNIC |
| **Middle East** | UAE, Turkey, Saudi Arabia | 3 | Etisalat, Turk Telekom, STC |
| **Oceania** | Australia, New Zealand | 4 | Telstra, Oracle Dyn, Optus, Orcon |

All resolvers are public, documented services with stable APIs. Geographic metadata is maintained in `internal/resolver/resolver.go`.

## Architecture

Sonar is structured into focused, testable packages with clear separation of concerns:

### `cmd/root.go`
Entry point for the CLI. Handles flag parsing, input validation, output mode selection, and orchestration of the checker and UI. Owns the JSON output schema and plain-text table formatting.

### `internal/dns`
Custom RFC 1035 DNS client with zero external dependencies. Constructs wire-format DNS queries, executes them over UDP on port 53, and parses responses including DNS name compression. Used by the query package.

### `internal/query`
High-level DNS query execution. Takes a domain, record type, and resolver address; executes the query with timeout handling; and returns structured results including latency, status, records, and error information.

### `internal/checker`
Orchestrates concurrent DNS propagation checks across multiple resolvers. Implements a worker pool pattern to control concurrency, aggregates results, and provides both streaming and blocking interfaces for results delivery.

### `internal/resolver`
Curated list of 54 public DNS resolvers with geographic metadata (country, city, continent). The list is built once at package load, and `All()` returns a copy of it.

### `internal/ui`
Inline output model for the default view. Prints a banner with a spinner while queries run, then reveals results continent-by-continent with paced timing. All output is append-only and scrollable — no alternate screen buffer.

### `internal/style`
ANSI truecolor terminal styling. Provides color definitions, bold text, and render functions. Centralizes all color and style decisions.

### `internal/version`
Build-time version metadata injection via `-ldflags`. Stores semantic version, commit SHA, and build timestamp. Provides formatting functions for CLI output.

## Building from Source

### Prerequisites

- Go 1.26 or later
- GNU Make (optional, but recommended)

### Build Steps

```bash
# Clone the repository
git clone https://github.com/samuelbailey123/sonar.git
cd sonar

# Build the binary (creates ./sonar executable)
make build

# Run directly without installing
./sonar example.com

# Install to $GOPATH/bin (usually ~/go/bin)
make install

# Clean build artifacts
make clean
```

### Manual Build (without Make)

```bash
# Build without version metadata
go build -o sonar .

# Build with version metadata
go build -ldflags "-X github.com/samuelbailey123/sonar/internal/version.Version=1.0.0 \
  -X github.com/samuelbailey123/sonar/internal/version.Commit=$(git rev-parse --short HEAD) \
  -X github.com/samuelbailey123/sonar/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o sonar .
```

## Testing

### Run All Tests

```bash
make test
```

This runs all tests with race detection enabled and count=1 (no caching).

### Run Tests for a Specific Package

```bash
go test ./internal/dns
go test ./internal/query -v
```

### Lint Code

```bash
make lint
```

Runs `go vet` across all packages.

## JSON Output Schema

### Top-Level Envelope

The `--json` output is a single JSON object with these fields:

```json
{
  "domain": "string",           // The queried domain (e.g. "example.com")
  "record_type": "string",      // The DNS record type (e.g. "A", "AAAA", "MX")
  "timestamp": "string",        // ISO 8601 UTC timestamp of query execution
  "propagation_pct": "number",  // Percentage of all resolvers returning the most common record set (0-100)
  "consensus": ["string"],      // Record set returned by more than half of all resolvers (null if none)
  "results": [...]              // Per-resolver results array
}
```

### Per-Resolver Result Object

Each element in the `results` array has:

```json
{
  "resolver_name": "string",    // Human-readable resolver name
  "resolver_ip": "string",      // IPv4 address of the resolver
  "country": "string",          // Country where resolver is located
  "city": "string",             // City (or "Anycast" for global resolvers)
  "continent": "string",        // Continent
  "status": "string",           // "success", "error", or "timeout"
  "records": ["string"],        // Array of DNS records in presentation format
  "latency_ms": "number",       // Query round-trip time in milliseconds
  "error": "string"             // Error message if status != "success" (omitted if empty)
}
```

### Consensus Calculation

**Propagation percentage** is calculated as:
```
(resolvers returning the most common record set) / (all resolvers queried) * 100
```

**Consensus** is the most common record set when more than half of all resolvers queried returned it; timeouts and errors count toward the total. Record sets are compared after sorting, and the consensus is listed sorted. Every output uses this rule: if no set reaches a majority, `consensus` is `null`, `--no-tui` prints `none`, and the default view shows `—` and marks no row ✓.

**Query status**:
- `success` — resolver returned a valid DNS response
- `timeout` — query did not complete within 5 seconds
- `error` — network error or protocol failure

## How It Works

### Query Execution

1. **Validation** — User provides a domain and optional record type. Sonar validates the domain format and record type string.

2. **Resolver List** — Load the curated list of 54 public resolvers with geographic metadata.

3. **Concurrent Queries** — Spawn a worker pool (default 20 concurrent workers) to query each resolver:
   - Construct RFC 1035 wire-format query for the domain and record type
   - Send UDP packet to resolver on port 53 with 5-second timeout
   - Parse the response and extract answer records
   - Record latency, status, and any errors

4. **Propagation Analysis** — Once all queries complete:
   - Group results by record set content (normalized)
   - Find the record set returned by the largest group of successful resolvers
   - Calculate propagation percentage as (largest group size) / (total resolvers)
   - If largest group > 50%, designate it as consensus

5. **Output** — Render results in the requested format:
   - Default — animated banner, spinner during queries, continent-by-continent progressive reveal
   - Plain text (`--no-tui`) — streaming table output as results arrive
   - JSON (`--json`) — structured output for programmatic consumption

### Timeout Handling

Each query has a 5-second timeout enforced by the context and UDP dialer. If a resolver doesn't respond within this window, the query is marked as a timeout error.

### Error Classification

Query errors are classified as:
- **Network errors** — connection refused, host unreachable, etc.
- **Protocol errors** — malformed response, parse failures
- **Timeouts** — no response within 5 seconds

All errors are logged and included in the results with a status and error message.

## License

MIT — See LICENSE file for details.
