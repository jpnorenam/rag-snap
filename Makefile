# ------------------------------------------------------------------------------
#  Variables

BINDIR      := bin
BINNAME     := cli
SRC         := $(shell find . -type f -name '*.go' -not -path "./vendor/*")
CGO_ENABLED := 0
GOFLAGS     :=
TAGS        :=
LDFLAGS     := -w -s
# Embed Google Drive OAuth2 credentials at build time (required for Drive import).
# Usage: make build DRIVE_CLIENT_ID=<id> DRIVE_CLIENT_SECRET=<secret>
# For development, set GOOGLE_DRIVE_CLIENT_ID / GOOGLE_DRIVE_CLIENT_SECRET env vars instead.
ifdef DRIVE_CLIENT_ID
LDFLAGS += -X github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge.driveClientID=$(DRIVE_CLIENT_ID)
endif
ifdef DRIVE_CLIENT_SECRET
LDFLAGS += -X github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge.driveClientSecret=$(DRIVE_CLIENT_SECRET)
endif

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
#  Lint

.PHONY: lint
lint:
	golangci-lint run ./...

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
all: tidy fmt vet lint test build