PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
USER_BINDIR ?= $(HOME)/.local/bin
CARGO ?= cargo
GO ?= go
.DEFAULT_GOAL := build

BACKEND = target/release/oxidrop
TUI = target/release/oxidrop-tui

.PHONY: build install install-user

build: $(BACKEND) $(TUI)

# Let Cargo and Go check their own build caches on explicit builds.
ifneq ($(filter build,$(or $(MAKECMDGOALS),$(.DEFAULT_GOAL))),)
.PHONY: force-build
$(BACKEND) $(TUI): force-build
endif

$(BACKEND): Cargo.toml Cargo.lock $(wildcard src/*.rs src/*/*.rs)
	$(CARGO) build --release --locked --target-dir target

$(TUI): tui/go.mod tui/go.sum $(wildcard tui/*.go)
	mkdir -p target/release
	$(GO) -C tui build -mod=readonly -o ../$(TUI) .

install: $(BACKEND) $(TUI)
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 $(BACKEND) $(TUI) "$(DESTDIR)$(BINDIR)/"

install-user: $(BACKEND) $(TUI)
	install -d "$(USER_BINDIR)"
	install -m 0755 $(BACKEND) $(TUI) "$(USER_BINDIR)/"
