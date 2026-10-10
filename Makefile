TINYGO ?= tinygo
GO ?= go
PICO_IP ?= 192.168.68.65
BLOCKED_HOST ?=
ALLOWED_EXCEPTION ?=
PICO_FLAGS = -target=pico-w -scheduler=tasks -stack-size=8kb

.PHONY: build flash test test-integration benchmark-policy

build:
	mkdir -p build
	$(TINYGO) build $(PICO_FLAGS) -o build/pico-webserver.uf2 .

flash:
	$(TINYGO) flash $(PICO_FLAGS) -monitor .

test:
	$(GO) test ./...
	python3 -m unittest tests.test_dns_benchmark

test-integration:
	./tests/integration/test-dns-podman.sh "$(PICO_IP)" "$(BLOCKED_HOST)" "$(ALLOWED_EXCEPTION)"

benchmark-policy:
	$(GO) test -run '^$$' -bench 'Benchmark(BuiltInBlocklistLookup|DNSPolicy|DNSBlockedResponse|DNSForwardedResponse)' -benchmem -benchtime=2s
