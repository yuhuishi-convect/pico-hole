package main

import (
	"net/netip"
	"testing"

	"github.com/soypat/lneto/dns"
)

var benchmarkDNSResponse []byte

func BenchmarkDNSBlockedResponse(b *testing.B) {
	packet := makeDNSQuery(b, "cdn.eu.doubleclick.net", dns.TypeA)
	dst := make([]byte, dns.MaxSizeUDP)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response, _, err := answerDNSQueryObserved(packet, dst, shouldBlockDomain, nil)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkDNSResponse = response
	}
}

func BenchmarkDNSForwardedResponse(b *testing.B) {
	packet := makeDNSQuery(b, "www.example.org", dns.TypeA)
	dst := make([]byte, dns.MaxSizeUDP)
	lookup := func(string, dns.Type) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.8")}, nil
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response, _, err := answerDNSQueryObserved(packet, dst, shouldBlockDomain, lookup)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkDNSResponse = response
	}
}
