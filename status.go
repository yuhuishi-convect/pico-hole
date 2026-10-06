package main

import (
	"encoding/json"
	"sync"
	"time"
)

var serviceStatus serviceStats

type serviceStats struct {
	mu               sync.Mutex
	startedAt        time.Time
	ip               string
	wifiReady        bool
	httpReady        bool
	dnsReady         bool
	httpRequests     uint64
	dnsQueries       uint64
	blockedQueries   uint64
	forwardedQueries uint64
	dnsErrors        uint64
}

type dashboardSnapshot struct {
	WiFiStatus       string `json:"wifi_status"`
	HTTPStatus       string `json:"http_status"`
	DNSStatus        string `json:"dns_status"`
	IP               string `json:"ip"`
	UptimeSeconds    uint64 `json:"uptime_seconds"`
	HTTPRequests     uint64 `json:"http_requests"`
	DNSQueries       uint64 `json:"dns_queries"`
	BlockedQueries   uint64 `json:"blocked_queries"`
	ForwardedQueries uint64 `json:"forwarded_queries"`
	DNSErrors        uint64 `json:"dns_errors"`
}

func (s *serviceStats) start() {
	s.mu.Lock()
	s.startedAt = time.Now()
	s.mu.Unlock()
}

func (s *serviceStats) setNetwork(ip string) {
	s.mu.Lock()
	s.ip = ip
	s.wifiReady = true
	s.mu.Unlock()
}

func (s *serviceStats) setHTTPReady(ready bool) {
	s.mu.Lock()
	s.httpReady = ready
	s.mu.Unlock()
}

func (s *serviceStats) setDNSReady(ready bool) {
	s.mu.Lock()
	s.dnsReady = ready
	s.mu.Unlock()
}

func (s *serviceStats) recordHTTPRequest() {
	s.mu.Lock()
	s.httpRequests++
	s.mu.Unlock()
}

func (s *serviceStats) recordDNSQuery() {
	s.mu.Lock()
	s.dnsQueries++
	s.mu.Unlock()
}

func (s *serviceStats) recordDNSOutcome(outcome dnsOutcome) {
	s.mu.Lock()
	switch outcome {
	case dnsOutcomeBlocked:
		s.blockedQueries++
	case dnsOutcomeForwarded:
		s.forwardedQueries++
	case dnsOutcomeFailure, dnsOutcomeInvalid:
		s.dnsErrors++
	}
	s.mu.Unlock()
}

func (s *serviceStats) snapshot() dashboardSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	wifiStatus, httpStatus, dnsStatus := "offline", "offline", "offline"
	if s.wifiReady {
		wifiStatus = "online"
	}
	if s.httpReady {
		httpStatus = "online"
	}
	if s.dnsReady {
		dnsStatus = "online"
	}
	var uptime uint64
	if !s.startedAt.IsZero() {
		uptime = uint64(time.Since(s.startedAt).Seconds())
	}
	return dashboardSnapshot{
		WiFiStatus:       wifiStatus,
		HTTPStatus:       httpStatus,
		DNSStatus:        dnsStatus,
		IP:               s.ip,
		UptimeSeconds:    uptime,
		HTTPRequests:     s.httpRequests,
		DNSQueries:       s.dnsQueries,
		BlockedQueries:   s.blockedQueries,
		ForwardedQueries: s.forwardedQueries,
		DNSErrors:        s.dnsErrors,
	}
}

func dashboardJSON() []byte {
	body, err := json.Marshal(serviceStatus.snapshot())
	if err != nil {
		return []byte("{}")
	}
	return body
}
