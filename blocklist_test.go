package main

import "testing"

func TestBuiltInBlocklistAndSuffixMatching(t *testing.T) {
	if len(blocklistFilter) != blocklistFilterBytes {
		t.Fatalf("built-in index has %d bytes, want %d", len(blocklistFilter), blocklistFilterBytes)
	}
	for _, host := range []string{
		"doubleclick.net",
		"ads.doubleclick.net",
		"PAGEAD.GOOGLESYNDICATION.COM.",
	} {
		if !shouldBlockDomain(host) {
			t.Errorf("request policy did not block built-in domain %q", host)
		}
	}
	if shouldBlockDomain("example.com") {
		t.Fatal("built-in blocklist unexpectedly matched example.com")
	}
}

func TestAllowlistOverridesBuiltInBlocklist(t *testing.T) {
	for _, host := range []string{"safe.doubleclick.net", "cdn.safe.doubleclick.net"} {
		if !blocklistContainsDomain(host) {
			t.Fatalf("fixture %q should match the built-in parent block", host)
		}
		if domainBlockedWithIndex(host, nil, []string{"safe.doubleclick.net"}, blocklistContainsDomain) {
			t.Errorf("allowlist did not override built-in block for %q", host)
		}
	}
}

func BenchmarkBuiltInBlocklistLookup(b *testing.B) {
	for i := 0; i < b.N; i++ {
		blocklistContainsDomain("cdn.eu.doubleclick.net")
	}
}

func BenchmarkDNSPolicyBlockedHit(b *testing.B) {
	for i := 0; i < b.N; i++ {
		shouldBlockDomain("cdn.eu.doubleclick.net")
	}
}

func BenchmarkDNSPolicyAllowedMiss(b *testing.B) {
	for i := 0; i < b.N; i++ {
		shouldBlockDomain("www.example.org")
	}
}
