package main

import "strings"

// Keep these lists short and hand-maintained. A rule matches the named domain
// and all of its subdomains; an allow rule is an exception to a block rule.
var blockedDomains = [...]string{
	// "ads.example",
}

var allowedDomains = [...]string{
	// "news.example",
}

func shouldBlockDomain(host string) bool {
	return domainBlockedWithIndex(host, blockedDomains[:], allowedDomains[:], blocklistContainsDomain)
}

func domainBlocked(host string, blocked, allowed []string) bool {
	return domainBlockedWithIndex(host, blocked, allowed, nil)
}

func domainBlockedWithIndex(host string, blocked, allowed []string, indexedBlock func(string) bool) bool {
	for _, rule := range allowed {
		if matchesDomainRule(host, rule) {
			return false
		}
	}
	if indexedBlock != nil && indexedBlock(host) {
		return true
	}
	for _, rule := range blocked {
		if matchesDomainRule(host, rule) {
			return true
		}
	}
	return false
}

func matchesDomainRule(host, rule string) bool {
	host = strings.TrimSuffix(host, ".")
	rule = strings.TrimSuffix(rule, ".")
	if host == "" || rule == "" {
		return false
	}
	if strings.EqualFold(host, rule) {
		return true
	}
	if len(host) <= len(rule) {
		return false
	}
	start := len(host) - len(rule)
	return host[start-1] == '.' && strings.EqualFold(host[start:], rule)
}
