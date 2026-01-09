package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
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
var logger *slog.Logger

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
		logger.Error("Error decoding request", "error", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	logger.Info("New suggestion request",
		"file_path", req.FilePath,
		"line", req.Line,
		"column", req.Column,
		"language_id", req.LanguageID,
		"workspace_path", req.WorkspacePath,
		"content_length", len(req.FileContents),
	)

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
		logger.Error("Failed to stream from Cursor API", "error", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	// Parse stream into suggestions
	suggestions, err := parseSuggestions(stream)
	if err != nil {
		logger.Error("Failed to parse suggestions from stream", "error", err)
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
			logger.Info("Stored suggestion",
				"index", i+1,
				"suggestion_id", suggestionIDs[i],
				"next_suggestion_id", suggestions[i].NextSuggestionID,
				"chars", len(suggestions[i].Text),
			)
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

	// Build log attributes
	logAttrs := []any{
		"suggestion_length", len(firstSuggestion.Text),
		"suggestion_lines", len(strings.Split(firstSuggestion.Text, "\n")),
		"total_suggestions", len(suggestions),
	}
	if firstSuggestion.Range != nil {
		logAttrs = append(logAttrs, "range_start_line", firstSuggestion.Range.StartLine)
		logAttrs = append(logAttrs, "range_end_line", firstSuggestion.Range.EndLine)
	}
	if response.NextSuggestionID != "" {
		logAttrs = append(logAttrs, "next_suggestion_id", response.NextSuggestionID)
	}
	if len(firstSuggestion.Text) > 100 {
		logAttrs = append(logAttrs, "suggestion_preview", firstSuggestion.Text[:100]+"...")
	} else {
		logAttrs = append(logAttrs, "suggestion_preview", firstSuggestion.Text)
	}
	logger.Info("Sending response", logAttrs...)

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
		logger.Debug("Received stream chunk", "chunk_number", chunkCount, "response", fmt.Sprintf("%+v", resp))

		// Log debug information if available
		if resp.DebugModelInput != nil || resp.DebugModelOutput != nil {
			debugAttrs := []any{}
			if resp.DebugModelInput != nil {
				debugAttrs = append(debugAttrs, "model_input", *resp.DebugModelInput)
			}
			if resp.DebugModelOutput != nil {
				debugAttrs = append(debugAttrs, "model_output", *resp.DebugModelOutput)
			}
			logger.Debug("Model debug info", debugAttrs...)
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
				logger.Info("Completed suggestion",
					"index", len(suggestions),
					"chars", len(currentSuggestion.Text),
					"range", currentSuggestion.Range,
				)
				currentSuggestion = nil
			}
		}

		// Beginning new suggestion
		if resp.BeginEdit != nil && *resp.BeginEdit {
			logger.Debug("Beginning new suggestion")
		}

		// Stream is done
		if resp.DoneStream != nil && *resp.DoneStream {
			logger.Debug("Stream complete")
			break
		}
	}

	if err := stream.Err(); err != nil && err != io.EOF {
		return nil, fmt.Errorf("stream error: %w", err)
	}

	logger.Info("Parsed suggestions from stream", "total_suggestions", len(suggestions))
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

	logger.Info("Get suggestion request", "suggestion_id", suggestionID)

	// Get suggestion from store
	suggestion := store.Get(suggestionID)
	if suggestion == nil {
		logger.Warn("Suggestion not found in store", "suggestion_id", suggestionID)
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

	logger.Info("Returning stored suggestion",
		"suggestion_id", suggestionID,
		"chars", len(suggestion.Text),
		"next_suggestion_id", suggestion.NextSuggestionID,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	// Parse command-line flags
	port := flag.Int("port", 0, "Port to listen on (0 = OS assigns available port)")
	flag.Parse()

	// Set up structured logging
	logFile, err := os.OpenFile("/tmp/cursor-tab.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	// Create JSON handler for structured logging
	logger = slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cursorClient, err = cursor.NewClient()
	if err != nil {
		logger.Error("Failed to initialize Cursor client", "error", err)
	}

	// POST /suggestion/new - generate new suggestions from Cursor
	http.HandleFunc("/suggestion/new", handleNewSuggestion)

	// GET /suggestion/{id} - retrieve existing suggestion from store
	http.HandleFunc("/suggestion/", handleGetSuggestion)

	// Create listener to get actual port
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *port))
	if err != nil {
		logger.Error("Failed to create listener", "error", err)
		os.Exit(1)
	}

	// Get the actual port that was assigned
	serverPort := listener.Addr().(*net.TCPAddr).Port

	// Add port to logger context for all subsequent logs
	logger = logger.With("port", serverPort)

	// Print port to stdout for Lua to parse (before any other output)
	fmt.Printf("SERVER_PORT=%d\n", serverPort)

	logger.Info("Server starting",
		"address", fmt.Sprintf("localhost:%d", serverPort),
		"endpoints", []string{
			"POST /suggestion/new",
			"GET /suggestion/{id}",
		},
	)

	if err := http.Serve(listener, nil); err != nil {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}
}
