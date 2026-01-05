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
	FileContents  string `json:"file_contents"`
	Line          int32  `json:"line"`
	Column        int32  `json:"column"`
	FilePath      string `json:"file_path"`
	LanguageID    string `json:"language_id"`
	WorkspacePath string `json:"workspace_path"`
}

type SuggestionResponse struct {
	Suggestion   string      `json:"suggestion"`
	Error        string      `json:"error,omitempty"`
	RangeReplace *RangeInfo `json:"range_replace,omitempty"`
}

type RangeInfo struct {
	StartLine   int32 `json:"start_line"`
	StartColumn int32 `json:"start_column"`
	EndLine     int32 `json:"end_line"`
	EndColumn   int32 `json:"end_column"`
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
	log.Printf("REQUEST:\n%s", string(requestJSON))

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

	var suggestion strings.Builder
	var rangeToReplace *RangeInfo
	chunkCount := 0
	for stream.Receive() {
		resp := stream.Msg()
		chunkCount++

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
		if resp.RangeToReplace != nil {
			rangeToReplace = &RangeInfo{
				StartLine:   resp.RangeToReplace.StartLineNumber,
				StartColumn: 0, // Will be inferred from cursor position
				EndLine:     resp.RangeToReplace.EndLineNumberInclusive,
				EndColumn:   -1, // -1 means end of line
			}
		}

		if resp.Text != "" {
			suggestion.WriteString(resp.Text)
		}
	}

	if err := stream.Err(); err != nil && err != io.EOF {
		log.Printf("ERROR: Stream error: %v", err)
		json.NewEncoder(w).Encode(SuggestionResponse{Error: err.Error()})
		return
	}

	result := suggestion.String()

	// Log response info
	responseLog := map[string]interface{}{
		"suggestion_length": len(result),
		"suggestion_lines":  len(strings.Split(result, "\n")),
	}
	if rangeToReplace != nil {
		responseLog["range_start_line"] = rangeToReplace.StartLine
		responseLog["range_end_line"] = rangeToReplace.EndLine
	}
	if len(result) > 100 {
		responseLog["suggestion_preview"] = result[:100] + "..."
	} else {
		responseLog["suggestion_preview"] = result
	}
	responseJSON, _ := json.MarshalIndent(responseLog, "", "  ")
	log.Printf("RESPONSE:\n%s", string(responseJSON))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SuggestionResponse{
		Suggestion:   result,
		RangeReplace: rangeToReplace,
	})
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

	http.HandleFunc("/suggestion", handleSuggestion)

	port := "37292"
	log.Printf("SERVER: Listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
