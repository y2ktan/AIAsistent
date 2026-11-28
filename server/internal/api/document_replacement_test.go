package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/rag"
)

func installAPITextPDFTools(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	scripts := map[string]string{
		"pdfinfo":   "#!/bin/sh\nprintf 'Pages: 1\\n'\n",
		"pdftotext": "#!/bin/sh\nprintf '%s' \"$PDF_TEXT\"\n",
	}
	for name, content := range scripts {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PDF_TEXT", "Recovered PDF policy text.")
}

func makeDocumentReplacementRequest(t *testing.T, cookie *http.Cookie, docID, filename string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/"+docID+"/replace", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	request.Header.Set("Origin", "https://aegis.local")
	request.Header.Set("X-CSRF-Token", "fixture-csrf")
	return request
}

func TestReplaceFailedPDFAdminAPI(t *testing.T) {
	t.Run("replacement is reprocessed", func(t *testing.T) {
		installAPITextPDFTools(t)
		embeddingCalls := 0
		embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			embeddingCalls++
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}]}`))
		}))
		defer embeddingServer.Close()

		store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
		store.agentCfg.EmbeddingBaseURL = embeddingServer.URL
		store.agentCfg.EmbeddingModel = "fixture"
		store.agentCfg.EmbeddingDim = 3
		doc := &rag.Document{
			Title: "Policy", Filename: "failed.pdf", MimeType: "application/pdf",
			Status: "failed", ErrorMessage: "text extraction failed",
		}
		if err := store.ragStore.CreateDocument(context.Background(), doc); err != nil {
			t.Fatal(err)
		}

		request := makeDocumentReplacementRequest(t, cookie, doc.ID, "corrected.pdf", []byte("%PDF-1.7\ncorrected"))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var result struct {
			Document rag.Document `json:"document"`
			Warning  string       `json:"warning"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || result.Document.ID != doc.ID ||
			result.Document.Status != "ready" || result.Document.Filename != "corrected.pdf" ||
			result.Document.ExtractedText != "" || embeddingCalls != 1 {
			t.Fatalf("replacement failed: status=%d body=%s embeddings=%d", response.Code, response.Body.String(), embeddingCalls)
		}
		stored, err := store.ragStore.GetDocument(context.Background(), doc.ID)
		if err != nil || stored.Status != "ready" || stored.ExtractedText != "Recovered PDF policy text." {
			t.Fatalf("replacement was not persisted: doc=%+v err=%v", stored, err)
		}
	})

	t.Run("invalid and oversized source rejected", func(t *testing.T) {
		_, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
		invalid := makeDocumentReplacementRequest(t, cookie, "missing", "replacement.txt", []byte("not a PDF"))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, invalid)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid replacement status=%d body=%s", response.Code, response.Body.String())
		}

		oversized := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), int(rag.DefaultMaxUploadBytes))...)
		request := makeDocumentReplacementRequest(t, cookie, "missing", "replacement.pdf", oversized)
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), rag.ErrPayloadTooLarge.Error()) {
			t.Fatalf("oversized replacement status=%d body=%s", response.Code, response.Body.String())
		}
	})
}

func TestFailedPDFReplacementThenHostedDocumentSync(t *testing.T) {
	installAPITextPDFTools(t)
	t.Setenv("PDF_TEXT", strings.Repeat("Recovered searchable support policy. ", 30))
	embeddingCalls := 0
	knowledgeCalls := 0
	createCalls := 0
	agentResponse := hostedAgentWithKnowledgeBase(
		`{"id":"dashboard_doc","name":"Dashboard policy","type":"text","usage_mode":"auto"}`,
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/embeddings":
			embeddingCalls++
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}]}`))
		case strings.HasPrefix(r.URL.Path, "/v1/convai/"):
			knowledgeCalls++
			switch {
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/convai/agents/"):
				_, _ = w.Write([]byte(agentResponse))
			case r.Method == http.MethodPatch:
				var patch map[string]any
				if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
					t.Errorf("decode hosted patch: %v", err)
					http.Error(w, "bad patch", http.StatusBadRequest)
					return
				}
				var current map[string]any
				if err := json.Unmarshal([]byte(agentResponse), &current); err != nil {
					t.Fatal(err)
				}
				currentPrompt := current["conversation_config"].(map[string]any)["agent"].(map[string]any)["prompt"].(map[string]any)
				patchPrompt := patch["conversation_config"].(map[string]any)["agent"].(map[string]any)["prompt"].(map[string]any)
				currentPrompt["knowledge_base"] = patchPrompt["knowledge_base"]
				currentPrompt["rag"] = patchPrompt["rag"]
				encoded, err := json.Marshal(current)
				if err != nil {
					t.Fatal(err)
				}
				agentResponse = string(encoded)
				_, _ = w.Write(encoded)
			case r.Method == http.MethodPost && r.URL.Path == "/v1/convai/knowledge-base/text":
				createCalls++
				_, _ = w.Write([]byte(`{"id":"kb_recovered"}`))
			case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rag-index"):
				docID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/convai/knowledge-base/"), "/rag-index")
				_, _ = w.Write([]byte(`{"id":"` + docID + `","status":"succeeded"}`))
			default:
				t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.String())
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	store.agentCfg.ElevenLabsRAGSyncEnabled = false
	store.agentCfg.EmbeddingBaseURL = upstream.URL
	store.agentCfg.EmbeddingModel = "fixture"
	store.agentCfg.EmbeddingDim = 3
	doc := &rag.Document{
		Title: "Support policy", Filename: "failed.pdf", MimeType: "application/pdf",
		Status: "failed", ErrorMessage: "No readable text found.",
	}
	if err := store.ragStore.CreateDocument(context.Background(), doc); err != nil {
		t.Fatal(err)
	}

	request := makeDocumentReplacementRequest(t, cookie, doc.ID, "corrected.pdf", []byte("%PDF-1.7\ncorrected source"))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || embeddingCalls != 1 || knowledgeCalls != 0 {
		t.Fatalf("replacement did not remain local under disabled policy: status=%d body=%s embeddings=%d hosted=%d", response.Code, response.Body.String(), embeddingCalls, knowledgeCalls)
	}
	ready, err := store.ragStore.GetDocument(context.Background(), doc.ID)
	if err != nil || ready.Status != "ready" || ready.Filename != "corrected.pdf" {
		t.Fatalf("replacement did not reach local ready state: doc=%+v err=%v", ready, err)
	}

	store.agentCfg.ElevenLabsRAGSyncEnabled = true
	response = hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/admin/documents/"+doc.ID+"/sync", `{}`)
	if response.Code != http.StatusOK || createCalls != 1 {
		t.Fatalf("targeted hosted sync failed after recovery: status=%d body=%s creates=%d", response.Code, response.Body.String(), createCalls)
	}
	synced, err := store.ragStore.GetDocument(context.Background(), doc.ID)
	if err != nil || synced.Status != "ready" || synced.ElevenLabsSyncStatus != "synced" {
		t.Fatalf("hosted sync changed local readiness or failed to sync: doc=%+v err=%v", synced, err)
	}
	var agentState map[string]any
	if err := json.Unmarshal([]byte(agentResponse), &agentState); err != nil {
		t.Fatal(err)
	}
	entries := agentState["conversation_config"].(map[string]any)["agent"].(map[string]any)["prompt"].(map[string]any)["knowledge_base"].([]any)
	found := map[string]bool{}
	for _, raw := range entries {
		found[raw.(map[string]any)["id"].(string)] = true
	}
	if !found["dashboard_doc"] || !found["kb_recovered"] {
		t.Fatalf("hosted synchronization did not preserve dashboard knowledge: %#v", entries)
	}
}
