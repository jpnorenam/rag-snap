package api

import (
	"context"
	"errors"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// unwritableConfig is a config store whose writes always fail, standing in for a
// snapctl set the daemon is not allowed to make.
type unwritableConfig struct {
	*memConfig
}

var errConfigWrite = errors.New("snapctl set refused")

func (unwritableConfig) Set(string, string, storage.ConfigType) error { return errConfigWrite }

// newRecorderServer builds the minimal Server recordModelID needs (a config store)
// plus a running operation to record into.
func newRecorderServer(t *testing.T, cfg storage.Config) (*Server, *Operation) {
	t.Helper()
	srv := &Server{ctx: &common.Context{Config: cfg}}
	reg := newOperations(context.Background(), newEventsHub())
	op, err := reg.create(operationClassTask, "test init", nil, false)
	if err != nil {
		t.Fatalf("creating operation: %v", err)
	}
	return srv, op
}

// TestRecordModelIDPersists covers the happy path: the ID reaches both the
// operation metadata and the package layer of the config.
func TestRecordModelIDPersists(t *testing.T) {
	cfg := newMemConfig(nil, nil)
	srv, op := newRecorderServer(t, cfg)

	srv.recordModelID(op, knowledge.ConfEmbeddingModelID, metaEmbeddingModelID, "model-abc")

	meta := op.view().Metadata
	if got := meta[metaEmbeddingModelID]; got != "model-abc" {
		t.Errorf("metadata %s = %v, want model-abc", metaEmbeddingModelID, got)
	}
	if got := meta[metaEmbeddingModelID+metaPersistedSuffix]; got != true {
		t.Errorf("metadata persisted flag = %v, want true", got)
	}
	if got := cfg.pkg[knowledge.ConfEmbeddingModelID]; got != "model-abc" {
		t.Errorf("package config %s = %v, want model-abc", knowledge.ConfEmbeddingModelID, got)
	}
}

// TestRecordModelIDReportsFailedPersist is the regression this change exists for:
// a config write the daemon cannot make must not cost the operator the model ID.
// The ID is still reported, flagged as unpersisted so the client can tell them to
// set it themselves.
func TestRecordModelIDReportsFailedPersist(t *testing.T) {
	srv, op := newRecorderServer(t, unwritableConfig{newMemConfig(nil, nil)})

	srv.recordModelID(op, knowledge.ConfRerankModelID, metaRerankModelID, "model-xyz")

	meta := op.view().Metadata
	if got := meta[metaRerankModelID]; got != "model-xyz" {
		t.Errorf("metadata %s = %v, want model-xyz", metaRerankModelID, got)
	}
	if got := meta[metaRerankModelID+metaPersistedSuffix]; got != false {
		t.Errorf("metadata persisted flag = %v, want false", got)
	}
	if op.view().StatusCode == statusCodeFailure {
		t.Error("operation failed, want a failed persist to leave the operation alone")
	}
}

// TestRecordModelIDIgnoresEmpty verifies an unresolved model neither writes config
// nor plants an empty ID that a client would report as a real value.
func TestRecordModelIDIgnoresEmpty(t *testing.T) {
	cfg := newMemConfig(nil, nil)
	srv, op := newRecorderServer(t, cfg)

	srv.recordModelID(op, knowledge.ConfEmbeddingModelID, metaEmbeddingModelID, "")

	if _, found := op.view().Metadata[metaEmbeddingModelID]; found {
		t.Error("metadata carries an empty model ID, want the key absent")
	}
	if _, found := cfg.pkg[knowledge.ConfEmbeddingModelID]; found {
		t.Error("config was written for an unresolved model")
	}
}
