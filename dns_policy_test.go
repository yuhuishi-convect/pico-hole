package main

import "testing"

func TestDomainBlocked(t *testing.T) {
	blocked := []string{"ads.example", "trackers.test."}
	allowed := []string{"safe.ads.example"}
	for _, tc := range []struct {
		host  string
		block bool
	}{
		{"ads.example", true},
		{"ADS.EXAMPLE.", true},
		{"img.ads.example", true},
		{"notads.example", false},
		{"safe.ads.example", false},
		{"img.safe.ads.example", false},
		{"trackers.test", true},
		{"www.example.org", false},
		{"", false},
	} {
		t.Run(tc.host, func(t *testing.T) {
			if got := domainBlocked(tc.host, blocked, allowed); got != tc.block {
				t.Fatalf("domainBlocked(%q) = %t, want %t", tc.host, got, tc.block)
			}
		})
	}
}
