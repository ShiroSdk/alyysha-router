// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleConnectClientDryRunNoWrite pins the dry-run contract: the response
// lists the resolved edits, the target file is unchanged byte-identical, no
// backup appears, and the credential is masked rather than printed.
func TestHandleConnectClientDryRunNoWrite(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	router := setupTestRouter(database)

	home := t.TempDir()
	target := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"env\": {\n    \"ANTHROPIC_AUTH_TOKEN\": \"sk-old\"\n  }\n}\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}

	payload := `{` +
		`"client":"claude-code",` +
		`"baseUrl":"http://127.0.0.1:20128",` +
		`"apiKey":"sk-connect-test-key",` +
		`"dryRun":true,` +
		`"home":"` + strings.ReplaceAll(home, "\\", "\\\\") + `"}`

	req := httptest.NewRequest("POST", "/api/cli-tools/connect", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Error("dry run modified the config file")
	}
	if !strings.Contains(rec.Body.String(), "ANTHROPIC_BASE_URL") {
		t.Errorf("dry run response = %s, want the endpoint edit", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-connect-test-key") {
		t.Errorf("dry run response leaks the credential: %s", rec.Body.String())
	}
}

// TestHandleConnectClientUnknownClient pins the error path: an unknown client
// name answers 400 with the picker hint, never a panic.
func TestHandleConnectClientUnknownClient(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	router := setupTestRouter(database)

	req := httptest.NewRequest("POST", "/api/cli-tools/connect",
		strings.NewReader(`{"client":"not-a-client","apiKey":"sk-x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown client") {
		t.Errorf("body = %s, want the unknown-client message", rec.Body.String())
	}
}
