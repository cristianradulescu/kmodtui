BINARY := kmodtui
HELPER := kmodtui-helper
POLICY := io.github.cristianradulescu.kmodtui.policy

# Install layout. The helper path is compiled into kmodtui and written into
# the polkit policy, so build and install with the same PREFIX.
PREFIX     ?= /usr/local
BINDIR     := $(PREFIX)/bin
LIBEXECDIR := $(PREFIX)/libexec
HELPER_PATH := $(LIBEXECDIR)/$(HELPER)
# polkit reads policies only from here, whatever the PREFIX.
POLKITDIR  := /usr/share/polkit-1/actions

LDFLAGS := -s -w -X github.com/cristianradulescu/kmodtui/internal/ui.HelperPath=$(HELPER_PATH)

.PHONY: all build run test vet fmt fmt-check check clean install uninstall

all: check build

# Static binaries: tcell and gocui are pure Go, so CGO is not needed.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags='$(LDFLAGS)' -o bin/$(BINARY) ./cmd/kmodtui
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/$(HELPER) ./cmd/kmodtui-helper
	sed 's|@HELPER@|$(HELPER_PATH)|' packaging/$(POLICY).in > bin/$(POLICY)

run:
	go run ./cmd/kmodtui

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

check: fmt-check vet test

clean:
	rm -rf bin

# Run as root: sudo make install. Build first as your user (make build) so
# no build artifacts end up owned by root.
install:
	@test -f bin/$(BINARY) -a -f bin/$(HELPER) -a -f bin/$(POLICY) || { echo "run 'make build' first"; exit 1; }
	@grep -q '>$(HELPER_PATH)<' bin/$(POLICY) || { echo "bin/ was built for another PREFIX; run 'make build PREFIX=$(PREFIX)' first"; exit 1; }
	install -D -m 0755 bin/$(BINARY) $(DESTDIR)$(BINDIR)/$(BINARY)
	install -D -m 0755 bin/$(HELPER) $(DESTDIR)$(HELPER_PATH)
	install -D -m 0644 bin/$(POLICY) $(DESTDIR)$(POLKITDIR)/$(POLICY)

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/$(BINARY) $(DESTDIR)$(HELPER_PATH) $(DESTDIR)$(POLKITDIR)/$(POLICY)
