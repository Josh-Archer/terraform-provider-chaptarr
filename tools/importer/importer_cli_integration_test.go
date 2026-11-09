package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	providerOnce sync.Once
	providerDir  string
	providerErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if providerDir != "" {
		_ = os.RemoveAll(providerDir)
	}
	os.Exit(code)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate integration test source")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(sourceFile), "../.."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func cliTools(t *testing.T) []string {
	t.Helper()
	var tools []string
	for _, candidate := range []string{"tofu", "terraform"} {
		if _, err := exec.LookPath(candidate); err == nil {
			tools = append(tools, candidate)
		}
	}
	if len(tools) == 0 {
		t.Skip("requires OpenTofu or Terraform; CI installs OpenTofu")
	}
	return tools
}

func localProviderDir(t *testing.T) string {
	t.Helper()
	providerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "chaptarr-provider-*")
		if err != nil {
			providerErr = err
			return
		}
		binary := filepath.Join(dir, "terraform-provider-chaptarr")
		cmd := exec.Command("go", "build", "-o", binary, ".")
		cmd.Dir = repoRoot(t)
		if output, err := cmd.CombinedOutput(); err != nil {
			providerErr = fmt.Errorf("build provider: %w\n%s", err, output)
			return
		}
		providerDir = dir
	})
	if providerErr != nil {
		t.Fatal(providerErr)
	}
	return providerDir
}

func writeDevOverride(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "terraform.rc")
	body := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    \"josh-archer/chaptarr\" = %q\n  }\n  direct {}\n}\n", dir)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCLI(t *testing.T, ctx context.Context, tool, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", tool, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func TestImporterCLIOutputValidates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	tools := cliTools(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/config/ui" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "theme": "dark"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("CHAPTARR_URL", "")
	t.Setenv("CHAPTARR_API_KEY", "")
	outputDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-url", server.URL, "-out-dir", outputDir, "-format", "hcl"}, &stdout, &stderr); code != 0 {
		t.Fatalf("importer exit %d\n%s%s", code, stdout.String(), stderr.String())
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.tf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `version = "~> 0.11"`) {
		t.Fatalf("generated provider configuration:\n%s", generated)
	}

	cliConfig := writeDevOverride(t, localProviderDir(t))
	env := mergeEnv(os.Environ(), []string{"TF_CLI_CONFIG_FILE=" + cliConfig, "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1"})
	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			if !cliAtLeast(t, tool, 1, 11, 2) {
				t.Skipf("%s is older than the generated required_version >= 1.11.2", tool)
			}
			work := t.TempDir()
			if err := os.WriteFile(filepath.Join(work, "main.tf"), generated, 0o600); err != nil {
				t.Fatal(err)
			}
			runCLI(t, ctx, tool, work, env, "init", "-input=false", "-no-color")
			runCLI(t, ctx, tool, work, env, "validate", "-no-color")
		})
	}
}

func TestImporterImportPreservesLiveSettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	tools := cliTools(t)

	live := newPopulatedChaptarr(t)
	defer live.Close()
	t.Setenv("CHAPTARR_URL", "")
	t.Setenv("CHAPTARR_API_KEY", "")

	outputDir := t.TempDir()
	importerKey := fakeCanary("importer-key")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-url", live.URL, "-api-key", importerKey, "-out-dir", outputDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("importer exit %d\n%s%s", code, stdout.String(), stderr.String())
	}
	for _, call := range live.Calls() {
		if strings.Contains(call.Path, "preserved") || strings.Contains(call.Path, "postgres") {
			t.Fatalf("discovery requested %s %s", call.Method, call.Path)
		}
	}
	live.Reset()

	generated := map[string]string{}
	for _, name := range []string{"main.tf", "variables.tf", "imports.tf", "import.sh"} {
		body, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		generated[name] = string(body)
	}
	tfCanary := fakeCanary("tf-supplied")
	forbidden := append(live.Canaries.values(), tfCanary, "Example Narrator")
	combinedCLI := stdout.String() + stderr.String()
	for _, secret := range forbidden {
		if strings.Contains(combinedCLI, secret) {
			t.Fatalf("importer output included %s", secret)
		}
		for name, body := range generated {
			if strings.Contains(body, secret) {
				t.Fatalf("%s included %s", name, secret)
			}
		}
	}
	if strings.Contains(generated["main.tf"], "chaptarr_postgres_database") && strings.Contains(generated["main.tf"], `resource "chaptarr_postgres_database"`) {
		t.Fatal("postgres database was emitted")
	}
	if strings.Contains(generated["main.tf"], `resource "chaptarr_hardcover_config"`) {
		t.Fatal("hardcover config was emitted")
	}
	if !strings.Contains(generated["main.tf"], "chaptarr_postgres_database") || !strings.Contains(generated["main.tf"], "chaptarr_hardcover_config") {
		t.Fatal("manual adoption notes were not written into main.tf")
	}

	names := regexp.MustCompile(`(?m)^variable "([^"]+)"`).FindAllStringSubmatch(generated["variables.tf"], -1)
	if len(names) == 0 {
		t.Fatal("variables.tf declared no sensitive variables")
	}
	overrides := []string{
		"TF_CLI_CONFIG_FILE=" + writeDevOverride(t, localProviderDir(t)),
		"TF_IN_AUTOMATION=1",
		"CHECKPOINT_DISABLE=1",
		"CHAPTARR_URL=" + live.URL,
		"CHAPTARR_API_KEY=" + fakeCanary("provider-key"),
	}
	for _, match := range names {
		overrides = append(overrides, "TF_VAR_"+match[1]+"="+tfCanary)
	}
	env := mergeEnv(os.Environ(), overrides)

	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			if !cliAtLeast(t, tool, 1, 11, 2) {
				t.Skipf("%s is older than the generated required_version >= 1.11.2", tool)
			}
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 30*time.Second {
				t.Fatal("importer integration deadline exhausted")
			}
			work := t.TempDir()
			for name, body := range generated {
				mode := os.FileMode(0o600)
				if name == "import.sh" {
					mode = 0o700
				}
				if err := os.WriteFile(filepath.Join(work, name), []byte(body), mode); err != nil {
					t.Fatal(err)
				}
			}
			live.Reset()
			runCLI(t, ctx, tool, work, env, "init", "-input=false", "-no-color")
			applyOut := runCLI(t, ctx, tool, work, env, "apply", "-auto-approve", "-input=false", "-no-color")
			if !strings.Contains(applyOut, "Apply complete") || !strings.Contains(applyOut, "imported") {
				t.Fatalf("apply did not import the generated configuration:\n%s", applyOut)
			}
			assertApplyDidNotMutateUnexpectedly(t, live)
			live.Reset()
			plan := planCLI(t, ctx, tool, work, env)
			if !strings.Contains(plan, "No changes") {
				t.Fatalf("plan was not empty:\n%s", plan)
			}
			if strings.Contains(plan, tfCanary) {
				t.Fatalf("plan included the supplied secret variable")
			}
			for _, secret := range live.Canaries.values() {
				if strings.Contains(plan, secret) {
					t.Fatalf("plan included live secret %s", secret)
				}
			}
			assertApplyDidNotMutateUnexpectedly(t, live)
		})
	}
}

func cliAtLeast(t *testing.T, tool string, major, minor, patch int) bool {
	t.Helper()
	out, err := exec.Command(tool, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s version: %v\n%s", tool, err, out)
	}
	match := regexp.MustCompile(`v(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if match == nil {
		t.Fatalf("could not parse %s version:\n%s", tool, out)
	}
	got := [3]int{atoi(match[1]), atoi(match[2]), atoi(match[3])}
	want := [3]int{major, minor, patch}
	return got[0] > want[0] || (got[0] == want[0] && (got[1] > want[1] || (got[1] == want[1] && got[2] >= want[2])))
}

func atoi(value string) int {
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}

func mergeEnv(base, overrides []string) []string {
	drop := map[string]struct{}{}
	for _, item := range overrides {
		key, _, _ := strings.Cut(item, "=")
		drop[key] = struct{}{}
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, skip := drop[key]; skip || strings.HasPrefix(key, "TF_VAR_") {
			continue
		}
		out = append(out, item)
	}
	return append(out, overrides...)
}

func planCLI(t *testing.T, ctx context.Context, tool, dir string, env []string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, tool, "plan", "-detailed-exitcode", "-input=false", "-no-color")
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s plan failed: %v\n%s", tool, err, output)
	}
	return string(output)
}

func assertApplyDidNotMutateUnexpectedly(t *testing.T, live *populatedServer) {
	t.Helper()
	for _, call := range live.Calls() {
		switch {
		case call.Method == http.MethodPost || call.Method == http.MethodDelete:
			t.Errorf("apply %s %s", call.Method, call.Path)
		case strings.Contains(call.Path, "preserved") || strings.Contains(call.Path, "postgres") || strings.Contains(call.Path, "hardcover"):
			t.Errorf("apply requested unmanaged %s %s", call.Method, call.Path)
		case call.Method == http.MethodPut && call.Path == "/api/v1/book/20":
			t.Errorf("apply updated the book")
		}
	}
}
