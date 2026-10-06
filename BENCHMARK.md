# Pico W HTTP benchmark

Measured on the connected Pico W on October 5, 2026, over the local Wi-Fi LAN.
The current firmware reliably handled approximately **20 requests per second**
in a short paced test. A single unrestricted client achieved **23.55 QPS** with
no errors. These are observations of this firmware and network, not a maximum
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
python3 benchmark.py
python3 benchmark.py --concurrency 3 --seconds 30 --rate 20 --output benchmark-paced-results.json
```

The raw measurements are in `benchmark-results.json` and
`benchmark-paced-results.json`. The IP may change after a DHCP refresh.
