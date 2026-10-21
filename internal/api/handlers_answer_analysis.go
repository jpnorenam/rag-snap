package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/analysis"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
)

// analysisAvailable reports whether the daemon can actually run analysis, and why not
// when it cannot.
//
// A client asks this so it can present the path as unavailable rather than offering it
// and failing at submit time. The reason is returned alongside so the UI can say what
// the operator must fix.
func (s *Server) analysisAvailable() (bool, string) {
	settings, err := analysis.LoadSettings(s.ctx.Config)
	if err != nil {
		return false, err.Error()
	}
	if err := analysis.Validate(settings); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// analysisClient builds the analysis client and the live catalogue, the two things every
// analysis request needs.
func (s *Server) analysisClient() (*analysis.Client, *analysis.Catalogue, error) {
	settings, err := analysis.LoadSettings(s.ctx.Config)
	if err != nil {
		return nil, nil, err
	}
	client, err := analysis.NewClient(settings, s.ctx.Verbose)
	if err != nil {
		return nil, nil, err
	}
	knowledgeClient, err := s.clients.openSearchClient()
	if err != nil {
		return nil, nil, err
	}
	// Kapa retrieval is not wired into the daemon, so the catalogue offers no source
	// groups here and a generated manifest selects none.
	cat, err := analysis.LoadCatalogue(context.Background(), knowledgeClient, nil)
	if err != nil {
		return nil, nil, err
	}
	return client, cat, nil
}

// swagger:route POST /1.0/answer/analyze answer answerAnalyze
//
// Triage RFP documents for manifest generation.
//
// Accepts an RFP/RFI document plus any number of supporting context documents as
// multipart/form-data ("file" for the primary document, repeated "context" fields for
// the rest) and runs the first analysis phase as an asynchronous operation. Publishes
// on the operation metadata: the triage (RFP identity, section structure, estimated
// question count, whether OpenStack is in scope, whether questions carry several answer
// fields, suggested knowledge bases), a per-document conveyance report saying which
// documents were read natively and which were converted, and an opaque
// "analysis_token" with its "expires_at" for the follow-up generation call. An optional
// "instructions" field carries the operator's free-text instructions (granularity,
// scope, which questions to include), applied to every analysis call for this session
// and echoed as "instructions" in the metadata; over 4000 characters is a 400 before any
// operation starts. Generates no manifest and answers no question. Rejected with 400 when analysis is not
// configured or no API key is present in the environment; the API key never appears in
// any response.
//
//	Consumes:
//	- multipart/form-data
//
//	Responses:
//	  202: asyncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleAnswerAnalyze(w http.ResponseWriter, r *http.Request) {
	if ok, reason := s.analysisAvailable(); !ok {
		respondError(w, http.StatusBadRequest, "analysis is unavailable: "+reason)
		return
	}
	if err := r.ParseMultipartForm(processing.MaxIngestFileSize); err != nil {
		respondError(w, http.StatusBadRequest, "parsing upload: "+err.Error())
		return
	}

	// Checked before anything is staged or any operation started, so an over-long set of
	// instructions costs nothing and fails with the same message the CLI gives.
	instructions := strings.TrimSpace(r.FormValue("instructions"))
	if err := analysis.ValidateInstructions(instructions); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	primary, cleanupPrimary, name, err := stageUpload(r, "file")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	staged := []func(){cleanupPrimary}
	cleanupAll := func() {
		for _, fn := range staged {
			fn()
		}
	}

	var contextPaths []string
	if r.MultipartForm != nil {
		for _, header := range r.MultipartForm.File["context"] {
			path, cleanup, err := stageUploadHeader(header)
			if err != nil {
				cleanupAll()
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			staged = append(staged, cleanup)
			contextPaths = append(contextPaths, path)
		}
	}

	client, catalogue, err := s.analysisClient()
	if err != nil {
		cleanupAll()
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	tikaURL := s.clients.tikaURL()

	op, err := s.ops.runTask(
		fmt.Sprintf("Analysing %q", name),
		nil, true,
		func(ctx context.Context, op *Operation) error {
			defer cleanupAll()

			converter := analysis.NewConverter(tikaURL)
			conveyance, err := converter.ConveyAll(primary, contextPaths)
			if err != nil {
				return err
			}
			// Bounded before the model call rather than after, so an over-large set
			// costs nothing.
			if err := checkAnalysisRequestSize(conveyance.TotalBytes()); err != nil {
				return err
			}

			triage, err := client.TriageWith(ctx, conveyance, catalogue, instructions)
			if err != nil {
				return err
			}

			token, err := s.analyses.stageWith(conveyance, triage, catalogue, instructions)
			if err != nil {
				return err
			}
			expires, _ := s.analyses.expiresAt(token)

			op.UpdateMetadata(map[string]any{
				"analysis_token": token,
				"expires_at":     expires.UTC().Format(time.RFC3339),
				"model":          client.Model(),
				"triage":         triage,
				"report":         triage.Report,
				"instructions":   instructions,
			})
			return nil
		},
	)
	if err != nil {
		cleanupAll()
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// analysisGenerateRequest is the JSON body of POST /1.0/answer/analyze/generate.
type analysisGenerateRequest struct {
	AnalysisToken string `json:"analysis_token"`
	AnswerWords   int    `json:"answer_words"`
	AnswerLimit   string `json:"answer_limit"`
	// OpenStack is "sunbeam" or "charmed", and is meaningful only when the triage
	// reported OpenStack in scope. Empty otherwise.
	OpenStack string `json:"openstack,omitempty"`
	// AcceptShortfall continues a session whose last generation fell short, using the
	// questions it produced rather than extracting them again. Rejected when the session
	// holds no shortfall.
	AcceptShortfall bool `json:"accept_shortfall,omitempty"`
	// RetryQuestions re-runs only the question extraction, one reasoning-effort level
	// above the one that fell short. Rejected when the session holds no shortfall, and
	// mutually exclusive with AcceptShortfall.
	RetryQuestions bool `json:"retry_questions,omitempty"`
}

// generationMetadata is what a completed generation publishes.
//
// The prompt's two halves are included so a client can let the model-written part be
// edited while the appended rules stay untouched; joined by analysis.JoinPrompt they are
// the manifest's prompt. They are omitted, rather than guessed, if the prompt does not end
// with the exact appended block.
func generationMetadata(manifest *chat.BatchManifest, result chat.CheckResult, model string,
	iv analysis.Interview, triage *analysis.Triage, accepted *analysis.ShortfallAcceptance) map[string]any {
	meta := map[string]any{
		"manifest": manifestToJSON(manifest),
		"check":    result,
		"model":    model,
	}
	if generated, appended, ok := analysis.SplitAssembledPrompt(manifest.Prompt, iv, triage); ok {
		meta["prompt_generated"] = generated
		meta["prompt_appended"] = appended
	}
	if accepted != nil {
		meta["shortfall_accepted"] = accepted
	}
	return meta
}

// shortfallMetadata is the structured detail a client reads from a generation that
// failed on a shortfall. Clients must read this, never parse the error string.
func shortfallMetadata(sf *analysis.ShortfallError) map[string]any {
	return map[string]any{
		"expected":      sf.Expected,
		"produced":      sf.Produced,
		"finish_reason": sf.FinishReason,
		"answer_tokens": sf.AnswerTokens,
		"ceiling":       sf.Ceiling,
		"effort":        string(sf.Effort),
		"next_effort":   string(analysis.NextEffort(sf.Effort)),
		"sections":      sf.Sections,
	}
}

// swagger:route POST /1.0/answer/analyze/generate answer answerAnalyzeGenerate
//
// Generate a manifest from a triage token.
//
// Takes the "analysis_token" from a completed POST /1.0/answer/analyze call together
// with the interview answers — "answer_words", "answer_limit" ("hard" or "soft"), and
// "openstack" ("sunbeam" or "charmed") when the triage reported OpenStack in scope —
// and runs the second analysis phase as an asynchronous operation. The documents are
// not re-uploaded: generation runs against the set the token refers to. Publishes the
// generated "manifest" in the same JSON shape POST /1.0/answer/batch accepts, its
// "check" findings, and the generating "model". It also publishes the prompt's two
// halves: "prompt_generated", the model-written part, and "prompt_appended", the rules
// rag-cli adds; joined with a blank line between them they are the manifest's prompt,
// so a client can let the first be edited without touching the second. The session's
// instructions are applied without being sent again. An unknown or expired token is a 400
// stating that the session expired and the documents must be uploaded again. A
// truncated response, or an empty response at the model's output ceiling, fails the
// operation rather than completing with a short manifest, and cannot be accepted.
//
// A question count materially short of the triage estimate, from an extraction that
// finished normally, also fails the operation, with a structured "shortfall" object in
// its metadata: "expected", "produced", "finish_reason", "answer_tokens", "ceiling",
// "effort", "next_effort" (empty at the top of the ladder) and per-section "sections"
// counts. The produced questions are retained on the session. Posting the same token and
// interview answers again with "accept_shortfall": true continues from those questions
// without another extraction, and the completed metadata carries "shortfall_accepted"
// with both counts; with "retry_questions": true, only the extraction is re-run, at
// "next_effort". Either flag on a session holding no shortfall, or both together, is a
// 400.
//
//	Responses:
//	  202: asyncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleAnswerAnalyzeGenerate(w http.ResponseWriter, r *http.Request) {
	var req analysisGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.AnalysisToken == "" {
		respondError(w, http.StatusBadRequest, "analysis_token is required")
		return
	}

	entry, ok := s.analyses.get(req.AnalysisToken)
	if !ok {
		respondError(w, http.StatusBadRequest,
			"the analysis session expired or is unknown; upload the documents again to start a new one")
		return
	}

	limit, err := analysis.ParseLengthMode(req.AnswerLimit)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	flavour, err := analysis.ParseOpenStackFlavour(req.OpenStack)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	interview := analysis.Interview{AnswerWords: req.AnswerWords, AnswerLimit: limit, OpenStack: flavour}
	if err := interview.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Resolve the operator's decision on a previous shortfall, if there is one.
	// The session's instructions apply to every generation for this token, including an
	// accept or a re-run after a shortfall.
	var (
		opts     = analysis.GenerateOptions{Instructions: entry.instructions}
		accepted *analysis.ShortfallAcceptance
	)
	if req.AcceptShortfall && req.RetryQuestions {
		respondError(w, http.StatusBadRequest, "accept_shortfall and retry_questions are alternatives; send one")
		return
	}
	if req.AcceptShortfall || req.RetryQuestions {
		sf, held := s.analyses.shortfallFor(req.AnalysisToken)
		if !held {
			respondError(w, http.StatusBadRequest,
				"there is no shortfall to accept or re-run for this analysis session")
			return
		}
		if req.AcceptShortfall {
			opts.AcceptedQuestions = sf.questions
			accepted = &analysis.ShortfallAcceptance{Expected: sf.expected, Produced: sf.produced}
		} else {
			next := analysis.NextEffort(sf.effort)
			if next == analysis.EffortUnset {
				respondError(w, http.StatusBadRequest,
					"the extraction already ran at the highest reasoning effort; accept the questions or start again")
				return
			}
			opts.QuestionsEffort = next
		}
	}

	client, _, err := s.analysisClient()
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	token := req.AnalysisToken
	op, err := s.ops.runTask(
		"Generating manifest",
		nil, true,
		func(ctx context.Context, op *Operation) error {
			manifest, err := client.GenerateWith(ctx, entry.conveyance, entry.catalogue, entry.triage, interview, opts)
			if sf, isShortfall := analysis.AsShortfall(err); isShortfall {
				// Kept on the session for the operator's decision, and described in the
				// metadata so a client never has to parse the error text.
				s.analyses.retainShortfall(token, sf)
				op.UpdateMetadata(map[string]any{"shortfall": shortfallMetadata(sf), "model": client.Model()})
				return err
			}
			if err != nil {
				return err
			}
			// The session ends here: the staged documents are released rather than
			// left resident for the remainder of the TTL.
			s.analyses.consume(token)

			result := chat.CheckManifest(manifest, s.checkRefs())
			op.UpdateMetadata(generationMetadata(manifest, result, client.Model(), interview, entry.triage, accepted))
			return nil
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// swagger:route POST /1.0/answer/check answer answerCheck
//
// Check a manifest without running it.
//
// Accepts a batch manifest in the same JSON shape POST /1.0/answer/batch accepts and
// returns its check findings synchronously. Answers no question and calls no model.
// The response carries the question and domain counts, the per-domain match counts
// (resolved with the run's own routing, so the check cannot disagree with a run), and
// the findings at two severities: a defect would fail a run or contribute nothing,
// while a warning may be deliberate — a domain pattern reserved for a section with no
// questions yet is the canonical example. When the knowledge store is unreachable, or
// kapa is not configured, the corresponding reference checks are reported as not
// performed ("references_checked", "kapa_checked") rather than as passing. Modifies
// and stores nothing.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
func (s *Server) handleAnswerCheck(w http.ResponseWriter, r *http.Request) {
	var req batchManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	manifest := req.toManifest()
	if len(manifest.Questions) == 0 {
		respondError(w, http.StatusBadRequest, "manifest contains no questions")
		return
	}
	respondSync(w, chat.CheckManifest(manifest, s.checkRefs()))
}

// checkRefs builds the existing-target lists the check resolves against, returning nil
// when the knowledge store cannot be reached so the reference checks are reported as not
// performed rather than as passing.
func (s *Server) checkRefs() *chat.CheckRefs {
	knowledgeClient, err := s.clients.openSearchClient()
	if err != nil {
		return nil
	}
	cat, err := analysis.LoadCatalogue(context.Background(), knowledgeClient, nil)
	if err != nil {
		return nil
	}
	refs := &chat.CheckRefs{}
	for _, kb := range cat.KnowledgeBases {
		refs.KnowledgeBases = append(refs.KnowledgeBases, kb.Name)
	}
	// KapaChecked stays false: kapa retrieval is not wired into the daemon, so it
	// cannot report whether a source group exists, and claiming otherwise would flag
	// every kapa group in a valid manifest.
	return refs
}

// manifestToJSON renders a manifest in the shape POST /1.0/answer/batch accepts, so a
// client can submit what it received without converting anything.
func manifestToJSON(m *chat.BatchManifest) batchManifestRequest {
	// KapaSourceGroups is deliberately not carried: the batch endpoint has no such
	// field because kapa retrieval is not wired into the daemon, and a field accepted
	// then silently ignored would be worse than an absent one. A manifest generated
	// here selects no kapa groups anyway, since the catalogue offers none.
	out := batchManifestRequest{
		Version:        m.Version,
		Model:          m.Model,
		KnowledgeBases: m.KnowledgeBases,
		Prompt:         m.Prompt,
		PromptRef:      m.PromptRef,
	}
	for _, d := range m.Domains {
		out.Domains = append(out.Domains, batchDomainRequest{
			Match:    d.Match,
			Context:  d.Context,
			Keywords: []string(d.Keywords),
		})
	}
	for _, q := range m.Questions {
		out.Questions = append(out.Questions, batchQuestionRequest{
			ID:       q.ID,
			Question: q.Question,
			Source:   q.Source,
			Keywords: []string(q.Keywords),
		})
	}
	return out
}

// stageUpload copies the named multipart file field to a temp file, returning its path,
// a cleanup function, and the original filename.
func stageUpload(r *http.Request, field string) (path string, cleanup func(), name string, err error) {
	if r.MultipartForm == nil || len(r.MultipartForm.File[field]) == 0 {
		return "", nil, "", fmt.Errorf("missing file upload field %q", field)
	}
	header := r.MultipartForm.File[field][0]
	path, cleanup, err = stageUploadHeader(header)
	return path, cleanup, header.Filename, err
}

// stageUploadHeader copies one uploaded file to a temp file on disk.
//
// Conveyance reads from a path, and an upload arriving over the API was never on disk,
// so it is staged here and removed when the operation finishes.
func stageUploadHeader(header *multipart.FileHeader) (string, func(), error) {
	if format := rfp.DetectFormat(header.Filename, header.Header.Get("Content-Type")); format == "unknown" {
		return "", nil, fmt.Errorf("unsupported file type %q — analysis reads PDF, XLSX, CSV, DOCX", header.Filename)
	}
	src, err := header.Open()
	if err != nil {
		return "", nil, fmt.Errorf("opening upload %q: %w", header.Filename, err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp("", "ragd-analyze-*"+filepath.Ext(header.Filename))
	if err != nil {
		return "", nil, fmt.Errorf("staging upload: %w", err)
	}
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("staging upload: %w", err)
	}
	tmp.Close()

	path := tmp.Name()
	return path, func() { _ = os.Remove(path) }, nil
}
