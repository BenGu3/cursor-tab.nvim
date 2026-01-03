package main

import (
	"log"
	"os"
	"strings"

	"github.com/neovim/go-client/nvim"
)

var suggestions = map[string]string{
	"function": " hello() {\n\treturn 'Hello, World!';\n}",
	"console":  ".log('Debug:', );",
	"const":    " result = ",
	"if":       " (condition) {\n\t// TODO\n}",
}

func getSuggestion(lineText string) (string, error) {
	if lineText == "" {
		return "", nil
	}

	for trigger, completion := range suggestions {
		if strings.HasSuffix(lineText, trigger) {
			return completion, nil
		}
	}

	return "", nil
}

func main() {
	logFile, err := os.OpenFile("/tmp/cursor-tab.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	log.Println("Starting cursor-tab RPC server...")

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
