# ESP_hole port to Pico W + TinyGo

## What to carry over

[ESP_hole](https://github.com/twedds95/ESP_hole) is a DNS-based ad blocker. Its request path checks an allowlist and a blocklist (including a few easy substring rules), then resolves permitted names and returns an address. The upstream build uses an approximately 1.5 MB Bloom filter generated from hundreds of thousands of domains, alongside an ESP32 web UI and configuration flow. The DNS decision logic is in [DNSHelper.cpp](https://github.com/twedds95/ESP_hole/blob/main/src/DNSHelper.cpp); the DNS service and web server startup are in [ESP_hole.cpp](https://github.com/twedds95/ESP_hole/blob/main/src/ESP_hole.cpp).

For the Pico W, the ESP32 setup does not carry over. The firmware uses a generated, flash-resident Bloom index for the common Pi-hole/StevenBlack domain set, while local allow/block overrides remain editable in Go. The upstream ESP_hole repository is licensed under [MPL-2.0](https://github.com/twedds95/ESP_hole/blob/main/LICENSE); if we carry over source code, retain its license notices.

## First port scope

- Keep the existing static web page on TCP port 80.
- Add a DNS listener on UDP port 53, using the Pico W's existing Wi-Fi/IP stack.
- Keep custom exceptions and additions as small editable Go lists. The common blocklist is generated separately into a fixed-size flash index; no downloaded rules are parsed on the Pico.
- Normalize names without regard to letter case and match whole DNS labels: a rule for `ads.example` also matches `img.ads.example`, but not `notads.example`.
- Check the allowlist first, so an allowed name or subdomain is an explicit exception to a broader block rule. Otherwise, block listed names and forward unmatched names to the DNS server supplied by DHCP.
- Support A and AAAA lookups in the initial implementation. Return a local sink address for blocked names and the resolved address for allowed or unlisted names. Leave CNAME rewriting, captive-portal setup, statistics, and a browser-based rule editor for later.

The TinyGo network dependencies already used by this project expose a UDP packet listener and DNS lookup helpers, so this shares the current network stack rather than bringing in another Wi-Fi driver. The lookup helper has shared client state, so upstream lookups are serialized instead of launching overlapping queries. This is sufficient for a small home-network filter, but it is not a general-purpose, byte-for-byte DNS proxy.

## Current status

- Implemented: hardware-independent block/allow matching, a bounded UDP/53 listener, A/AAAA answers, DHCP-provided upstream resolution, and a port-80 status dashboard. Allow entries take precedence; blocked names get `0.0.0.0` or `::`.
- Host-tested: case-insensitive suffix matching and subdomain boundaries; end-to-end DNS tests check blocked A/AAAA subdomains get sink answers without upstream lookups, while exact and nested allowlisted exceptions resolve upstream. DNS response tests also cover malformed requests and upstream errors.
- Device-tested: the blocklist-enabled firmware is online at `192.168.68.65`. The isolated Podman client resolved A and AAAA through the Pico and received `0.0.0.0` for `doubleclick.net`; dashboard counters recorded two forwarded and one blocked query. The host DNS settings were unchanged.
- The local Go override lists are empty; add exceptions or extra blocks in `dns_policy.go`.

Repeat the isolated DNS check with `./tests/integration/test-dns-podman.sh <pico-ip>`. It sets the resolver only inside a short-lived Podman client and checks A/AAAA forwarding. Once rules have been added to `dns_policy.go` and flashed, pass a blocked host and its allowed exception as the second and third arguments to also verify the sink response and allowlist override. The web page remains reachable at `http://<pico-ip>/`.

The first version should not change router-wide DNS settings. That keeps testing scoped to one client and makes rollback as simple as restoring its previous DNS setting.
