package main

import (
	"errors"
	"net/netip"

	"github.com/soypat/lneto/dns"
)

type dnsLookup func(host string, qtype dns.Type) ([]netip.Addr, error)
type dnsPolicy func(host string) bool

type dnsOutcome uint8

const (
	dnsOutcomeInvalid dnsOutcome = iota
	dnsOutcomeUnsupported
	dnsOutcomeBlocked
	dnsOutcomeForwarded
	dnsOutcomeFailure
)

var errDNSQuery = errors.New("invalid DNS query")

// answerDNSQuery parses one bounded UDP DNS query and writes a compact reply.
// It handles one IN-class A or AAAA question; other types receive NOTIMP.
func answerDNSQuery(packet, dst []byte, lookup dnsLookup) ([]byte, error) {
	answer, _, err := answerDNSQueryObserved(packet, dst, shouldBlockDomain, lookup)
	return answer, err
}

func answerDNSQueryWithPolicy(packet, dst []byte, blocked dnsPolicy, lookup dnsLookup) ([]byte, error) {
	answer, _, err := answerDNSQueryObserved(packet, dst, blocked, lookup)
	return answer, err
}

func answerDNSQueryObserved(packet, dst []byte, blocked dnsPolicy, lookup dnsLookup) ([]byte, dnsOutcome, error) {
	requestFrame, err := dns.NewFrame(packet)
	if err != nil {
		return nil, dnsOutcomeInvalid, err
	}
	if requestFrame.Flags().IsResponse() || requestFrame.QDCount() != 1 {
		return nil, dnsOutcomeInvalid, errDNSQuery
	}
	var request dns.Message
	// Accept a standard EDNS0 OPT record in the additional section. Its
	// advertised UDP size is not used; replies stay within 512 bytes.
	request.LimitResourceDecoding(1, 0, 0, 1)
	_, incomplete, err := request.Decode(packet)
	if err != nil || incomplete || len(request.Questions) != 1 {
		return nil, dnsOutcomeInvalid, errDNSQuery
	}
	question := request.Questions[0]
	flags := dns.HeaderFlags(1<<15 | 1<<7)
	flags |= dns.HeaderFlags(requestFrame.Flags().OpCode()) << 11
	if requestFrame.Flags().IsRecursionDesired() {
		flags |= 1 << 8
	}
	response := dns.Message{}
	response.AddQuestions(request.Questions)
	if requestFrame.Flags().OpCode() != dns.OpCodeQuery || question.Class != dns.ClassINET || (question.Type != dns.TypeA && question.Type != dns.TypeAAAA) {
		flags |= dns.HeaderFlags(dns.RCodeNotImplemented)
		answer, err := response.AppendTo(dst[:0], requestFrame.TxID(), flags)
		return answer, dnsOutcomeUnsupported, err
	}

	var addresses []netip.Addr
	outcome := dnsOutcomeForwarded
	if blocked(question.Name.String()) {
		outcome = dnsOutcomeBlocked
		if question.Type == dns.TypeA {
			addresses = []netip.Addr{netip.IPv4Unspecified()}
		} else {
			addresses = []netip.Addr{netip.IPv6Unspecified()}
		}
	} else {
		addresses, err = lookup(question.Name.String(), question.Type)
		if err != nil {
			flags |= dns.HeaderFlags(dns.RCodeServerFailure)
			answer, encodeErr := response.AppendTo(dst[:0], requestFrame.TxID(), flags)
			return answer, dnsOutcomeFailure, encodeErr
		}
	}
	for _, addr := range addresses {
		var data []byte
		switch question.Type {
		case dns.TypeA:
			if !addr.Is4() {
				continue
			}
			v := addr.As4()
			data = v[:]
		case dns.TypeAAAA:
			if !addr.Is6() || addr.Is4In6() {
				continue
			}
			v := addr.As16()
			data = v[:]
		}
		// Match ESP_hole's one-address response and stay inside classic UDP DNS
		// size limits without depending on EDNS or TCP DNS support.
		if int(response.Len())+int(question.Name.Len())+10+len(data) > dns.MaxSizeUDP {
			flags |= dns.HeaderFlags(dns.RCodeServerFailure)
			outcome = dnsOutcomeFailure
			break
		}
		response.Answers = append(response.Answers, dns.NewResource(question.Name, question.Type, dns.ClassINET, 60, data))
		break
	}
	answer, err := response.AppendTo(dst[:0], requestFrame.TxID(), flags)
	if err != nil {
		return nil, dnsOutcomeInvalid, err
	}
	return answer, outcome, nil
}
