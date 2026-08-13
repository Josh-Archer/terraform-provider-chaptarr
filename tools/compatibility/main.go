package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	matrixPath = "compatibility/chaptarr.json"
	docPath    = "docs/compatibility.md"
	ciPath     = ".github/workflows/ci.yml"
)

type matrix struct {
	Version  int             `json:"version"`
	Contract contract        `json:"contract"`
	Versions []versionRecord `json:"versions"`
}

type contract struct {
	ChaptarrVersion string `json:"chaptarr_version"`
	Tag             string `json:"tag"`
	Commit          string `json:"commit"`
	OpenAPISHA256   string `json:"openapi_sha256"`
}

type versionRecord struct {
	ChaptarrVersion     string   `json:"chaptarr_version"`
	APIVersion          string   `json:"api_version"`
	Image               string   `json:"image"`
	ContractStatus      string   `json:"contract_status"`
	AcceptanceStatus    string   `json:"acceptance_status"`
	VerifiedResources   []string `json:"verified_resources"`
	VerifiedDataSources []string `json:"verified_data_sources"`
	Evidence            string   `json:"evidence"`
}

type inventory struct {
	Operations []inventoryOperation `json:"operations"`
}

type inventoryOperation struct {
	Classification string `json:"classification"`
	Status         string `json:"status"`
	Target         string `json:"target"`
}

func main() {
	command := "check"
	if len(os.Args) == 2 {
		command = os.Args[1]
	} else if len(os.Args) > 2 {
		fatal(errors.New("usage: go run ./tools/compatibility [check|generate]"))
	}

	root, err := repositoryRoot()
	if err != nil {
		fatal(err)
	}
	content, m, err := generatedContent(root)
	if err != nil {
		fatal(err)
	}

	switch command {
	case "generate":
		if err := os.WriteFile(filepath.Join(root, docPath), content, 0o644); err != nil {
			fatal(err)
		}
		fmt.Println("Generated", docPath)
	case "check":
		current, err := os.ReadFile(filepath.Join(root, docPath))
		if err != nil {
			fatal(err)
		}
		if !bytes.Equal(current, content) {
			fatal(fmt.Errorf("%s is stale; run go run ./tools/compatibility generate", docPath))
		}
		workflow, err := os.ReadFile(filepath.Join(root, ciPath))
		if err != nil {
			fatal(err)
		}
		for _, record := range m.Versions {
			if !bytes.Contains(workflow, []byte(record.Image)) || !bytes.Contains(workflow, []byte("chaptarr-version: \""+record.ChaptarrVersion+"\"")) {
				fatal(fmt.Errorf("acceptance matrix does not pin Chaptarr %s and %s", record.ChaptarrVersion, record.Image))
			}
		}
		fmt.Println("Compatibility matrix, generated documentation, and CI images are current")
	default:
		fatal(fmt.Errorf("unknown command %q", command))
	}
}

func repositoryRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("repository root not found")
		}
		current = parent
	}
}

func generatedContent(root string) ([]byte, matrix, error) {
	raw, err := os.ReadFile(filepath.Join(root, matrixPath))
	if err != nil {
		return nil, matrix{}, err
	}
	var m matrix
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, matrix{}, fmt.Errorf("decode compatibility matrix: %w", err)
	}
	if err := validateMatrix(m); err != nil {
		return nil, matrix{}, err
	}

	rawInventory, err := os.ReadFile(filepath.Join(root, "tools/openapi/inventory.json"))
	if err != nil {
		return nil, matrix{}, err
	}
	var inv inventory
	if err := json.Unmarshal(rawInventory, &inv); err != nil {
		return nil, matrix{}, fmt.Errorf("decode OpenAPI inventory: %w", err)
	}
	resources, dataSources := implementedTargets(inv)
	return render(m, resources, dataSources), m, nil
}

func validateMatrix(m matrix) error {
	if m.Version != 1 || m.Contract.ChaptarrVersion == "" || len(m.Contract.Commit) != 40 || len(m.Contract.OpenAPISHA256) != 64 {
		return errors.New("compatibility matrix has invalid contract metadata")
	}
	if len(m.Versions) < 2 {
		return errors.New("compatibility matrix must contain at least two Chaptarr versions")
	}
	seen := map[string]bool{}
	validContractStatuses := map[string]bool{"pinned-contract": true, "compatibility-candidate": true}
	validAcceptanceStatuses := map[string]bool{"candidate": true, "live-verified": true}
	for _, record := range m.Versions {
		if seen[record.ChaptarrVersion] || record.ChaptarrVersion == "" {
			return fmt.Errorf("duplicate or empty Chaptarr version %q", record.ChaptarrVersion)
		}
		seen[record.ChaptarrVersion] = true
		parts := strings.Split(record.Image, "@sha256:")
		if len(parts) != 2 || !strings.Contains(parts[0], ":"+record.ChaptarrVersion) || len(parts[1]) != 64 {
			return fmt.Errorf("chaptarr %s image is not tag-and-digest pinned", record.ChaptarrVersion)
		}
		if record.APIVersion != "v1" || !validContractStatuses[record.ContractStatus] || !validAcceptanceStatuses[record.AcceptanceStatus] || record.Evidence == "" {
			return fmt.Errorf("chaptarr %s has invalid or missing status evidence", record.ChaptarrVersion)
		}
		if !sortedUniqueNonempty(record.VerifiedResources) || !sortedUniqueNonempty(record.VerifiedDataSources) {
			return fmt.Errorf("chaptarr %s has invalid representative verified targets", record.ChaptarrVersion)
		}
	}
	return nil
}

func sortedUniqueNonempty(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for index, value := range values {
		if value == "" || (index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return true
}

func implementedTargets(inv inventory) ([]string, []string) {
	resources := map[string]bool{}
	dataSources := map[string]bool{}
	for _, operation := range inv.Operations {
		if operation.Status != "implemented" || operation.Target == "" {
			continue
		}
		switch operation.Classification {
		case "resource":
			resources[operation.Target] = true
		case "data-source":
			dataSources[operation.Target] = true
		}
	}
	return sortedKeys(resources), sortedKeys(dataSources)
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func render(m matrix, resources, dataSources []string) []byte {
	var output strings.Builder
	output.WriteString("# Chaptarr compatibility\n\n")
	output.WriteString("This file is generated from `compatibility/chaptarr.json`. Run `go run ./tools/compatibility generate` after changing the matrix.\n\n")
	output.WriteString("## Version evidence\n\n")
	output.WriteString("| Chaptarr | API | Immutable image | Contract status | Acceptance status | Representative verified targets | Evidence |\n|---|---|---|---|---|---|---|\n")
	for _, record := range m.Versions {
		verified := append([]string{}, record.VerifiedResources...)
		verified = append(verified, record.VerifiedDataSources...)
		fmt.Fprintf(&output, "| `%s` | `%s` | `%s` | %s | %s | %s | %s |\n", record.ChaptarrVersion, record.APIVersion, record.Image, record.ContractStatus, record.AcceptanceStatus, codeList(verified), record.Evidence)
	}
	output.WriteString("\n`pinned-contract` means the checked-in OpenAPI artifact is authoritative for code generation. `compatibility-candidate` means only the representative acceptance lane is proposed. `candidate` is not a live-verified claim; change it only with exact-head disposable-environment evidence.\n\n")
	fmt.Fprintf(&output, "## Pinned contract coverage\n\nThe provider contract is Chaptarr `%s` (`%s`, commit `%s`). It records %d implemented resource targets and %d implemented data-source targets. Representative acceptance proves only tag lifecycle and safe API/system reads; it does not claim live coverage of every target.\n\n", m.Contract.ChaptarrVersion, m.Contract.Tag, m.Contract.Commit, len(resources), len(dataSources))
	fmt.Fprintf(&output, "Resources: %s.\n\n", codeList(resources))
	fmt.Fprintf(&output, "Data sources: %s.\n\n", codeList(dataSources))
	output.WriteString("## Verification boundary\n\nContract checks, unit tests, and generated documentation are offline evidence. The acceptance tier starts a disposable Chaptarr container exposed only on a random loopback port and uses a runtime-only synthetic API key. It attempts teardown on every exit, verifies that project containers, volumes, and networks are absent, and fails visibly if cleanup is incomplete. The dedicated Docker network is not an outbound-egress control. Production deployments, media, credentials, migration outcomes, and `terraform/arr-config` integration are outside this evidence.\n")
	return []byte(output.String())
}

func codeList(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = "`" + value + "`"
	}
	return strings.Join(quoted, ", ")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
