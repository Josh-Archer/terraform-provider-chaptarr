package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestOperationalIssueNeverImplementsMutatingEndpoints(t *testing.T) {
	t.Parallel()

	policy := loadOperationalPolicy(t)
	tracked := 0
	for operation, decision := range policy.Operations {
		if decision.TrackingIssue != "#11" {
			continue
		}
		tracked++
		method, _, ok := strings.Cut(operation, " ")
		if !ok {
			t.Fatalf("invalid operation key %q", operation)
		}
		if method == "GET" || method == "HEAD" {
			if decision.Classification != "data-source" && decision.Classification != "out-of-scope" {
				t.Errorf("read-only operational endpoint %s classified %s", operation, decision.Classification)
			}
			continue
		}
		if decision.Classification != "action-only" && decision.Classification != "out-of-scope" {
			t.Errorf("mutating operational endpoint %s classified %s", operation, decision.Classification)
		}
		if decision.Status != "excluded" {
			t.Errorf("mutating operational endpoint %s has status %s", operation, decision.Status)
		}
		if strings.TrimSpace(decision.Rationale) == "" {
			t.Errorf("mutating operational endpoint %s lacks rationale", operation)
		}
	}
	if tracked != 77 {
		t.Fatalf("%d operations track #11, want the pinned complete set of 77", tracked)
	}
}

func TestHighRiskOperationalEndpointsRemainUnsupported(t *testing.T) {
	t.Parallel()

	policy := loadOperationalPolicy(t)
	for _, operation := range []string{
		"POST /api/v1/command",
		"DELETE /api/v1/queue/{id}",
		"POST /api/v1/manualimport",
		"POST /api/v1/release/push",
		"POST /api/v1/system/backup/restore/{id}",
		"POST /api/v1/system/backup/restore/upload",
		"POST /api/v1/system/settingsbackup/create",
		"POST /api/v1/system/settingsbackup/restore",
		"POST /api/v1/system/reset",
		"POST /api/v1/system/restart",
		"POST /api/v1/system/shutdown",
	} {
		decision, ok := policy.Operations[operation]
		if !ok {
			t.Fatalf("operational policy is missing %s", operation)
		}
		if decision.Classification != "action-only" || decision.Status != "excluded" {
			t.Errorf("%s = %s/%s, want action-only/excluded", operation, decision.Classification, decision.Status)
		}
	}
}

func TestLogAndMatchingLogEndpointsRemainOutOfScope(t *testing.T) {
	t.Parallel()

	policy := loadOperationalPolicy(t)
	matched := 0
	for operation, decision := range policy.Operations {
		if !strings.Contains(operation, "/api/v1/log") && !strings.Contains(operation, "/api/v1/matchinglog") {
			continue
		}
		matched++
		if decision.Classification != "out-of-scope" || decision.Status != "excluded" || strings.TrimSpace(decision.Rationale) == "" {
			t.Errorf("%s must remain out-of-scope/excluded with rationale", operation)
		}
	}
	if matched != 6 {
		t.Fatalf("matched %d log operations, want pinned complete set of 6", matched)
	}
}

func loadOperationalPolicy(t *testing.T) policy {
	t.Helper()
	raw, err := os.ReadFile("classifications.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed policy
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed
}
