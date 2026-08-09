// Command openapi validates the pinned Chaptarr API contract and generates its
// operation-level Terraform coverage inventory.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	specPath            = "third_party/chaptarr/openapi.json"
	classificationsPath = "tools/openapi/classifications.json"
	inventoryPath       = "tools/openapi/inventory.json"
	documentationPath   = "docs/openapi-coverage.md"
	maximumSpecBytes    = 16 << 20
)

var methods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"patch": true, "head": true, "options": true, "trace": true,
}

type source struct {
	Repository                string `json:"repository"`
	Tag                       string `json:"tag"`
	Commit                    string `json:"commit"`
	BlobSHA                   string `json:"blob_sha"`
	SHA256                    string `json:"sha256"`
	OpenAPIVersion            string `json:"openapi_version"`
	PathCount                 int    `json:"path_count"`
	OperationCount            int    `json:"operation_count"`
	TagCount                  int    `json:"tag_count"`
	SchemaCount               int    `json:"schema_count"`
	GlobalContractFingerprint string `json:"global_contract_fingerprint"`
	DriftURL                  string `json:"drift_url"`
}

type policy struct {
	Version    int                 `json:"version"`
	Source     source              `json:"source"`
	Operations map[string]decision `json:"operations"`
}

type decision struct {
	Classification string `json:"classification"`
	Status         string `json:"status"`
	Target         string `json:"target,omitempty"`
	TrackingIssue  string `json:"tracking_issue,omitempty"`
	Rationale      string `json:"rationale,omitempty"`
}

type specification struct {
	OpenAPI    string                                `json:"openapi"`
	Security   json.RawMessage                       `json:"security"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components map[string]json.RawMessage            `json:"components"`
}

type operationContract struct {
	Tags           []string        `json:"tags,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	Parameters     json.RawMessage `json:"parameters,omitempty"`
	PathParameters json.RawMessage `json:"-"`
	RequestBody    json.RawMessage `json:"requestBody,omitempty"`
	Responses      json.RawMessage `json:"responses,omitempty"`
	Security       json.RawMessage `json:"security,omitempty"`
}

type operation struct {
	Key                 string   `json:"key"`
	Method              string   `json:"method"`
	Path                string   `json:"path"`
	Tags                []string `json:"tags"`
	Summary             string   `json:"summary,omitempty"`
	ContractFingerprint string   `json:"contract_fingerprint"`
	decision
}

type inventory struct {
	Version    int         `json:"version"`
	Source     source      `json:"source"`
	Operations []operation `json:"operations"`
}

func main() {
	mode := "check"
	if len(os.Args) > 2 {
		fatalf("usage: go run ./tools/openapi [generate|check|drift]")
	}
	if len(os.Args) == 2 {
		mode = os.Args[1]
	}

	switch mode {
	case "generate", "check":
		generated, docs, err := build()
		if err != nil {
			fatalf("OpenAPI coverage: %v", err)
		}
		if mode == "generate" {
			mustWrite(inventoryPath, generated)
			mustWrite(documentationPath, docs)
			fmt.Printf("generated %s and %s\n", inventoryPath, documentationPath)
			return
		}
		mustMatch(inventoryPath, generated)
		mustMatch(documentationPath, docs)
		fmt.Println("OpenAPI contract and generated coverage are current")
	case "drift":
		if err := drift(); err != nil {
			fatalf("OpenAPI drift: %v", err)
		}
	default:
		fatalf("unknown mode %q", mode)
	}
}

func build() ([]byte, []byte, error) {
	p, err := loadPolicy()
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read pinned specification: %w", err)
	}
	if got := sha256Hex(raw); !strings.EqualFold(got, p.Source.SHA256) {
		return nil, nil, fmt.Errorf("pinned specification SHA-256 is %s, want %s", got, p.Source.SHA256)
	}
	parsed, operations, tags, globalContractFingerprint, err := parse(raw)
	if err != nil {
		return nil, nil, err
	}
	if err := validateSource(p.Source, parsed, operations, tags, globalContractFingerprint); err != nil {
		return nil, nil, err
	}

	seen := make(map[string]bool, len(operations))
	for i := range operations {
		key := operations[i].Key
		classification, ok := p.Operations[key]
		if !ok {
			return nil, nil, fmt.Errorf("operation %s has no classification", key)
		}
		if err := validateDecision(key, classification); err != nil {
			return nil, nil, err
		}
		operations[i].decision = classification
		seen[key] = true
	}
	for key := range p.Operations {
		if !seen[key] {
			return nil, nil, fmt.Errorf("classification references missing operation %s", key)
		}
	}

	result := inventory{Version: 1, Source: p.Source, Operations: operations}
	generated, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal inventory: %w", err)
	}
	generated = append(generated, '\n')
	return generated, renderMarkdown(result), nil
}

func loadPolicy() (policy, error) {
	raw, err := os.ReadFile(classificationsPath)
	if err != nil {
		return policy{}, fmt.Errorf("read classifications: %w", err)
	}
	var p policy
	if err := json.Unmarshal(raw, &p); err != nil {
		return policy{}, fmt.Errorf("parse classifications: %w", err)
	}
	if p.Version != 1 {
		return policy{}, fmt.Errorf("unsupported classifications version %d", p.Version)
	}
	return p, nil
}

func parse(raw []byte) (specification, []operation, map[string]bool, string, error) {
	var spec specification
	if err := json.Unmarshal(raw, &spec); err != nil {
		return specification{}, nil, nil, "", fmt.Errorf("parse specification: %w", err)
	}
	globalContractFingerprint, err := fingerprintGlobalContract(spec.OpenAPI, spec.Security, spec.Components)
	if err != nil {
		return specification{}, nil, nil, "", fmt.Errorf("fingerprint root security and components: %w", err)
	}
	var operations []operation
	tags := map[string]bool{}
	for path, item := range spec.Paths {
		for method, rawOperation := range item {
			if !methods[strings.ToLower(method)] {
				continue
			}
			var contract operationContract
			if err := json.Unmarshal(rawOperation, &contract); err != nil {
				return specification{}, nil, nil, "", fmt.Errorf("parse %s %s: %w", method, path, err)
			}
			contract.PathParameters = item["parameters"]
			if len(contract.Security) == 0 {
				contract.Security = spec.Security
			}
			sort.Strings(contract.Tags)
			for _, tag := range contract.Tags {
				tags[tag] = true
			}
			fingerprint, err := fingerprint(contract)
			if err != nil {
				return specification{}, nil, nil, "", fmt.Errorf("fingerprint %s %s: %w", method, path, err)
			}
			upperMethod := strings.ToUpper(method)
			operations = append(operations, operation{
				Key: upperMethod + " " + path, Method: upperMethod, Path: path,
				Tags: contract.Tags, Summary: contract.Summary, ContractFingerprint: fingerprint,
			})
		}
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Key < operations[j].Key })
	return spec, operations, tags, globalContractFingerprint, nil
}

func fingerprint(contract operationContract) (string, error) {
	normalized := make(map[string]any)
	normalized["tags"] = contract.Tags
	for name, raw := range map[string]json.RawMessage{
		"parameters":     contract.Parameters,
		"pathParameters": contract.PathParameters,
		"requestBody":    contract.RequestBody,
		"responses":      contract.Responses,
		"security":       contract.Security,
	} {
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		normalized[name] = value
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func fingerprintGlobalContract(openAPIVersion string, security json.RawMessage, components map[string]json.RawMessage) (string, error) {
	normalizedComponents := make(map[string]any, len(components))
	for name, raw := range components {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", fmt.Errorf("component %s: %w", name, err)
		}
		normalizedComponents[name] = value
	}
	normalized := map[string]any{"openapi": openAPIVersion, "components": normalizedComponents}
	if len(security) != 0 {
		var normalizedSecurity any
		if err := json.Unmarshal(security, &normalizedSecurity); err != nil {
			return "", fmt.Errorf("root security: %w", err)
		}
		normalized["security"] = normalizedSecurity
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func validateSource(want source, spec specification, operations []operation, tags map[string]bool, globalContractFingerprint string) error {
	if err := validateHeaderAuthentication(spec.Security, spec.Components); err != nil {
		return err
	}
	var schemas map[string]json.RawMessage
	if err := json.Unmarshal(spec.Components["schemas"], &schemas); err != nil {
		return fmt.Errorf("parse components.schemas: %w", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"OpenAPI version", spec.OpenAPI, want.OpenAPIVersion},
		{"path count", len(spec.Paths), want.PathCount},
		{"operation count", len(operations), want.OperationCount},
		{"tag count", len(tags), want.TagCount},
		{"schema count", len(schemas), want.SchemaCount},
		{"global contract fingerprint", globalContractFingerprint, want.GlobalContractFingerprint},
	}
	for _, check := range checks {
		if fmt.Sprint(check.got) != fmt.Sprint(check.want) {
			return fmt.Errorf("%s is %v, want %v", check.name, check.got, check.want)
		}
	}
	return nil
}

func validateHeaderAuthentication(security json.RawMessage, components map[string]json.RawMessage) error {
	var requirements []map[string]json.RawMessage
	if err := json.Unmarshal(security, &requirements); err != nil {
		return fmt.Errorf("parse root security: %w", err)
	}
	headerAllowed := false
	for _, requirement := range requirements {
		if _, ok := requirement["X-Api-Key"]; ok {
			headerAllowed = true
			break
		}
	}
	if !headerAllowed {
		return errors.New("root security must permit X-Api-Key authentication")
	}

	var schemes map[string]struct {
		Type string `json:"type"`
		Name string `json:"name"`
		In   string `json:"in"`
	}
	if err := json.Unmarshal(components["securitySchemes"], &schemes); err != nil {
		return fmt.Errorf("parse components.securitySchemes: %w", err)
	}
	scheme, ok := schemes["X-Api-Key"]
	if !ok || scheme.Type != "apiKey" || scheme.Name != "X-Api-Key" || scheme.In != "header" {
		return errors.New("components.securitySchemes.X-Api-Key must be an apiKey named X-Api-Key in the header")
	}
	return nil
}

func validateDecision(key string, d decision) error {
	switch d.Classification {
	case "resource", "data-source":
		if d.Status != "planned" && d.Status != "implemented" {
			return fmt.Errorf("%s has invalid status %q", key, d.Status)
		}
		if strings.TrimSpace(d.Target) == "" || strings.TrimSpace(d.TrackingIssue) == "" {
			return fmt.Errorf("%s %s classification requires target and tracking issue", key, d.Classification)
		}
	case "action-only", "out-of-scope":
		if d.Status != "excluded" {
			return fmt.Errorf("%s %s classification must have excluded status", key, d.Classification)
		}
		if strings.TrimSpace(d.Rationale) == "" {
			return fmt.Errorf("%s %s classification requires a rationale", key, d.Classification)
		}
	default:
		return fmt.Errorf("%s has invalid classification %q", key, d.Classification)
	}
	return nil
}

func renderMarkdown(inv inventory) []byte {
	counts := map[string]int{}
	for _, op := range inv.Operations {
		counts[op.Classification+"/"+op.Status]++
	}
	var b strings.Builder
	b.WriteString("# Chaptarr OpenAPI coverage\n\n")
	b.WriteString("Generated by `go run ./tools/openapi generate`; do not edit by hand.\n\n")
	fmt.Fprintf(&b, "Pinned upstream: `%s` tag `%s` commit `%s` (blob `%s`).\n\n", inv.Source.Repository, inv.Source.Tag, inv.Source.Commit, inv.Source.BlobSHA)
	fmt.Fprintf(&b, "Contract: OpenAPI %s, %d paths, %d operations, %d tags, %d schemas.\n\n", inv.Source.OpenAPIVersion, inv.Source.PathCount, inv.Source.OperationCount, inv.Source.TagCount, inv.Source.SchemaCount)
	b.WriteString("A `planned` row is roadmap intent only; it is not implemented provider coverage. `action-only` and `out-of-scope` rows are intentionally excluded from declarative refresh/apply behavior.\n\n")
	b.WriteString("## Summary\n\n")
	b.WriteString("| Classification | Status | Operations |\n|---|---|---:|\n")
	var countKeys []string
	for key := range counts {
		countKeys = append(countKeys, key)
	}
	sort.Strings(countKeys)
	for _, key := range countKeys {
		parts := strings.SplitN(key, "/", 2)
		fmt.Fprintf(&b, "| %s | %s | %d |\n", parts[0], parts[1], counts[key])
	}
	b.WriteString("\n## Operation inventory\n\n")
	b.WriteString("| Operation | Tags | Decision | Status | Target / rationale | Issue | Contract |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, op := range inv.Operations {
		detail := op.Target
		if detail == "" {
			detail = op.Rationale
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | `%s` |\n",
			sanitizeCell(op.Key), sanitizeCell(strings.Join(op.Tags, ", ")), op.Classification,
			op.Status, sanitizeCell(detail), sanitizeCell(op.TrackingIssue), op.ContractFingerprint[:12])
	}
	return []byte(b.String())
}

func drift() error {
	p, err := loadPolicy()
	if err != nil {
		return err
	}
	pinnedRaw, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	_, pinned, _, pinnedGlobalContractFingerprint, err := parse(pinnedRaw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Source.DriftURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "terraform-provider-chaptarr-openapi-drift")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch upstream: HTTP %d", resp.StatusCode)
	}
	currentRaw, err := io.ReadAll(io.LimitReader(resp.Body, maximumSpecBytes+1))
	if err != nil {
		return err
	}
	if len(currentRaw) > maximumSpecBytes {
		return fmt.Errorf("upstream specification exceeds %d bytes", maximumSpecBytes)
	}
	_, current, _, currentGlobalContractFingerprint, err := parse(currentRaw)
	if err != nil {
		return err
	}

	pinnedMap := operationMap(pinned)
	currentMap := operationMap(current)
	var changes []string
	if pinnedGlobalContractFingerprint != currentGlobalContractFingerprint {
		changes = append(changes, "changed root security or components")
	}
	for key, old := range pinnedMap {
		newOperation, ok := currentMap[key]
		if !ok {
			changes = append(changes, "removed "+key)
		} else if old.ContractFingerprint != newOperation.ContractFingerprint || !equalStrings(old.Tags, newOperation.Tags) {
			changes = append(changes, "changed "+key)
		}
	}
	for key := range currentMap {
		if _, ok := pinnedMap[key]; !ok {
			changes = append(changes, "added "+key)
		}
	}
	sort.Strings(changes)
	if len(changes) > 0 {
		return errors.New("upstream develop differs from the pinned contract:\n  " + strings.Join(changes, "\n  ") + "\nreview the new upstream release, classifications, and fingerprints before updating")
	}
	fmt.Println("upstream develop matches the pinned OpenAPI operation contract")
	return nil
}

func operationMap(operations []operation) map[string]operation {
	result := make(map[string]operation, len(operations))
	for _, op := range operations {
		result[op.Key] = op
	}
	return result
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func sanitizeCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func mustWrite(path string, content []byte) {
	if err := os.WriteFile(path, content, 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}

func mustMatch(path string, want []byte) {
	got, err := os.ReadFile(path)
	if err != nil {
		fatalf("read generated file %s: %v (run generate)", path, err)
	}
	if !bytes.Equal(got, want) {
		fatalf("%s is stale; run go run ./tools/openapi generate", path)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
