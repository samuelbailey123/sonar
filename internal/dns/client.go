// Package dns implements a zero-dependency DNS client that constructs
// wire-format queries per RFC 1035 and parses responses using only the
// Go standard library. It supports UDP transport on port 53 and handles
// DNS name compression in responses.
package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"strings"
	"time"
)

// DNS record type constants as defined in RFC 1035 and RFC 3596.
const (
	TypeA     uint16 = 1
	TypeNS    uint16 = 2
	TypeCNAME uint16 = 5
	TypeSOA   uint16 = 6
	TypePTR   uint16 = 12
	TypeMX    uint16 = 15
	TypeTXT   uint16 = 16
	TypeAAAA  uint16 = 28
)

// TypeToString maps DNS type codes to their canonical string names.
var TypeToString = map[uint16]string{
	TypeA:     "A",
	TypeNS:    "NS",
	TypeCNAME: "CNAME",
	TypeSOA:   "SOA",
	TypePTR:   "PTR",
	TypeMX:    "MX",
	TypeTXT:   "TXT",
	TypeAAAA:  "AAAA",
}

// defaultQueryTimeout is applied when the caller's context carries no deadline.
const defaultQueryTimeout = 5 * time.Second

// dialUDP is the function used to open a UDP connection to the resolver. It is
// a package-level variable so tests can substitute a fake dialer without
// modifying exported API surface.
var dialUDP = func(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}

// maxResponseSize is the read buffer size for UDP responses. RFC 1035 §2.3.4
// sets the theoretical UDP limit at 512 bytes, but EDNS0 extensions can
// produce larger payloads; 4096 bytes is a safe practical ceiling.
const maxResponseSize = 4096

// Record represents a single parsed DNS answer record.
type Record struct {
	// Name is the owner name of the resource record.
	Name string

	// Type is the numeric DNS record type (e.g. TypeA, TypeMX).
	Type uint16

	// Class is the DNS class (1 = IN for internet).
	Class uint16

	// TTL is the time-to-live in seconds as reported by the resolver.
	TTL uint32

	// Data is a human-readable representation of the RDATA field.
	Data string
}

// String returns the record in standard DNS presentation format:
//
//	"name TTL IN TYPE data"
func (r Record) String() string {
	typeName, ok := TypeToString[r.Type]
	if !ok {
		typeName = fmt.Sprintf("TYPE%d", r.Type)
	}
	return fmt.Sprintf("%s %d IN %s %s", r.Name, r.TTL, typeName, r.Data)
}

// Query sends a single DNS query over UDP to resolverIP:53 for the given
// domain and record type. It returns the parsed answer records and the
// measured round-trip latency.
//
// The function respects context cancellation. When the context carries a
// deadline, that deadline is used; otherwise a 5-second timeout is applied.
//
// resolverIP must be a bare IP address without a port.
func Query(ctx context.Context, resolverIP string, domain string, qtype uint16) ([]Record, time.Duration, error) {
	log := slog.With(
		slog.String("resolver_ip", resolverIP),
		slog.String("domain", domain),
		slog.String("record_type", typeString(qtype)),
	)

	log.Debug("dns: query starting")

	packet, txID := buildQuery(domain, qtype)

	log.Debug("dns: query packet built",
		slog.Uint64("tx_id", uint64(txID)),
		slog.Int("packet_bytes", len(packet)),
	)

	deadline := effectiveDeadline(ctx)
	remaining := time.Until(deadline)
	if remaining <= 0 {
		log.Warn("dns: context already expired before query", slog.Time("deadline", deadline))
		return nil, 0, fmt.Errorf("dns: context deadline already exceeded")
	}

	target := net.JoinHostPort(resolverIP, "53")

	log.Debug("dns: dialing resolver", slog.String("target", target), slog.Duration("timeout", remaining))

	conn, err := dialUDP("udp", target, remaining)
	if err != nil {
		log.Error("dns: dial failed", slog.String("target", target), slog.String("error", err.Error()))
		return nil, 0, fmt.Errorf("dns: dial %s: %w", target, err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			log.Warn("dns: connection close failed", slog.String("error", cerr.Error()))
		}
	}()

	if err := conn.SetDeadline(deadline); err != nil {
		log.Error("dns: set deadline failed", slog.String("error", err.Error()))
		return nil, 0, fmt.Errorf("dns: set deadline: %w", err)
	}

	start := time.Now()

	if _, err := conn.Write(packet); err != nil {
		latency := time.Since(start)
		log.Error("dns: write failed", slog.String("error", err.Error()), slog.Duration("latency", latency))
		return nil, latency, fmt.Errorf("dns: write query: %w", err)
	}

	log.Debug("dns: query packet sent, awaiting response")

	buf := make([]byte, maxResponseSize)
	n, err := conn.Read(buf)
	latency := time.Since(start)

	if err != nil {
		log.Error("dns: read failed",
			slog.String("error", err.Error()),
			slog.Duration("latency", latency),
		)
		return nil, latency, fmt.Errorf("dns: read response: %w", err)
	}

	log.Debug("dns: response received",
		slog.Int("bytes", n),
		slog.Duration("latency", latency),
	)

	response := buf[:n]

	records, err := parseResponse(response)
	var failure rcodeError
	if errors.As(err, &failure) {
		log.Warn("dns: server answered with a failure code",
			slog.String("error", failure.Error()),
			slog.Duration("latency", latency),
		)
		return nil, latency, failure
	}
	if err != nil {
		log.Error("dns: response parse failed", slog.String("error", err.Error()))
		return nil, latency, fmt.Errorf("dns: parse response: %w", err)
	}

	log.Info("dns: query completed",
		slog.Int("answer_count", len(records)),
		slog.Duration("latency", latency),
	)

	return records, latency, nil
}

// buildQuery constructs a DNS wire-format query packet for the given domain
// and record type. It returns the packet bytes and the randomly chosen
// transaction ID so the caller can validate the response if needed.
//
// The header sets the RD (recursion desired) flag. QDCOUNT is 1. The question
// section contains the encoded domain name, QTYPE, and QCLASS=1 (IN).
func buildQuery(domain string, qtype uint16) ([]byte, uint16) {
	txID := uint16(rand.N(65536)) //nolint:gosec // transaction ID does not need cryptographic randomness

	slog.Debug("dns: building query",
		slog.Uint64("tx_id", uint64(txID)),
		slog.String("domain", domain),
		slog.String("qtype", typeString(qtype)),
	)

	// DNS header: 12 bytes
	// +--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
	// |                      ID                       |
	// |QR|   Opcode  |AA|TC|RD|RA|   Z    |   RCODE   |
	// |                    QDCOUNT                    |
	// |                    ANCOUNT                    |
	// |                    NSCOUNT                    |
	// |                    ARCOUNT                    |
	// +--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], txID)   // ID
	binary.BigEndian.PutUint16(header[2:4], 0x0100) // Flags: RD=1
	binary.BigEndian.PutUint16(header[4:6], 1)      // QDCOUNT
	binary.BigEndian.PutUint16(header[6:8], 0)      // ANCOUNT
	binary.BigEndian.PutUint16(header[8:10], 0)     // NSCOUNT
	binary.BigEndian.PutUint16(header[10:12], 0)    // ARCOUNT

	// Question section: encoded name + QTYPE (2 bytes) + QCLASS (2 bytes)
	question := encodeName(domain)
	question = append(question, 0, 0) // QTYPE placeholder
	question = append(question, 0, 1) // QCLASS = IN (1)
	binary.BigEndian.PutUint16(question[len(question)-4:len(question)-2], qtype)

	packet := append(header, question...)

	slog.Debug("dns: query packet ready",
		slog.Int("total_bytes", len(packet)),
		slog.Int("name_bytes", len(encodeName(domain))),
	)

	return packet, txID
}

// encodeName converts a domain name into the DNS wire-format label sequence
// defined in RFC 1035 §3.1. Each label is preceded by a one-byte length.
// The sequence is terminated with a zero-length label (0x00).
//
// A trailing dot (FQDN notation) is stripped before encoding.
//
// Example: "example.com" → [7]example[3]com[0]
func encodeName(domain string) []byte {
	// Strip trailing dot so "example.com." and "example.com" are equivalent.
	domain = strings.TrimRight(domain, ".")

	labels := strings.Split(domain, ".")

	// Pre-calculate capacity: sum of (1 + len(label)) for each label, plus 1
	// for the terminal zero byte.
	capacity := 1
	for _, l := range labels {
		capacity += 1 + len(l)
	}

	buf := make([]byte, 0, capacity)

	for _, label := range labels {
		if len(label) == 0 {
			continue
		}
		if len(label) > 63 {
			// RFC 1035 §2.3.4: labels must be 63 octets or less. Truncate
			// rather than panic so callers receive a gracefully degraded result.
			slog.Warn("dns: label exceeds 63 bytes, truncating",
				slog.String("label", label),
				slog.Int("length", len(label)),
			)
			label = label[:63]
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}

	buf = append(buf, 0x00) // root label terminator

	return buf
}

const (
	rcodeNoError  uint16 = 0
	rcodeNXDomain uint16 = 3
)

var rcodeNames = map[uint16]string{
	1: "FORMERR",
	2: "SERVFAIL",
	4: "NOTIMP",
	5: "REFUSED",
}

type rcodeError uint16

func (e rcodeError) Error() string {
	name, ok := rcodeNames[uint16(e)]
	if !ok {
		name = fmt.Sprintf("RCODE%d", uint16(e))
	}
	return "dns: " + name
}

func parseResponse(data []byte) ([]Record, error) {
	slog.Debug("dns: parsing response", slog.Int("bytes", len(data)))

	if len(data) < 12 {
		return nil, fmt.Errorf("dns: response too short: %d bytes (minimum 12)", len(data))
	}

	flags := binary.BigEndian.Uint16(data[2:4])
	qdcount := binary.BigEndian.Uint16(data[4:6])
	ancount := binary.BigEndian.Uint16(data[6:8])

	rcode := flags & 0x000F
	if rcode == rcodeNXDomain {
		slog.Debug("dns: server returned NXDOMAIN", slog.Uint64("flags", uint64(flags)))
		return nil, nil
	}
	if rcode != rcodeNoError {
		return nil, rcodeError(rcode)
	}

	slog.Debug("dns: response header parsed",
		slog.Uint64("flags", uint64(flags)),
		slog.Uint64("qdcount", uint64(qdcount)),
		slog.Uint64("ancount", uint64(ancount)),
	)

	offset := 12 // start of question section

	// Skip all question records.
	for i := range int(qdcount) {
		var err error
		_, offset, err = decodeName(data, offset)
		if err != nil {
			return nil, fmt.Errorf("dns: skip question name (index %d): %w", i, err)
		}
		// QTYPE (2) + QCLASS (2)
		offset += 4
		if offset > len(data) {
			return nil, fmt.Errorf("dns: truncated packet skipping question %d", i)
		}
	}

	slog.Debug("dns: question section skipped", slog.Int("offset", offset))

	if ancount == 0 {
		slog.Debug("dns: no answer records in response")
		return nil, nil
	}

	records := make([]Record, 0, int(ancount))

	for i := range int(ancount) {
		rec, newOffset, err := parseRecord(data, offset)
		if err != nil {
			return nil, fmt.Errorf("dns: parse answer record %d: %w", i, err)
		}
		offset = newOffset

		slog.Debug("dns: answer record parsed",
			slog.Int("index", i),
			slog.String("name", rec.Name),
			slog.String("type", typeString(rec.Type)),
			slog.String("data", rec.Data),
			slog.Uint64("ttl", uint64(rec.TTL)),
		)

		records = append(records, rec)
	}

	slog.Info("dns: response parsed",
		slog.Int("answer_count", len(records)),
	)

	return records, nil
}

// parseRecord reads a single resource record starting at offset within data.
// It returns the populated Record and the next offset after the record.
//
// Wire format (RFC 1035 §3.2.1):
//
//	NAME     variable  compressed domain name
//	TYPE     2 bytes
//	CLASS    2 bytes
//	TTL      4 bytes
//	RDLENGTH 2 bytes
//	RDATA    RDLENGTH bytes
func parseRecord(data []byte, offset int) (Record, int, error) {
	name, offset, err := decodeName(data, offset)
	if err != nil {
		return Record{}, 0, fmt.Errorf("parse record name: %w", err)
	}

	// TYPE + CLASS + TTL + RDLENGTH = 10 bytes minimum
	if offset+10 > len(data) {
		return Record{}, 0, fmt.Errorf("truncated record fixed fields at offset %d", offset)
	}

	rtype := binary.BigEndian.Uint16(data[offset : offset+2])
	rclass := binary.BigEndian.Uint16(data[offset+2 : offset+4])
	ttl := binary.BigEndian.Uint32(data[offset+4 : offset+8])
	rdlen := int(binary.BigEndian.Uint16(data[offset+8 : offset+10]))
	offset += 10

	if offset+rdlen > len(data) {
		return Record{}, 0, fmt.Errorf("truncated RDATA: need %d bytes at offset %d, have %d",
			rdlen, offset, len(data)-offset)
	}

	rdata := data[offset : offset+rdlen]

	rdataStr, err := parseRDATA(data, offset, rtype, rdata)
	if err != nil {
		// Non-fatal: fall back to hex representation so the caller still
		// receives a record rather than an error for unknown types.
		slog.Warn("dns: RDATA parse failed, using hex fallback",
			slog.String("type", typeString(rtype)),
			slog.String("error", err.Error()),
		)
		rdataStr = fmt.Sprintf("0x%x", rdata)
	}

	rec := Record{
		Name:  name,
		Type:  rtype,
		Class: rclass,
		TTL:   ttl,
		Data:  rdataStr,
	}

	return rec, offset + rdlen, nil
}

// parseRDATA converts the raw RDATA bytes for a resource record into a
// human-readable string. The full message buffer (msg) is required for
// decoding compressed names embedded in RDATA (CNAME, NS, MX, PTR, SOA).
//
// rdataOffset is the absolute byte offset of rdata within msg.
func parseRDATA(msg []byte, rdataOffset int, rtype uint16, rdata []byte) (string, error) {
	switch rtype {
	case TypeA:
		return parseA(rdata)

	case TypeAAAA:
		return parseAAAA(rdata)

	case TypeCNAME, TypeNS, TypePTR:
		name, _, err := decodeName(msg, rdataOffset)
		if err != nil {
			return "", fmt.Errorf("decode %s name: %w", typeString(rtype), err)
		}
		return name, nil

	case TypeMX:
		return parseMX(msg, rdataOffset, rdata)

	case TypeTXT:
		return parseTXT(rdata)

	case TypeSOA:
		return parseSOA(msg, rdataOffset, rdata)

	default:
		slog.Debug("dns: unknown RDATA type, using hex",
			slog.Uint64("type", uint64(rtype)),
			slog.Int("rdlen", len(rdata)),
		)
		return fmt.Sprintf("0x%x", rdata), nil
	}
}

// parseA parses a 4-byte IPv4 address from RDATA.
func parseA(rdata []byte) (string, error) {
	if len(rdata) != 4 {
		return "", fmt.Errorf("TypeA RDATA must be 4 bytes, got %d", len(rdata))
	}
	ip := net.IP(rdata)
	return ip.String(), nil
}

// parseAAAA parses a 16-byte IPv6 address from RDATA.
func parseAAAA(rdata []byte) (string, error) {
	if len(rdata) != 16 {
		return "", fmt.Errorf("TypeAAAA RDATA must be 16 bytes, got %d", len(rdata))
	}
	ip := net.IP(rdata)
	return ip.String(), nil
}

// parseMX parses the MX RDATA: a 2-byte preference value followed by a
// compressed domain name. The result is formatted as "PREF NAME.".
func parseMX(msg []byte, rdataOffset int, rdata []byte) (string, error) {
	if len(rdata) < 3 {
		return "", fmt.Errorf("TypeMX RDATA too short: %d bytes", len(rdata))
	}
	pref := binary.BigEndian.Uint16(rdata[0:2])
	// The name starts 2 bytes into the RDATA, but is resolved against the
	// full message for compression pointer support.
	name, _, err := decodeName(msg, rdataOffset+2)
	if err != nil {
		return "", fmt.Errorf("decode MX exchange name: %w", err)
	}
	return fmt.Sprintf("%d %s", pref, name), nil
}

// parseTXT concatenates all TXT strings. Each string is preceded by a single
// length byte per RFC 1035 §3.3.14.
func parseTXT(rdata []byte) (string, error) {
	var sb strings.Builder
	i := 0
	for i < len(rdata) {
		strLen := int(rdata[i])
		i++
		if i+strLen > len(rdata) {
			return "", fmt.Errorf("TypeTXT string overruns RDATA at offset %d", i-1)
		}
		sb.Write(rdata[i : i+strLen])
		i += strLen
	}
	return sb.String(), nil
}

// parseSOA parses the SOA RDATA fields and returns them in the canonical
// presentation format:
//
//	"MNAME RNAME SERIAL REFRESH RETRY EXPIRE MINIMUM"
func parseSOA(msg []byte, rdataOffset int, rdata []byte) (string, error) {
	// MNAME: primary nameserver
	mname, moffset, err := decodeName(msg, rdataOffset)
	if err != nil {
		return "", fmt.Errorf("decode SOA MNAME: %w", err)
	}

	// RNAME: responsible mailbox
	rname, roffset, err := decodeName(msg, moffset)
	if err != nil {
		return "", fmt.Errorf("decode SOA RNAME: %w", err)
	}

	// The five 32-bit integers begin at roffset within msg. Validate against
	// the rdata slice end to guard against truncation.
	rdataEnd := rdataOffset + len(rdata)
	if roffset+20 > rdataEnd {
		return "", fmt.Errorf("SOA RDATA too short for fixed fields: need offset %d+20, rdata ends at %d",
			roffset, rdataEnd)
	}

	serial := binary.BigEndian.Uint32(msg[roffset : roffset+4])
	refresh := binary.BigEndian.Uint32(msg[roffset+4 : roffset+8])
	retry := binary.BigEndian.Uint32(msg[roffset+8 : roffset+12])
	expire := binary.BigEndian.Uint32(msg[roffset+12 : roffset+16])
	minimum := binary.BigEndian.Uint32(msg[roffset+16 : roffset+20])

	return fmt.Sprintf("%s %s %d %d %d %d %d",
		mname, rname, serial, refresh, retry, expire, minimum), nil
}

// decodeName reads a DNS domain name from msg starting at offset. It follows
// compression pointers as defined in RFC 1035 §4.1.4 and returns the
// fully-qualified domain name with a trailing dot.
//
// It returns the decoded name and the offset immediately after the name in
// the original (non-pointer) stream — i.e. the position the caller should
// continue reading from.
//
// A pointer loop guard caps traversal at 128 hops to prevent infinite loops
// on malformed packets.
func decodeName(msg []byte, offset int) (string, int, error) {
	const maxHops = 128

	var labels []string
	hops := 0
	// finalOffset records the position after the name in the original stream.
	// It is set the first time we follow a pointer so subsequent jumps do not
	// advance it.
	finalOffset := -1

	for {
		if hops > maxHops {
			return "", 0, fmt.Errorf("dns: compression pointer loop detected after %d hops", maxHops)
		}

		if offset >= len(msg) {
			return "", 0, fmt.Errorf("dns: name decode overran packet at offset %d (len=%d)", offset, len(msg))
		}

		length := int(msg[offset])

		switch {
		case length == 0:
			// Root label — end of name.
			if finalOffset == -1 {
				finalOffset = offset + 1
			}
			// Assemble the FQDN with a trailing dot to match presentation format.
			name := strings.Join(labels, ".") + "."
			return name, finalOffset, nil

		case length&0xC0 == 0xC0:
			// Compression pointer: the high two bits are set. The remaining 14
			// bits (across this byte and the next) encode the absolute offset.
			if offset+1 >= len(msg) {
				return "", 0, fmt.Errorf("dns: compression pointer overruns packet at offset %d", offset)
			}
			ptr := int(binary.BigEndian.Uint16(msg[offset:offset+2]) & 0x3FFF)
			if finalOffset == -1 {
				// Record where we resume after the pointer field (2 bytes).
				finalOffset = offset + 2
			}
			if ptr >= len(msg) {
				return "", 0, fmt.Errorf("dns: compression pointer %d out of range (len=%d)", ptr, len(msg))
			}
			offset = ptr
			hops++

		default:
			// Regular label: length byte followed by that many octets.
			start := offset + 1
			end := start + length
			if end > len(msg) {
				return "", 0, fmt.Errorf("dns: label overruns packet: offset=%d length=%d msglen=%d",
					offset, length, len(msg))
			}
			labels = append(labels, string(msg[start:end]))
			offset = end
		}
	}
}

// effectiveDeadline returns the deadline to apply to the DNS exchange. It
// uses the caller's context deadline when one is present and sooner than the
// default timeout; otherwise it falls back to now+defaultQueryTimeout.
func effectiveDeadline(ctx context.Context) time.Time {
	fallback := time.Now().Add(defaultQueryTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(fallback) {
		return ctxDeadline
	}
	return fallback
}

// typeString returns the string name for a DNS type code, or a TYPE<n>
// placeholder for unknown values.
func typeString(qtype uint16) string {
	if s, ok := TypeToString[qtype]; ok {
		return s
	}
	return fmt.Sprintf("TYPE%d", qtype)
}
