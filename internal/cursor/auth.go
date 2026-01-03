package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func GetAccessToken() (string, error) {
	homeDir := os.Getenv("HOME")
	dbPath := fmt.Sprintf("%s/Library/Application Support/Cursor/User/globalStorage/state.vscdb", homeDir)

	cmd := exec.Command("sqlite3", dbPath, "SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken';")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error getting access token: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func GetMachineID() (string, error) {
	homeDir := os.Getenv("HOME")
	dbPath := fmt.Sprintf("%s/Library/Application Support/Cursor/User/globalStorage/state.vscdb", homeDir)

	cmd := exec.Command("sqlite3", dbPath, "SELECT value FROM ItemTable WHERE key = 'telemetry.macMachineId';")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error getting machine ID: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func GetCursorVersion() (string, error) {
	packagePath := "/Applications/Cursor.app/Contents/Resources/app/package.json"

	data, err := os.ReadFile(packagePath)
	if err != nil {
		return "0.45.0", nil
	}

	var pkg struct {
		Version string `json:"version"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return "0.45.0", nil
	}

	if pkg.Version == "" {
		return "0.45.0", nil
	}

	return pkg.Version, nil
}
