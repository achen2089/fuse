PREFIX ?= $(HOME)/.local/bin
BIN ?= fuse
COMPAT_BIN ?= .bin/fuse-live

.PHONY: help build $(BIN) dev-bin install uninstall clean test smoke-live

help:
	@printf '%s\n' \
		'Fuse make targets' \
		'' \
		'  make build      Build the canonical ./fuse binary and refresh ./.bin/fuse-live' \
		'  make dev-bin    Refresh the legacy ./.bin/fuse-live compatibility wrapper' \
		'  make install    Install fuse to $(PREFIX)/fuse' \
		'  make test       Run go test ./...' \
		'  make smoke-live Run the live Fuse smoke through ./fuse'

build: $(BIN) dev-bin

$(BIN):
	go build -o $(BIN) ./cmd/fuse/

dev-bin: $(BIN)
	@mkdir -p $(dir $(COMPAT_BIN))
	@printf '%s\n' \
		'#!/usr/bin/env bash' \
		'set -euo pipefail' \
		'repo_root="$$(cd "$$(dirname "$${BASH_SOURCE[0]}")/.." && pwd)"' \
		'target="$$repo_root/fuse"' \
		'if [[ ! -x "$$target" ]]; then' \
		'  echo "missing canonical fuse binary at $$target; run '\''make build'\'' or '\''make install'\''" >&2' \
		'  exit 1' \
		'fi' \
		'exec "$$target" "$$@"' > $(COMPAT_BIN)
	@chmod +x $(COMPAT_BIN)

install: build
	mkdir -p $(PREFIX)
	install -m 0755 $(BIN) $(PREFIX)/fuse

uninstall:
	rm -f $(PREFIX)/fuse

clean:
	rm -f $(BIN) $(COMPAT_BIN)

test:
	go test ./...

smoke-live: build
	./scripts/fuse-smoke.sh --fuse-bin ./fuse
