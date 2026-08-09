package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const integrationSecretSentinel = "synthetic-integration-password"

func TestIntegrationSchemasAreValidAndCredentialsWriteOnly(t *testing.T) {
	t.Parallel()
	resources := []resource.Resource{&indexerResource{}, &downloadClientResource{}, &notificationResource{}}
	for _, instance := range resources {
		response := &resource.SchemaResponse{}
		instance.Schema(t.Context(), resource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%T schema invalid: %v", instance, diagnostics)
		}
		attribute := response.Schema.Attributes["secret_fields"]
		if !attribute.IsSensitive() || !attribute.IsWriteOnly() {
			t.Fatalf("%T secret_fields must be Sensitive+WriteOnly", instance)
		}
	}
	dataSources := []datasource.DataSource{&customizationSchemaDataSource{kind: "indexer"}, &customizationSchemaDataSource{kind: "download_client"}, &customizationSchemaDataSource{kind: "notification"}, &indexerFlagsDataSource{}}
	for _, instance := range dataSources {
		response := &datasource.SchemaResponse{}
		instance.Schema(t.Context(), datasource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%T schema invalid: %v", instance, diagnostics)
		}
	}
}

func TestIntegrationSecretsAreApplyOnlyAndSchemaValidated(t *testing.T) {
	t.Parallel()
	secrets := types.MapValueMust(types.StringType, map[string]attr.Value{"apiToken": types.StringValue(integrationSecretSentinel)})
	var diagnostics diag.Diagnostics
	payload := integrationBasePayload(t.Context(), 0, types.StringValue("provider"), types.StringValue("Prowlarr"), types.StringValue("ProwlarrSettings"), types.BoolValue(false), types.SetNull(types.Int64Type), types.StringValue(`{"baseUrl":"https://prowlarr.example.test"}`), secrets, &diagnostics)
	if diagnostics.HasError() || len(payload.Fields) != 2 {
		t.Fatalf("apply payload failed: %#v diagnostics=%v", payload, diagnostics)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/indexer/schema" {
			t.Fatalf("unexpected schema request %s %s", request.Method, request.URL.Path)
		}
		_, _ = writer.Write([]byte(`[{"implementation":"Prowlarr","configContract":"ProwlarrSettings","fields":[{"name":"baseUrl","type":"url"},{"name":"apiToken","privacy":"apiKey","type":"password","value":"redacted"}]}]`))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "schema-validation-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !validateIntegrationFields(t.Context(), apiClient, "/api/v1/indexer/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &diagnostics) || diagnostics.HasError() {
		t.Fatalf("valid separated settings rejected: %v", diagnostics)
	}
	canonical, _, protected := integrationFieldState(payload.Fields)
	if strings.Contains(canonical, integrationSecretSentinel) || len(protected) != 1 || protected[0] != "apiToken" {
		t.Fatal("protected integration field entered persistent state")
	}
}

func TestEnabledIntegrationRequiresExplicitTestAuthorization(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	if validateIntegrationActivation(types.BoolValue(true), types.BoolValue(false), &diagnostics) || !diagnostics.HasError() {
		t.Fatal("enabled provider without test_on_apply must be rejected before API mutation")
	}
	diagnostics = nil
	if !validateIntegrationActivation(types.BoolValue(true), types.BoolValue(true), &diagnostics) || diagnostics.HasError() {
		t.Fatalf("explicit test authorization was rejected: %v", diagnostics)
	}
	diagnostics = nil
	if !validateIntegrationActivation(types.BoolValue(false), types.BoolValue(false), &diagnostics) || diagnostics.HasError() {
		t.Fatalf("disabled provider should not require test authorization: %v", diagnostics)
	}
}

func TestIntegrationTestAuthorizationNormalizesFailClosed(t *testing.T) {
	t.Parallel()
	for _, initial := range []types.Bool{types.BoolNull(), types.BoolUnknown()} {
		value := initial
		normalizeIntegrationTestAuthorization(&value)
		if value.IsNull() || value.IsUnknown() || value.ValueBool() {
			t.Fatalf("omitted/imported authorization did not normalize to known false: %v", value)
		}
	}
	explicit := types.BoolValue(true)
	normalizeIntegrationTestAuthorization(&explicit)
	if !explicit.ValueBool() {
		t.Fatal("explicit test authorization was not preserved")
	}
}

func TestEnabledIndexerWithoutTestAuthorizationMakesNoRequest(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "no-request-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &indexerResource{client: apiClient}
	model := indexerModel{ID: types.StringNull(), Name: types.StringValue("blocked"), ImplementationName: types.StringNull(), Implementation: types.StringValue("Fixture"), ConfigContract: types.StringValue("FixtureSettings"), Enable: types.BoolValue(true), TestOnApply: types.BoolNull(), Tags: types.SetNull(types.Int64Type), FieldValuesJSON: types.StringValue(`{}`), FieldValuesSHA256: types.StringNull(), SecretFields: types.MapNull(types.StringType), ProtectedFieldNames: types.SetNull(types.StringType), EnableRSS: types.BoolValue(false), EnableAutomaticSearch: types.BoolValue(false), EnableInteractiveSearch: types.BoolValue(false), SupportsRSS: types.BoolNull(), SupportsSearch: types.BoolNull(), Protocol: types.StringNull(), Priority: types.Int64Value(25), DownloadClientID: types.Int64Value(0), ProxyID: types.Int64Null()}
	planState := stateForResource(t, instance, model)
	response := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(planState), Config: tfsdk.Config{Raw: planState.Raw, Schema: planState.Schema}}, response)
	if !response.Diagnostics.HasError() || requests != 0 {
		t.Fatalf("enabled provider without opt-in made requests=%d diagnostics=%v", requests, response.Diagnostics)
	}
}

func TestIntegrationIdentityAndDynamicJSONValidation(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	integrationBasePayload(t.Context(), 0, types.StringValue(" "), types.StringValue(""), types.StringValue(""), types.BoolValue(false), types.SetNull(types.Int64Type), types.StringValue(`not-json`), types.MapNull(types.StringType), &diagnostics)
	if !diagnostics.HasError() {
		t.Fatal("empty identity and invalid dynamic JSON must be rejected locally")
	}
}

func TestDisabledIndexerLifecycleDoesNotInvokeTestsAndRefreshes(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v1/indexer/schema":
			_, _ = writer.Write([]byte(`[{"implementation":"Prowlarr","configContract":"ProwlarrSettings","fields":[{"name":"baseUrl","type":"url"}]}]`))
		case "POST /api/v1/indexer":
			_, _ = writer.Write([]byte(`{"id":41}`))
		case "GET /api/v1/indexer/41":
			_, _ = writer.Write([]byte(`{"id":41,"name":"server-indexer","implementationName":"Prowlarr","implementation":"Prowlarr","configContract":"ProwlarrSettings","enable":false,"tags":[5],"fields":[{"name":"baseUrl","value":"https://prowlarr.example.test"},{"name":"apiToken","privacy":"apiKey","type":"password","value":"` + integrationSecretSentinel + `"}],"enableRss":true,"enableAutomaticSearch":true,"enableInteractiveSearch":true,"supportsRss":true,"supportsSearch":true,"protocol":"torrent","priority":25,"downloadClientId":0}`))
		default:
			t.Fatalf("unexpected indexer lifecycle request %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "indexer-lifecycle-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &indexerResource{client: apiClient}
	model := indexerModel{ID: types.StringNull(), Name: types.StringValue("configured"), ImplementationName: types.StringNull(), Implementation: types.StringValue("Prowlarr"), ConfigContract: types.StringValue("ProwlarrSettings"), Enable: types.BoolValue(false), TestOnApply: types.BoolNull(), Tags: types.SetNull(types.Int64Type), FieldValuesJSON: types.StringValue(`{"baseUrl":"https://prowlarr.example.test"}`), FieldValuesSHA256: types.StringNull(), SecretFields: types.MapNull(types.StringType), ProtectedFieldNames: types.SetNull(types.StringType), EnableRSS: types.BoolValue(true), EnableAutomaticSearch: types.BoolValue(true), EnableInteractiveSearch: types.BoolValue(true), SupportsRSS: types.BoolNull(), SupportsSearch: types.BoolNull(), Protocol: types.StringNull(), Priority: types.Int64Value(25), DownloadClientID: types.Int64Value(0), ProxyID: types.Int64Null()}
	planState := stateForResource(t, instance, model)
	response := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(planState), Config: tfsdk.Config{Raw: planState.Raw, Schema: planState.Schema}}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", response.Diagnostics)
	}
	var state indexerModel
	response.Diagnostics.Append(response.State.Get(t.Context(), &state)...)
	if response.Diagnostics.HasError() || state.ID.ValueString() != "41" || state.Name.ValueString() != "server-indexer" || state.TestOnApply.IsNull() || state.TestOnApply.IsUnknown() || state.TestOnApply.ValueBool() || !state.SecretFields.IsNull() || strings.Contains(response.State.Raw.String(), integrationSecretSentinel) || state.FieldValuesSHA256.ValueString() == "" {
		t.Fatalf("indexer refresh was not drift/secret safe: %#v diagnostics=%v", state, response.Diagnostics)
	}
	if strings.Contains(strings.Join(requests, ","), "/test") || strings.Contains(strings.Join(requests, ","), "/release") {
		t.Fatal("lifecycle invoked an operational action")
	}
}

func TestIntegrationMutationRoutesAndDelete404AreSafe(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"/api/v1/indexer", "/api/v1/downloadclient", "/api/v1/notification"} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			seen := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				seen = append(seen, request.Method+" "+request.URL.RequestURI())
				if request.Method == http.MethodPost {
					_, _ = writer.Write([]byte(`{"id":9}`))
				}
				if request.Method == http.MethodDelete {
					writer.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "mutation-key", UserAgent: "test/1.0"})
			if err != nil {
				t.Fatal(err)
			}
			var diagnostics diag.Diagnostics
			createProfile(t.Context(), apiClient, endpoint, map[string]any{"name": "fixture"}, "integration", &diagnostics)
			updateProfile(t.Context(), apiClient, endpoint+"/9", map[string]any{"id": 9}, "integration", &diagnostics)
			deleteProfile(t.Context(), apiClient, endpoint+"/", types.StringValue("9"), "integration", &diagnostics)
			if diagnostics.HasError() || len(seen) != 3 {
				t.Fatalf("mutation routes failed: %v diagnostics=%v", seen, diagnostics)
			}
		})
	}
}

func TestIntegrationSchemaDataSourceSanitizesNestedSecrets(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/notification/schema" {
			t.Fatalf("unexpected schema request %s %s", request.Method, request.URL.Path)
		}
		_, _ = writer.Write([]byte(`[{"fields":[{"name":"accessToken","privacy":"apiKey","value":"` + integrationSecretSentinel + `"}],"presets":[{"fields":[{"name":"password","type":"password","value":"` + integrationSecretSentinel + `"}]}]}]`))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "schema-datasource-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &customizationSchemaDataSource{client: apiClient, kind: "notification"}
	request, response := dataSourceRequest(t, instance, nil)
	instance.Read(t.Context(), request, response)
	var templates types.String
	response.Diagnostics.Append(response.State.GetAttribute(t.Context(), path.Root("templates_json"), &templates)...)
	if response.Diagnostics.HasError() || strings.Contains(templates.ValueString(), integrationSecretSentinel) || strings.Count(templates.ValueString(), `"value":null`) != 2 {
		t.Fatalf("schema data source leaked protected values: %s diagnostics=%v", templates.ValueString(), response.Diagnostics)
	}
}
