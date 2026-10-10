# Pico W HTTP and DNS benchmarks

Measured on the connected Pico W on October 5, 2026, over the local Wi-Fi LAN.
This HTTP data is a historical baseline from the earlier, 902-byte static page
before the current dashboard was added. The dashboard is larger and has not
been rebenchmarked here. The earlier page handled about **20 requests per
second** in a paced test; one unrestricted client achieved **23.55 QPS** with
no errors. These are observations of that firmware and network, not a maximum
rating for the Pico W hardware.

## Unrestricted client tests

Each phase ran for 10 seconds. Each worker immediately sent its next request
after the previous one completed. Failures were followed by a 50 ms delay.

| Concurrent clients | Successful QPS | Successful requests | Failed requests | Error rate | Median latency | p95 latency |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 23.55 | 236 | 0 | 0% | 41.59 ms | 51.93 ms |
| 2 | 43.67 | 437 | 7 | 1.58% | 43.08 ms | 57.84 ms |
| 3 | 46.02 | 463 | 118 | 20.31% | 44.80 ms | 64.03 ms |
| 6 | 51.79 | 521 | 489 | 48.42% | 46.01 ms | 66.14 ms |

All failures were connection refusals. The firmware's pool has only three TCP
connections, including connections that are closing. It can therefore refuse
new connections under load even when fewer than three requests are executing.
The approximately 52 QPS peak is not an error-free capacity.

## Paced check

Three workers scheduled requests at an aggregate target of 20 QPS for 30
seconds. Scheduling does not catch up with bursts if a worker falls behind.

- Actual completed throughput: **19.81 QPS**.
- **594 successful requests, zero failures**.
- Median response latency: **37.11 ms**; p95: **51.14 ms**; p99: **76.65 ms**.

Approximately 20 QPS is a reasonable initial load budget for this configuration.
These short tests do not establish performance or reliability over hours.

## Method and reproduction

Requests were `GET /` over HTTP, from this computer to `192.168.68.65`.
Each request opened a new TCP connection, with no keep-alive or TLS.
The HTML body is 902 bytes, and every successful response was checked for HTTP
200 and a byte-for-byte match with `index.html`. Latency includes connection
setup and receiving the complete body. Percentiles include successful requests
only. QPS uses wall-clock duration including completion of in-flight requests.

Firmware: TinyGo 0.42.0, CYW43439 driver
`v0.1.2-0.20260918152259-56295b807581`, `lneto v0.3.2`, three TCP pool slots,
and a 5 ms network polling sleep when idle. The firmware was not changed during
the benchmark.

```sh
python3 benchmarks/http.py
python3 benchmarks/http.py --concurrency 3 --seconds 30 --rate 20 --output benchmarks/http-paced-results.json
```

The raw measurements are in `../benchmarks/http-results.json` and
`../benchmarks/http-paced-results.json`. The IP may change after a DHCP refresh.

## DNS sinkhole benchmark

`benchmarks/dns.py` sends real UDP DNS packets to the Pico and checks each
answer. A blocked test requires the sink address (`0.0.0.0` for A or `::` for
AAAA); a forwarded test requires a non-empty upstream answer. It reports
successful request rate, failures, and successful-response latency percentiles.
The default tests are paced and use one client. Start with moderate rates; the
firmware processes DNS requests serially, and increasing concurrency can add
timeouts rather than useful throughput.

Measure a built-in blocked name at 1, 5, and 10 QPS:

```sh
python3 benchmarks/dns.py --server 192.168.68.65 --mode blocked --domain doubleclick.net --qtype A --rates 1 5 10 --seconds 10 --output benchmarks/dns-blocked-a.json
```

Measure forwarding separately at lower rates:

```sh
python3 benchmarks/dns.py --server 192.168.68.65 --mode forwarded --domain example.com --qtype A --rates 1 2 --seconds 10 --output benchmarks/dns-forwarded-a.json
```

### Live Pico W measurements

Measured on October 5, 2026, from a client on the same LAN, with one paced
client and a 10-second window per target rate. Each request used a fresh UDP
socket. The target is a scheduling cap; when a response takes longer than its
slot, the client skips missed slots instead of sending a burst. QPS counts only
valid answers, and latency percentiles include successful answers only.

| Query path | Type | Target QPS | Attempted / valid | Valid QPS | Errors | p50 / p95 latency |
| --- | --- | ---: | ---: | ---: | --- | ---: |
| Blocked | A | 1 | 10 / 10 | 1.00 | 0 | 53 / 156 ms |
| Blocked | A | 5 | 50 / 50 | 5.00 | 0 | 19 / 203 ms |
| Blocked | A | 10 | 81 / 80 | 8.00 | 1 timeout | 18 / 28 ms |
| Blocked | A | 20 | 77 / 74 | 7.40 | 3 timeouts | 18 / 38 ms |
| Blocked | AAAA | 1 | 10 / 10 | 1.00 | 0 | 54 / 202 ms |
| Blocked | AAAA | 5 | 50 / 49 | 4.13 | 1 timeout | 16 / 25 ms |
| Blocked | AAAA | 10 | 100 / 100 | 10.00 | 0 | 14 / 22 ms |
| Forwarded | A | 1 | 9 / 8 | 0.80 | 1 timeout | 94 / 256 ms |
| Forwarded | A | 2 | 20 / 20 | 2.00 | 0 | 149 / 321 ms |
| Forwarded | AAAA | 2 | 20 / 20 | 2.00 | 0 | 124 / 200 ms |

Blocked-query results vary between short runs: this sample had a clean 5 QPS
for A and a clean 10 QPS for AAAA, with timeouts at some other offered rates.
Forwarded-query time includes the upstream resolver and was slower. Treat these
as observations on this Pico and LAN, not a guaranteed capacity or an estimate
for every network. Raw samples: [blocked A](../benchmarks/dns-blocked-a.json),
[blocked AAAA](../benchmarks/dns-blocked-aaaa.json),
[forwarded A](../benchmarks/dns-forwarded-a.json), and
[forwarded AAAA](../benchmarks/dns-forwarded-aaaa.json).

The host-side Go microbenchmarks isolate the Bloom lookup, policy check, and
DNS packet response path without Wi-Fi or upstream variability:

```sh
go test -run '^$' -bench 'Benchmark(BuiltInBlocklistLookup|DNSPolicy|DNSBlockedResponse|DNSForwardedResponse)' -benchmem -benchtime=2s
```

These measure different things: Go benchmarks report desktop CPU cost, while
the UDP test includes Wi-Fi, packet handling, and (for forwarded lookups) the
upstream resolver. Use the live-device results for LAN expectations and the Go
benchmarks to compare matcher changes.

On the local desktop (Linux/amd64, Intel Core i7-1355U), the Go 1.26.1 run
reported:

| Host-side operation | Time | Allocations |
| --- | ---: | ---: |
| Built-in Bloom lookup | 68.62 ns/op | 0 B, 0 allocs |
| Complete block-policy hit | 69.47 ns/op | 0 B, 0 allocs |
| Complete block-policy miss | 35.45 ns/op | 0 B, 0 allocs |
| Blocked A DNS packet parse/policy/response | 423.6 ns/op | 288 B, 10 allocs |
| Forwarded A packet with a stub resolver | 490.1 ns/op | 344 B, 13 allocs |

The Go microbenchmarks run on the desktop CPU, not on the RP2040; the forwarded
case uses a stub and excludes Wi-Fi and upstream latency. They show the matcher
is a small part of end-to-end query time, but do not predict Pico throughput.
