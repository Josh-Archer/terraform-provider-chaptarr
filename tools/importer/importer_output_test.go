package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOutputPermissionsAndOverwriteControl(t *testing.T) {
	secret := fakeCanary("output-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/tag" {
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 1, "label": "fiction"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("CHAPTARR_URL", "")
	t.Setenv("CHAPTARR_API_KEY", "")
	outDir := filepath.Join(t.TempDir(), "secure-out")
	args := []string{"-url", server.URL, "-api-key", secret, "-out-dir", outDir, "-format", "all"}

	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("first run exit %d; stderr:\n%s", code, stderr.String())
	}
	info, err := os.Stat(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("out-dir mode = %04o, want 0700", perm)
	}
	want := map[string]os.FileMode{"main.tf": 0o600, "imports.tf": 0o600, "import.sh": 0o700}
	for name, mode := range want {
		path := filepath.Join(outDir, name)
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := fileInfo.Mode().Perm(); perm != mode {
			t.Errorf("%s mode = %04o, want %04o", name, perm, mode)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), secret) {
			t.Errorf("API key leaked into %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "variables.tf")); err == nil {
		t.Fatal("variables.tf was written for a fixture with no secrets")
	}
	if strings.Contains(stdout.String()+stderr.String(), secret) {
		t.Errorf("API key leaked into CLI output:\n%s%s", stdout.String(), stderr.String())
	}

	mainPath := filepath.Join(outDir, "main.tf")
	if err := os.WriteFile(mainPath, []byte("# edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(args, &stdout, &stderr); code != 1 {
		t.Errorf("rerun without -overwrite exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "refusing to overwrite existing file") {
		t.Errorf("missing refusal message; stderr:\n%s", stderr.String())
	}
	if body, _ := os.ReadFile(mainPath); string(body) != "# edited\n" {
		t.Errorf("main.tf was modified despite refusal")
	}

	if err := os.Chmod(mainPath, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(append(append([]string{}, args...), "-overwrite"), &stdout, &stderr); code != 0 {
		t.Fatalf("rerun with -overwrite exit %d; stderr:\n%s", code, stderr.String())
	}
	if info, err := os.Stat(mainPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("main.tf mode after overwrite = %v, want 0600", info)
	}
	if code := run(append(append([]string{}, args...), "-force"), &stdout, &stderr); code != 0 {
		t.Fatalf("rerun with -force exit %d; stderr:\n%s", code, stderr.String())
	}
}

func TestCLIAPIErrorPathDoesNotLeakCredentials(t *testing.T) {
	apiKey := fakeCanary("api-key-auth-fail")
	echoed := fakeCanary("error-echoed-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "bad key " + apiKey, "token": echoed})
	}))
	defer server.Close()

	t.Setenv("CHAPTARR_URL", "")
	t.Setenv("CHAPTARR_API_KEY", "")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-url", server.URL, "-api-key", apiKey, "-out-dir", t.TempDir()}, &stdout, &stderr); code == 0 {
		t.Fatal("expected nonzero exit on server error")
	}
	out := stdout.String() + stderr.String()
	for _, secret := range []string{apiKey, echoed} {
		if strings.Contains(out, secret) {
			t.Errorf("credential leaked into CLI output:\n%s", out)
		}
	}
	if strings.Contains(out, "bad key") || strings.Contains(out, "token") {
		t.Errorf("response body leaked into CLI output:\n%s", out)
	}
}

func TestInvalidFormatExitsBeforeNetwork(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer server.Close()

	t.Setenv("CHAPTARR_URL", server.URL)
	t.Setenv("CHAPTARR_API_KEY", fakeCanary("unused-key"))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-format", "nope"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d, want 2; stderr:\n%s", code, stderr.String())
	}
	if hits.Load() != 0 {
		t.Fatalf("invalid format made %d HTTP requests", hits.Load())
	}
	if !strings.Contains(stderr.String(), "invalid output format") {
		t.Fatalf("stderr:\n%s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-url", server.URL, "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unexpected argument exit %d, want 2", code)
	}
	if hits.Load() != 0 {
		t.Fatalf("unexpected argument made %d HTTP requests", hits.Load())
	}
}

func TestEnvURLAndAPIKeyAreSentWhenFlagsAreOmitted(t *testing.T) {
	apiKey := fakeCanary("env-api-key")
	var got atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/tag" {
			got.Store(r.Header.Get("X-Api-Key"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 4, "label": "env"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("CHAPTARR_URL", server.URL)
	t.Setenv("CHAPTARR_API_KEY", apiKey)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-out-dir", t.TempDir(), "-format", "hcl"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	sent, _ := got.Load().(string)
	if sent != apiKey {
		t.Fatalf("X-Api-Key = %q", sent)
	}
	if strings.Contains(stdout.String()+stderr.String(), apiKey) {
		t.Fatal("API key leaked into CLI output")
	}
}

func TestRedirectDoesNotForwardAPIKey(t *testing.T) {
	apiKey := fakeCanary("redirect-api-key")
	var steal atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/steal") {
			steal.Add(1)
			if r.Header.Get("X-Api-Key") != "" {
				t.Errorf("API key was forwarded to %s", r.URL.Path)
			}
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", server.URL+"/steal")
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte(`{"message":"` + apiKey + `"}`))
	}))
	defer server.Close()

	t.Setenv("CHAPTARR_URL", "")
	t.Setenv("CHAPTARR_API_KEY", "")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-url", server.URL, "-api-key", apiKey, "-out-dir", t.TempDir()}, &stdout, &stderr); code == 0 {
		t.Fatal("expected discovery to fail on redirect")
	}
	if steal.Load() != 0 {
		t.Fatalf("redirect target was requested %d times", steal.Load())
	}
	if strings.Contains(stdout.String()+stderr.String(), apiKey) {
		t.Fatalf("API key leaked into CLI output:\n%s%s", stdout.String(), stderr.String())
	}
}
