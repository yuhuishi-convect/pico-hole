package main

import (
	_ "embed"
	"strings"
)

const (
	blocklistFilterBytes = 512 * 1024
	blocklistHashCount   = 12
	blocklistBitCount    = blocklistFilterBytes * 8
	blocklistBitMask     = uint32(blocklistBitCount - 1)
)

// The immutable string keeps the generated index in flash-backed read-only
// storage. Embedding it as []byte would copy the 512 KiB into RP2040 SRAM.
//
//go:embed blocklist_filter.bin
var blocklistFilter string

// blocklistContainsDomain checks the requested name and each parent suffix.
// The number of probes depends on the number of labels, not the list size.
func blocklistContainsDomain(host string) bool {
	if len(blocklistFilter) != blocklistFilterBytes {
		return false
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	for start := 0; ; {
		if blocklistMayContain(host[start:]) {
			return true
		}
		dot := strings.IndexByte(host[start:], '.')
		if dot < 0 {
			return false
		}
		start += dot + 1
	}
}

func blocklistMayContain(domain string) bool {
	h1, h2 := blocklistHashes(domain)
	bit := h1 & blocklistBitMask
	for i := 0; i < blocklistHashCount; i++ {
		byteIndex := bit >> 3
		bitMask := byte(1 << (bit & 7))
		if blocklistFilter[byteIndex]&bitMask == 0 {
			return false
		}
		bit = (bit + h2) & blocklistBitMask
	}
	return true
}

// Keep this hash in sync with tools/build_blocklist.py.
func blocklistHashes(domain string) (uint32, uint32) {
	h1 := uint32(2166136261)
	h2 := uint32(0x9e3779b9) ^ uint32(len(domain))
	for i := 0; i < len(domain); i++ {
		b := domain[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		h1 = (h1 ^ uint32(b)) * 16777619
		h2 ^= uint32(b)
		h2 *= 0x85ebca6b
		h2 = (h2 << 13) | (h2 >> 19)
	}
	h1 ^= h1 >> 16
	h1 *= 0x7feb352d
	h1 ^= h1 >> 15
	h1 *= 0x846ca68b
	h1 ^= h1 >> 16
	h2 ^= h2 >> 16
	h2 *= 0x7feb352d
	h2 ^= h2 >> 15
	h2 *= 0x846ca68b
	h2 ^= h2 >> 16
	return h1, h2 | 1
}
