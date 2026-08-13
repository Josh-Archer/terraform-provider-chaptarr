package main

import (
	"strings"
	"testing"
)

func TestValidateMatrixRequiresImmutableDistinctVersions(t *testing.T) {
	t.Parallel()
	m := matrix{Version: 1, Contract: contract{ChaptarrVersion: "1.0.0", Commit: strings.Repeat("a", 40), OpenAPISHA256: strings.Repeat("b", 64)}, Versions: []versionRecord{
		{ChaptarrVersion: "1.0.0", APIVersion: "v1", Image: "example/chaptarr:1.0.0@sha256:" + strings.Repeat("c", 64), ContractStatus: "pinned-contract", AcceptanceStatus: "candidate", VerifiedResources: []string{"chaptarr_tag"}, VerifiedDataSources: []string{"chaptarr_api_info"}, Evidence: "test"},
		{ChaptarrVersion: "0.9.0", APIVersion: "v1", Image: "example/chaptarr:0.9.0@sha256:" + strings.Repeat("d", 64), ContractStatus: "compatibility-candidate", AcceptanceStatus: "candidate", VerifiedResources: []string{"chaptarr_tag"}, VerifiedDataSources: []string{"chaptarr_api_info"}, Evidence: "test"},
	}}
	if err := validateMatrix(m); err != nil {
		t.Fatal(err)
	}
	m.Versions[1].Image = "example/chaptarr:latest"
	if err := validateMatrix(m); err == nil {
		t.Fatal("mutable image reference was accepted")
	}
	m.Versions[1].Image = "example/chaptarr:0.9.0@sha256:" + strings.Repeat("d", 64)
	m.Versions[1].AcceptanceStatus = "unverified-claim"
	if err := validateMatrix(m); err == nil {
		t.Fatal("unknown acceptance status was accepted")
	}
	m.Versions[1].AcceptanceStatus = "candidate"
	m.Versions[1].VerifiedDataSources = nil
	if err := validateMatrix(m); err == nil {
		t.Fatal("missing representative verified targets were accepted")
	}
}

func TestImplementedTargetsAreUniqueAndSorted(t *testing.T) {
	t.Parallel()
	resources, dataSources := implementedTargets(inventory{Operations: []inventoryOperation{
		{Classification: "resource", Status: "implemented", Target: "z_resource"},
		{Classification: "resource", Status: "implemented", Target: "a_resource"},
		{Classification: "resource", Status: "implemented", Target: "a_resource"},
		{Classification: "data-source", Status: "implemented", Target: "b_data"},
		{Classification: "data-source", Status: "planned", Target: "ignored"},
	}})
	if strings.Join(resources, ",") != "a_resource,z_resource" || strings.Join(dataSources, ",") != "b_data" {
		t.Fatalf("unexpected targets: resources=%v dataSources=%v", resources, dataSources)
	}
}
