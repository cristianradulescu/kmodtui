BINARY := kmodtui
PKG    := ./cmd/kmodtui

.PHONY: all build run test vet fmt fmt-check check clean

all: check build

# Static binary: tcell and gocui are pure Go, so CGO is not needed.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/$(BINARY) $(PKG)

run:
	go run $(PKG)

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
