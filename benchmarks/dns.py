#!/usr/bin/env python3
"""Paced UDP DNS benchmark for the Pico W sinkhole."""

from __future__ import annotations

import argparse
import concurrent.futures
import datetime
import ipaddress
import json
import math
import random
import socket
import struct
import threading
import time
from pathlib import Path


QTYPE = {"A": 1, "AAAA": 28}


def encode_query(domain: str, qtype: str, txid: int) -> bytes:
    name = domain.rstrip(".")
    labels = name.split(".")
    if not name or any(not label or len(label.encode("idna")) > 63 for label in labels):
        raise ValueError("invalid DNS name")
    encoded_labels = [label.encode("idna") for label in labels]
    qname = b"".join(bytes((len(label),)) + label for label in encoded_labels) + b"\0"
    if len(qname) > 255:
        raise ValueError("DNS name is too long")
    return struct.pack("!HHHHHH", txid, 0x0100, 1, 0, 0, 0) + qname + struct.pack("!HH", QTYPE[qtype], 1)


def skip_name(packet: bytes, offset: int) -> int:
    while True:
        if offset >= len(packet):
            raise ValueError("truncated DNS name")
        length = packet[offset]
        if length & 0xC0 == 0xC0:
            if offset + 1 >= len(packet):
                raise ValueError("truncated DNS compression pointer")
            return offset + 2
        if length & 0xC0:
            raise ValueError("invalid DNS label length")
        offset += 1
        if length == 0:
            return offset
        offset += length


def decode_answers(packet: bytes, expected_id: int, qtype: str) -> tuple[int, list[bytes]]:
    if len(packet) < 12:
        raise ValueError("short DNS response")
    txid, flags, questions, answers, _, _ = struct.unpack_from("!HHHHHH", packet)
    if txid != expected_id or flags & 0x8000 == 0:
        raise ValueError("DNS transaction ID or response flag mismatch")
    if questions != 1:
        raise ValueError(f"unexpected question count: {questions}")
    offset = skip_name(packet, 12)
    if offset + 4 > len(packet):
        raise ValueError("truncated DNS question")
    offset += 4
    want_type = QTYPE[qtype]
    data = []
    for _ in range(answers):
        offset = skip_name(packet, offset)
        if offset + 10 > len(packet):
            raise ValueError("truncated DNS answer")
        rtype, rclass, _, length = struct.unpack_from("!HHIH", packet, offset)
        offset += 10
        if offset + length > len(packet):
            raise ValueError("truncated DNS answer data")
        if rtype == want_type and rclass == 1:
            data.append(packet[offset : offset + length])
        offset += length
    return flags & 0x000F, data


def check_response(packet: bytes, expected_id: int, qtype: str, mode: str) -> None:
    rcode, answers = decode_answers(packet, expected_id, qtype)
    if rcode != 0:
        raise ValueError(f"DNS response code {rcode}")
    address_bytes = 4 if qtype == "A" else 16
    sink_address = bytes(address_bytes)
    valid_addresses = [answer for answer in answers if len(answer) == address_bytes]
    if mode == "blocked":
        if sink_address not in valid_addresses:
            raise ValueError(f"blocked response did not contain the {qtype} sink address")
    elif not valid_addresses or all(answer == sink_address for answer in valid_addresses):
        raise ValueError("forwarded response had no non-sink address")


def query_once(server: str, domain: str, qtype: str, mode: str, timeout: float, txid: int) -> float:
    packet = encode_query(domain, qtype, txid)
    before = time.perf_counter()
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
        client.settimeout(timeout)
        client.sendto(packet, (server, 53))
        response, _ = client.recvfrom(4096)
    check_response(response, txid, qtype, mode)
    return time.perf_counter() - before


def percentile_ms(samples: list[float], percent: float) -> float | None:
    if not samples:
        return None
    ordered = sorted(samples)
    index = max(0, math.ceil(len(ordered) * percent / 100) - 1)
    return round(ordered[index] * 1000, 2)


def run_phase(server: str, domain: str, qtype: str, mode: str, rate: float, seconds: float, concurrency: int, timeout: float) -> dict:
    barrier = threading.Barrier(concurrency + 1)
    timing: dict[str, float] = {}

    def worker(index: int) -> tuple[list[float], dict[str, int], int]:
        latencies: list[float] = []
        failures: dict[str, int] = {}
        attempts = 0
        sequence = index
        barrier.wait()
        next_start = timing["start"] + index / rate
        while next_start < timing["deadline"]:
            time.sleep(max(0, next_start - time.perf_counter()))
            if time.perf_counter() >= timing["deadline"]:
                break
            attempts += 1
            try:
                latencies.append(query_once(server, domain, qtype, mode, timeout, random.randrange(0, 65536) ^ sequence))
            except Exception as exc:
                key = type(exc).__name__ + ": " + str(exc)
                failures[key] = failures.get(key, 0) + 1
                time.sleep(0.05)
            sequence += concurrency
            # Skip missed slots instead of emitting catch-up bursts.
            next_start = max(next_start + concurrency / rate, time.perf_counter())
        return latencies, failures, attempts

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as executor:
        workers = [executor.submit(worker, index) for index in range(concurrency)]
        timing["start"] = time.perf_counter()
        timing["deadline"] = timing["start"] + seconds
        barrier.wait()
        results = [worker.result() for worker in workers]
    elapsed = max(seconds, time.perf_counter() - timing["start"])
    latencies: list[float] = []
    failures: dict[str, int] = {}
    attempts = 0
    for samples, errors, count in results:
        latencies.extend(samples)
        attempts += count
        for key, count in errors.items():
            failures[key] = failures.get(key, 0) + count
    successes = len(latencies)
    failed = sum(failures.values())
    return {
        "target_qps": rate,
        "concurrency": concurrency,
        "active_seconds": seconds,
        "elapsed_seconds_including_drain": round(elapsed, 3),
        "attempted_requests": attempts,
        "successful_requests": successes,
        "failed_requests": failed,
        "successful_qps": round(successes / elapsed, 2),
        "error_percent": round(100 * failed / attempts, 2) if attempts else 0,
        "latency_ms_successful_only": {
            "p50": percentile_ms(latencies, 50),
            "p95": percentile_ms(latencies, 95),
            "p99": percentile_ms(latencies, 99),
        },
        "errors": failures,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server", required=True, help="Pico W IPv4 address")
    parser.add_argument("--mode", choices=("blocked", "forwarded"), required=True)
    parser.add_argument("--domain", required=True, help="blocked name or resolvable allowed name")
    parser.add_argument("--qtype", choices=tuple(QTYPE), default="A")
    parser.add_argument("--rates", type=float, nargs="+", default=[1, 5, 10], help="aggregate target QPS per phase")
    parser.add_argument("--seconds", type=float, default=10)
    parser.add_argument("--concurrency", type=int, default=1)
    parser.add_argument("--timeout", type=float, default=2)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    try:
        ipaddress.IPv4Address(args.server)
    except ipaddress.AddressValueError:
        parser.error("--server must be a Pico W IPv4 address")
    if args.seconds <= 0 or args.concurrency < 1 or args.timeout <= 0 or any(rate <= 0 for rate in args.rates):
        parser.error("rates, seconds, concurrency, and timeout must be positive")
    try:
        encode_query(args.domain, args.qtype, 1)
    except (ValueError, UnicodeError, KeyError) as exc:
        parser.error(str(exc))

    report = {
        "timestamp_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "server": args.server,
        "mode": args.mode,
        "domain": args.domain,
        "query_type": args.qtype,
        "transport": "UDP/53; one fresh client socket per query",
        "timeout_seconds": args.timeout,
        "scheduling": "paced aggregate request rate; missed slots are skipped",
        "results": [],
    }
    for rate in args.rates:
        print(f"Testing {args.mode} {args.qtype} at {rate:g} QPS for {args.seconds:g} s...", flush=True)
        result = run_phase(args.server, args.domain, args.qtype, args.mode, rate, args.seconds, args.concurrency, args.timeout)
        report["results"].append(result)
        if args.output:
            args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print(json.dumps(result), flush=True)
    if args.output:
        print(f"Wrote {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
