package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const hardcoverSyntheticToken = "synthetic-hardcover-credential"

func TestImportSchemasAndCredentialContract(t *testing.T) {
	t.Parallel()
	for _, instance := range []resource.Resource{&importListResource{}, &importListExclusionResource{}, &hardcoverConfigResource{}} {
		response := &resource.SchemaResponse{}
		instance.Schema(t.Context(), resource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%T schema invalid: %v", instance, diagnostics)
		}
	}
	response := &resource.SchemaResponse{}
	(&hardcoverConfigResource{}).Schema(t.Context(), resource.SchemaRequest{}, response)
	attribute := response.Schema.Attributes["token"]
	if !attribute.IsSensitive() || !attribute.IsWriteOnly() {
		t.Fatal("Hardcover token must be Sensitive+WriteOnly")
	}
	if _, ok := response.Schema.Attributes["has_token"]; !ok {
		t.Fatal("Hardcover state must expose only a non-secret configuration indicator")
	}
}

func TestHardcoverCreateRequiresExplicitExternalValidationAndStoresNoToken(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/config/hardcover" {
			t.Fatalf("unexpected Hardcover request %s %s", request.Method, request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), hardcoverSyntheticToken) {
			t.Fatal("apply-only token did not reach the POST payload")
		}
		_, _ = writer.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "hardcover-test-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &hardcoverConfigResource{client: apiClient}
	planModel := hardcoverConfigModel{ID: types.StringUnknown(), Token: types.StringNull(), AllowExternalValidation: types.BoolValue(false), ObserveServer: types.BoolValue(false), Enabled: types.BoolUnknown(), HasToken: types.BoolUnknown(), Username: types.StringUnknown(), AvatarURL: types.StringUnknown()}
	planState := stateForResource(t, instance, planModel)
	config := hardcoverConfigWithToken(t, instance, hardcoverSyntheticToken)
	blocked := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(planState), Config: config}, blocked)
	if !blocked.Diagnostics.HasError() || requests != 0 {
		t.Fatalf("unauthorized Hardcover validation made requests=%d diagnostics=%v", requests, blocked.Diagnostics)
	}

	planModel.AllowExternalValidation = types.BoolValue(true)
	planState = stateForResource(t, instance, planModel)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(planState), Config: config}, created)
	if created.Diagnostics.HasError() || requests != 1 || strings.Contains(created.State.Raw.String(), hardcoverSyntheticToken) {
		t.Fatalf("Hardcover create was not request/state safe: requests=%d diagnostics=%v", requests, created.Diagnostics)
	}
	var state hardcoverConfigModel
	created.Diagnostics.Append(created.State.Get(t.Context(), &state)...)
	if created.Diagnostics.HasError() || !state.Token.IsNull() || !state.Enabled.ValueBool() || !state.HasToken.ValueBool() || state.ObserveServer.ValueBool() {
		t.Fatalf("unexpected Hardcover state: %#v diagnostics=%v", state, created.Diagnostics)
	}
}

func TestHardcoverRefreshIsOfflineUnlessExplicitlyObserved(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/config/hardcover" {
			t.Fatalf("unexpected request")
		}
		_, _ = writer.Write([]byte(`{"enabled":true,"hasToken":true,"username":"reader","avatarUrl":"https://images.example.test/avatar"}`))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "hardcover-refresh-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &hardcoverConfigResource{client: apiClient}
	state := hardcoverConfigModel{ID: types.StringValue("hardcover"), Token: types.StringNull(), AllowExternalValidation: types.BoolNull(), ObserveServer: types.BoolNull(), Enabled: types.BoolValue(true), HasToken: types.BoolValue(true), Username: types.StringNull(), AvatarURL: types.StringNull()}
	target := stateForResource(t, instance, state)
	var diagnostics diag.Diagnostics
	instance.refresh(t.Context(), &state, &target, &diagnostics)
	if diagnostics.HasError() || requests != 0 || state.ObserveServer.IsUnknown() || state.ObserveServer.IsNull() || state.ObserveServer.ValueBool() {
		t.Fatalf("default refresh was not offline/fail-closed: %v requests=%d", diagnostics, requests)
	}
	state.ObserveServer = types.BoolValue(true)
	instance.refresh(t.Context(), &state, &target, &diagnostics)
	if diagnostics.HasError() || requests != 1 || state.Username.ValueString() != "reader" || !state.HasToken.ValueBool() || !state.Token.IsNull() {
		t.Fatalf("explicit observation failed: %#v diagnostics=%v", state, diagnostics)
	}
}

func TestImportedHardcoverOfflineReadNormalizesComputedStateWithoutRequest(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "offline-import-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &hardcoverConfigResource{client: apiClient}
	model := hardcoverConfigModel{ID: types.StringValue("hardcover"), Token: types.StringNull(), AllowExternalValidation: types.BoolUnknown(), ObserveServer: types.BoolUnknown(), Enabled: types.BoolUnknown(), HasToken: types.BoolUnknown(), Username: types.StringUnknown(), AvatarURL: types.StringUnknown()}
	prior := stateForResource(t, instance, model)
	response := &resource.ReadResponse{State: prior}
	instance.Read(t.Context(), resource.ReadRequest{State: prior}, response)
	if response.Diagnostics.HasError() || requests != 0 {
		t.Fatalf("offline import read made request or failed: requests=%d diagnostics=%v", requests, response.Diagnostics)
	}
	var state hardcoverConfigModel
	response.Diagnostics.Append(response.State.Get(t.Context(), &state)...)
	if response.Diagnostics.HasError() || state.Enabled.IsUnknown() || state.HasToken.IsUnknown() || state.Username.IsUnknown() || state.AvatarURL.IsUnknown() || state.Enabled.ValueBool() || state.HasToken.ValueBool() || state.ObserveServer.ValueBool() || state.AllowExternalValidation.ValueBool() {
		t.Fatalf("offline import state did not normalize fail-closed: %#v diagnostics=%v", state, response.Diagnostics)
	}
}

func TestHardcoverDestroyRelinquishesOwnershipWithoutDisconnecting(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "destroy-safety-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &hardcoverConfigResource{client: apiClient}
	model := hardcoverConfigModel{ID: types.StringValue("hardcover"), Token: types.StringNull(), AllowExternalValidation: types.BoolValue(false), ObserveServer: types.BoolValue(false), Enabled: types.BoolValue(true), HasToken: types.BoolValue(true), Username: types.StringNull(), AvatarURL: types.StringNull()}
	response := &resource.DeleteResponse{}
	instance.Delete(t.Context(), resource.DeleteRequest{State: stateForResource(t, instance, model)}, response)
	if response.Diagnostics.HasError() || requests != 0 {
		t.Fatalf("destroy disconnected Hardcover or failed: requests=%d diagnostics=%v", requests, response.Diagnostics)
	}
}

func TestImportedHardcoverObservationDoesNotImplicitlyDisable(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/config/hardcover" {
			t.Fatalf("observation made unsafe request %s %s", request.Method, request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"enabled":true,"hasToken":true,"username":"reader"}`))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "observe-import-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &hardcoverConfigResource{client: apiClient}
	model := hardcoverConfigModel{ID: types.StringValue("hardcover"), Token: types.StringNull(), AllowExternalValidation: types.BoolValue(false), ObserveServer: types.BoolValue(true), Enabled: types.BoolValue(false), HasToken: types.BoolValue(false), Username: types.StringNull(), AvatarURL: types.StringNull()}
	response := &resource.UpdateResponse{State: emptyStateForResource(t, instance)}
	instance.Update(t.Context(), resource.UpdateRequest{Plan: tfsdk.Plan(stateForResource(t, instance, model)), Config: hardcoverConfigWithControls(t, instance, true)}, response)
	if response.Diagnostics.HasError() || len(requests) != 1 || requests[0] != "GET /api/v1/config/hardcover" {
		t.Fatalf("observation disabled Hardcover or failed: requests=%v diagnostics=%v", requests, response.Diagnostics)
	}
}

func TestImportListDisabledLifecycleAndCanonicalRefresh(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v1/importlist/schema":
			_, _ = writer.Write([]byte(`[{"implementation":"FixtureList","configContract":"FixtureListSettings","fields":[{"name":"listId","type":"textbox"},{"name":"apiKey","privacy":"apiKey","type":"password"}]}]`))
		case "POST /api/v1/importlist":
			_, _ = writer.Write([]byte(`{"id":12}`))
		case "GET /api/v1/importlist/12":
			_, _ = writer.Write([]byte(`{"id":12,"name":"server-list","implementationName":"Fixture","implementation":"FixtureList","configContract":"FixtureListSettings","enable":false,"tags":[4],"fields":[{"name":"listId","value":"abc"},{"name":"apiKey","privacy":"apiKey","type":"password","value":"redacted"}],"enableAutomaticAdd":true,"shouldMonitor":"entireAuthor","shouldMonitorExisting":true,"shouldSearch":false,"rootFolderPath":"/books","monitorNewItems":"new","qualityProfileId":2,"metadataProfileId":3,"listType":"other","minRefreshInterval":"01:00:00"}`))
		default:
			t.Fatalf("unexpected import-list request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "import-list-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &importListResource{client: apiClient}
	model := importListModel{ID: types.StringNull(), Name: types.StringValue("configured"), ImplementationName: types.StringNull(), Implementation: types.StringValue("FixtureList"), ConfigContract: types.StringValue("FixtureListSettings"), Enable: types.BoolValue(false), TestOnApply: types.BoolNull(), Tags: types.SetNull(types.Int64Type), FieldValuesJSON: types.StringValue(`{"listId":"abc"}`), FieldValuesSHA256: types.StringNull(), SecretFields: types.MapNull(types.StringType), ProtectedFieldNames: types.SetNull(types.StringType), EnableAutomaticAdd: types.BoolValue(true), ShouldMonitor: types.StringValue("entireAuthor"), ShouldMonitorExist: types.BoolValue(true), ShouldSearch: types.BoolValue(false), RootFolderPath: types.StringValue("/books"), MonitorNewItems: types.StringValue("new"), QualityProfileID: types.Int64Value(2), MetadataProfileID: types.Int64Value(3), ListType: types.StringValue("other"), MinRefreshInterval: types.StringValue("01:00:00"), HardcoverUsername: types.StringNull(), HardcoverAvatarURL: types.StringNull()}
	plan := stateForResource(t, instance, model)
	response := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", response.Diagnostics)
	}
	var state importListModel
	response.Diagnostics.Append(response.State.Get(t.Context(), &state)...)
	if response.Diagnostics.HasError() || state.Name.ValueString() != "server-list" || state.FieldValuesJSON.ValueString() != `{"listId":"abc"}` || state.FieldValuesSHA256.ValueString() == "" || !state.SecretFields.IsNull() || strings.Contains(response.State.Raw.String(), "redacted") {
		t.Fatalf("unsafe import-list state: %#v diagnostics=%v", state, response.Diagnostics)
	}
	if strings.Contains(strings.Join(requests, ","), "/test") || strings.Contains(strings.Join(requests, ","), "/action") {
		t.Fatal("import-list lifecycle invoked action endpoint")
	}
}

func TestImportListExclusionAllNormalizationAndPerIDRoutes(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	payload := importListExclusionPayload(importListExclusionModel{ForeignID: types.StringValue(" author-1 "), AuthorName: types.StringValue(" Author "), MediaType: types.StringValue("all")}, 0, &diagnostics)
	if diagnostics.HasError() || payload.ForeignID != "author-1" || payload.MediaType != "" {
		t.Fatalf("unexpected exclusion payload %#v diagnostics=%v", payload, diagnostics)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "bulk") {
			t.Fatal("bulk endpoint must never be used")
		}
		switch request.Method {
		case http.MethodPost:
			_, _ = writer.Write([]byte(`{"id":8}`))
		case http.MethodPut:
		case http.MethodDelete:
			writer.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "exclusion-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if id := createProfile(t.Context(), apiClient, "/api/v1/importlistexclusion", payload, "exclusion", &diagnostics); id != 8 {
		t.Fatalf("create id=%d diagnostics=%v", id, diagnostics)
	}
	updateProfile(t.Context(), apiClient, "/api/v1/importlistexclusion/8", payload, "exclusion", &diagnostics)
	deleteProfile(t.Context(), apiClient, "/api/v1/importlistexclusion/", types.StringValue("8"), "exclusion", &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("per-ID lifecycle failed: %v", diagnostics)
	}
}

func hardcoverConfigWithToken(t *testing.T, instance resource.Resource, token string) tfsdk.Config {
	t.Helper()
	response := &resource.SchemaResponse{}
	instance.Schema(t.Context(), resource.SchemaRequest{}, response)
	terraformType := response.Schema.Type().TerraformType(t.Context())
	objectType := terraformType.(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	values["token"] = tftypes.NewValue(tftypes.String, token)
	return tfsdk.Config{Raw: tftypes.NewValue(terraformType, values), Schema: response.Schema}
}

func hardcoverConfigWithControls(t *testing.T, instance resource.Resource, observeServer bool) tfsdk.Config {
	t.Helper()
	response := &resource.SchemaResponse{}
	instance.Schema(t.Context(), resource.SchemaRequest{}, response)
	terraFormType := response.Schema.Type().TerraformType(t.Context())
	objectType := terraFormType.(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	values["observe_server"] = tftypes.NewValue(tftypes.Bool, observeServer)
	return tfsdk.Config{Raw: tftypes.NewValue(terraFormType, values), Schema: response.Schema}
}
