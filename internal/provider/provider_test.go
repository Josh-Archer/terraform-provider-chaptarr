package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const providerSyntheticKey = "provider-api-key-sentinel-41b7"

func TestProviderMetadataAndRegistrations(t *testing.T) {
	t.Parallel()

	p := &ChaptarrProvider{version: "test-version"}
	metadata := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, metadata)
	if metadata.TypeName != "chaptarr" || metadata.Version != "test-version" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if resources := p.Resources(context.Background()); len(resources) != 10 {
		t.Fatalf("registered %d resources, want 10", len(resources))
	}
	if dataSources := p.DataSources(context.Background()); len(dataSources) != 2 {
		t.Fatalf("registered %d data sources, want 2", len(dataSources))
	}
}

func TestProviderSchemaMarksAPIKeySensitive(t *testing.T) {
	t.Parallel()

	p := &ChaptarrProvider{version: "test"}
	response := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, response)
	attribute, ok := response.Schema.Attributes["api_key"].(providerschema.StringAttribute)
	if !ok {
		t.Fatalf("api_key schema type = %T", response.Schema.Attributes["api_key"])
	}
	if !attribute.Sensitive {
		t.Fatal("api_key must be marked sensitive")
	}
}

func TestResolveProviderConfigUsesEnvironmentFallbacks(t *testing.T) {
	t.Parallel()

	config, diagnostics := resolveProviderConfig(providerModel{}, func(name string) string {
		switch name {
		case "CHAPTARR_URL":
			return "https://environment.example.test/proxy/"
		case "CHAPTARR_API_KEY":
			return providerSyntheticKey
		default:
			return ""
		}
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if config.baseURL != "https://environment.example.test/proxy/" || config.apiKey != providerSyntheticKey {
		t.Fatalf("unexpected environment configuration: %#v", config)
	}
	if config.insecureSkipVerify {
		t.Fatal("insecure_skip_verify must default false")
	}
	if config.requestTimeout != client.DefaultRequestTimeout {
		t.Fatalf("timeout = %s, want %s", config.requestTimeout, client.DefaultRequestTimeout)
	}
}

func TestResolveProviderConfigExplicitValuesTakePrecedence(t *testing.T) {
	t.Parallel()

	config, diagnostics := resolveProviderConfig(providerModel{
		URL:                types.StringValue("https://configured.example.test/subpath"),
		APIKey:             types.StringValue(providerSyntheticKey),
		InsecureSkipVerify: types.BoolValue(true),
		RequestTimeout:     types.Int64Value(45),
	}, func(string) string {
		return "environment-value-that-must-not-win"
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if config.baseURL != "https://configured.example.test/subpath" || config.apiKey != providerSyntheticKey {
		t.Fatalf("explicit values did not take precedence: %#v", config)
	}
	if !config.insecureSkipVerify || config.requestTimeout != 45*time.Second {
		t.Fatalf("explicit settings were not preserved: %#v", config)
	}
}

func TestResolveProviderConfigExplicitEmptyDoesNotFallBack(t *testing.T) {
	t.Parallel()

	_, diagnostics := resolveProviderConfig(providerModel{
		URL:    types.StringValue(""),
		APIKey: types.StringValue(""),
	}, func(string) string {
		return "environment-value-that-must-not-win"
	})
	if !diagnostics.HasError() {
		t.Fatal("explicit empty values should override environment fallbacks and produce diagnostics")
	}
}

func TestResolveProviderConfigRejectsInvalidValuesWithoutCredentialLeak(t *testing.T) {
	t.Parallel()

	for _, model := range []providerModel{
		{
			URL:    types.StringValue("https://user:url-secret@example.test?api_key=url-query-secret"),
			APIKey: types.StringValue(providerSyntheticKey),
		},
		{
			URL:            types.StringValue("https://example.test"),
			APIKey:         types.StringValue(providerSyntheticKey),
			RequestTimeout: types.Int64Value(maximumRequestTimeoutSeconds + 1),
		},
	} {
		_, diagnostics := resolveProviderConfig(model, func(string) string { return "" })
		if !diagnostics.HasError() {
			t.Fatal("expected invalid provider configuration")
		}
		for _, diagnostic := range diagnostics {
			message := diagnostic.Summary() + " " + diagnostic.Detail()
			for _, secret := range []string{providerSyntheticKey, "url-secret", "url-query-secret"} {
				if strings.Contains(message, secret) {
					t.Fatalf("diagnostic leaked %q: %s", secret, message)
				}
			}
		}
	}
}

func TestConfigurePerformsNoNetworkIO(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	p := &ChaptarrProvider{version: "offline-test"}
	schemaResponse := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, schemaResponse)
	typeValue := schemaResponse.Schema.Type().TerraformType(context.Background())
	raw := tftypes.NewValue(typeValue, map[string]tftypes.Value{
		"url":                     tftypes.NewValue(tftypes.String, server.URL+"/proxy/"),
		"api_key":                 tftypes.NewValue(tftypes.String, providerSyntheticKey),
		"insecure_skip_verify":    tftypes.NewValue(tftypes.Bool, false),
		"request_timeout_seconds": tftypes.NewValue(tftypes.Number, int64(30)),
	})

	configureResponse := &provider.ConfigureResponse{}
	p.Configure(context.Background(), provider.ConfigureRequest{
		Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema},
	}, configureResponse)
	if configureResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected configure diagnostics: %v", configureResponse.Diagnostics)
	}
	if _, ok := configureResponse.ResourceData.(*client.Client); !ok {
		t.Fatalf("ResourceData type = %T, want *client.Client", configureResponse.ResourceData)
	}
	if _, ok := configureResponse.DataSourceData.(*client.Client); !ok {
		t.Fatalf("DataSourceData type = %T, want *client.Client", configureResponse.DataSourceData)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Configure performed %d network requests", got)
	}
}
