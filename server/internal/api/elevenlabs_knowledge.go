package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/security"
)

const (
	hostedRAGMinimumBytes = 500
	hostedRAGModel        = "e5_mistral_7b_instruct"
)

func (s *Server) syncHostedDocuments(ctx context.Context, config *AgentConfigRecord) error {
	if config == nil || config.Mode != "elevenlabs_agent" {
		return errors.New("Hosted document synchronization requires hosted mode.")
	}
	if !config.ElevenLabsRAGSyncEnabled {
		return nil
	}
	if s.ragSvc == nil {
		return errors.New("Local document storage is unavailable for hosted synchronization.")
	}
	readyDocs, err := s.ragSvc.ListReadyDocumentsForHostedSync(ctx)
	if err != nil {
		return errors.New("Could not load ready local documents for hosted synchronization.")
	}
	markReadyFailed := func(syncErr error) error {
		for i := range readyDocs {
			doc := &readyDocs[i]
			if err := s.saveHostedSyncState(
				ctx,
				doc,
				doc.ElevenLabsAgentID,
				doc.ElevenLabsDocID,
				doc.ElevenLabsSyncedHash,
				"failed",
				syncErr,
			); err != nil {
				return err
			}
		}
		return syncErr
	}
	key, err := s.hostedKey(config)
	if err != nil {
		return markReadyFailed(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	client := newHostedClient(s.allowPrivate)
	agentPath := "/v1/convai/agents/" + url.PathEscape(config.ElevenLabsAgentID)
	current := &hostedAgent{}
	if err := hostedRequest(ctx, client, key, agentPath, current); err != nil {
		return markReadyFailed(err)
	}
	if _, err := validateHostedAgentConfig(ctx, client, key, config.ElevenLabsAgentID, current); err != nil {
		return markReadyFailed(err)
	}
	managedIDs := make(map[string]struct{}, len(readyDocs))
	for _, doc := range readyDocs {
		if doc.ElevenLabsDocID != "" {
			managedIDs[doc.ElevenLabsDocID] = struct{}{}
		}
	}
	var syncFailures []error
	for i := range readyDocs {
		doc := &readyDocs[i]
		if strings.TrimSpace(doc.ExtractedText) == "" {
			err := errors.New("document has no extracted text")
			if stateErr := s.saveHostedSyncState(ctx, doc, config.ElevenLabsAgentID, doc.ElevenLabsDocID, doc.ElevenLabsSyncedHash, "failed", err); stateErr != nil {
				return stateErr
			}
			syncFailures = append(syncFailures, fmt.Errorf("%s: %w", doc.Title, err))
			continue
		}

		textHashBytes := sha256.Sum256([]byte(doc.ExtractedText))
		textHash := hex.EncodeToString(textHashBytes[:])
		hostedDocID := doc.ElevenLabsDocID
		sameContent := hostedDocID != "" &&
			doc.ElevenLabsAgentID == config.ElevenLabsAgentID &&
			doc.ElevenLabsSyncedHash == textHash
		if !sameContent {
			if hostedDocID != "" {
				if err := deleteHostedKnowledgeBaseDocument(ctx, client, key, hostedDocID); err != nil {
					if stateErr := s.saveHostedSyncState(ctx, doc, doc.ElevenLabsAgentID, hostedDocID, doc.ElevenLabsSyncedHash, "failed", err); stateErr != nil {
						return stateErr
					}
					syncFailures = append(syncFailures, fmt.Errorf("%s: %w", doc.Title, err))
					continue
				}
				if err := s.saveHostedSyncState(ctx, doc, config.ElevenLabsAgentID, "", "", "not_synced", nil); err != nil {
					return err
				}
			}
			hostedDocID, err = createHostedKnowledgeBaseDocument(ctx, client, key, doc.Title, doc.ExtractedText)
			if err != nil {
				if stateErr := s.saveHostedSyncState(ctx, doc, doc.ElevenLabsAgentID, doc.ElevenLabsDocID, doc.ElevenLabsSyncedHash, "failed", err); stateErr != nil {
					return stateErr
				}
				syncFailures = append(syncFailures, fmt.Errorf("%s: %w", doc.Title, err))
				continue
			}
			if err := s.saveHostedSyncState(ctx, doc, config.ElevenLabsAgentID, hostedDocID, textHash, "not_synced", nil); err != nil {
				return err
			}
		}

		status := doc.ElevenLabsSyncStatus
		if !sameContent || status != "synced" {
			if len([]byte(doc.ExtractedText)) >= hostedRAGMinimumBytes {
				status, err = computeHostedRAGIndex(ctx, client, key, hostedDocID)
				if err != nil {
					if stateErr := s.saveHostedSyncState(ctx, doc, config.ElevenLabsAgentID, hostedDocID, textHash, "failed", err); stateErr != nil {
						return stateErr
					}
					syncFailures = append(syncFailures, fmt.Errorf("%s: %w", doc.Title, err))
					continue
				}
			} else {
				status = "synced"
			}
		}
		if err := s.saveHostedSyncState(ctx, doc, config.ElevenLabsAgentID, hostedDocID, textHash, status, nil); err != nil {
			return err
		}
	}

	allDocs, err := s.ragSvc.List(ctx)
	if err != nil {
		return errors.New("Could not reload local document synchronization state.")
	}
	for _, doc := range allDocs {
		if doc.ElevenLabsDocID != "" {
			managedIDs[doc.ElevenLabsDocID] = struct{}{}
		}
	}
	knowledgeBase, err := hostedKnowledgeBaseForSync(current.Conversation.Agent.Prompt.KnowledgeBase, managedIDs, allDocs, config.ElevenLabsAgentID)
	if err != nil {
		return err
	}
	ragSettings, err := hostedRAGSettings(current.Conversation.Agent.Prompt.RAG)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"conversation_config": map[string]any{
			"agent": map[string]any{
				"prompt": map[string]any{
					"knowledge_base": knowledgeBase,
					"rag":            ragSettings,
				},
			},
		},
	}
	if err := hostedPatch(ctx, client, key, agentPath, payload); err != nil {
		if stateErr := s.markHostedSyncFailures(ctx, allDocs, err); stateErr != nil {
			return stateErr
		}
		return err
	}
	if _, err := inspectHostedAgent(ctx, client, key, config.ElevenLabsAgentID); err != nil {
		if stateErr := s.markHostedSyncFailures(ctx, allDocs, err); stateErr != nil {
			return stateErr
		}
		return fmt.Errorf("Hosted Knowledge Base update could not be verified: %w", err)
	}
	if len(syncFailures) > 0 {
		return fmt.Errorf("Some local documents could not be synchronized: %w", errors.Join(syncFailures...))
	}
	return nil
}

func (s *Server) syncHostedDocument(ctx context.Context, config *AgentConfigRecord, docID string) error {
	if config == nil || config.Mode != "elevenlabs_agent" {
		return errors.New("Hosted document synchronization requires hosted mode.")
	}
	if !config.ElevenLabsRAGSyncEnabled {
		return errors.New("Application RAG synchronization is disabled. Enable and save it in AI Agent settings before retrying.")
	}
	if s.ragSvc == nil {
		return errors.New("Local document storage is unavailable for hosted synchronization.")
	}
	docs, err := s.ragSvc.List(ctx)
	if err != nil {
		return errors.New("Could not load local document synchronization state.")
	}
	managedIDs := make(map[string]struct{}, len(docs)+1)
	for _, currentDoc := range docs {
		if currentDoc.ElevenLabsDocID != "" {
			managedIDs[currentDoc.ElevenLabsDocID] = struct{}{}
		}
	}
	doc, err := s.ragSvc.GetDocument(ctx, docID)
	if err != nil {
		return err
	}
	if doc.Status != "ready" {
		return errors.New("Document must be locally ready before hosted synchronization.")
	}
	if doc.ElevenLabsDocID != "" {
		managedIDs[doc.ElevenLabsDocID] = struct{}{}
	}
	if strings.TrimSpace(doc.ExtractedText) == "" {
		err := errors.New("Document has no extracted text.")
		if stateErr := s.saveHostedSyncState(ctx, doc, doc.ElevenLabsAgentID, doc.ElevenLabsDocID, doc.ElevenLabsSyncedHash, "failed", err); stateErr != nil {
			return stateErr
		}
		return err
	}

	key, err := s.hostedKey(config)
	if err != nil {
		if stateErr := s.saveHostedSyncState(ctx, doc, doc.ElevenLabsAgentID, doc.ElevenLabsDocID, doc.ElevenLabsSyncedHash, "failed", err); stateErr != nil {
			return stateErr
		}
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	client := newHostedClient(s.allowPrivate)
	agentPath := "/v1/convai/agents/" + url.PathEscape(config.ElevenLabsAgentID)
	current, err := loadHostedAgent(ctx, client, key, config.ElevenLabsAgentID)
	if err != nil {
		if stateErr := s.saveHostedSyncState(ctx, doc, doc.ElevenLabsAgentID, doc.ElevenLabsDocID, doc.ElevenLabsSyncedHash, "failed", err); stateErr != nil {
			return stateErr
		}
		return err
	}
	fail := func(syncErr error) error {
		if stateErr := s.saveHostedSyncState(
			ctx, doc, doc.ElevenLabsAgentID, doc.ElevenLabsDocID, doc.ElevenLabsSyncedHash, "failed", syncErr,
		); stateErr != nil {
			return stateErr
		}
		return syncErr
	}

	textHashBytes := sha256.Sum256([]byte(doc.ExtractedText))
	textHash := hex.EncodeToString(textHashBytes[:])
	hostedDocID := doc.ElevenLabsDocID
	sameContent := hostedDocID != "" &&
		doc.ElevenLabsAgentID == config.ElevenLabsAgentID &&
		doc.ElevenLabsSyncedHash == textHash
	if !sameContent {
		if hostedDocID != "" {
			if err := deleteHostedKnowledgeBaseDocument(ctx, client, key, hostedDocID); err != nil {
				return fail(err)
			}
			if err := s.saveHostedSyncState(ctx, doc, config.ElevenLabsAgentID, "", "", "not_synced", nil); err != nil {
				return err
			}
		}
		hostedDocID, err = createHostedKnowledgeBaseDocument(ctx, client, key, doc.Title, doc.ExtractedText)
		if err != nil {
			return fail(err)
		}
		if err := s.saveHostedSyncState(ctx, doc, config.ElevenLabsAgentID, hostedDocID, textHash, "not_synced", nil); err != nil {
			return err
		}
	}

	status := doc.ElevenLabsSyncStatus
	if !sameContent || status != "synced" {
		if len([]byte(doc.ExtractedText)) >= hostedRAGMinimumBytes {
			status, err = computeHostedRAGIndex(ctx, client, key, hostedDocID)
			if err != nil {
				return fail(err)
			}
		} else {
			status = "synced"
		}
	}
	if err := s.saveHostedSyncState(ctx, doc, config.ElevenLabsAgentID, hostedDocID, textHash, status, nil); err != nil {
		return err
	}

	allDocs, err := s.ragSvc.List(ctx)
	if err != nil {
		return errors.New("Could not reload local document synchronization state.")
	}
	for _, currentDoc := range allDocs {
		if currentDoc.ElevenLabsDocID != "" {
			managedIDs[currentDoc.ElevenLabsDocID] = struct{}{}
		}
	}
	knowledgeBase, err := hostedKnowledgeBaseForSync(
		current.Conversation.Agent.Prompt.KnowledgeBase,
		managedIDs,
		allDocs,
		config.ElevenLabsAgentID,
	)
	if err != nil {
		return err
	}
	ragSettings, err := hostedRAGSettings(current.Conversation.Agent.Prompt.RAG)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"conversation_config": map[string]any{
			"agent": map[string]any{
				"prompt": map[string]any{
					"knowledge_base": knowledgeBase,
					"rag":            ragSettings,
				},
			},
		},
	}
	if err := hostedPatch(ctx, client, key, agentPath, payload); err != nil {
		return fail(err)
	}
	verified, err := loadHostedAgent(ctx, client, key, config.ElevenLabsAgentID)
	if err != nil {
		return fail(fmt.Errorf("Hosted Knowledge Base update could not be verified: %w", err))
	}
	if err := verifyHostedKnowledgeBase(verified.Conversation.Agent.Prompt.KnowledgeBase, knowledgeBase, managedIDs); err != nil {
		return fail(err)
	}
	return nil
}

func verifyHostedKnowledgeBase(actual, expected []json.RawMessage, managedIDs map[string]struct{}) error {
	actualIDs := make(map[string]struct{}, len(actual))
	for _, raw := range actual {
		var entry hostedKnowledgeBaseEntry
		if err := json.Unmarshal(raw, &entry); err != nil || entry.ID == "" {
			return errors.New("ElevenLabs returned an invalid Knowledge Base entry.")
		}
		actualIDs[entry.ID] = struct{}{}
	}
	expectedIDs := make(map[string]struct{}, len(expected))
	for _, raw := range expected {
		var entry hostedKnowledgeBaseEntry
		if err := json.Unmarshal(raw, &entry); err != nil || entry.ID == "" {
			return errors.New("Could not verify the targeted hosted Knowledge Base entry.")
		}
		expectedIDs[entry.ID] = struct{}{}
		if _, ok := actualIDs[entry.ID]; !ok {
			return errors.New("Hosted Knowledge Base update could not be verified.")
		}
	}
	for id := range managedIDs {
		if _, expected := expectedIDs[id]; !expected {
			if _, actual := actualIDs[id]; actual {
				return errors.New("Hosted Knowledge Base retained a stale application-managed entry.")
			}
		}
	}
	return nil
}

func (s *Server) saveHostedSyncState(
	ctx context.Context,
	doc *rag.Document,
	agentID, hostedDocID, contentHash, status string,
	syncErr error,
) error {
	errMessage := ""
	if syncErr != nil {
		errMessage = security.RedactString(syncErr.Error())
	}
	if err := s.ragSvc.UpdateElevenLabsSyncState(
		ctx, doc.ID, hostedDocID, contentHash, agentID, status, errMessage,
	); err != nil {
		return errors.New("Could not save local hosted document synchronization status.")
	}
	doc.ElevenLabsDocID = hostedDocID
	doc.ElevenLabsSyncedHash = contentHash
	doc.ElevenLabsAgentID = agentID
	doc.ElevenLabsSyncStatus = status
	doc.ElevenLabsSyncError = errMessage
	return nil
}

func (s *Server) markHostedSyncFailures(
	ctx context.Context,
	docs []rag.Document,
	syncErr error,
) error {
	for i := range docs {
		if docs[i].Status != "ready" || docs[i].ElevenLabsSyncStatus == "failed" {
			continue
		}
		if err := s.saveHostedSyncState(
			ctx,
			&docs[i],
			docs[i].ElevenLabsAgentID,
			docs[i].ElevenLabsDocID,
			docs[i].ElevenLabsSyncedHash,
			"failed",
			syncErr,
		); err != nil {
			return err
		}
	}
	return nil
}

func createHostedKnowledgeBaseDocument(ctx context.Context, client *http.Client, key, title, text string) (string, error) {
	var response struct {
		ID string `json:"id"`
	}
	err := hostedJSONRequest(ctx, client, key, http.MethodPost, "/v1/convai/knowledge-base/text", map[string]string{
		"name": title,
		"text": text,
	}, &response)
	if err != nil {
		return "", err
	}
	if !validHostedID(response.ID) {
		return "", errors.New("ElevenLabs returned an invalid Knowledge Base document ID.")
	}
	return response.ID, nil
}

func computeHostedRAGIndex(ctx context.Context, client *http.Client, key, docID string) (string, error) {
	if !validHostedID(docID) {
		return "", errors.New("The tracked ElevenLabs Knowledge Base document ID is invalid.")
	}
	var response struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	err := hostedJSONRequest(
		ctx,
		client,
		key,
		http.MethodPost,
		"/v1/convai/knowledge-base/"+url.PathEscape(docID)+"/rag-index",
		map[string]string{"model": hostedRAGModel},
		&response,
	)
	if err != nil {
		return "", err
	}
	if response.ID != docID {
		return "", errors.New("ElevenLabs returned an unverifiable RAG index status.")
	}
	switch response.Status {
	case "succeeded":
		return "synced", nil
	case "new", "created", "processing":
		return "indexing", nil
	case "failed", "rag_limit_exceeded", "document_too_small", "cannot_index_folder":
		return "", fmt.Errorf("ElevenLabs RAG indexing failed with status %q.", response.Status)
	default:
		return "", errors.New("ElevenLabs returned an unknown RAG index status.")
	}
}

func deleteHostedKnowledgeBaseDocument(ctx context.Context, client *http.Client, key, docID string) error {
	if !validHostedID(docID) {
		return errors.New("The tracked ElevenLabs Knowledge Base document ID is invalid.")
	}
	path := "/v1/convai/knowledge-base/" + url.PathEscape(docID) + "?force=true"
	err := hostedJSONRequest(ctx, client, key, http.MethodDelete, path, nil, nil)
	var providerErr *hostedProviderError
	if errors.As(err, &providerErr) && providerErr.status == http.StatusNotFound {
		return nil
	}
	return err
}

func hostedKnowledgeBaseForSync(
	currentEntries []json.RawMessage,
	managedIDs map[string]struct{},
	docs []rag.Document,
	agentID string,
) ([]json.RawMessage, error) {
	entries := make([]json.RawMessage, 0, len(currentEntries)+len(docs))
	entryIDs := make(map[string]struct{}, len(currentEntries)+len(docs))
	for _, raw := range currentEntries {
		var entry hostedKnowledgeBaseEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, errors.New("ElevenLabs returned an invalid Knowledge Base entry.")
		}
		if _, managed := managedIDs[entry.ID]; managed {
			continue
		}
		entries = append(entries, raw)
		if entry.ID != "" {
			entryIDs[entry.ID] = struct{}{}
		}
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Title == docs[j].Title {
			return docs[i].ID < docs[j].ID
		}
		return docs[i].Title < docs[j].Title
	})
	for _, doc := range docs {
		if doc.Status != "ready" ||
			doc.ElevenLabsAgentID != agentID ||
			doc.ElevenLabsDocID == "" ||
			doc.ElevenLabsSyncStatus != "synced" && doc.ElevenLabsSyncStatus != "indexing" {
			continue
		}
		if _, exists := entryIDs[doc.ElevenLabsDocID]; exists {
			continue
		}
		entry, err := json.Marshal(hostedKnowledgeBaseEntry{
			ID:        doc.ElevenLabsDocID,
			Name:      doc.Title,
			Type:      "text",
			UsageMode: "auto",
		})
		if err != nil {
			return nil, errors.New("Could not encode the hosted Knowledge Base configuration.")
		}
		entries = append(entries, entry)
		entryIDs[doc.ElevenLabsDocID] = struct{}{}
	}
	return entries, nil
}

func hostedRAGSettings(raw json.RawMessage) (map[string]json.RawMessage, error) {
	settings := make(map[string]json.RawMessage)
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, errors.New("ElevenLabs returned invalid RAG settings.")
		}
	}
	if settings == nil {
		settings = make(map[string]json.RawMessage)
	}
	settings["enabled"] = json.RawMessage("true")
	return settings, nil
}

func (s *Server) syncHostedDocumentsIfActive(ctx context.Context) error {
	config, err := s.adminStore.GetAgentConfig(ctx)
	if err != nil {
		return errors.New("Could not load hosted configuration for document synchronization.")
	}
	if config == nil || config.Mode != "elevenlabs_agent" || !config.ElevenLabsRAGSyncEnabled {
		return nil
	}
	return s.syncHostedDocuments(ctx, config)
}

func (s *Server) syncHostedResources(ctx context.Context, config *AgentConfigRecord) error {
	if err := s.syncHostedPrompt(ctx, config); err != nil {
		return err
	}
	if err := s.syncHostedDocuments(ctx, config); err != nil {
		return err
	}
	if err := s.syncHostedMCP(ctx, config); err != nil {
		return err
	}
	return s.validateHostedSkillAvailability(ctx, config)
}

func (s *Server) cleanupDisabledHostedRAG(ctx context.Context, config *AgentConfigRecord) error {
	if s.ragSvc == nil {
		return errors.New("Local document storage is unavailable for hosted cleanup.")
	}
	docs, err := s.ragSvc.List(ctx)
	if err != nil {
		return errors.New("Could not load tracked hosted documents for cleanup.")
	}
	byAgent := make(map[string][]rag.Document)
	for _, doc := range docs {
		if doc.ElevenLabsDocID == "" {
			continue
		}
		if !validHostedID(doc.ElevenLabsAgentID) || !validHostedID(doc.ElevenLabsDocID) {
			return errors.New("Tracked hosted document ownership could not be verified. RAG synchronization remains unchanged.")
		}
		byAgent[doc.ElevenLabsAgentID] = append(byAgent[doc.ElevenLabsAgentID], doc)
	}
	if len(byAgent) == 0 {
		return nil
	}
	key, err := s.hostedKey(config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	client := newHostedClient(s.allowPrivate)
	agentIDs := make([]string, 0, len(byAgent))
	for id := range byAgent {
		agentIDs = append(agentIDs, id)
	}
	sort.Strings(agentIDs)
	for _, agentID := range agentIDs {
		tracked := byAgent[agentID]
		managedIDs := make(map[string]struct{}, len(tracked))
		for _, doc := range tracked {
			managedIDs[doc.ElevenLabsDocID] = struct{}{}
		}
		current, err := loadHostedAgent(ctx, client, key, agentID)
		if err != nil {
			return fmt.Errorf("Hosted RAG cleanup could not inspect the tracked agent: %w", err)
		}
		retained, err := hostedKnowledgeBaseForSync(current.Conversation.Agent.Prompt.KnowledgeBase, managedIDs, nil, agentID)
		if err != nil {
			return err
		}
		agentPath := "/v1/convai/agents/" + url.PathEscape(agentID)
		if len(retained) != len(current.Conversation.Agent.Prompt.KnowledgeBase) {
			if err := hostedPatch(ctx, client, key, agentPath, map[string]any{
				"conversation_config": map[string]any{
					"agent": map[string]any{
						"prompt": map[string]any{"knowledge_base": retained},
					},
				},
			}); err != nil {
				return fmt.Errorf("Hosted RAG cleanup could not detach tracked documents: %w", err)
			}
		}
		verified, err := loadHostedAgent(ctx, client, key, agentID)
		if err != nil {
			return fmt.Errorf("Hosted RAG detachment could not be verified: %w", err)
		}
		actual := make(map[string]bool)
		for _, raw := range verified.Conversation.Agent.Prompt.KnowledgeBase {
			var entry hostedKnowledgeBaseEntry
			if json.Unmarshal(raw, &entry) != nil {
				return errors.New("Hosted RAG cleanup returned an invalid attachment.")
			}
			if _, managed := managedIDs[entry.ID]; managed {
				return errors.New("Hosted RAG cleanup could not verify document detachment. Retry the configuration save.")
			}
			actual[entry.ID] = true
		}
		for _, raw := range retained {
			var entry hostedKnowledgeBaseEntry
			if json.Unmarshal(raw, &entry) != nil || !actual[entry.ID] {
				return errors.New("Hosted RAG cleanup could not verify preservation of dashboard attachments.")
			}
		}
		sort.Slice(tracked, func(i, j int) bool { return tracked[i].ID < tracked[j].ID })
		for i := range tracked {
			doc := &tracked[i]
			if err := deleteHostedKnowledgeBaseDocument(ctx, client, key, doc.ElevenLabsDocID); err != nil {
				return fmt.Errorf("Hosted RAG cleanup could not remove a tracked document. Retry the configuration save: %w", err)
			}
			err := hostedRequest(ctx, client, key, "/v1/convai/knowledge-base/"+url.PathEscape(doc.ElevenLabsDocID), nil)
			var providerErr *hostedProviderError
			if !errors.As(err, &providerErr) || providerErr.status != http.StatusNotFound {
				return errors.New("Hosted RAG cleanup could not verify tracked document removal. Retry the configuration save.")
			}
			if err := s.saveHostedSyncState(ctx, doc, "", "", "", "not_synced", nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) deleteTrackedHostedDocument(ctx context.Context, doc *rag.Document) error {
	if doc == nil || doc.ElevenLabsDocID == "" {
		return nil
	}
	config, err := s.adminStore.GetAgentConfig(ctx)
	if err != nil {
		return errors.New("Local document was deleted, but hosted configuration could not be loaded.")
	}
	if config == nil || len(config.ElevenLabsAPIKeyCiphertext) == 0 {
		return nil
	}
	key, err := s.hostedKey(config)
	if err != nil {
		return err
	}
	return deleteHostedKnowledgeBaseDocument(ctx, newHostedClient(s.allowPrivate), key, doc.ElevenLabsDocID)
}
