package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func uploadRequest(t *testing.T, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("file", "spec.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("%PDF-1.4 test")); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/1.0/knowledge/kb/sources", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestUploadRecordsOriginalNameAndSourceURI(t *testing.T) {
	s := &Server{}
	items, _, err := s.collectUploadedItems(uploadRequest(t, map[string]string{"source_uri": "https://example.com/spec.pdf"}))
	if err != nil {
		t.Fatalf("collectUploadedItems: %v", err)
	}
	defer cleanupItems(items)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	it := items[0]
	if it.sourceURI != "https://example.com/spec.pdf" {
		t.Errorf("sourceURI = %q, want the supplied URI", it.sourceURI)
	}
	if it.fileName != "spec.pdf" {
		t.Errorf("fileName = %q, want the original upload name", it.fileName)
	}
	if !strings.Contains(it.filePath, "ragd-upload-") {
		t.Errorf("filePath = %q, want a staged temp file", it.filePath)
	}
}

func TestUploadWithoutSourceURI(t *testing.T) {
	s := &Server{}
	items, _, err := s.collectUploadedItems(uploadRequest(t, nil))
	if err != nil {
		t.Fatalf("collectUploadedItems: %v", err)
	}
	defer cleanupItems(items)
	if items[0].sourceURI != "" {
		t.Errorf("sourceURI = %q, want empty", items[0].sourceURI)
	}
}

func TestUploadRejectsInvalidSourceURI(t *testing.T) {
	s := &Server{}
	_, _, err := s.collectUploadedItems(uploadRequest(t, map[string]string{"source_uri": "file:///home/me/spec.pdf"}))
	if err == nil || !strings.Contains(err.Error(), "http(s)") {
		t.Fatalf("err = %v, want an http(s) URL error", err)
	}
}
