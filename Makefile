APP := prr
PACKAGE := ./cmd/prr
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test verify cross-build clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) $(PACKAGE)

test:
	go test ./...

verify:
	go test -race ./...
	go vet ./...

cross-build:
	@mkdir -p dist
	@set -e; for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; extension=""; \
		if [ "$$os" = "windows" ]; then extension=".exe"; fi; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o "dist/$(APP)-$$os-$$arch$$extension" $(PACKAGE); \
	done

clean:
	rm -rf bin dist
