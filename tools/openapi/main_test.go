package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFingerprintIsIndependentOfJSONMemberOrder(t *testing.T) {
	t.Parallel()

	first := operationContract{
		Tags:        []string{"System"},
		RequestBody: json.RawMessage(`{"content":{"application/json":{"schema":{"type":"object","properties":{"b":{"type":"string"},"a":{"type":"integer"}}}}}}`),
		Responses:   json.RawMessage(`{"200":{"description":"ok"}}`),
	}
	second := operationContract{
		Tags:        []string{"System"},
		RequestBody: json.RawMessage(`{"content":{"application/json":{"schema":{"properties":{"a":{"type":"integer"},"b":{"type":"string"}},"type":"object"}}}}`),
		Responses:   json.RawMessage(`{"200":{"description":"ok"}}`),
	}

	firstHash, err := fingerprint(first)
	if err != nil {
		t.Fatalf("fingerprint first contract: %v", err)
	}
	secondHash, err := fingerprint(second)
	if err != nil {
		t.Fatalf("fingerprint second contract: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("normalized fingerprints differ: %s != %s", firstHash, secondHash)
	}
}

func TestValidateDecisionRequiresRoadmapEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision decision
		want     string
	}{
		{name: "planned target", decision: decision{Classification: "resource", Status: "planned", TrackingIssue: "#2"}, want: "requires target"},
		{name: "planned issue", decision: decision{Classification: "data-source", Status: "planned", Target: "chaptarr_status"}, want: "tracking issue"},
		{name: "excluded rationale", decision: decision{Classification: "action-only", Status: "excluded"}, want: "rationale"},
		{name: "false implemented status", decision: decision{Classification: "resource", Status: "excluded", Target: "chaptarr_test", TrackingIssue: "#2"}, want: "invalid status"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateDecision("GET /example", test.decision)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateDecision() error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestGlobalContractFingerprintDetectsReferencedSchemaChanges(t *testing.T) {
	t.Parallel()

	first := []byte(`{"openapi":"3.0.4","paths":{"/items":{"get":{"tags":["Test"],"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Item"}}}}}}}},"components":{"schemas":{"Item":{"type":"string"}}}}`)
	second := []byte(`{"openapi":"3.0.4","paths":{"/items":{"get":{"tags":["Test"],"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Item"}}}}}}}},"components":{"schemas":{"Item":{"type":"integer"}}}}`)

	_, firstOperations, _, firstSchemas, err := parse(first)
	if err != nil {
		t.Fatalf("parse first specification: %v", err)
	}
	_, secondOperations, _, secondSchemas, err := parse(second)
	if err != nil {
		t.Fatalf("parse second specification: %v", err)
	}
	if firstOperations[0].ContractFingerprint != secondOperations[0].ContractFingerprint {
		t.Fatal("operation JSON unexpectedly changed; test no longer isolates a referenced schema change")
	}
	if firstSchemas == secondSchemas {
		t.Fatal("component schema fingerprint did not detect a referenced schema change")
	}
}

func TestFingerprintsDetectRootSecurityAndSchemeChanges(t *testing.T) {
	t.Parallel()

	first := []byte(`{"openapi":"3.0.4","security":[{"X-Api-Key":[]}],"paths":{"/status":{"get":{"tags":["System"],"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{},"securitySchemes":{"X-Api-Key":{"type":"apiKey","in":"header","name":"X-Api-Key"}}}}`)
	second := []byte(`{"openapi":"3.0.4","security":[{"apikey":[]}],"paths":{"/status":{"get":{"tags":["System"],"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{},"securitySchemes":{"apikey":{"type":"apiKey","in":"query","name":"apikey"}}}}`)

	_, firstOperations, _, firstGlobal, err := parse(first)
	if err != nil {
		t.Fatalf("parse first specification: %v", err)
	}
	_, secondOperations, _, secondGlobal, err := parse(second)
	if err != nil {
		t.Fatalf("parse second specification: %v", err)
	}
	if firstOperations[0].ContractFingerprint == secondOperations[0].ContractFingerprint {
		t.Fatal("operation fingerprint ignored inherited root security change")
	}
	if firstGlobal == secondGlobal {
		t.Fatal("global contract fingerprint ignored root security or security-scheme change")
	}
}

func TestGlobalFingerprintDetectsOpenAPIVersionChange(t *testing.T) {
	t.Parallel()

	first := []byte(`{"openapi":"3.0.4","paths":{},"components":{"schemas":{}}}`)
	second := []byte(`{"openapi":"3.1.0","paths":{},"components":{"schemas":{}}}`)

	_, _, _, firstGlobal, err := parse(first)
	if err != nil {
		t.Fatalf("parse first specification: %v", err)
	}
	_, _, _, secondGlobal, err := parse(second)
	if err != nil {
		t.Fatalf("parse second specification: %v", err)
	}
	if firstGlobal == secondGlobal {
		t.Fatal("global contract fingerprint ignored OpenAPI version change")
	}
}

func TestValidateHeaderAuthenticationEnforcesProviderInvariant(t *testing.T) {
	t.Parallel()

	validSecurity := json.RawMessage(`[{"X-Api-Key":[]},{"apikey":[]}]`)
	validComponents := map[string]json.RawMessage{
		"securitySchemes": json.RawMessage(`{"X-Api-Key":{"type":"apiKey","in":"header","name":"X-Api-Key"},"apikey":{"type":"apiKey","in":"query","name":"apikey"}}`),
	}
	if err := validateHeaderAuthentication(validSecurity, validComponents); err != nil {
		t.Fatalf("valid header authentication rejected: %v", err)
	}

	for name, test := range map[string]struct {
		security   json.RawMessage
		components map[string]json.RawMessage
	}{
		"root requirement removed": {
			security:   json.RawMessage(`[{"apikey":[]}]`),
			components: validComponents,
		},
		"scheme moved to query": {
			security: validSecurity,
			components: map[string]json.RawMessage{
				"securitySchemes": json.RawMessage(`{"X-Api-Key":{"type":"apiKey","in":"query","name":"X-Api-Key"}}`),
			},
		},
		"scheme renamed": {
			security: validSecurity,
			components: map[string]json.RawMessage{
				"securitySchemes": json.RawMessage(`{"X-Api-Key":{"type":"apiKey","in":"header","name":"Authorization"}}`),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateHeaderAuthentication(test.security, test.components); err == nil {
				t.Fatal("unsafe authentication contract was accepted")
			}
		})
	}
}

func TestOperationFingerprintIncludesPathItemParameters(t *testing.T) {
	t.Parallel()

	first := []byte(`{"openapi":"3.0.4","paths":{"/items/{id}":{"parameters":[{"in":"path","name":"id","schema":{"type":"string"}}],"get":{"tags":["Test"],"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{}}}`)
	second := []byte(`{"openapi":"3.0.4","paths":{"/items/{id}":{"parameters":[{"in":"path","name":"id","schema":{"type":"integer"}}],"get":{"tags":["Test"],"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{}}}`)

	_, firstOperations, _, _, err := parse(first)
	if err != nil {
		t.Fatalf("parse first specification: %v", err)
	}
	_, secondOperations, _, _, err := parse(second)
	if err != nil {
		t.Fatalf("parse second specification: %v", err)
	}
	if firstOperations[0].ContractFingerprint == secondOperations[0].ContractFingerprint {
		t.Fatal("operation fingerprint ignored path-item parameter change")
	}
}
