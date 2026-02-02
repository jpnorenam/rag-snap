# ------------------------------------------------------------------------------
#  Variables

BINDIR      := bin
BINNAME     := cli
SRC         := $(shell find . -type f -name '*.go' -not -path "./vendor/*")
CGO_ENABLED := 0
GOFLAGS     :=
TAGS        :=
LDFLAGS     := -w -s

# ------------------------------------------------------------------------------
#  Build

.PHONY: build
build: $(BINDIR)/$(BINNAME)

$(BINDIR)/$(BINNAME): $(SRC)
	@mkdir -p $(BINDIR)
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -trimpath -tags '$(TAGS)' -ldflags '$(LDFLAGS)' -o '$(BINDIR)'/$(BINNAME) ./cmd/cli

# ------------------------------------------------------------------------------
#  Development

.PHONY: run
run:
	go run ./cmd/cli $(ARGS)

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

# ------------------------------------------------------------------------------
#  Testing

.PHONY: test
test:
	go test ./...

.PHONY: test-verbose
test-verbose:
	go test -v ./...

.PHONY: test-coverage
test-coverage:
	go test -cover ./...

# ------------------------------------------------------------------------------
#  Clean

.PHONY: clean
clean:
	rm -rf $(BINDIR)

# ------------------------------------------------------------------------------
#  All

.PHONY: all
all: tidy fmt vet test build