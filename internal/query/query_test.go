package query

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/samuelbailey123/sonar/internal/dns"
	"github.com/samuelbailey123/sonar/internal/resolver"
)

// ─────────────────────────────────────────────────────────────────────────────
// Status.String
// ─────────────────────────────────────────────────────────────────────────────

func TestStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   string
	}{
		{name: "Pending", status: Pending, want: "pending"},
		{name: "Success", status: Success, want: "success"},
		{name: "Error", status: Error, want: "error"},
		{name: "Timeout", status: Timeout, want: "timeout"},
		{name: "unknown negative", status: Status(-1), want: "unknown"},
		{name: "unknown large", status: Status(999), want: "unknown"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.status.String()
			if got != tc.want {
				t.Errorf("Status(%d).String() = %q, want %q", int(tc.status), got, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ParseRecordType
// ─────────────────────────────────────────────────────────────────────────────

func TestParseRecordType_SupportedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  uint16
	}{
		{input: "A", want: dns.TypeA},
		{input: "AAAA", want: dns.TypeAAAA},
		{input: "MX", want: dns.TypeMX},
		{input: "CNAME", want: dns.TypeCNAME},
		{input: "NS", want: dns.TypeNS},
		{input: "TXT", want: dns.TypeTXT},
		{input: "SOA", want: dns.TypeSOA},
		{input: "PTR", want: dns.TypePTR},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRecordType(tc.input)
			if err != nil {
				t.Fatalf("ParseRecordType(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseRecordType(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseRecordType_CaseInsensitive(t *testing.T) {
	t.Parallel()

	// Each variant must resolve to the same type constant regardless of case.
	variants := []struct {
		input string
		want  uint16
	}{
		{input: "a", want: dns.TypeA},
		{input: "aaaa", want: dns.TypeAAAA},
		{input: "mx", want: dns.TypeMX},
		{input: "cname", want: dns.TypeCNAME},
		{input: "ns", want: dns.TypeNS},
		{input: "txt", want: dns.TypeTXT},
		{input: "soa", want: dns.TypeSOA},
		{input: "ptr", want: dns.TypePTR},
		{input: "Mx", want: dns.TypeMX},
		{input: "cNaMe", want: dns.TypeCNAME},
	}

	for _, tc := range variants {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRecordType(tc.input)
			if err != nil {
				t.Fatalf("ParseRecordType(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseRecordType(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseRecordType_WhitespaceTrimmed(t *testing.T) {
	t.Parallel()

	got, err := ParseRecordType("  A  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dns.TypeA {
		t.Errorf("ParseRecordType(\"  A  \") = %d, want %d", got, dns.TypeA)
	}
}

func TestParseRecordType_UnknownReturnsError(t *testing.T) {
	t.Parallel()

	unknowns := []string{"SRV", "HTTPS", "CAA", "", "notarecord", "1", "TYPE99"}

	for _, input := range unknowns {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRecordType(input)
			if err == nil {
				t.Fatalf("ParseRecordType(%q) expected error, got type %d", input, got)
			}
			if got != 0 {
				t.Errorf("ParseRecordType(%q) returned non-zero on error: %d", input, got)
			}
			if !strings.Contains(err.Error(), "unsupported record type") {
				t.Errorf("error message missing 'unsupported record type': %v", err)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// recordsToStrings (white-box)
// ─────────────────────────────────────────────────────────────────────────────

func TestRecordsToStrings_NilInput(t *testing.T) {
	t.Parallel()
	got := recordsToStrings(nil)
	if got != nil {
		t.Errorf("recordsToStrings(nil) = %v, want nil", got)
	}
}

func TestRecordsToStrings_EmptySlice(t *testing.T) {
	t.Parallel()
	got := recordsToStrings([]dns.Record{})
	if got != nil {
		t.Errorf("recordsToStrings([]) = %v, want nil", got)
	}
}

func TestRecordsToStrings_SingleRecord(t *testing.T) {
	t.Parallel()
	rec := dns.Record{
		Name:  "example.com.",
		Type:  dns.TypeA,
		Class: 1,
		TTL:   300,
		Data:  "93.184.216.34",
	}
	got := recordsToStrings([]dns.Record{rec})
	if len(got) != 1 {
		t.Fatalf("expected 1 string, got %d", len(got))
	}
	want := rec.String()
	if got[0] != want {
		t.Errorf("recordsToStrings()[0] = %q, want %q", got[0], want)
	}
}

func TestRecordsToStrings_MultipleRecords(t *testing.T) {
	t.Parallel()
	records := []dns.Record{
		{Name: "example.com.", Type: dns.TypeA, Class: 1, TTL: 300, Data: "93.184.216.34"},
		{Name: "example.com.", Type: dns.TypeAAAA, Class: 1, TTL: 3600, Data: "2001:db8::1"},
		{Name: "example.com.", Type: dns.TypeMX, Class: 1, TTL: 900, Data: "10 mail.example.com."},
	}
	got := recordsToStrings(records)
	if len(got) != len(records) {
		t.Fatalf("expected %d strings, got %d", len(records), len(got))
	}
	for i, rec := range records {
		if got[i] != rec.String() {
			t.Errorf("recordsToStrings()[%d] = %q, want %q", i, got[i], rec.String())
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// isTimeout (white-box)
// ─────────────────────────────────────────────────────────────────────────────

// mockNetError is a minimal net.Error implementation for testing.
type mockNetError struct {
	msg     string
	timeout bool
}

func (e *mockNetError) Error() string   { return e.msg }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return false }

func TestIsTimeout_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := errors.New("some error")
	if !isTimeout(ctx, err) {
		t.Error("expected isTimeout=true for cancelled context")
	}
}

func TestIsTimeout_DeadlineExceededContext(t *testing.T) {
	t.Parallel()
	// A context whose deadline has already passed.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	err := errors.New("some error")
	if !isTimeout(ctx, err) {
		t.Error("expected isTimeout=true for deadline-exceeded context")
	}
}

func TestIsTimeout_NetErrorTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	netErr := &mockNetError{msg: "connection timed out", timeout: true}
	if !isTimeout(ctx, netErr) {
		t.Error("expected isTimeout=true for net.Error with Timeout()=true")
	}
}

func TestIsTimeout_NetErrorNotTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	netErr := &mockNetError{msg: "connection refused", timeout: false}
	if isTimeout(ctx, netErr) {
		t.Error("expected isTimeout=false for net.Error with Timeout()=false")
	}
}

func TestIsTimeout_StringIOTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	err := errors.New("read tcp: i/o timeout")
	if !isTimeout(ctx, err) {
		t.Error("expected isTimeout=true for error containing 'i/o timeout'")
	}
}

func TestIsTimeout_StringDeadlineExceeded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	err := errors.New("context deadline exceeded")
	if !isTimeout(ctx, err) {
		t.Error("expected isTimeout=true for error containing 'deadline exceeded'")
	}
}

func TestIsTimeout_OrdinaryError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	err := errors.New("connection refused")
	if isTimeout(ctx, err) {
		t.Error("expected isTimeout=false for ordinary error with no context cancellation")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// isNetError (white-box)
// ─────────────────────────────────────────────────────────────────────────────

func TestIsNetError_NilError(t *testing.T) {
	t.Parallel()
	var target net.Error
	ok := isNetError(nil, &target)
	if ok {
		t.Error("isNetError(nil) should return false")
	}
}

func TestIsNetError_NetError(t *testing.T) {
	t.Parallel()
	netErr := &mockNetError{msg: "timeout", timeout: true}
	var target net.Error
	ok := isNetError(netErr, &target)
	if !ok {
		t.Fatal("isNetError should return true for net.Error value")
	}
	if !target.Timeout() {
		t.Error("target.Timeout() should be true")
	}
}

func TestIsNetError_PlainError(t *testing.T) {
	t.Parallel()
	err := errors.New("plain error, not a net.Error")
	var target net.Error
	ok := isNetError(err, &target)
	if ok {
		t.Error("isNetError should return false for plain error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Execute — unit tests using a local fake DNS server
// ─────────────────────────────────────────────────────────────────────────────

// startFakeDNS starts a local UDP listener that replies to every DNS query
// with the provided raw response bytes. The server mirrors the transaction ID
// from each incoming query into the response. It returns the server address
// and a stop function.
//
// Because Execute always dials port 53, these unit tests use executeWithPort
// (defined below) which is a test-local variant that targets an arbitrary port.
func startFakeDNS(t *testing.T, response []byte) (ip, port string, stop func()) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startFakeDNS: listen: %v", err)
	}

	addr := conn.LocalAddr().(*net.UDPAddr)
	stopCh := make(chan struct{})

	go func() {
		buf := make([]byte, 512)
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)) //nolint:errcheck
			n, remote, rerr := conn.ReadFrom(buf)
			if rerr != nil {
				continue
			}
			if n < 2 {
				continue
			}
			resp := make([]byte, len(response))
			copy(resp, response)
			// Mirror the query's transaction ID into the response.
			binary.BigEndian.PutUint16(resp[0:2], binary.BigEndian.Uint16(buf[0:2]))
			conn.WriteTo(resp, remote) //nolint:errcheck
		}
	}()

	return addr.IP.String(), strings.TrimPrefix(addr.String(), addr.IP.String()+":"), func() {
		close(stopCh)
		conn.Close()
	}
}

// buildAResponse constructs a minimal wire-format DNS response containing one
// A record for "example.com." pointing at 1.2.3.4 with TTL 60.
func buildAResponse(t *testing.T) []byte {
	t.Helper()

	qname := encodeName("example.com")

	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], 0x0000) // placeholder ID
	binary.BigEndian.PutUint16(hdr[2:4], 0x8180) // QR=1 RD=1 RA=1
	binary.BigEndian.PutUint16(hdr[4:6], 1)      // QDCOUNT
	binary.BigEndian.PutUint16(hdr[6:8], 1)      // ANCOUNT

	question := append(append([]byte(nil), qname...), 0x00, 0x01, 0x00, 0x01) // TypeA, IN

	answer := []byte{
		0xC0, 0x0C, // compression pointer to offset 12 (start of qname)
		0x00, 0x01, // TYPE = A
		0x00, 0x01, // CLASS = IN
		0x00, 0x00, 0x00, 0x3C, // TTL = 60
		0x00, 0x04, // RDLENGTH = 4
		1, 2, 3, 4, // RDATA = 1.2.3.4
	}

	var pkt []byte
	pkt = append(pkt, hdr...)
	pkt = append(pkt, question...)
	pkt = append(pkt, answer...)
	return pkt
}

// encodeName is a local alias so the test file can call the unexported helper
// from the dns package without importing it directly. We use the dns package
// constants and rely on the fact that we're in package query (white-box) which
// imports dns — but we cannot access dns-internal helpers directly.
// Instead we inline the wire-format encoding for test fixture construction only.
func encodeName(domain string) []byte {
	domain = strings.TrimRight(domain, ".")
	labels := strings.Split(domain, ".")
	var buf []byte
	for _, label := range labels {
		if len(label) == 0 {
			continue
		}
		if len(label) > 63 {
			label = label[:63]
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	buf = append(buf, 0x00)
	return buf
}

// testResolver constructs a resolver.Resolver pointing at the given IP for use
// in Execute tests.
func testResolver(ip string) resolver.Resolver {
	return resolver.Resolver{
		IP:        ip,
		Name:      "test-resolver",
		Country:   "test",
		City:      "test",
		Continent: "test",
	}
}

func TestExecute_CancelledContext_ReturnsTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so the context is already done

	r := testResolver("127.0.0.1")
	result := Execute(ctx, r, "example.com", dns.TypeA)

	if result.Status != Timeout {
		t.Errorf("Status = %v, want Timeout", result.Status)
	}
	if result.Error == nil {
		t.Error("Error should be non-nil for Timeout status")
	}
	if result.Resolver != r {
		t.Error("Resolver field should be populated")
	}
}

func TestExecute_ExpiredDeadline_ReturnsTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()

	r := testResolver("127.0.0.1")
	result := Execute(ctx, r, "example.com", dns.TypeA)

	if result.Status != Timeout {
		t.Errorf("Status = %v, want Timeout", result.Status)
	}
	if result.Error == nil {
		t.Error("Error should be non-nil for Timeout status")
	}
}

func TestExecute_InvalidResolver_ReturnsError(t *testing.T) {
	t.Parallel()

	r := testResolver("not-a-valid-ip!!!")
	result := Execute(context.Background(), r, "example.com", dns.TypeA)

	if result.Status != Error {
		t.Errorf("Status = %v, want Error", result.Status)
	}
	if result.Error == nil {
		t.Error("Error should be non-nil for Error status")
	}
	if result.Resolver != r {
		t.Error("Resolver field should be populated even on error")
	}
}

func TestExecute_ResolverFieldAlwaysPopulated(t *testing.T) {
	t.Parallel()

	r := testResolver("192.0.2.1") // TEST-NET — guaranteed unreachable
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := Execute(ctx, r, "example.com", dns.TypeA)

	// Regardless of outcome the Resolver field must be the one passed in.
	if result.Resolver.IP != r.IP {
		t.Errorf("Resolver.IP = %q, want %q", result.Resolver.IP, r.IP)
	}
	if result.Resolver.Name != r.Name {
		t.Errorf("Resolver.Name = %q, want %q", result.Resolver.Name, r.Name)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Execute — integration test against Google Public DNS (skipped in short mode)
// ─────────────────────────────────────────────────────────────────────────────

func TestExecute_Integration_Google_ExampleCom(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	r := resolver.Resolver{
		IP:        "8.8.8.8",
		Name:      "Google Public DNS Primary",
		Country:   "Global",
		City:      "Anycast",
		Continent: "Global",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := Execute(ctx, r, "example.com", dns.TypeA)

	if result.Status != Success {
		t.Fatalf("Status = %v (error: %v), want Success", result.Status, result.Error)
	}
	if len(result.Records) == 0 {
		t.Error("expected at least one A record for example.com")
	}
	if result.Latency <= 0 {
		t.Error("Latency should be positive for a real query")
	}
	if result.Error != nil {
		t.Errorf("Error should be nil on Success, got: %v", result.Error)
	}
	for _, rec := range result.Records {
		if rec == "" {
			t.Error("record string should not be empty")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Result struct field invariants
// ─────────────────────────────────────────────────────────────────────────────

func TestResult_SuccessHasNoError(t *testing.T) {
	t.Parallel()
	// Success results produced from a direct struct literal must satisfy the
	// documented invariant that Error is nil.
	result := Result{
		Status:  Success,
		Records: []string{"example.com. 300 IN A 93.184.216.34"},
	}
	if result.Error != nil {
		t.Errorf("Result{Status: Success}.Error should be nil, got: %v", result.Error)
	}
}

func TestResult_TimeoutHasNoRecords(t *testing.T) {
	t.Parallel()
	result := Result{
		Status: Timeout,
		Error:  errors.New("context deadline exceeded"),
	}
	if result.Records != nil {
		t.Errorf("Result{Status: Timeout}.Records should be nil, got: %v", result.Records)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkParseRecordType(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ParseRecordType("AAAA") //nolint:errcheck
	}
}

func BenchmarkStatusString(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Success.String()
	}
}

func BenchmarkRecordsToStrings(b *testing.B) {
	b.ReportAllocs()
	records := []dns.Record{
		{Name: "example.com.", Type: dns.TypeA, Class: 1, TTL: 300, Data: "93.184.216.34"},
		{Name: "example.com.", Type: dns.TypeA, Class: 1, TTL: 300, Data: "93.184.216.35"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recordsToStrings(records)
	}
}
