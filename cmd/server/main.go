package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"connectrpc.com/connect"
	aiserverv1 "github.com/bengu3/cursor-tab.nvim/cursor-api/gen/aiserver/v1"
	"github.com/bengu3/cursor-tab.nvim/internal/cursor"
	"github.com/bengu3/cursor-tab.nvim/internal/suggestionstore"
)

var cursorClient *cursor.Client
var store = suggestionstore.NewStore()
var suggestionCounter int

type NewSuggestionRequest struct {
	FileContents  string `json:"file_contents"`
	Line          int32  `json:"line"`
	Column        int32  `json:"column"`
	FilePath      string `json:"file_path"`
	LanguageID    string `json:"language_id"`
	WorkspacePath string `json:"workspace_path"`
}

type SuggestionResponse struct {
	Suggestion             string                 `json:"suggestion"`
	Error                  string                 `json:"error,omitempty"`
	RangeReplace           *suggestionstore.RangeInfo   `json:"range_replace,omitempty"`
	NextSuggestionID       string                 `json:"next_suggestion_id,omitempty"`
	BindingID              string                 `json:"binding_id,omitempty"`
	ShouldRemoveLeadingEol bool                   `json:"should_remove_leading_eol,omitempty"`
}

func handleNewSuggestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req NewSuggestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding request: %v", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	// Log request params as pretty-printed JSON
	requestLog := map[string]interface{}{
		"file_path":      req.FilePath,
		"line":           req.Line,
		"column":         req.Column,
		"language_id":    req.LanguageID,
		"workspace_path": req.WorkspacePath,
		"content_length": len(req.FileContents),
	}
	requestJSON, _ := json.MarshalIndent(requestLog, "", "  ")
	log.Printf("NEW SUGGESTION REQUEST:\n%s", string(requestJSON))

	if cursorClient == nil {
		json.NewEncoder(w).Encode(SuggestionResponse{Error: "cursor client not initialized"})
		return
	}

	lines := strings.Split(req.FileContents, "\n")
	totalLines := int32(len(lines))

	giveDebug := true
	supportsCpt := true
	supportsCrlfCpt := true
	streamReq := &aiserverv1.StreamCppRequest{
		CurrentFile: &aiserverv1.CurrentFileInfo{
			Contents:              req.FileContents,
			RelativeWorkspacePath: req.FilePath,
			LanguageId:            req.LanguageID,
			TotalNumberOfLines:    totalLines,
			WorkspaceRootPath:     req.WorkspacePath,
			CursorPosition: &aiserverv1.CursorPosition{
				Line:   req.Line,
				Column: req.Column,
			},
		},
		CppIntentInfo: &aiserverv1.CppIntentInfo{
			Source: "typing",
		},
		SupportsCpt:     &supportsCpt,
		SupportsCrlfCpt: &supportsCrlfCpt,
		GiveDebugOutput: &giveDebug,
	}

	ctx := context.Background()
	stream, err := cursorClient.StreamCpp(ctx, streamReq)
	if err != nil {
		log.Printf("ERROR: %v", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	// Parse stream into suggestions
	suggestions, err := parseSuggestions(stream)
	if err != nil {
		log.Printf("ERROR: %v", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	if len(suggestions) == 0 {
		json.NewEncoder(w).Encode(SuggestionResponse{Error: "no suggestions returned"})
		return
	}

	// Pre-allocate IDs for all suggestions
	suggestionIDs := make([]string, len(suggestions))
	for i := 0; i < len(suggestions); i++ {
		suggestionCounter++
		suggestionIDs[i] = fmt.Sprintf("sugg_%d", suggestionCounter)
	}

	// Link suggestions together and store them
	for i := 0; i < len(suggestions); i++ {
		// Link to next suggestion (or empty if last)
		if i < len(suggestions)-1 {
			suggestions[i].NextSuggestionID = suggestionIDs[i+1]
		}

		// Store each suggestion with its own ID (except first, which we return directly)
		if i > 0 {
			store.Store(suggestionIDs[i], suggestions[i])
			log.Printf("Stored suggestion #%d with ID: %s, next_id: %s, chars: %d",
				i+1, suggestionIDs[i], suggestions[i].NextSuggestionID, len(suggestions[i].Text))
		}
	}

	// Return only the first suggestion
	firstSuggestion := suggestions[0]

	response := SuggestionResponse{
		Suggestion:             firstSuggestion.Text,
		RangeReplace:           firstSuggestion.Range,
		BindingID:              firstSuggestion.BindingID,
		ShouldRemoveLeadingEol: firstSuggestion.ShouldRemoveLeadingEol,
	}

	if len(suggestions) > 1 {
		response.NextSuggestionID = firstSuggestion.NextSuggestionID
	}

	// Log response info
	responseLog := map[string]interface{}{
		"suggestion_length":  len(firstSuggestion.Text),
		"suggestion_lines":   len(strings.Split(firstSuggestion.Text, "\n")),
		"total_suggestions":  len(suggestions),
	}
	if firstSuggestion.Range != nil {
		responseLog["range_start_line"] = firstSuggestion.Range.StartLine
		responseLog["range_end_line"] = firstSuggestion.Range.EndLine
	}
	if response.NextSuggestionID != "" {
		responseLog["next_suggestion_id"] = response.NextSuggestionID
	}
	if len(firstSuggestion.Text) > 100 {
		responseLog["suggestion_preview"] = firstSuggestion.Text[:100] + "..."
	} else {
		responseLog["suggestion_preview"] = firstSuggestion.Text
	}
	responseJSON, _ := json.MarshalIndent(responseLog, "", "  ")
	log.Printf("RESPONSE:\n%s", string(responseJSON))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func parseSuggestions(stream *connect.ServerStreamForClient[aiserverv1.StreamCppResponse]) ([]*suggestionstore.Suggestion, error) {
	var suggestions []*suggestionstore.Suggestion
	var currentSuggestion *suggestionstore.Suggestion
	chunkCount := 0

	for stream.Receive() {
		resp := stream.Msg()
		chunkCount++

		// Log entire response object structure
		log.Printf("CHUNK %d:\n%+v", chunkCount, resp)

		// Log debug information if available
		if resp.DebugModelInput != nil || resp.DebugModelOutput != nil {
			debugLog := map[string]interface{}{}
			if resp.DebugModelInput != nil {
				debugLog["model_input"] = *resp.DebugModelInput
			}
			if resp.DebugModelOutput != nil {
				debugLog["model_output"] = *resp.DebugModelOutput
			}
			debugJSON, _ := json.MarshalIndent(debugLog, "", "  ")
			log.Printf("DEBUG:\n%s", string(debugJSON))
		}

		// Handle different chunk types
		if resp.RangeToReplace != nil {
			if currentSuggestion == nil {
				currentSuggestion = &suggestionstore.Suggestion{}
			}
			currentSuggestion.Range = &suggestionstore.RangeInfo{
				StartLine:   resp.RangeToReplace.StartLineNumber,
				StartColumn: 0,
				EndLine:     resp.RangeToReplace.EndLineNumberInclusive,
				EndColumn:   -1,
			}
			if resp.BindingId != nil {
				currentSuggestion.BindingID = *resp.BindingId
			}
			if resp.ShouldRemoveLeadingEol != nil {
				currentSuggestion.ShouldRemoveLeadingEol = *resp.ShouldRemoveLeadingEol
			}
		}

		if resp.Text != "" {
			if currentSuggestion == nil {
				currentSuggestion = &suggestionstore.Suggestion{}
			}
			currentSuggestion.Text += resp.Text
		}

		// Done with current suggestion
		if resp.DoneEdit != nil && *resp.DoneEdit {
			if currentSuggestion != nil {
				suggestions = append(suggestions, currentSuggestion)
				log.Printf("Completed suggestion #%d: %d chars, range %v", len(suggestions), len(currentSuggestion.Text), currentSuggestion.Range)
				currentSuggestion = nil
			}
		}

		// Beginning new suggestion
		if resp.BeginEdit != nil && *resp.BeginEdit {
			log.Printf("Beginning new suggestion...")
		}

		// Stream is done
		if resp.DoneStream != nil && *resp.DoneStream {
			log.Printf("Stream complete")
			break
		}
	}

	if err := stream.Err(); err != nil && err != io.EOF {
		return nil, fmt.Errorf("stream error: %w", err)
	}

	log.Printf("Parsed %d total suggestions", len(suggestions))
	return suggestions, nil
}

func handleGetSuggestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract ID from path: /suggestion/{id}
	suggestionID := strings.TrimPrefix(r.URL.Path, "/suggestion/")
	if suggestionID == "" || suggestionID == r.URL.Path {
		json.NewEncoder(w).Encode(SuggestionResponse{Error: "suggestion ID required"})
		return
	}

	log.Printf("GET suggestion request for ID: %s", suggestionID)

	// Get suggestion from store
	suggestion := store.Get(suggestionID)
	if suggestion == nil {
		json.NewEncoder(w).Encode(SuggestionResponse{Error: "suggestion not found"})
		return
	}

	response := SuggestionResponse{
		Suggestion:             suggestion.Text,
		RangeReplace:           suggestion.Range,
		BindingID:              suggestion.BindingID,
		ShouldRemoveLeadingEol: suggestion.ShouldRemoveLeadingEol,
		NextSuggestionID:       suggestion.NextSuggestionID,
	}

	// Delete this suggestion from store (already retrieved)
	store.Delete(suggestionID)

	log.Printf("Returning stored suggestion: %d chars, next_id=%s", len(suggestion.Text), suggestion.NextSuggestionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	logFile, err := os.OpenFile("/tmp/cursor-tab.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	cursorClient, err = cursor.NewClient()
	if err != nil {
		log.Printf("ERROR: Failed to initialize Cursor client: %v", err)
	}

	// POST /suggestion/new - generate new suggestions from Cursor
	http.HandleFunc("/suggestion/new", handleNewSuggestion)

	// GET /suggestion/{id} - retrieve existing suggestion from store
	http.HandleFunc("/suggestion/", handleGetSuggestion)

	port := "37292"
	log.Printf("SERVER: Listening on :%s", port)
	log.Printf("  POST /suggestion/new - generate new suggestions")
	log.Printf("  GET  /suggestion/{id} - retrieve stored suggestion")
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
