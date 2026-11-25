package dns

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// encodeName
// ─────────────────────────────────────────────────────────────────────────────

func TestEncodeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		domain string
		want   []byte
	}{
		{
			name:   "simple two-label domain",
			domain: "example.com",
			want:   []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0},
		},
		{
			name:   "trailing dot stripped",
			domain: "example.com.",
			want:   []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0},
		},
		{
			name:   "single label TLD",
			domain: "localhost",
			want:   []byte{9, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't', 0},
		},
		{
			name:   "three-label domain",
			domain: "mail.example.com",
			want:   []byte{4, 'm', 'a', 'i', 'l', 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0},
		},
		{
			name:   "root",
			domain: ".",
			want:   []byte{0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := encodeName(tc.domain)
			if len(got) != len(tc.want) {
				t.Fatalf("encodeName(%q) length = %d, want %d\ngot:  %v\nwant: %v",
					tc.domain, len(got), len(tc.want), got, tc.want)
			}
			for i, b := range got {
				if b != tc.want[i] {
					t.Fatalf("encodeName(%q)[%d] = %d, want %d", tc.domain, i, b, tc.want[i])
				}
			}
		})
	}
}

func TestEncodeNameTerminatedByZero(t *testing.T) {
	t.Parallel()
	encoded := encodeName("example.com")
	if encoded[len(encoded)-1] != 0x00 {
		t.Errorf("encodeName: last byte should be 0x00, got 0x%02x", encoded[len(encoded)-1])
	}
}

func TestEncodeNameLabelTruncation(t *testing.T) {
	t.Parallel()
	// A label longer than 63 bytes should be truncated rather than panicking.
	longLabel := strings.Repeat("a", 100)
	encoded := encodeName(longLabel + ".com")
	// First byte should be 63 (the cap).
	if encoded[0] != 63 {
		t.Errorf("expected truncated label length 63, got %d", encoded[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildQuery
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildQuery(t *testing.T) {
	t.Parallel()

	packet, txID := buildQuery("example.com", TypeA)

	// Minimum valid packet: 12-byte header + encoded name + 4-byte question suffix
	// encodeName("example.com") = 13 bytes, question suffix = 4 bytes → total = 29
	if len(packet) < 29 {
		t.Fatalf("packet too short: %d bytes", len(packet))
	}

	// Transaction ID in header matches returned value.
	headerID := binary.BigEndian.Uint16(packet[0:2])
	if headerID != txID {
		t.Errorf("header ID = %d, want %d", headerID, txID)
	}

	// Flags: RD bit set (0x0100).
	flags := binary.BigEndian.Uint16(packet[2:4])
	if flags != 0x0100 {
		t.Errorf("flags = 0x%04x, want 0x0100", flags)
	}

	// QDCOUNT must be 1.
	qdcount := binary.BigEndian.Uint16(packet[4:6])
	if qdcount != 1 {
		t.Errorf("QDCOUNT = %d, want 1", qdcount)
	}

	// ANCOUNT, NSCOUNT, ARCOUNT must be 0.
	for i, name := range []string{"ANCOUNT", "NSCOUNT", "ARCOUNT"} {
		v := binary.BigEndian.Uint16(packet[6+i*2 : 8+i*2])
		if v != 0 {
			t.Errorf("%s = %d, want 0", name, v)
		}
	}
}

func TestBuildQueryQTYPE(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ qtype uint16 }{
		{TypeA}, {TypeAAAA}, {TypeMX}, {TypeNS}, {TypeCNAME}, {TypeTXT}, {TypeSOA}, {TypePTR},
	} {
		t.Run(typeString(tc.qtype), func(t *testing.T) {
			t.Parallel()
			packet, _ := buildQuery("example.com", tc.qtype)
			// QTYPE sits 2 bytes before QCLASS at the end of the question section.
			// The question section starts at offset 12.
			// name = encodeName("example.com") = 13 bytes → QTYPE at offset 25.
			qtypeOffset := 12 + len(encodeName("example.com"))
			got := binary.BigEndian.Uint16(packet[qtypeOffset : qtypeOffset+2])
			if got != tc.qtype {
				t.Errorf("QTYPE = %d, want %d", got, tc.qtype)
			}
			// QCLASS must be 1 (IN).
			qclass := binary.BigEndian.Uint16(packet[qtypeOffset+2 : qtypeOffset+4])
			if qclass != 1 {
				t.Errorf("QCLASS = %d, want 1", qclass)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// decodeName
// ─────────────────────────────────────────────────────────────────────────────

func TestDecodeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		msg        []byte
		offset     int
		wantName   string
		wantOffset int
		wantErr    bool
	}{
		{
			name: "simple uncompressed name",
			// [7]example[3]com[0]
			msg:        []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0},
			offset:     0,
			wantName:   "example.com.",
			wantOffset: 13,
		},
		{
			name: "name starting at non-zero offset",
			// padding + [3]www[7]example[3]com[0]
			msg:        []byte{0xff, 0xff, 3, 'w', 'w', 'w', 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0},
			offset:     2,
			wantName:   "www.example.com.",
			wantOffset: 19,
		},
		{
			name: "compression pointer",
			// msg[0..12] = [7]example[3]com[0]
			// msg[13..14] = pointer to offset 0 (0xC0 0x00)
			msg: func() []byte {
				b := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
				b = append(b, 0xC0, 0x00) // pointer to offset 0
				return b
			}(),
			offset:     13,
			wantName:   "example.com.",
			wantOffset: 15, // offset + 2 (pointer field)
		},
		{
			name: "label then pointer",
			// msg[0..12] = [7]example[3]com[0]
			// msg[13..19] = [3]www + pointer to 0
			msg: func() []byte {
				base := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
				part := []byte{3, 'w', 'w', 'w', 0xC0, 0x00}
				return append(base, part...)
			}(),
			offset:     13,
			wantName:   "www.example.com.",
			wantOffset: 19, // 13 + 4 (label) + 2 (pointer)
		},
		{
			name:    "packet too short",
			msg:     []byte{7, 'e'},
			offset:  0,
			wantErr: true,
		},
		{
			name:    "pointer out of range",
			msg:     []byte{0xC0, 0xFF}, // points to offset 255, which is out of range
			offset:  0,
			wantErr: true,
		},
		{
			name:    "overrun at offset",
			msg:     []byte{},
			offset:  5,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			name, offset, err := decodeName(tc.msg, tc.offset)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got name=%q offset=%d", name, offset)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
			if offset != tc.wantOffset {
				t.Errorf("offset = %d, want %d", offset, tc.wantOffset)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RDATA parsers
// ─────────────────────────────────────────────────────────────────────────────

func TestParseA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rdata   []byte
		want    string
		wantErr bool
	}{
		{name: "valid IPv4", rdata: []byte{1, 2, 3, 4}, want: "1.2.3.4"},
		{name: "loopback", rdata: []byte{127, 0, 0, 1}, want: "127.0.0.1"},
		{name: "wrong length", rdata: []byte{1, 2, 3}, wantErr: true},
		{name: "too long", rdata: []byte{1, 2, 3, 4, 5}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseA(tc.rdata)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseA = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseAAAA(t *testing.T) {
	t.Parallel()

	// 2001:db8::1
	rdata := net.ParseIP("2001:db8::1").To16()
	got, err := parseAAAA(rdata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2001:db8::1" {
		t.Errorf("parseAAAA = %q, want %q", got, "2001:db8::1")
	}

	// Wrong length
	_, err = parseAAAA([]byte{1, 2, 3, 4})
	if err == nil {
		t.Error("expected error for 4-byte RDATA")
	}
}

func TestParseTXT(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rdata   []byte
		want    string
		wantErr bool
	}{
		{
			name:  "single string",
			rdata: []byte{5, 'h', 'e', 'l', 'l', 'o'},
			want:  "hello",
		},
		{
			name:  "two strings concatenated",
			rdata: []byte{3, 'f', 'o', 'o', 3, 'b', 'a', 'r'},
			want:  "foobar",
		},
		{
			name:    "string overruns rdata",
			rdata:   []byte{10, 'h', 'i'},
			wantErr: true,
		},
		{
			name:  "empty rdata",
			rdata: []byte{},
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTXT(tc.rdata)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseTXT = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseMX(t *testing.T) {
	t.Parallel()

	// Build a minimal message:
	//   offset 0: [4]mail[7]example[3]com[0]  = 18 bytes
	//   offset 18: RDATA for MX = [0x00 0x0A] (pref=10) + pointer to 0 (0xC0 0x00)
	mailName := []byte{4, 'm', 'a', 'i', 'l', 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
	rdataOffset := len(mailName)
	rdata := []byte{0x00, 0x0A, 0xC0, 0x00} // pref=10, pointer to offset 0

	msg := append(mailName, rdata...)

	got, err := parseMX(msg, rdataOffset, rdata)
	if err != nil {
		t.Fatalf("parseMX error: %v", err)
	}
	if got != "10 mail.example.com." {
		t.Errorf("parseMX = %q, want %q", got, "10 mail.example.com.")
	}

	// Too short
	_, err = parseMX(msg, rdataOffset, []byte{0x00})
	if err == nil {
		t.Error("expected error for 1-byte MX RDATA")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseResponse
// ─────────────────────────────────────────────────────────────────────────────

// buildRawResponseBytes constructs a minimal but valid DNS response packet
// containing a single A record for "example.com." → 93.184.216.34.
// It is a pure function so it can be called from both tests and benchmarks.
func buildRawResponseBytes(txID uint16) []byte {
	// Header (12 bytes)
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], txID)   // ID
	binary.BigEndian.PutUint16(hdr[2:4], 0x8180) // QR=1, RD=1, RA=1
	binary.BigEndian.PutUint16(hdr[4:6], 1)      // QDCOUNT
	binary.BigEndian.PutUint16(hdr[6:8], 1)      // ANCOUNT
	binary.BigEndian.PutUint16(hdr[8:10], 0)
	binary.BigEndian.PutUint16(hdr[10:12], 0)

	// Question: example.com, TypeA, IN
	question := encodeName("example.com")
	question = append(question, 0x00, 0x01) // QTYPE = A
	question = append(question, 0x00, 0x01) // QCLASS = IN

	// Answer: compression pointer to question's name (offset 12), TypeA, IN, TTL=300, RDATA=93.184.216.34
	answer := []byte{
		0xC0, 0x0C, // pointer to offset 12 (start of question name)
		0x00, 0x01, // TYPE = A
		0x00, 0x01, // CLASS = IN
		0x00, 0x00, 0x01, 0x2C, // TTL = 300
		0x00, 0x04, // RDLENGTH = 4
		93, 184, 216, 34, // RDATA = 93.184.216.34
	}

	var pkt []byte
	pkt = append(pkt, hdr...)
	pkt = append(pkt, question...)
	pkt = append(pkt, answer...)
	return pkt
}

// buildRawResponse wraps buildRawResponseBytes for use in tests that have a *testing.T.
func buildRawResponse(t *testing.T, txID uint16) []byte {
	t.Helper()
	return buildRawResponseBytes(txID)
}

func TestParseResponse_ARecord(t *testing.T) {
	t.Parallel()

	pkt := buildRawResponse(t, 0xABCD)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("parseResponse error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	rec := records[0]
	if rec.Type != TypeA {
		t.Errorf("record type = %d, want %d (TypeA)", rec.Type, TypeA)
	}
	if rec.Data != "93.184.216.34" {
		t.Errorf("record data = %q, want %q", rec.Data, "93.184.216.34")
	}
	if rec.TTL != 300 {
		t.Errorf("record TTL = %d, want 300", rec.TTL)
	}
	if rec.Name != "example.com." {
		t.Errorf("record name = %q, want %q", rec.Name, "example.com.")
	}
}

func TestParseResponse_TooShort(t *testing.T) {
	t.Parallel()
	_, err := parseResponse([]byte{0x00, 0x01})
	if err == nil {
		t.Error("expected error for truncated packet")
	}
}

func TestParseResponse_NonZeroRCODE(t *testing.T) {
	t.Parallel()

	// RCODE = 3 (NXDOMAIN), no answers
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], 0x1234)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8183) // QR=1, RA=1, RCODE=3
	// All counts = 0

	records, err := parseResponse(hdr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for NXDOMAIN, got %d", len(records))
	}
}

func TestParseResponse_NoAnswers(t *testing.T) {
	t.Parallel()

	// Valid header, ANCOUNT=0
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], 0x5678)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8180)
	// All counts = 0

	records, err := parseResponse(hdr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if records != nil {
		t.Errorf("expected nil records, got %v", records)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Record.String
// ─────────────────────────────────────────────────────────────────────────────

func TestRecordString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rec  Record
		want string
	}{
		{
			rec:  Record{Name: "example.com.", Type: TypeA, Class: 1, TTL: 300, Data: "93.184.216.34"},
			want: "example.com. 300 IN A 93.184.216.34",
		},
		{
			rec:  Record{Name: "example.com.", Type: TypeAAAA, Class: 1, TTL: 3600, Data: "2001:db8::1"},
			want: "example.com. 3600 IN AAAA 2001:db8::1",
		},
		{
			rec:  Record{Name: "example.com.", Type: TypeMX, Class: 1, TTL: 900, Data: "10 mail.example.com."},
			want: "example.com. 900 IN MX 10 mail.example.com.",
		},
		{
			// Unknown type falls back to TYPE<n> notation.
			rec:  Record{Name: "example.com.", Type: 255, Class: 1, TTL: 60, Data: "0xff"},
			want: "example.com. 60 IN TYPE255 0xff",
		},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got := tc.rec.String()
			if got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// effectiveDeadline
// ─────────────────────────────────────────────────────────────────────────────

func TestEffectiveDeadline_NoContextDeadline(t *testing.T) {
	t.Parallel()
	before := time.Now().Add(defaultQueryTimeout)
	dl := effectiveDeadline(context.Background())
	after := time.Now().Add(defaultQueryTimeout)

	if dl.Before(before) || dl.After(after) {
		t.Errorf("deadline %v not in expected range [%v, %v]", dl, before, after)
	}
}

func TestEffectiveDeadline_ContextDeadlineSooner(t *testing.T) {
	t.Parallel()
	sooner := time.Now().Add(500 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), sooner)
	defer cancel()

	dl := effectiveDeadline(ctx)
	if !dl.Equal(sooner) {
		t.Errorf("expected context deadline %v, got %v", sooner, dl)
	}
}

func TestEffectiveDeadline_ContextDeadlineLater(t *testing.T) {
	t.Parallel()
	later := time.Now().Add(10 * time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), later)
	defer cancel()

	// The default 5s should win over 10 minutes.
	before := time.Now().Add(defaultQueryTimeout)
	dl := effectiveDeadline(ctx)
	after := time.Now().Add(defaultQueryTimeout)

	if dl.Before(before) || dl.After(after) {
		t.Errorf("deadline %v not in default-timeout range [%v, %v]", dl, before, after)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TypeToString / typeString
// ─────────────────────────────────────────────────────────────────────────────

func TestTypeToString(t *testing.T) {
	t.Parallel()

	cases := map[uint16]string{
		TypeA:     "A",
		TypeNS:    "NS",
		TypeCNAME: "CNAME",
		TypeSOA:   "SOA",
		TypePTR:   "PTR",
		TypeMX:    "MX",
		TypeTXT:   "TXT",
		TypeAAAA:  "AAAA",
	}

	for typ, want := range cases {
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			got, ok := TypeToString[typ]
			if !ok {
				t.Fatalf("TypeToString[%d] not found", typ)
			}
			if got != want {
				t.Errorf("TypeToString[%d] = %q, want %q", typ, got, want)
			}
		})
	}
}

func TestTypeStringFallback(t *testing.T) {
	t.Parallel()
	got := typeString(9999)
	if got != "TYPE9999" {
		t.Errorf("typeString(9999) = %q, want %q", got, "TYPE9999")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Query — integration test using a real local UDP server
// ─────────────────────────────────────────────────────────────────────────────

// startFakeDNS spins up a UDP listener on a random local port and registers
// a single handler that responds to ANY query with the supplied raw response
// bytes. It returns the server's IP and port, and a stop function.
func startFakeDNS(t *testing.T, response []byte) (ip string, port string, stop func()) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startFakeDNS: listen: %v", err)
	}

	addr := conn.LocalAddr().(*net.UDPAddr)
	ip = addr.IP.String()
	port = "53" // Query always uses port 53; override target below

	// For integration tests we use a helper that accepts a custom port.
	// The real Query() always dials :53, so this server is accessed via
	// queryWithPort which is a test-local wrapper.
	_ = port

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
			// Echo back the response with the query's transaction ID.
			resp := make([]byte, len(response))
			copy(resp, response)
			binary.BigEndian.PutUint16(resp[0:2], binary.BigEndian.Uint16(buf[0:2]))
			conn.WriteTo(resp, remote) //nolint:errcheck
		}
	}()

	return ip, fmt.Sprintf("%d", addr.Port), func() {
		close(stopCh)
		conn.Close()
	}
}

// queryWithPort is a test-local variant of Query that targets an arbitrary
// port rather than always using 53. This allows integration tests to run
// without requiring root/CAP_NET_BIND_SERVICE.
func queryWithPort(ctx context.Context, resolverIP, port, domain string, qtype uint16) ([]Record, time.Duration, error) {
	packet, _ := buildQuery(domain, qtype)

	deadline := effectiveDeadline(ctx)
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil, 0, fmt.Errorf("dns: context deadline already exceeded")
	}

	target := net.JoinHostPort(resolverIP, port)
	conn, err := net.DialTimeout("udp", target, remaining)
	if err != nil {
		return nil, 0, fmt.Errorf("dns: dial %s: %w", target, err)
	}
	defer conn.Close() //nolint:errcheck

	conn.SetDeadline(deadline) //nolint:errcheck

	start := time.Now()
	conn.Write(packet) //nolint:errcheck

	buf := make([]byte, maxResponseSize)
	n, err := conn.Read(buf)
	latency := time.Since(start)
	if err != nil {
		return nil, latency, fmt.Errorf("dns: read: %w", err)
	}

	records, err := parseResponse(buf[:n])
	if err != nil {
		return nil, latency, fmt.Errorf("dns: parse: %w", err)
	}
	return records, latency, nil
}

func TestQuery_FakeDNS_ARecord(t *testing.T) {
	t.Parallel()

	// Build a response with a single A record for "example.com." → 1.2.3.4.
	// The response uses a real wire-format packet so parseResponse is exercised end-to-end.
	qname := encodeName("example.com")

	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], 0x0000) // ID replaced by server
	binary.BigEndian.PutUint16(hdr[2:4], 0x8180) // QR=1 RD=1 RA=1
	binary.BigEndian.PutUint16(hdr[4:6], 1)      // QDCOUNT
	binary.BigEndian.PutUint16(hdr[6:8], 1)      // ANCOUNT
	// NSCOUNT, ARCOUNT = 0

	question := append(qname, 0x00, 0x01, 0x00, 0x01) // TypeA, IN

	// Answer with compression pointer to offset 12 (start of question name in full packet)
	answer := []byte{
		0xC0, 0x0C, // pointer to offset 12
		0x00, 0x01, // TYPE=A
		0x00, 0x01, // CLASS=IN
		0x00, 0x00, 0x00, 0x3C, // TTL=60
		0x00, 0x04, // RDLENGTH=4
		1, 2, 3, 4, // 1.2.3.4
	}

	var rawResp []byte
	rawResp = append(rawResp, hdr...)
	rawResp = append(rawResp, question...)
	rawResp = append(rawResp, answer...)

	ip, port, stop := startFakeDNS(t, rawResp)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	records, latency, err := queryWithPort(ctx, ip, port, "example.com", TypeA)
	if err != nil {
		t.Fatalf("queryWithPort error: %v", err)
	}
	if latency <= 0 {
		t.Errorf("latency should be positive, got %v", latency)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Data != "1.2.3.4" {
		t.Errorf("record data = %q, want %q", records[0].Data, "1.2.3.4")
	}
	if records[0].TTL != 60 {
		t.Errorf("record TTL = %d, want 60", records[0].TTL)
	}
}

func TestQuery_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, _, err := Query(ctx, "127.0.0.1", "example.com", TypeA)
	if err == nil {
		t.Error("expected error for already-cancelled context")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks
// ─────────────────────────────────────────────────────────────────────────────

func BenchmarkEncodeName(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		encodeName("www.example.com")
	}
}

func BenchmarkBuildQuery(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		buildQuery("www.example.com", TypeA)
	}
}

func BenchmarkDecodeName(b *testing.B) {
	b.ReportAllocs()
	msg := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
	for b.Loop() {
		decodeName(msg, 0) //nolint:errcheck
	}
}

func BenchmarkParseResponse(b *testing.B) {
	b.ReportAllocs()
	pkt := buildRawResponseBytes(0x1234)
	for b.Loop() {
		parseResponse(pkt) //nolint:errcheck
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseRDATA — full branch coverage
// ─────────────────────────────────────────────────────────────────────────────

// buildResponseWithRecord builds a wire-format DNS response that contains
// exactly one answer record. name must already be in wire format (including
// the trailing 0x00 root label). rdataBytes is the raw RDATA for the record.
func buildResponseWithRecord(t *testing.T, rtype uint16, rdataBytes []byte) []byte {
	t.Helper()

	qname := encodeName("example.com")

	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], 0x1234)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8180)
	binary.BigEndian.PutUint16(hdr[4:6], 1) // QDCOUNT
	binary.BigEndian.PutUint16(hdr[6:8], 1) // ANCOUNT

	question := append(append([]byte(nil), qname...), 0x00, byte(rtype), 0x00, 0x01)

	// Answer uses a compression pointer to offset 12 (start of qname).
	ans := []byte{0xC0, 0x0C}
	ans = append(ans, byte(rtype>>8), byte(rtype)) // TYPE
	ans = append(ans, 0x00, 0x01)                  // CLASS = IN
	ans = append(ans, 0x00, 0x00, 0x00, 0x3C)      // TTL = 60
	rdlen := make([]byte, 2)
	binary.BigEndian.PutUint16(rdlen, uint16(len(rdataBytes)))
	ans = append(ans, rdlen...)
	ans = append(ans, rdataBytes...)

	var pkt []byte
	pkt = append(pkt, hdr...)
	pkt = append(pkt, question...)
	pkt = append(pkt, ans...)
	return pkt
}

func TestParseRDATA_AAAA(t *testing.T) {
	t.Parallel()
	ip := net.ParseIP("2001:db8::1").To16()
	pkt := buildResponseWithRecord(t, TypeAAAA, ip)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Data != "2001:db8::1" {
		t.Errorf("data = %q, want %q", records[0].Data, "2001:db8::1")
	}
}

func TestParseRDATA_CNAME(t *testing.T) {
	t.Parallel()
	// CNAME RDATA is an encoded name: "target.example.com."
	rdata := encodeName("target.example.com")
	pkt := buildResponseWithRecord(t, TypeCNAME, rdata)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Data != "target.example.com." {
		t.Errorf("data = %q, want %q", records[0].Data, "target.example.com.")
	}
}

func TestParseRDATA_NS(t *testing.T) {
	t.Parallel()
	rdata := encodeName("ns1.example.com")
	pkt := buildResponseWithRecord(t, TypeNS, rdata)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Data != "ns1.example.com." {
		t.Errorf("data = %q, want %q", records[0].Data, "ns1.example.com.")
	}
}

func TestParseRDATA_PTR(t *testing.T) {
	t.Parallel()
	rdata := encodeName("host.example.com")
	pkt := buildResponseWithRecord(t, TypePTR, rdata)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Data != "host.example.com." {
		t.Errorf("data = %q, want %q", records[0].Data, "host.example.com.")
	}
}

func TestParseRDATA_TXT(t *testing.T) {
	t.Parallel()
	rdata := []byte{5, 'h', 'e', 'l', 'l', 'o'}
	pkt := buildResponseWithRecord(t, TypeTXT, rdata)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Data != "hello" {
		t.Errorf("data = %q, want %q", records[0].Data, "hello")
	}
}

func TestParseRDATA_MX_ViaResponse(t *testing.T) {
	t.Parallel()
	// RDATA: pref=20, name=mail.example.com. (uncompressed for simplicity)
	name := encodeName("mail.example.com")
	rdata := append([]byte{0x00, 0x14}, name...) // pref=20
	pkt := buildResponseWithRecord(t, TypeMX, rdata)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Data != "20 mail.example.com." {
		t.Errorf("data = %q, want %q", records[0].Data, "20 mail.example.com.")
	}
}

func TestParseRDATA_SOA(t *testing.T) {
	t.Parallel()
	// Build SOA RDATA:
	//   MNAME = ns1.example.com.
	//   RNAME = admin.example.com.
	//   serial=2024010101, refresh=3600, retry=900, expire=604800, minimum=300
	mname := encodeName("ns1.example.com")
	rname := encodeName("admin.example.com")
	fixed := make([]byte, 20)
	binary.BigEndian.PutUint32(fixed[0:4], 2024010101)
	binary.BigEndian.PutUint32(fixed[4:8], 3600)
	binary.BigEndian.PutUint32(fixed[8:12], 900)
	binary.BigEndian.PutUint32(fixed[12:16], 604800)
	binary.BigEndian.PutUint32(fixed[16:20], 300)

	var rdata []byte
	rdata = append(rdata, mname...)
	rdata = append(rdata, rname...)
	rdata = append(rdata, fixed...)

	pkt := buildResponseWithRecord(t, TypeSOA, rdata)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	want := "ns1.example.com. admin.example.com. 2024010101 3600 900 604800 300"
	if records[0].Data != want {
		t.Errorf("data = %q, want %q", records[0].Data, want)
	}
}

func TestParseRDATA_UnknownType_HexFallback(t *testing.T) {
	t.Parallel()
	// Use a type code that has no specific parser so the hex fallback fires.
	const unknownType uint16 = 9999
	rdata := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	pkt := buildResponseWithRecord(t, unknownType, rdata)
	records, err := parseResponse(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Data != "0xdeadbeef" {
		t.Errorf("data = %q, want %q", records[0].Data, "0xdeadbeef")
	}
}

// TestParseRecord_TruncatedFixedFields verifies that parseRecord errors when
// the fixed fields (TYPE/CLASS/TTL/RDLENGTH) are missing.
func TestParseRecord_TruncatedFixedFields(t *testing.T) {
	t.Parallel()
	// Only a name with no following fixed fields.
	msg := encodeName("example.com")
	// decodeName consumes the whole slice; offset will be at the end.
	_, _, err := parseRecord(msg, 0)
	if err == nil {
		t.Error("expected error for truncated record fixed fields")
	}
}

// TestParseRecord_TruncatedRDATA verifies that parseRecord errors when the
// RDLENGTH claims more bytes than remain in the packet.
func TestParseRecord_TruncatedRDATA(t *testing.T) {
	t.Parallel()
	// Name: [0] (root only, 1 byte). TYPE=A, CLASS=IN, TTL=0, RDLENGTH=100 (way too large).
	var msg []byte
	msg = append(msg, 0x00)                   // root label
	msg = append(msg, 0x00, 0x01)             // TYPE=A
	msg = append(msg, 0x00, 0x01)             // CLASS=IN
	msg = append(msg, 0x00, 0x00, 0x00, 0x00) // TTL=0
	msg = append(msg, 0x00, 0x64)             // RDLENGTH=100
	msg = append(msg, 0x01, 0x02)             // only 2 bytes of RDATA

	_, _, err := parseRecord(msg, 0)
	if err == nil {
		t.Error("expected error for truncated RDATA")
	}
}

// TestParseResponse_TruncatedQuestion verifies that parseResponse errors when
// the question section is malformed.
func TestParseResponse_TruncatedQuestion(t *testing.T) {
	t.Parallel()

	// Header says QDCOUNT=1 but provides only a partial name.
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[4:6], 1) // QDCOUNT=1
	// Append an incomplete name (no terminating 0x00).
	pkt := append(hdr, 0x07, 'e', 'x')

	_, err := parseResponse(pkt)
	if err == nil {
		t.Error("expected error for truncated question section")
	}
}

// TestQuery_DialError verifies that Query surfaces a dial error when the
// resolver IP is syntactically invalid (not a valid IP address string).
// net.DialTimeout rejects the address before any I/O is attempted, so this
// path is deterministic and does not require a network call.
func TestQuery_DialError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, _, err := Query(ctx, "not-a-valid-ip!!!", "example.com", TypeA)
	if err == nil {
		t.Error("expected dial error for invalid resolver IP")
	}
	if !strings.Contains(err.Error(), "dns: dial") {
		t.Errorf("error should mention dial: %v", err)
	}
}

// TestQuery_AlreadyExpiredContext verifies that Query returns an error before
// dialing when the context deadline has already passed.
func TestQuery_AlreadyExpiredContext(t *testing.T) {
	t.Parallel()
	// Create a context with a deadline in the distant past.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Hour))
	defer cancel()
	_, _, err := Query(ctx, "127.0.0.1", "example.com", TypeA)
	if err == nil {
		t.Error("expected error for already-expired context deadline")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Additional parse error paths
// ─────────────────────────────────────────────────────────────────────────────

// TestParseSOA_TruncatedFixed verifies that parseSOA errors when the packet
// is too short to contain the five 32-bit fixed fields after RNAME.
func TestParseSOA_TruncatedFixed(t *testing.T) {
	t.Parallel()
	mname := encodeName("ns1.example.com")
	rname := encodeName("admin.example.com")
	// Only provide 4 bytes of the required 20 fixed bytes.
	fixed := []byte{0x00, 0x00, 0x00, 0x01}

	var rdata []byte
	rdata = append(rdata, mname...)
	rdata = append(rdata, rname...)
	rdata = append(rdata, fixed...)

	// Construct a fake full message: just the RDATA block at offset 0.
	_, err := parseSOA(rdata, 0, rdata)
	if err == nil {
		t.Error("expected error for truncated SOA fixed fields")
	}
}

// TestParseMX_TruncatedName verifies that parseMX errors when the name
// portion of the RDATA cannot be decoded (e.g. a label overruns the packet).
func TestParseMX_TruncatedName(t *testing.T) {
	t.Parallel()
	// pref = 10, followed by a label of length 50 but only 2 bytes of data.
	rdata := []byte{0x00, 0x0A, 50, 'a', 'b'}
	_, err := parseMX(rdata, 0, rdata)
	if err == nil {
		t.Error("expected error for truncated MX name")
	}
}

// TestDecodeName_PointerFieldTruncated verifies that decodeName returns an
// error when a compression pointer starts at the last byte of the message
// (i.e. the second byte of the 2-byte pointer field is missing).
func TestDecodeName_PointerFieldTruncated(t *testing.T) {
	t.Parallel()
	// 0xC0 alone — the high two bits signal a pointer but there is no second byte.
	msg := []byte{0xC0}
	_, _, err := decodeName(msg, 0)
	if err == nil {
		t.Error("expected error for truncated compression pointer field")
	}
}

// TestParseResponse_QuestionTruncatedAfterName verifies that parseResponse
// returns an error when the question section has a valid name but no room
// for the 4-byte QTYPE+QCLASS suffix.
func TestParseResponse_QuestionTruncatedAfterName(t *testing.T) {
	t.Parallel()

	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[4:6], 1) // QDCOUNT=1

	// Name is valid (root label only), but nothing follows.
	pkt := append(hdr, 0x00) // just the root label; QTYPE+QCLASS missing

	_, err := parseResponse(pkt)
	if err == nil {
		t.Error("expected error for truncated question QTYPE+QCLASS")
	}
}

// TestParseResponse_AnswerRecordError verifies that parseResponse propagates
// errors from parsing a malformed answer record.
func TestParseResponse_AnswerRecordError(t *testing.T) {
	t.Parallel()

	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], 0x5678)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8180)
	binary.BigEndian.PutUint16(hdr[4:6], 0) // QDCOUNT=0
	binary.BigEndian.PutUint16(hdr[6:8], 1) // ANCOUNT=1

	// Answer record: valid root label then truncated fixed fields (only 2 bytes).
	pkt := append(hdr, 0x00, 0x00, 0x01) // root + 2 bytes (need 10)

	_, err := parseResponse(pkt)
	if err == nil {
		t.Error("expected error for truncated answer record")
	}
}

// TestParseRecord_BadName verifies that parseRecord returns an error when the
// owner name cannot be decoded.
func TestParseRecord_BadName(t *testing.T) {
	t.Parallel()
	// A label of length 10 but only 3 bytes of data — decodeName should fail.
	msg := []byte{10, 'a', 'b', 'c'}
	_, _, err := parseRecord(msg, 0)
	if err == nil {
		t.Error("expected error for truncated record name")
	}
}

// TestParseRDATA_CNAME_InvalidName ensures that parseRDATA propagates a name
// decode error for CNAME/NS/PTR when the embedded name is malformed.
func TestParseRDATA_CNAME_InvalidName(t *testing.T) {
	t.Parallel()
	// A label of length 50 but only 2 bytes of data.
	msg := []byte{50, 'a', 'b'}
	_, err := parseRDATA(msg, 0, TypeCNAME, msg)
	if err == nil {
		t.Error("expected error for malformed CNAME name")
	}
}

// TestParseRecord_RDATAErrorFallback exercises the non-fatal hex fallback
// inside parseRecord when parseRDATA returns an error for a known type.
// We craft a TypeA record whose RDATA is only 2 bytes (parseA requires 4),
// which causes parseRDATA to return an error and parseRecord to fall back to
// a hex representation rather than propagating the error.
func TestParseRecord_RDATAErrorFallback(t *testing.T) {
	t.Parallel()

	// Build a minimal record: root label, TYPE=A, CLASS=IN, TTL=0, RDLENGTH=2, RDATA=0xAB 0xCD.
	var msg []byte
	msg = append(msg, 0x00)                   // root label (name = ".")
	msg = append(msg, 0x00, 0x01)             // TYPE = A
	msg = append(msg, 0x00, 0x01)             // CLASS = IN
	msg = append(msg, 0x00, 0x00, 0x00, 0x00) // TTL = 0
	msg = append(msg, 0x00, 0x02)             // RDLENGTH = 2 (invalid for TypeA)
	msg = append(msg, 0xAB, 0xCD)             // RDATA (2 bytes)

	rec, _, err := parseRecord(msg, 0)
	if err != nil {
		t.Fatalf("parseRecord should not return an error for degraded RDATA, got: %v", err)
	}
	if rec.Data != "0xabcd" {
		t.Errorf("expected hex fallback data %q, got %q", "0xabcd", rec.Data)
	}
}

// TestParseSOA_TruncatedRNAME verifies that parseSOA returns an error when
// the RNAME field (second name) is truncated.
func TestParseSOA_TruncatedRNAME(t *testing.T) {
	t.Parallel()
	// MNAME is valid, RNAME is a label with length 50 but only 2 bytes of content.
	mname := encodeName("ns1.example.com")
	badRname := []byte{50, 'a', 'b'} // truncated label

	var rdata []byte
	rdata = append(rdata, mname...)
	rdata = append(rdata, badRname...)

	_, err := parseSOA(rdata, 0, rdata)
	if err == nil {
		t.Error("expected error for truncated SOA RNAME")
	}
}

// TestParseSOA_TruncatedMNAME verifies that parseSOA returns an error when
// the MNAME field itself is truncated.
func TestParseSOA_TruncatedMNAME(t *testing.T) {
	t.Parallel()
	// A label of length 50 but only 2 bytes of content — decodeName will fail.
	rdata := []byte{50, 'a', 'b'}
	_, err := parseSOA(rdata, 0, rdata)
	if err == nil {
		t.Error("expected error for truncated SOA MNAME")
	}
}

// TestDecodeName_PointerLoop verifies that decodeName detects and errors on
// a compression pointer loop (pointer that points to itself).
func TestDecodeName_PointerLoop(t *testing.T) {
	t.Parallel()
	// Two-byte packet: 0xC0 0x00 → pointer to offset 0 → infinite loop.
	// decodeName should return an error after maxHops iterations.
	msg := []byte{0xC0, 0x00}
	_, _, err := decodeName(msg, 0)
	if err == nil {
		t.Error("expected error for compression pointer loop")
	}
	if !strings.Contains(err.Error(), "loop") {
		t.Errorf("error message should mention loop: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Query — error path coverage via fakeConn dialer injection
//
// Each test below replaces the package-level dialUDP function with one that
// returns a fakeConn whose behaviour is tuned to exercise a specific branch
// inside Query. The original dialUDP is restored after each test.
// ─────────────────────────────────────────────────────────────────────────────

// fakeConn is a minimal net.Conn whose individual operations can be made to
// succeed or fail on demand. Unused methods satisfy the interface by returning
// nil or zero values.
type fakeConn struct {
	setDeadlineErr error
	writeErr       error
	readResponse   []byte // bytes returned by Read when readErr is nil
	readErr        error
	closeErr       error
}

func (c *fakeConn) SetDeadline(_ time.Time) error      { return c.setDeadlineErr }
func (c *fakeConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(_ time.Time) error { return nil }

func (c *fakeConn) Write(b []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(b), nil
}

func (c *fakeConn) Read(b []byte) (int, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}
	n := copy(b, c.readResponse)
	return n, nil
}

func (c *fakeConn) Close() error         { return c.closeErr }
func (c *fakeConn) LocalAddr() net.Addr  { return &net.UDPAddr{} }
func (c *fakeConn) RemoteAddr() net.Addr { return &net.UDPAddr{} }

// withFakeDialer temporarily replaces dialUDP with a dialer that returns fc,
// and returns a restore function. Must be called in non-parallel tests only.
func withFakeDialer(fc *fakeConn) (restore func()) {
	original := dialUDP
	dialUDP = func(_, _ string, _ time.Duration) (net.Conn, error) {
		return fc, nil
	}
	return func() { dialUDP = original }
}

// TestQuery_SetDeadlineError exercises the path where conn.SetDeadline returns
// an error, causing Query to return before sending any packet.
func TestQuery_SetDeadlineError(t *testing.T) {
	// Not parallel: modifies the package-level dialUDP variable.
	fc := &fakeConn{setDeadlineErr: fmt.Errorf("synthetic SetDeadline failure")}
	restore := withFakeDialer(fc)
	defer restore()

	ctx := context.Background()
	_, _, err := Query(ctx, "127.0.0.1", "example.com", TypeA)
	if err == nil {
		t.Fatal("expected error from SetDeadline failure, got nil")
	}
	if !strings.Contains(err.Error(), "set deadline") {
		t.Errorf("error should mention set deadline: %v", err)
	}
}

// TestQuery_WriteError exercises the path where conn.Write returns an error.
func TestQuery_WriteError(t *testing.T) {
	// Not parallel: modifies the package-level dialUDP variable.
	fc := &fakeConn{writeErr: fmt.Errorf("synthetic Write failure")}
	restore := withFakeDialer(fc)
	defer restore()

	ctx := context.Background()
	_, _, err := Query(ctx, "127.0.0.1", "example.com", TypeA)
	if err == nil {
		t.Fatal("expected error from Write failure, got nil")
	}
	if !strings.Contains(err.Error(), "write query") {
		t.Errorf("error should mention write query: %v", err)
	}
}

// TestQuery_ReadError exercises the path where conn.Read returns an error.
func TestQuery_ReadError(t *testing.T) {
	// Not parallel: modifies the package-level dialUDP variable.
	fc := &fakeConn{readErr: fmt.Errorf("synthetic Read failure")}
	restore := withFakeDialer(fc)
	defer restore()

	ctx := context.Background()
	_, _, err := Query(ctx, "127.0.0.1", "example.com", TypeA)
	if err == nil {
		t.Fatal("expected error from Read failure, got nil")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Errorf("error should mention read response: %v", err)
	}
}

// TestQuery_HappyPath exercises the full success path through Query including
// the response-received log and the final return of parsed records.
func TestQuery_HappyPath(t *testing.T) {
	// Not parallel: modifies the package-level dialUDP variable.

	// Build a valid wire-format response with a single A record.
	rawResp := buildRawResponseBytes(0x1234)
	fc := &fakeConn{readResponse: rawResp}
	restore := withFakeDialer(fc)
	defer restore()

	ctx := context.Background()
	records, latency, err := Query(ctx, "127.0.0.1", "example.com", TypeA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Type != TypeA {
		t.Errorf("record type = %d, want TypeA (%d)", records[0].Type, TypeA)
	}
	if latency < 0 {
		t.Errorf("latency should be non-negative, got %v", latency)
	}
}

// TestQuery_ParseResponseError exercises the path where conn.Read succeeds but
// parseResponse returns an error (packet is too short to be a valid response).
func TestQuery_ParseResponseError(t *testing.T) {
	// Not parallel: modifies the package-level dialUDP variable.

	// Four bytes is a valid Read result but too short for parseResponse (needs 12).
	fc := &fakeConn{readResponse: []byte{0x00, 0x01, 0x02, 0x03}}
	restore := withFakeDialer(fc)
	defer restore()

	ctx := context.Background()
	_, _, err := Query(ctx, "127.0.0.1", "example.com", TypeA)
	if err == nil {
		t.Fatal("expected error from parse failure, got nil")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error should mention parse response: %v", err)
	}
}

// TestQuery_CloseWarning exercises the deferred conn.Close() warning path by
// returning a fakeConn whose Close method returns an error. Query should still
// return normally (the close error is only logged, not returned).
func TestQuery_CloseWarning(t *testing.T) {
	// Not parallel: modifies the package-level dialUDP variable.

	rawResp := buildRawResponseBytes(0xBEEF)
	fc := &fakeConn{
		readResponse: rawResp,
		closeErr:     fmt.Errorf("synthetic Close failure"),
	}
	restore := withFakeDialer(fc)
	defer restore()

	ctx := context.Background()
	// Even though Close returns an error, Query should succeed; the warning is
	// only emitted via slog and does not affect the returned values.
	records, _, err := Query(ctx, "127.0.0.1", "example.com", TypeA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
}
