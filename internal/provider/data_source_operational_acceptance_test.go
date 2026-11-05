package provider

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestAccOperationalAndReadOnlyDataSources(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests require TF_ACC=1")
	}
	baseURL := strings.TrimSpace(os.Getenv("CHAPTARR_URL"))
	apiKey := strings.TrimSpace(os.Getenv("CHAPTARR_API_KEY"))
	if baseURL == "" || apiKey == "" {
		t.Skip("acceptance tests require CHAPTARR_URL and CHAPTARR_API_KEY")
	}

	apiClient, err := client.New(client.Config{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		UserAgent: "terraform-provider-chaptarr-acceptance/1.0",
	})
	if err != nil {
		t.Fatalf("configure acceptance client: %v", err)
	}

	cases := []struct {
		name      string
		factory   func() datasource.DataSource
		overrides map[string]tftypes.Value
	}{
		{
			name:    "chaptarr_ping_get",
			factory: newReadOnlyDataSource(pingDefinition()),
		},
		{
			name:    "chaptarr_ping_head",
			factory: newReadOnlyDataSource(pingDefinition()),
			overrides: map[string]tftypes.Value{
				"method": tftypes.NewValue(tftypes.String, http.MethodHead),
			},
		},
		{
			name:    "chaptarr_backups",
			factory: newBackupsDataSource,
		},
		{
			name:    "chaptarr_settings_backups",
			factory: newSettingsBackupsDataSource,
		},
		{
			name:    "chaptarr_history",
			factory: newHistoryDataSource,
		},
		{
			name:    "chaptarr_ignored",
			factory: newIgnoredDataSource,
		},
		{
			name:    "chaptarr_manual_import_candidates",
			factory: newManualImportCandidatesDataSource,
		},
		{
			name:    "chaptarr_pending_author_import",
			factory: newPendingAuthorImportDataSource,
		},
		{
			name:    "chaptarr_cutoff_wanted",
			factory: newCutoffWantedDataSource,
		},
		{
			name:    "chaptarr_missing",
			factory: newMissingDataSource,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			instance := tc.factory()
			withConfigure, ok := instance.(datasource.DataSourceWithConfigure)
			if !ok {
				t.Fatalf("%T does not implement DataSourceWithConfigure", instance)
			}
			cfgResp := &datasource.ConfigureResponse{}
			withConfigure.Configure(t.Context(), datasource.ConfigureRequest{ProviderData: apiClient}, cfgResp)
			if cfgResp.Diagnostics.HasError() {
				t.Fatalf("configure %s: %v", tc.name, cfgResp.Diagnostics)
			}

			req, resp := operationalReadRequest(t, instance, tc.overrides)
			instance.Read(t.Context(), req, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("read %s: %v", tc.name, resp.Diagnostics)
			}
			if strings.Contains(resp.State.Raw.String(), apiKey) {
				t.Fatalf("%s leaked CHAPTARR_API_KEY in state", tc.name)
			}
		})
	}
}
