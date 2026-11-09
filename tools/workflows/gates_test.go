package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasePromotionNeedsConsumerReplay(t *testing.T) {
	t.Parallel()
	release := readRepoFile(t, ".github/workflows/release.yml")
	if !strings.Contains(release, "consumer-replay-gate:") {
		t.Fatal("release workflow is missing the consumer replay gate")
	}
	if !strings.Contains(release, "needs: consumer-replay-gate") {
		t.Fatal("signed release job does not wait for the consumer replay gate")
	}
	if !strings.Contains(release, "Consumer Replay") {
		t.Fatal("release gate does not look for the Consumer Replay check")
	}
	if strings.Contains(release, "workflow_dispatch:") && strings.Contains(release, "gh workflow run release.yml") {
		t.Fatal("release workflow must not dispatch itself")
	}
}

func TestCIRunsReplayAndBothEngines(t *testing.T) {
	t.Parallel()
	ci := readRepoFile(t, ".github/workflows/ci.yml")
	for _, needle := range []string{
		"name: Consumer Replay (arr-config)",
		"name: Acceptance Terraform (",
		"CHAPTARR_ENGINE: terraform",
		"bash ./scripts/test-consumer-replay.sh",
		"bash ./scripts/test-acceptance.sh",
		"terraform_version: ${{ matrix.terraform-version }}",
	} {
		if !strings.Contains(ci, needle) {
			t.Fatalf("ci.yml missing %q", needle)
		}
	}
	// The existing OpenTofu acceptance matrix stays in place.
	if strings.Count(ci, "chaptarr-version: \"0.9.929\"") < 1 ||
		!strings.Contains(ci, "chaptarr-version: \"0.9.925\"") ||
		!strings.Contains(ci, "chaptarr-version: \"0.9.914\"") {
		t.Fatal("OpenTofu acceptance matrix was reduced")
	}
}

func TestRegistryInstallChecksBothRegistries(t *testing.T) {
	t.Parallel()
	workflow := readRepoFile(t, ".github/workflows/registry-install.yml")
	for _, needle := range []string{
		"tool: [terraform, tofu]",
		"registry.opentofu.org/josh-archer/chaptarr",
		"registry.terraform.io/josh-archer/chaptarr",
		"zh:",
		"types: [published]",
	} {
		if !strings.Contains(workflow, needle) {
			t.Fatalf("registry-install.yml missing %q", needle)
		}
	}
	if strings.Contains(workflow, "pull_request:") {
		t.Fatal("registry install check must not run on pull requests")
	}
}

func TestLifecycleMatrixDocumentsEnginePins(t *testing.T) {
	t.Parallel()
	workflow := readRepoFile(t, ".github/workflows/lifecycle.yml")
	for _, needle := range []string{
		`engine_version: "1.11.2"`,
		`engine_version: "1.17.1"`,
		`engine_version: "1.13.9"`,
		"engine: terraform",
		"engine: tofu",
	} {
		if !strings.Contains(workflow, needle) {
			t.Fatalf("lifecycle.yml missing %q", needle)
		}
	}
}

func TestConsumerReplayRefusesAmbientTargets(t *testing.T) {
	t.Parallel()
	script := readRepoFile(t, "scripts/test-consumer-replay.sh")
	for _, needle := range []string{
		"discarding ambient Chaptarr endpoint",
		"refusing non-loopback target",
		"99.99.99",
		"filesystem_mirror",
		"test/consumer/arr-config",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("consumer replay script missing %q", needle)
		}
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
