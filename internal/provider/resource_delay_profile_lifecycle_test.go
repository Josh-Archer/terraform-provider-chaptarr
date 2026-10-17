package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestDelayProfileLifecycleCreateReorderFailureAndRetry(t *testing.T) {
	var reorderAttempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/delayprofile":
			_, _ = writer.Write([]byte(`[{"id":1},{"id":4},{"id":2}]`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/delayprofile":
			_, _ = writer.Write([]byte(`{"id":2,"enableUsenet":true,"enableTorrent":false,"preferredProtocol":"usenet","usenetDelay":0,"torrentDelay":0,"bypassIfHighestQuality":false,"bypassIfAboveCustomFormatScore":false,"minimumCustomFormatScore":0,"order":2,"tags":[1]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/api/v1/delayprofile/reorder/2":
			attempt := reorderAttempts.Add(1)
			if attempt == 1 {
				// Reorder fails on create
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte(`{"message":"simulated reorder failure on create"}`))
				return
			}
			// Subsequent reorders succeed on retry
			writer.WriteHeader(http.StatusOK)
		case request.Method == http.MethodPut && request.URL.Path == "/api/v1/delayprofile/2":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"id":2,"enableUsenet":true,"enableTorrent":false,"preferredProtocol":"usenet","usenetDelay":0,"torrentDelay":0,"bypassIfHighestQuality":false,"bypassIfAboveCustomFormatScore":false,"minimumCustomFormatScore":0,"order":2,"tags":[1]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/delayprofile/2":
			_, _ = writer.Write([]byte(`{"id":2,"enableUsenet":true,"enableTorrent":false,"preferredProtocol":"usenet","usenetDelay":0,"torrentDelay":0,"bypassIfHighestQuality":false,"bypassIfAboveCustomFormatScore":false,"minimumCustomFormatScore":0,"order":2,"tags":[1]}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/api/v1/delayprofile/2":
			writer.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	testConfig := fmt.Sprintf(`
provider "chaptarr" {
  url     = %q
  api_key = "lifecycle-test-key"
}

resource "chaptarr_delay_profile" "test" {
  enable_usenet        = true
  enable_torrent       = false
  preferred_protocol   = "usenet"
  usenet_delay_minutes = 0
  torrent_delay_minutes = 0
  bypass_if_highest_quality = false
  bypass_if_above_custom_format_score = false
  minimum_custom_format_score = 0
  after_id             = 4
  tags                 = [1]
}
`, server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"chaptarr": providerserver.NewProtocol6WithError(New("test")()),
		},
		Steps: []resource.TestStep{
			// Step 1: Initial creation where reorder fails.
			// Apply succeeds without error, resource is not tainted, ID is 2, after_id is 4 in state.
			// Because reorder is pending, subsequent refresh surfaces the pending plan.
			{
				Config:             testConfig,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("chaptarr_delay_profile.test", "id", "2"),
					resource.TestCheckResourceAttr("chaptarr_delay_profile.test", "after_id", "4"),
				),
			},
			// Step 2: Next plan surfaces the reorder as pending (non-empty plan showing pending update).
			{
				Config:             testConfig,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Step 3: Next apply retries the reorder; PreApply verifies an in-place update action,
			// reorder succeeds on retry, ID remains unchanged (2), after_id is 4.
			{
				Config: testConfig,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("chaptarr_delay_profile.test", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("chaptarr_delay_profile.test", "id", "2"),
					resource.TestCheckResourceAttr("chaptarr_delay_profile.test", "after_id", "4"),
				),
			},
		},
	})

	if attempts := reorderAttempts.Load(); attempts < 2 {
		t.Fatalf("expected at least 2 reorder attempts (create failure + retry), got %d", attempts)
	}
}
