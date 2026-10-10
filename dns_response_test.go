package main

import (
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/soypat/lneto/dns"
)

func makeDNSQuery(t testing.TB, host string, qtype dns.Type) []byte {
	t.Helper()
	name, err := dns.NewName(host)
	if err != nil {
		t.Fatal(err)
	}
	var request dns.Message
	request.AddQuestions([]dns.Question{{Name: name, Type: qtype, Class: dns.ClassINET}})
	var edns dns.Resource
	edns.SetEDNS0(1232, dns.RCodeSuccess, 0, nil)
	request.Additionals = append(request.Additionals, edns)
	packet, err := request.AppendTo(make([]byte, 0, dns.MaxSizeUDP), 0x1234, dns.NewClientHeaderFlags(dns.OpCodeQuery, true))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestAnswerDNSQuery(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		qtype      dns.Type
		blocked    bool
		lookupAddr netip.Addr
		lookupErr  error
		wantRCode  dns.RCode
		wantAddr   netip.Addr
		wantLookup bool
	}{
		{"unlisted A", "www.example.org", dns.TypeA, false, netip.MustParseAddr("203.0.113.8"), nil, dns.RCodeSuccess, netip.MustParseAddr("203.0.113.8"), true},
		{"blocked A", "ads.example", dns.TypeA, true, netip.Addr{}, nil, dns.RCodeSuccess, netip.IPv4Unspecified(), false},
		{"allowed AAAA", "safe.example", dns.TypeAAAA, false, netip.MustParseAddr("2001:db8::8"), nil, dns.RCodeSuccess, netip.MustParseAddr("2001:db8::8"), true},
		{"blocked AAAA", "ads.example", dns.TypeAAAA, true, netip.Addr{}, nil, dns.RCodeSuccess, netip.IPv6Unspecified(), false},
		{"upstream error", "www.example.org", dns.TypeA, false, netip.Addr{}, errors.New("offline"), dns.RCodeServerFailure, netip.Addr{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			lookup := func(string, dns.Type) ([]netip.Addr, error) {
				calls++
				if tc.lookupErr != nil {
					return nil, tc.lookupErr
				}
				return []netip.Addr{tc.lookupAddr}, nil
			}
			packet := makeDNSQuery(t, tc.host, tc.qtype)
			response, err := answerDNSQueryWithPolicy(packet, make([]byte, dns.MaxSizeUDP), func(string) bool { return tc.blocked }, lookup)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := dns.NewFrame(response)
			if err != nil {
				t.Fatal(err)
			}
			if frame.TxID() != 0x1234 || !frame.Flags().IsResponse() || frame.Flags().ResponseCode() != tc.wantRCode {
				t.Fatalf("unexpected DNS response header: id=%x flags=%s", frame.TxID(), frame.Flags())
			}
			if frame.Flags().IsRecursionDesired() == false || !frame.Flags().IsRecursionAvailable() {
				t.Fatal("response did not preserve recursion flags")
			}
			if got := calls > 0; got != tc.wantLookup {
				t.Fatalf("upstream lookup called=%t, want %t", got, tc.wantLookup)
			}
			var decoded dns.Message
			decoded.LimitResourceDecoding(1, 4, 0, 0)
			_, _, err = decoded.Decode(response)
			if err != nil {
				t.Fatal(err)
			}
			if len(decoded.Questions) != 1 {
				t.Fatalf("got %d echoed questions, want 1", len(decoded.Questions))
			}
			if tc.wantAddr.IsValid() {
				if len(decoded.Answers) != 1 {
					t.Fatalf("got %d answers, want 1", len(decoded.Answers))
				}
				got, ok := netip.AddrFromSlice(decoded.Answers[0].RawData())
				if !ok || got != tc.wantAddr {
					t.Fatalf("answer address = %v, want %v", got, tc.wantAddr)
				}
			} else if len(decoded.Answers) != 0 {
				t.Fatalf("got %d answers, want none", len(decoded.Answers))
			}
		})
	}
}

func TestAnswerDNSQueryRejectsMalformedAndZeroQuestion(t *testing.T) {
	for _, packet := range [][]byte{{1, 2}, make([]byte, dns.SizeHeader)} {
		if _, err := answerDNSQuery(packet, make([]byte, dns.MaxSizeUDP), func(string, dns.Type) ([]netip.Addr, error) {
			return nil, nil
		}); err == nil {
			t.Fatal("malformed query was accepted")
		}
	}
}

func TestAnswerDNSQueryBoundsResponseAndUsesOneAddress(t *testing.T) {
	packet := makeDNSQuery(t, "www.example.org", dns.TypeA)
	response, err := answerDNSQuery(packet, make([]byte, dns.MaxSizeUDP), func(string, dns.Type) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("203.0.113.8"),
			netip.MustParseAddr("203.0.113.9"),
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded dns.Message
	decoded.LimitResourceDecoding(1, 4, 0, 0)
	if _, _, err := decoded.Decode(response); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Answers) != 1 {
		t.Fatalf("got %d answers, want one", len(decoded.Answers))
	}

	longName := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 60)
	packet = makeDNSQuery(t, longName, dns.TypeA)
	response, err = answerDNSQuery(packet, make([]byte, dns.MaxSizeUDP), func(string, dns.Type) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.8")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := dns.NewFrame(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(response) > dns.MaxSizeUDP || frame.Flags().ResponseCode() != dns.RCodeServerFailure {
		t.Fatalf("oversized reply not bounded: size=%d flags=%s", len(response), frame.Flags())
	}
}

func TestDNSBlockAndAllowPolicyEndToEnd(t *testing.T) {
	blockedRules := []string{"ads.example"}
	allowedRules := []string{"safe.ads.example"}
	for _, tc := range []struct {
		name            string
		host            string
		qtype           dns.Type
		upstreamAddress netip.Addr
		wantAddress     netip.Addr
		wantLookup      bool
	}{
		{"blocked A", "img.ads.example", dns.TypeA, netip.Addr{}, netip.IPv4Unspecified(), false},
		{"blocked AAAA", "img.ads.example", dns.TypeAAAA, netip.Addr{}, netip.IPv6Unspecified(), false},
		{"allowed A", "safe.ads.example", dns.TypeA, netip.MustParseAddr("203.0.113.40"), netip.MustParseAddr("203.0.113.40"), true},
		{"allowed AAAA subdomain", "cdn.safe.ads.example", dns.TypeAAAA, netip.MustParseAddr("2001:db8::40"), netip.MustParseAddr("2001:db8::40"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookupCalls := 0
			packet := makeDNSQuery(t, tc.host, tc.qtype)
			response, err := answerDNSQueryWithPolicy(packet, make([]byte, dns.MaxSizeUDP), func(host string) bool {
				return domainBlocked(host, blockedRules, allowedRules)
			}, func(string, dns.Type) ([]netip.Addr, error) {
				lookupCalls++
				return []netip.Addr{tc.upstreamAddress}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var decoded dns.Message
			decoded.LimitResourceDecoding(1, 1, 0, 0)
			if _, _, err := decoded.Decode(response); err != nil {
				t.Fatal(err)
			}
			if len(decoded.Answers) != 1 {
				t.Fatalf("got %d answers, want one", len(decoded.Answers))
			}
			got, ok := netip.AddrFromSlice(decoded.Answers[0].RawData())
			if !ok || got != tc.wantAddress {
				t.Fatalf("answer address = %v, want %v", got, tc.wantAddress)
			}
			if (lookupCalls > 0) != tc.wantLookup {
				t.Fatalf("upstream lookup called=%t, want %t", lookupCalls > 0, tc.wantLookup)
			}
		})
	}
}
