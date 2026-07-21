package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// Operation metadata keys reporting what a knowledge-engine init resolved. The
// CLI and the browser UI read these; the "_persisted" companion says whether the
// daemon managed to write the ID to package config, so a client knows whether the
// operator still has to do it by hand.
const (
	metaEmbeddingModelID = "embedding_model_id"
	metaRerankModelID    = "rerank_model_id"
	metaPersistedSuffix  = "_persisted"
)

// recordModelID publishes a resolved model ID to the operation and persists it to
// package config. Persistence is best-effort and never fails the operation: an
// operator can always set the key by hand, but losing the ID because a config
// write failed leaves them with nothing to set.
func (s *Server) recordModelID(op *Operation, confKey, metaKey, id string) {
	if id == "" {
		return
	}

	persisted := true
	if err := s.ctx.Config.Set(confKey, id, storage.PackageConfig); err != nil {
		persisted = false
		log.Printf("knowledge engine init: could not persist %s=%s: %v", confKey, id, err)
	} else {
		log.Printf("knowledge engine init: %s=%s (saved to package config)", confKey, id)
	}

	op.UpdateMetadata(map[string]any{
		metaKey:                       id,
		metaKey + metaPersistedSuffix: persisted,
	})
}

// swagger:route POST /1.0/knowledge-engine knowledge engineInit
//
// Initialize the knowledge engine.
//
// Sets up models, pipelines, and indexes as an async operation. The operation
// metadata reports each resolved model ID as soon as it is known — including when
// a later step fails — along with whether it was persisted to package config.
//
//	Responses:
//	  202: asyncResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleEngineInit(w http.ResponseWriter, _ *http.Request) {
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	op, err := s.ops.runTask(
		"Initializing knowledge engine",
		map[string][]string{"knowledge": {"/1.0/knowledge"}}, false,
		func(ctx context.Context, op *Operation) error {
			// Record each model ID the moment init resolves it, rather than after
			// the whole task: the IDs are known a third of the way in, and a
			// failure in a later step must not leave the caller with nothing to
			// configure. There is no operator watching the daemon's stdout.
			hooks := knowledge.InitHooks{
				OnEmbeddingModel: func(id string) {
					s.recordModelID(op, knowledge.ConfEmbeddingModelID, metaEmbeddingModelID, id)
				},
				OnRerankModel: func(id string) {
					s.recordModelID(op, knowledge.ConfRerankModelID, metaRerankModelID, id)
				},
			}
			initErr := client.InitPipelines(ctx, hooks)

			// Safety net for a hook that never fired (an ID resolved but reported
			// as empty, or a future init path that skips the hooks): take what the
			// client holds so the operation still reports the IDs.
			if op.MetadataString(metaEmbeddingModelID) == "" {
				s.recordModelID(op, knowledge.ConfEmbeddingModelID, metaEmbeddingModelID, client.EmbeddingModelID())
			}
			if op.MetadataString(metaRerankModelID) == "" {
				s.recordModelID(op, knowledge.ConfRerankModelID, metaRerankModelID, client.RerankModelID())
			}
			return initErr
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// exportRequest is the body of POST /1.0/knowledge/{name}/export.
type exportRequest struct {
	OutputDir string `json:"output_dir"`
	Compress  bool   `json:"compress"`
}

// swagger:route POST /1.0/knowledge/{name}/export knowledge knowledgeExport
//
// Export a knowledge base.
//
// Exports a base as an async operation, reusing the elasticdump-based exporter.
//
//	Responses:
//	  202: asyncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleKnowledgeExport(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req exportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// A daemon-local caller may pass output_dir; a browser client omits it and
	// gets a compressed archive staged where the download handler can stream it.
	op, err := s.ops.runTask(
		"Exporting knowledge base "+name,
		map[string][]string{"knowledge": {"/1.0/knowledge/" + name}}, false,
		func(ctx context.Context, op *Operation) error {
			opts := knowledge.ExportOptions{OutputDir: req.OutputDir, Compress: req.Compress}
			if opts.OutputDir == "" {
				parent := filepath.Join(os.TempDir(), "ragd-exports")
				if err := os.MkdirAll(parent, 0o755); err != nil {
					return fmt.Errorf("preparing export directory: %w", err)
				}
				opts.OutputDir = filepath.Join(parent, name+"-"+op.view().ID)
				opts.Compress = true
			}
			if err := knowledge.ExportKnowledgeBase(ctx, client, name, opts); err != nil {
				return err
			}
			if opts.Compress {
				op.UpdateMetadata(map[string]any{
					"archive_path": opts.OutputDir + ".tar.gz",
					"archive_name": name + ".tar.gz",
				})
			}
			return nil
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// swagger:route GET /1.0/knowledge/{name}/export/{opId}/archive knowledge knowledgeExportDownload
//
// Download a completed export archive.
//
// Streams the compressed archive produced by a finished export operation, so a
// browser can download it without filesystem access to the daemon.
//
// On success the response body is the raw gzip archive (application/gzip) with a
// Content-Disposition attachment header.
//
//	Responses:
//	  403: errorResponse
//	  404: errorResponse
func (s *Server) handleKnowledgeExportDownload(w http.ResponseWriter, r *http.Request) {
	opID := r.PathValue("opId")
	op := s.ops.get(opID)
	if op == nil {
		respondError(w, http.StatusNotFound, "export operation not found: "+opID)
		return
	}
	meta := op.view().Metadata
	archivePath, _ := meta["archive_path"].(string)
	if archivePath == "" {
		respondError(w, http.StatusNotFound, "export archive not ready")
		return
	}
	f, err := os.Open(archivePath)
	if err != nil {
		respondError(w, http.StatusNotFound, "export archive unavailable: "+err.Error())
		return
	}
	defer f.Close()

	archiveName, _ := meta["archive_name"].(string)
	if archiveName == "" {
		archiveName = filepath.Base(archivePath)
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", archiveName))
	if _, err := io.Copy(w, f); err != nil {
		// Response is already streaming; nothing more we can signal to the client.
		return
	}
}

// importRequest is the body of POST /1.0/knowledge:import.
type importRequest struct {
	Name     string `json:"name"`
	InputDir string `json:"input_dir"`
	Force    bool   `json:"force"`
}

// swagger:route POST /1.0/knowledge/import knowledge knowledgeImport
//
// Import a knowledge base.
//
// Imports a base from a previously exported artifact as an async operation. This
// endpoint handles local uploads; Google Drive import has its own endpoints under
// /1.0/knowledge/gdrive (see handlers_gdrive.go).
//
//	Responses:
//	  202: asyncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleKnowledgeImport(w http.ResponseWriter, r *http.Request) {
	var (
		name        string
		inputPath   string
		force       bool
		uploadClean func()
	)

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		staged, n, f, cleanup, err := stageImportUpload(r)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		name, inputPath, force, uploadClean = n, staged, f, cleanup
	} else {
		var req importRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		if strings.TrimSpace(req.InputDir) == "" {
			respondError(w, http.StatusBadRequest, "input_dir is required")
			return
		}
		name, inputPath, force = req.Name, req.InputDir, req.Force
	}

	client, err := s.clients.openSearchClient()
	if err != nil {
		if uploadClean != nil {
			uploadClean()
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	opts := knowledge.ImportOptions{InputDir: inputPath, Force: force}
	op, err := s.ops.runTask(
		"Importing knowledge base",
		map[string][]string{"knowledge": {"/1.0/knowledge"}}, false,
		func(ctx context.Context, _ *Operation) error {
			if uploadClean != nil {
				defer uploadClean()
			}
			return knowledge.ImportKnowledgeBase(ctx, client, name, opts)
		},
	)
	if err != nil {
		if uploadClean != nil {
			uploadClean()
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// stageImportUpload reads a multipart import request, streaming the uploaded
// archive to a temp .tar.gz path. It returns the staged path, the optional
// target name, the force flag, and a cleanup func.
func stageImportUpload(r *http.Request) (path, name string, force bool, cleanup func(), err error) {
	if perr := r.ParseMultipartForm(processing.MaxIngestFileSize); perr != nil {
		return "", "", false, nil, fmt.Errorf("parsing upload: %w", perr)
	}
	name = r.FormValue("name")
	force = r.FormValue("force") == "true"

	file, _, ferr := r.FormFile("archive")
	if ferr != nil {
		return "", "", false, nil, fmt.Errorf("missing archive upload field %q: %w", "archive", ferr)
	}
	defer file.Close()

	tmp, terr := os.CreateTemp("", "ragd-import-*.tar.gz")
	if terr != nil {
		return "", "", false, nil, fmt.Errorf("staging upload: %w", terr)
	}
	if _, cerr := io.Copy(tmp, file); cerr != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", "", false, nil, fmt.Errorf("staging upload: %w", cerr)
	}
	tmp.Close()

	staged := tmp.Name()
	return staged, name, force, func() { _ = os.Remove(staged) }, nil
}
