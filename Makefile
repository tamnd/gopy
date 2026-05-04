# gopy developer Makefile.
# Run `make help` for the task list.

GO       ?= go
PKG      := ./...
BIN_DIR  := bin
BIN      := $(BIN_DIR)/gopy
LDFLAGS  ?= -s -w

.PHONY: help
help:
	@echo "Targets:"
	@echo "  build    Build the gopy binary into ./$(BIN)"
	@echo "  test     Run unit tests with the race detector"
	@echo "  cover    Produce coverage.txt across all packages"
	@echo "  vet      Run go vet"
	@echo "  fmt      Run gofmt -s -w"
	@echo "  lint     Run golangci-lint (if installed)"
	@echo "  tidy     Run go mod tidy"
	@echo "  clean    Remove build output"

.PHONY: build
build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/gopy

.PHONY: test
test:
	$(GO) test -race -count=1 $(PKG)

.PHONY: cover
cover:
	$(GO) test -race -covermode=atomic -coverprofile=coverage.txt $(PKG)
	$(GO) tool cover -func=coverage.txt | tail -n 1

.PHONY: vet
vet:
	$(GO) vet $(PKG)

.PHONY: fmt
fmt:
	gofmt -s -w .

.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed. See https://golangci-lint.run"; exit 1; }
	golangci-lint run

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) dist coverage.txt coverage.html
