#!/usr/bin/env python3
"""Build the Pico's fixed-size Bloom index from Pi-hole's default hosts list."""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import math
import re
import sys
import urllib.request
from pathlib import Path


SOURCE_URL = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
FILTER_BYTES = 512 * 1024
HASH_COUNT = 12
MASK32 = 0xFFFFFFFF
BLOCKING_IPS = {"0.0.0.0", "127.0.0.1", "::", "::1"}
SKIP_DOMAINS = {"localhost", "localhost.localdomain", "broadcasthost"}
LABEL_RE = re.compile(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\Z")


def rotate_left(value: int, count: int) -> int:
    return ((value << count) | (value >> (32 - count))) & MASK32


def avalanche(value: int) -> int:
    value ^= value >> 16
    value = (value * 0x7FEB352D) & MASK32
    value ^= value >> 15
    value = (value * 0x846CA68B) & MASK32
    value ^= value >> 16
    return value & MASK32


def hashes(domain: str) -> tuple[int, int]:
    encoded = domain.lower().encode("ascii")
    h1 = 2166136261
    h2 = 0x9E3779B9 ^ len(encoded)
    for byte in encoded:
        h1 = ((h1 ^ byte) * 16777619) & MASK32
        h2 ^= byte
        h2 = (h2 * 0x85EBCA6B) & MASK32
        h2 = rotate_left(h2, 13)
    return avalanche(h1), avalanche(h2) | 1


def normalize_domain(raw: str) -> str | None:
    raw = raw.rstrip(".").lower()
    if not raw or raw in SKIP_DOMAINS or len(raw) > 253:
        return None
    try:
        raw = raw.encode("idna").decode("ascii")
    except UnicodeError:
        return None
    labels = raw.split(".")
    if len(labels) < 2 or any(len(label) > 63 or not LABEL_RE.fullmatch(label) for label in labels):
        return None
    return raw


def parse_hosts(data: bytes) -> tuple[set[str], int, str | None, int | None]:
    text = data.decode("utf-8-sig", errors="replace")
    domains: set[str] = set()
    rejected = 0
    reported_date = None
    reported_count = None
    for line in text.splitlines():
        if line.startswith("# Date:"):
            reported_date = line.removeprefix("# Date:").strip()
        elif line.startswith("# Number of unique domains:"):
            try:
                reported_count = int(line.rsplit(":", 1)[1].strip().replace(",", ""))
            except ValueError:
                pass
        content = line.split("#", 1)[0].strip()
        if not content:
            continue
        fields = content.split()
        if len(fields) < 2:
            continue
        try:
            address = str(ipaddress.ip_address(fields[0]))
        except ValueError:
            continue
        if address not in BLOCKING_IPS:
            continue
        for field in fields[1:]:
            domain = normalize_domain(field)
            if domain is None:
                rejected += 1
            else:
                domains.add(domain)
    return domains, rejected, reported_date, reported_count


def false_positive_rate(domain_count: int) -> float:
    bit_count = FILTER_BYTES * 8
    return (1.0 - math.exp(-HASH_COUNT * domain_count / bit_count)) ** HASH_COUNT


def make_filter(domains: set[str]) -> bytes:
    bits = bytearray(FILTER_BYTES)
    bit_mask = FILTER_BYTES * 8 - 1
    for domain in domains:
        first, step = hashes(domain)
        bit = first & bit_mask
        for _ in range(HASH_COUNT):
            bits[bit >> 3] |= 1 << (bit & 7)
            bit = (bit + step) & bit_mask
    return bytes(bits)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, help="use a previously downloaded hosts file")
    parser.add_argument("--output", type=Path, default=Path("blocklist_filter.bin"))
    parser.add_argument("--metadata", type=Path, default=Path("blocklist_metadata.json"))
    args = parser.parse_args()

    if args.source:
        source_bytes = args.source.read_bytes()
    else:
        request = urllib.request.Request(SOURCE_URL, headers={"User-Agent": "pico-w-dns-filter/1.0"})
        with urllib.request.urlopen(request, timeout=45) as response:
            source_bytes = response.read()

    domains, rejected, source_date, reported_count = parse_hosts(source_bytes)
    if not domains:
        print("no valid domains found; refusing to replace the index", file=sys.stderr)
        return 1
    rate = false_positive_rate(len(domains))
    if rate > 1e-6:
        print(f"estimated false-positive rate {rate:.3g} exceeds 1e-6", file=sys.stderr)
        return 1

    bloom = make_filter(domains)
    args.output.write_bytes(bloom)
    metadata = {
        "source_url": SOURCE_URL,
        "source_date": source_date,
        "source_reported_domains": reported_count,
        "indexed_domains": len(domains),
        "rejected_host_fields": rejected,
        "source_sha256": hashlib.sha256(source_bytes).hexdigest(),
        "filter_bytes": FILTER_BYTES,
        "hash_count": HASH_COUNT,
        "estimated_false_positive_rate_per_suffix": rate,
    }
    args.metadata.write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"indexed {len(domains):,} domains from {source_date or 'unknown snapshot'}")
    print(f"wrote {len(bloom):,} bytes; estimated false-positive rate per suffix {rate:.3g}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
