package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	aiserverv1 "github.com/bengu3/cursor-tab.nvim/cursor-api/gen/aiserver/v1"
	"github.com/bengu3/cursor-tab.nvim/internal/cursor"
)

var cursorClient *cursor.Client

type SuggestionRequest struct {
	FileContents string `json:"file_contents"`
	Line         int32  `json:"line"`
	Column       int32  `json:"column"`
	FilePath     string `json:"file_path"`
	LanguageID   string `json:"language_id"`
}

type SuggestionResponse struct {
	Suggestion string `json:"suggestion"`
	Error      string `json:"error,omitempty"`
}

func handleSuggestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SuggestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding request: %v", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	log.Printf("Getting suggestion for %s at line=%d col=%d", req.FilePath, req.Line, req.Column)
	log.Printf("File contents length: %d bytes", len(req.FileContents))
	log.Printf("Language: %s", req.LanguageID)

	if cursorClient == nil {
		json.NewEncoder(w).Encode(SuggestionResponse{Error: "cursor client not initialized"})
		return
	}

	lines := strings.Split(req.FileContents, "\n")
	totalLines := int32(len(lines))

	streamReq := &aiserverv1.StreamCppRequest{
		CurrentFile: &aiserverv1.CurrentFileInfo{
			Contents:              req.FileContents,
			RelativeWorkspacePath: req.FilePath,
			LanguageId:            req.LanguageID,
			TotalNumberOfLines:    totalLines,
			CursorPosition: &aiserverv1.CursorPosition{
				Line:   req.Line,
				Column: req.Column,
			},
		},
	}

	log.Printf("Calling StreamCpp API...")
	ctx := context.Background()
	stream, err := cursorClient.StreamCpp(ctx, streamReq)
	if err != nil {
		log.Printf("Error calling StreamCpp: %v", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	var suggestion strings.Builder
	chunkCount := 0
	for stream.Receive() {
		resp := stream.Msg()
		chunkCount++
		log.Printf("Chunk %d: text=%q, done=%v", chunkCount, resp.Text, resp.DoneStream)
		if resp.Text != "" {
			suggestion.WriteString(resp.Text)
		}
	}

	if err := stream.Err(); err != nil && err != io.EOF {
		log.Printf("Stream error: %v", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	result := suggestion.String()
	log.Printf("Final suggestion (%d chunks): %q", chunkCount, result)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SuggestionResponse{Suggestion: result})
}

func main() {
	logFile, err := os.OpenFile("/tmp/cursor-tab.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	log.Println("Starting cursor-tab HTTP server...")

	cursorClient, err = cursor.NewClient()
	if err != nil {
		log.Printf("Warning: Failed to initialize Cursor client: %v", err)
	} else {
		log.Println("Cursor API client initialized")
	}

	http.HandleFunc("/suggestion", handleSuggestion)

	port := "37292"
	log.Printf("HTTP server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
