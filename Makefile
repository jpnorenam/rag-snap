
# ------------------------------------------------------------------------------
#  build

.PHONY: build
build: $(BINDIR)/$(BINNAME) tidy

$(BINDIR)/$(BINNAME): $(SRC)
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -trimpath -tags '$(TAGS)' -ldflags '$(LDFLAGS)' -o '$(BINDIR)'/$(BINNAME) ./cmd/go-rag-chat-client