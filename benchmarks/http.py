#!/usr/bin/env python3
"""Benchmark this Pico's page using a fresh TCP connection per request."""
import argparse
import concurrent.futures
import datetime
import http.client
import json
import math
from pathlib import Path
import threading
import time
from urllib.parse import urlsplit


def percentile(values, percent):
    if not values:
        return None
    ordered = sorted(values)
    return round(ordered[max(0, math.ceil(len(ordered) * percent / 100) - 1)] * 1000, 2)


def run_phase(url, expected, concurrency, duration, rate=None):
    parsed = urlsplit(url)
    barrier = threading.Barrier(concurrency + 1)
    timing = {}

    def worker(index):
        latencies, failures = [], {}
        barrier.wait()
        next_start = timing['start'] + (index / rate if rate else 0)
        while time.perf_counter() < timing['deadline']:
            if rate:
                if next_start >= timing['deadline']:
                    break
                time.sleep(max(0, next_start - time.perf_counter()))
                if time.perf_counter() >= timing['deadline']:
                    break
            conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=2)
            before = time.perf_counter()
            failed = False
            try:
                conn.request('GET', parsed.path or '/', headers={'Connection': 'close'})
                response = conn.getresponse()
                body = response.read()
                if response.status != 200:
                    raise ValueError('HTTP ' + str(response.status))
                if body != expected:
                    raise ValueError('HTML mismatch')
                latencies.append(time.perf_counter() - before)
            except Exception as exc:
                failed = True
                key = type(exc).__name__ + ': ' + str(exc)
                failures[key] = failures.get(key, 0) + 1
            finally:
                conn.close()
            if failed:
                # Avoid hammering a full connection pool with immediate retries.
                time.sleep(.05)
            if rate:
                next_start = max(next_start + concurrency / rate, time.perf_counter())
        return latencies, failures

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as executor:
        workers = [executor.submit(worker, index) for index in range(concurrency)]
        timing['start'] = time.perf_counter()
        timing['deadline'] = timing['start'] + duration
        barrier.wait()
        results = [worker.result() for worker in workers]
    elapsed = time.perf_counter() - timing['start']
    latencies, failures = [], {}
    for samples, errors in results:
        latencies.extend(samples)
        for key, count in errors.items():
            failures[key] = failures.get(key, 0) + count
    succeeded = len(latencies)
    failed = sum(failures.values())
    return {
        'concurrency': concurrency,
        'active_seconds': duration,
        'target_qps': rate,
        'elapsed_seconds_including_drain': round(elapsed, 3),
        'successful_requests': succeeded,
        'failed_requests': failed,
        'successful_qps': round(succeeded / elapsed, 2),
        'error_percent': round(100 * failed / (failed + succeeded), 2) if failed + succeeded else 0,
        'latency_ms': {
            'p50': percentile(latencies, 50),
            'p95': percentile(latencies, 95),
            'p99': percentile(latencies, 99),
        },
        'errors': failures,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', default='http://192.168.68.65/')
    parser.add_argument('--seconds', type=float, default=10)
    parser.add_argument('--concurrency', type=int, nargs='+', default=[1, 2, 3, 6])
    parser.add_argument('--output', default=str(Path(__file__).with_name('http-results.json')))
    parser.add_argument('--rate', type=float, help='pace requests at this aggregate QPS')
    args = parser.parse_args()
    if args.seconds <= 0 or any(c < 1 for c in args.concurrency) or (args.rate is not None and args.rate <= 0):
        parser.error('seconds and concurrency must be positive')
    project_root = Path(__file__).resolve().parents[1]
    expected = (project_root / 'index.html').read_bytes()
    report = {
        'timestamp_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'url': args.url,
        'page_bytes': len(expected),
        'method': 'GET',
        'connection': 'fresh TCP connection per request; no keep-alive',
        'load': 'paced workers' if args.rate else 'closed-loop workers',
        'retry_delay_seconds': .05,
        'request_timeout_seconds': 2,
        'results': [],
    }
    for concurrency in args.concurrency:
        print(f'Testing concurrency {concurrency} for {args.seconds:g} s...', flush=True)
        result = run_phase(args.url, expected, concurrency, args.seconds, args.rate)
        report['results'].append(result)
        Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(result), flush=True)
        time.sleep(2)


if __name__ == '__main__':
    main()
