//go:build tinygo && rp2040

package main

import (
	"errors"
	"net/netip"
	"time"

	"github.com/soypat/lneto/dns"
	"github.com/soypat/lneto/udp"
	"github.com/soypat/lneto/x/xnet"
)

const dnsLookupTimeout = 4 * time.Second

func serveDNSServer(stack *xnet.StackAsync) error {
	var rxBuf, txBuf [dns.MaxSizeUDP]byte
	var packets udp.PacketConn
	if err := packets.Configure(udp.PacketConnConfig{
		RxBuf:       rxBuf[:],
		TxBuf:       txBuf[:],
		RxQueueSize: 4,
		TxQueueSize: 4,
		RWBackoff:   tcpBackoff,
	}); err != nil {
		return err
	}
	if err := packets.Open(netip.AddrPortFrom(netip.IPv4Unspecified(), dns.ServerPort)); err != nil {
		return err
	}
	if err := stack.RegisterListenerUDP(&packets); err != nil {
		return err
	}
	serviceStatus.setDNSReady(true)
	defer serviceStatus.setDNSReady(false)
	println("DNS filter listening on UDP port 53")
	var query, response [dns.MaxSizeUDP]byte
	for {
		n, peer, err := packets.ReadFrom(query[:])
		if err != nil {
			return err
		}
		serviceStatus.recordDNSQuery()
		answer, outcome, err := answerDNSQueryObserved(query[:n], response[:], shouldBlockDomain, func(host string, qtype dns.Type) ([]netip.Addr, error) {
			return lookupDNS(stack, host, qtype)
		})
		serviceStatus.recordDNSOutcome(outcome)
		if err != nil {
			println("DNS query:", err.Error())
			continue
		}
		if _, err = packets.WriteTo(answer, peer); err != nil {
			println("DNS response:", err.Error())
		}
	}
}

// The underlying DNS client is shared by StackAsync, so this server resolves
// one upstream name at a time while the separate network poller stays active.
func lookupDNS(stack *xnet.StackAsync, host string, qtype dns.Type) ([]netip.Addr, error) {
	if err := stack.StartLookupIPType(host, qtype); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(dnsLookupTimeout)
	for time.Now().Before(deadline) {
		addrs, done, err := stack.ResultLookupIP(host)
		if done {
			return addrs, err
		}
		time.Sleep(pollInterval)
	}
	return nil, errors.New("upstream DNS timeout")
}
