// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package backup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"alyysha/proxy/internal/db"
)

// remoteFixture builds a manager over a real fixture repo and overrides both
// API bases at local test servers, so no push ever leaves the process.
func remoteFixture(t *testing.T) (*Manager, *db.Repo, string) {
	t.Helper()
	database, err := db.OpenDatabase(filepath.Join(t.TempDir(), "data.sqlite"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.EnsureCoreSchema(database); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	repo := db.NewRepo(database)
	m := NewManager(repo)
	dir := t.TempDir()
	t.Cleanup(func() {
		tgAPIBase = "https://api.telegram.org"
		ghAPIBase = "https://api.github.com"
	})
	return m, repo, dir
}

func (m *Manager) fakeResult(dir string) *BackupResult {
	file := filepath.Join(dir, "backup-x.json")
	_ = os.WriteFile(file, []byte(`{"export":true}`), 0600)
	return &BackupResult{JSONPath: file, CreatedAt: time.Now()}
}

// TestPushRemoteTelegram proves the document lands on the chat endpoint and
// the success is recorded on the status row.
func TestPushRemoteTelegram(t *testing.T) {
	m, repo, dir := remoteFixture(t)
	var gotChat, gotDoc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/botTESTTOKEN/sendDocument") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 22); err != nil {
			t.Errorf("multipart: %v", err)
		}
		gotChat = r.FormValue("chat_id")
		f, _, _ := r.FormFile("document")
		if f != nil {
			b, _ := io.ReadAll(f)
			gotDoc = string(b)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tgAPIBase = srv.URL

	cfg := db.AutoBackupConfig{Remote: db.AutoBackupRemote{
		Kind: db.RemoteKindTelegram, TelegramToken: "TESTTOKEN", TelegramChat: "12345",
	}}
	kind, status, errMsg := m.PushRemote(context.Background(), cfg, m.fakeResult(dir))
	if kind != "telegram" || status != "success" || errMsg != "" {
		t.Fatalf("push = %s/%s/%q", kind, status, errMsg)
	}
	if gotChat != "12345" || gotDoc != `{"export":true}` {
		t.Errorf("form chat=%q doc=%q", gotChat, gotDoc)
	}
	st, _ := repo.GetAutoBackupStatus(context.Background())
	if st.LastRemoteStatus != "success" || st.LastRemoteKind != "telegram" {
		t.Errorf("status recorded %+v", st)
	}
}

// TestPushRemoteGithubCreateThenUpdate proves the first push creates the file
// and the second carries the existing sha the API demands.
func TestPushRemoteGithubCreateThenUpdate(t *testing.T) {
	m, _, dir := remoteFixture(t)
	var mu sync.Mutex
	sha := ""
	putBodies := []map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			mu.Lock()
			cur := sha
			mu.Unlock()
			if cur == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": cur})
			return
		}
		if r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			putBodies = append(putBodies, body)
			sha = "abc123"
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			return
		}
		t.Errorf("unexpected method %s", r.Method)
	}))
	defer srv.Close()
	ghAPIBase = srv.URL

	cfg := db.AutoBackupConfig{Remote: db.AutoBackupRemote{
		Kind: db.RemoteKindGithub, GithubToken: "T", GithubRepo: "o/r", GithubBranch: "main", GithubPath: "backups/x.json",
	}}
	if _, status, _ := m.PushRemote(context.Background(), cfg, m.fakeResult(dir)); status != "success" {
		t.Fatalf("first push status %s", status)
	}
	if _, status, _ := m.PushRemote(context.Background(), cfg, m.fakeResult(dir)); status != "success" {
		t.Fatalf("second push status %s", status)
	}
	if len(putBodies) != 2 {
		t.Fatalf("puts = %d, want 2", len(putBodies))
	}
	if putBodies[0]["sha"] != nil {
		t.Errorf("create carried a sha: %v", putBodies[0]["sha"])
	}
	if putBodies[1]["sha"] != "abc123" {
		t.Errorf("update missed the sha: %v", putBodies[1]["sha"])
	}
}

// TestPushRemoteErrorRecorded proves a dead remote records the failure and
// never fails the caller — the local backup is already durable.
func TestPushRemoteErrorRecorded(t *testing.T) {
	m, repo, dir := remoteFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	tgAPIBase = srv.URL

	cfg := db.AutoBackupConfig{Remote: db.AutoBackupRemote{
		Kind: db.RemoteKindTelegram, TelegramToken: "T", TelegramChat: "1",
	}}
	kind, status, errMsg := m.PushRemote(context.Background(), cfg, m.fakeResult(dir))
	if status != "error" || errMsg == "" {
		t.Fatalf("push = %s/%s/%q, want error", kind, status, errMsg)
	}
	st, _ := repo.GetAutoBackupStatus(context.Background())
	if st.LastRemoteStatus != "error" || st.LastRemoteError == "" {
		t.Errorf("status recorded %+v", st)
	}
}

// TestPushRemoteUnarmed proves an unconfigured remote is a no-op.
func TestPushRemoteUnarmed(t *testing.T) {
	m, _, dir := remoteFixture(t)
	kind, status, _ := m.PushRemote(context.Background(), db.AutoBackupConfig{}, m.fakeResult(dir))
	if kind != "" || status != "" {
		t.Errorf("unarmed remote pushed: %s/%s", kind, status)
	}
}
