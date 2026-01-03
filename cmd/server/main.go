package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	aiserverv1 "github.com/bengu3/cursor-tab.nvim/cursor-api/gen/aiserver/v1"
	"github.com/bengu3/cursor-tab.nvim/internal/cursor"
	"github.com/neovim/go-client/nvim"
)

var cursorClient *cursor.Client

type SuggestionRequest struct {
	FileContents string `msgpack:"file_contents"`
	Line         int32  `msgpack:"line"`
	Column       int32  `msgpack:"column"`
	FilePath     string `msgpack:"file_path"`
	LanguageID   string `msgpack:"language_id"`
}

func getSuggestion(req *SuggestionRequest) (string, error) {
	if req == nil || req.FileContents == "" {
		return "", nil
	}

	log.Printf("Getting suggestion for %s at line=%d col=%d", req.FilePath, req.Line, req.Column)
	log.Printf("File contents length: %d bytes", len(req.FileContents))
	log.Printf("Language: %s", req.LanguageID)

	if cursorClient == nil {
		return "", fmt.Errorf("cursor client not initialized")
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
		return "", err
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
		return "", err
	}

	result := suggestion.String()
	log.Printf("Final suggestion (%d chunks): %q", chunkCount, result)
	return result, nil
}

func main() {
	logFile, err := os.OpenFile("/tmp/cursor-tab.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	log.Println("Starting cursor-tab RPC server...")

	cursorClient, err = cursor.NewClient()
	if err != nil {
		log.Printf("Warning: Failed to initialize Cursor client: %v", err)
	} else {
		log.Println("Cursor API client initialized")
	}

	v, err := nvim.New(os.Stdin, os.Stdout, os.Stdout, log.Printf)
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()

	v.RegisterHandler("get_suggestion", getSuggestion)

	log.Println("RPC handlers registered, serving...")

	if err := v.Serve(); err != nil {
		log.Printf("Error serving: %v", err)
		os.Exit(1)
	}
}
