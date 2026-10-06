# Built-in DNS blocklist

The firmware embeds the Pi-hole Docker project's default adlist source,
[StevenBlack Unified hosts](https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts).
The checked-in snapshot is dated 2026-10-02 and contains 72,233 valid domains.
Pi-hole's container startup selects this source when no adlist has been configured
([upstream setup code](https://github.com/pi-hole/docker-pi-hole/blob/master/src/bash_functions.sh#L877-L882));
StevenBlack describes the list as a de-duplicated aggregation of several hosts
sources ([list details and attributions](https://github.com/StevenBlack/hosts/blob/master/readme.md#sources-of-hosts-data-unified-in-this-variant)).

## Representation and limits

The source text is about 2.2 MB. The firmware stores a 512 KiB Bloom index in
flash and checks 12 bits for each queried name and parent suffix. The built-in
lookup therefore takes work proportional to the DNS name, not the 72,233 list
entries. Allow rules are checked first; they can exempt any domain and its
subdomains from the built-in blocklist. The Bloom filter has no false negatives;
its estimated false-positive rate is about 1.8 per billion per suffix for this
snapshot. A query tests several suffixes, so its total false-positive chance is
slightly higher, but remains very small.

The immutable filter is embedded as a Go string so TinyGo places its bytes in
flash-backed read-only storage instead of copying them into SRAM. With TinyGo
0.42.0, this firmware uses 1,108,560 bytes of the Pico W's 2 MiB flash and 39,560
bytes of static RAM, including the filter without increasing the baseline RAM
report. The board has 264 KiB SRAM and 2 MiB QSPI flash ([Raspberry Pi Pico W
specification](https://datasheets.raspberrypi.com/picow/pico-w-product-brief.pdf)).

The source list aggregates third-party data with different attributions and
licenses. Read the upstream attribution before redistributing the generated
filter or firmware outside your own project.

## Updating

Run `python3 tools/build_blocklist.py` to fetch the current source and rebuild
`blocklist_filter.bin` plus `blocklist_metadata.json`. The script uses only the
Python standard library, validates the domain names, writes a deterministic
index, and refuses a snapshot whose estimated false-positive rate exceeds
`1e-6` per suffix. It records the source SHA-256 so the checked-in snapshot can
be identified. Rebuild and flash the firmware after updating the filter.

The current matcher supports domain and subdomain rules. AdGuard's default DNS
filter is also DNS-oriented, but its upstream documentation requires basic
AdGuard rule syntax; that filter includes wildcards, regular expressions, and
modifiers that this firmware does not implement. It is not silently flattened
into the suffix index. See the [AdGuard filter description](https://github.com/AdguardTeam/AdGuardSDNSFilter#readme)
and [rule syntax](https://adguard-dns.io/kb/general/dns-filtering-syntax/).

## Validation

```sh
python3 tools/build_blocklist.py --source /path/to/hosts
go test ./...
go test -run '^$' -bench BenchmarkBuiltInBlocklistLookup -benchtime=2s
```

`test-dns-podman.sh <pico-ip> <blocked-host>` checks a real sink response
through the Pico. Add a third hostname after configuring it as an allowlist
exception in `dns_policy.go` to verify it resolves upstream despite a matching
parent block rule.
