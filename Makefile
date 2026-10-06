TINYGO ?= tinygo
GO ?= go
PICO_FLAGS = -target=pico-w -scheduler=tasks -stack-size=8kb

.PHONY: build flash test

build:
	mkdir -p build
	$(TINYGO) build $(PICO_FLAGS) -o build/pico-webserver.uf2 .

flash:
	$(TINYGO) flash $(PICO_FLAGS) -monitor .

test:
	$(GO) test ./...
