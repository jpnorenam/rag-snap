# ------------------------------------------------------------------------------
#  Variables

BINDIR      := bin
BINNAME     := cli
SRC         := $(shell find . -type f -name '*.go' -not -path "./vendor/*")
CGO_ENABLED := 0
GOFLAGS     :=
TAGS        :=
LDFLAGS     := -w -s

# OpenAPI spec generation (go-swagger). Pinned for reproducible output.
SWAGGER_PKG  := github.com/go-swagger/go-swagger/cmd/swagger@v0.35.0
SWAGGER      := go run $(SWAGGER_PKG)
SPEC_SRC     := ./internal/api
SPEC_FILE    := rest-api.yaml

# ------------------------------------------------------------------------------
#  Browser UI
#
# The browser UI (ui/) builds to a static export (ui/out) that ragd embeds via
# go:embed from internal/webui/dist. The `ui` target builds the SPA and copies
# it into the embed directory; it MUST run before the ragd build so ragd embeds
# the real UI rather than the committed placeholder.

UI_DIR       := ui
UI_OUT       := $(UI_DIR)/out
WEBUI_DIST   := internal/webui/dist

.PHONY: ui
ui:
	cd $(UI_DIR) && npm ci && npm run build
	rm -rf $(WEBUI_DIST)
	mkdir -p $(WEBUI_DIST)
	cp -r $(UI_OUT)/. $(WEBUI_DIST)/

# ------------------------------------------------------------------------------
#  Build

.PHONY: build
build: $(BINDIR)/$(BINNAME)

$(BINDIR)/$(BINNAME): $(SRC)
	@mkdir -p $(BINDIR)
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -trimpath -tags '$(TAGS)' -ldflags '$(LDFLAGS)' -o '$(BINDIR)'/$(BINNAME) ./cmd/cli

# Build the ragd daemon (embeds the UI from internal/webui/dist; run `make ui`
# first to embed the real SPA rather than the committed placeholder).
.PHONY: build-ragd
build-ragd:
	@mkdir -p $(BINDIR)
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -trimpath -tags '$(TAGS)' -ldflags '$(LDFLAGS)' -o '$(BINDIR)'/ragd ./cmd/ragd

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
#  OpenAPI spec

# Regenerate the REST API spec from the handler annotations in internal/api.
.PHONY: spec
spec:
	$(SWAGGER) generate spec -w $(SPEC_SRC) -o $(SPEC_FILE)

# Fail if the committed spec is out of sync with the handler annotations.
# Regenerates into a temp file and diffs it against the committed spec.
.PHONY: spec-check
spec-check:
	@tmp=$$(mktemp --suffix=.yaml); \
	$(SWAGGER) generate spec -w $(SPEC_SRC) -o $$tmp; \
	if ! diff -u $(SPEC_FILE) $$tmp >/dev/null 2>&1; then \
		echo "ERROR: $(SPEC_FILE) is out of sync with the API handlers."; \
		echo "Run 'make spec' and commit the result."; \
		diff -u $(SPEC_FILE) $$tmp || true; \
		rm -f $$tmp; \
		exit 1; \
	fi; \
	rm -f $$tmp; \
	echo "$(SPEC_FILE) is in sync."

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
all: tidy fmt vet lint spec-check test build