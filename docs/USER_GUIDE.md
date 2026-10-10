# Update block and allow rules

This firmware keeps its DNS rules in flash. There is no web form or runtime
configuration store on the dashboard, so changing rules requires editing the
project, rebuilding the UF2, and flashing the Pico W. These steps affect every
client that uses this Pico as its DNS server.

## Add or remove local rules

Open [`dns_policy.go`](../dns_policy.go) and edit the two arrays near the top. Enter bare domain
names: do not include `https://`, a path, a wildcard, or a leading dot.

```go
var blockedDomains = [...]string{
	"ads.example.net",
}

var allowedDomains = [...]string{
	"safe.ads.example.net",
}
```

Rules are case-insensitive and match the domain plus its subdomains. The
allowlist is checked first and overrides both the built-in blocklist and
`blockedDomains`; in this example `safe.ads.example.net` and its subdomains are
allowed even though `ads.example.net` is blocked. To undo a rule, remove its
line from the corresponding array. Partial-label wildcards such as
`*.ads.example.net` are not supported.

## Refresh the built-in list

The common StevenBlack Unified hosts snapshot is baked into
[`blocklist_filter.bin`](../blocklist_filter.bin); the Pico does not download
list updates itself. From the repository root, run:

```sh
python3 tools/build_blocklist.py
```

This downloads the current source and regenerates `blocklist_filter.bin` and
`blocklist_metadata.json`. To build from a hosts file you already downloaded,
use:

```sh
python3 tools/build_blocklist.py --source /path/to/hosts
```

The metadata records the source date, domain count, and SHA-256 digest. Review
the list source and its attribution before redistributing firmware that
contains the generated filter; see [BLOCKLIST.md](BLOCKLIST.md).

## Rebuild, flash, and verify

After changing either local rules or the built-in list, run the host tests and
build a new UF2 from the repository root:

```sh
go test ./...
make build
```

With the Pico in BOOTSEL mode, copy `build/pico-webserver.uf2` to its `RPI-RP2`
drive. Alternatively use `make flash` with the board in BOOTSEL mode. Wait for
Wi-Fi to reconnect, then open the device IP shown in the serial monitor or
router's DHCP client list.

To check live DNS behavior without changing the computer's DNS settings, use a
disposable Podman client:

```sh
./tests/integration/test-dns-podman.sh <pico-ip> <blocked-host> <resolvable-allowed-host>
```

The blocked name should be in the built-in or custom blocklist. The allowed
name should be a real hostname that is covered by a broader block rule and
resolves upstream; otherwise the test cannot distinguish an allow exception
from an ordinary upstream lookup failure. The script also checks forwarding
for `example.com`, so do not add a rule that blocks `example.com` while using
this test. The container alone uses the Pico for DNS; the host resolver stays
unchanged. Dashboard counters at `/` show blocked, forwarded, and failed DNS
queries.

For paced measurements against the device, see
[the DNS benchmark guide](BENCHMARK.md#dns-sinkhole-benchmark).
