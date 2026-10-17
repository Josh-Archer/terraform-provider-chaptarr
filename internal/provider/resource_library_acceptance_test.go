package provider

import (
	"os"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestAccSeriesNarratorRefresh(t *testing.T) {
	testAccSeriesRefresh(t, func(t *testing.T, state seriesModel) {
		if state.NarratorAnalysis.IsUnknown() || state.NarratorInheritance.IsUnknown() {
			t.Fatal("narrator refresh returned unknown state")
		}
	})
}

func TestAccSeriesVariantsRefresh(t *testing.T) {
	testAccSeriesRefresh(t, func(t *testing.T, state seriesModel) {
		if state.Variants.IsNull() || state.Variants.IsUnknown() {
			t.Fatal("variant refresh must return a known list, including when the endpoint is unavailable")
		}
	})
}

func testAccSeriesRefresh(t *testing.T, verify func(*testing.T, seriesModel)) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests require TF_ACC=1")
	}
	seriesID := os.Getenv("CHAPTARR_TEST_SERIES_ID")
	if seriesID == "" {
		t.Fatal("CHAPTARR_TEST_SERIES_ID must identify an existing series")
	}
	apiClient, err := client.New(client.Config{BaseURL: os.Getenv("CHAPTARR_URL"), APIKey: os.Getenv("CHAPTARR_API_KEY"), UserAgent: "terraform-provider-chaptarr-acceptance/1.0"})
	if err != nil {
		t.Fatalf("configure acceptance client: %v", err)
	}
	stateModel := seriesTestModel(t)
	stateModel.ID = types.StringValue(seriesID)
	instance := &seriesResource{client: apiClient}
	prior := stateForResource(t, instance, stateModel)
	response := &resource.ReadResponse{State: emptyStateForResource(t, instance)}
	instance.Read(t.Context(), resource.ReadRequest{State: tfsdk.State(prior)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("refresh existing series: %v", response.Diagnostics)
	}
	var refreshed seriesModel
	response.Diagnostics.Append(response.State.Get(t.Context(), &refreshed)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("decode refreshed series: %v", response.Diagnostics)
	}
	verify(t, refreshed)
}
