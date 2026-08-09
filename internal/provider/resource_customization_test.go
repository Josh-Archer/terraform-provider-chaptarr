package provider

import (
	"context"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const customizationSecretSentinel = "customization-secret-sentinel-7391"

func TestProxyPasswordIsApplyOnlyAndRequestScoped(t *testing.T) {
	t.Parallel()
	response := &resource.SchemaResponse{}
	(&proxyResource{}).Schema(t.Context(), resource.SchemaRequest{}, response)
	attribute := response.Schema.Attributes["password"].(schema.StringAttribute)
	if !attribute.Sensitive || !attribute.WriteOnly || attribute.Computed {
		t.Fatalf("proxy password must be Sensitive+WriteOnly and not Computed: %#v", attribute)
	}
	typeValue := response.Schema.Type().TerraformType(t.Context())
	objectType := typeValue.(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	values["password"] = tftypes.NewValue(tftypes.String, customizationSecretSentinel)
	config := tfsdk.Config{Raw: tftypes.NewValue(typeValue, values), Schema: response.Schema}
	plan := proxyModel{Password: types.StringNull()}
	var diagnostics diag.Diagnostics
	loadProxyPassword(t.Context(), config, &plan, &diagnostics)
	if diagnostics.HasError() || proxyPayload(plan, 0).Password != customizationSecretSentinel {
		t.Fatalf("write-only proxy password did not reach request payload: %v", diagnostics)
	}
	plan.Password = types.StringNull()
	if strings.Contains(plan.Password.String(), customizationSecretSentinel) {
		t.Fatal("proxy password remained in state model")
	}
}

func TestMetadataFieldPrivacyRejectsSecretInStatefulJSON(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/metadata/schema" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		_, _ = writer.Write([]byte(`[{"implementation":"Hardcover","configContract":"HardcoverSettings","fields":[{"name":"url","type":"textbox"},{"name":"apiToken","privacy":"apiKey","type":"password","value":"********"}]}]`))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "metadata-validation-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &metadataResource{client: apiClient}
	payload := metadataAPI{Implementation: "Hardcover", ConfigContract: "HardcoverSettings", Fields: []providerFieldAPI{{Name: "apiToken", Value: customizationSecretSentinel}}}
	var diagnostics diag.Diagnostics
	if instance.validateFieldPrivacy(t.Context(), payload, &diagnostics) || !diagnostics.HasError() {
		t.Fatal("secret placed in stateful JSON must be rejected")
	}
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Detail(), customizationSecretSentinel) {
			t.Fatal("privacy diagnostic leaked the secret value")
		}
	}
	diagnostics = nil
	payload.Fields = []providerFieldAPI{{Name: "url", Value: "https://metadata.example.test"}, {Name: "apiToken", Value: customizationSecretSentinel, Privacy: "password", Type: "password"}}
	if !instance.validateFieldPrivacy(t.Context(), payload, &diagnostics) || diagnostics.HasError() {
		t.Fatalf("properly separated metadata fields were rejected: %v", diagnostics)
	}
}

func TestMetadataTemplateSanitizationRecursesIntoPresets(t *testing.T) {
	t.Parallel()
	value := []any{map[string]any{"fields": []any{map[string]any{"name": "topSecret", "privacy": "apiKey", "value": customizationSecretSentinel}}, "presets": []any{map[string]any{"fields": []any{map[string]any{"name": "nestedSecret", "type": "password", "value": customizationSecretSentinel}}}}}}
	canonical, _ := canonicalValue(sanitizeMetadataTemplates(value))
	if strings.Contains(canonical, customizationSecretSentinel) || strings.Count(canonical, `"value":null`) != 2 {
		t.Fatalf("recursive schema sanitization failed: %s", canonical)
	}
}

func TestCanonicalCustomizationHashIgnoresJSONFormattingAndKeyOrder(t *testing.T) {
	t.Parallel()
	var firstDiagnostics, secondDiagnostics diag.Diagnostics
	firstCanonical, first := canonicalObjectArray(`[{"name":"Words","implementation":"ReleaseTitleSpecification","fields":[{"name":"value","value":"abc"}]}]`, "specifications_json", &firstDiagnostics)
	secondCanonical, second := canonicalObjectArray(`[ { "implementation" : "ReleaseTitleSpecification", "fields" : [ { "value" : "abc", "name" : "value" } ], "name" : "Words" } ]`, "specifications_json", &secondDiagnostics)
	_, firstHash := canonicalValue(first)
	_, secondHash := canonicalValue(second)
	if firstDiagnostics.HasError() || secondDiagnostics.HasError() || firstCanonical != secondCanonical || firstHash != secondHash {
		t.Fatalf("canonical specification hash drifted: %s %s", firstHash, secondHash)
	}
}

func TestMetadataTagsPreserveExternalAssociationsWhenOmitted(t *testing.T) {
	t.Parallel()
	response := &resource.SchemaResponse{}
	(&metadataResource{}).Schema(t.Context(), resource.SchemaRequest{}, response)
	attribute := response.Schema.Attributes["tags"].(schema.SetAttribute)
	if !attribute.Optional || !attribute.Computed || attribute.Required {
		t.Fatalf("metadata tags must be Optional+Computed for drift-safe preservation: %#v", attribute)
	}
}

func TestCustomizationSchemasAndValidation(t *testing.T) {
	t.Parallel()
	resources := []resource.Resource{&metadataResource{}, &customFormatResource{}, &customFilterResource{}, &tagResource{}, &proxyResource{}}
	for _, instance := range resources {
		response := &resource.SchemaResponse{}
		instance.Schema(t.Context(), resource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%T schema invalid: %v", instance, diagnostics)
		}
	}
	dataSources := []interface {
		Schema(context.Context, datasource.SchemaRequest, *datasource.SchemaResponse)
	}{&customizationSchemaDataSource{kind: "metadata"}, &customizationSchemaDataSource{kind: "custom_format"}, &tagDetailsDataSource{}}
	for _, instance := range dataSources {
		response := &datasource.SchemaResponse{}
		instance.Schema(t.Context(), datasource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%T schema invalid: %v", instance, diagnostics)
		}
	}

	var diagnostics diag.Diagnostics
	customFormatPayload(customFormatModel{SpecificationsJSON: types.StringValue(`[]`)}, 0, &diagnostics)
	if !diagnostics.HasError() {
		t.Fatal("empty custom-format specifications must be rejected")
	}
	diagnostics = nil
	customFilterPayload(customFilterModel{Type: types.StringValue("author"), Label: types.StringValue("filter"), FiltersJSON: types.StringValue(`not-json`)}, 0, &diagnostics)
	if !diagnostics.HasError() {
		t.Fatal("invalid custom-filter JSON must be rejected")
	}
	diagnostics = nil
	if validateProxy(proxyModel{Name: types.StringValue("proxy"), Hostname: types.StringValue("host"), Username: types.StringNull(), Password: types.StringValue(customizationSecretSentinel)}, &diagnostics) || !diagnostics.HasError() {
		t.Fatal("proxy password without username must be rejected")
	}
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Detail(), customizationSecretSentinel) {
			t.Fatal("validation diagnostic leaked proxy password")
		}
	}
}

func TestCustomizationCRUDRoutesAndDelete404Tolerance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		collection string
		payload    any
	}{
		{"tag", "/api/v1/tag", tagAPI{Label: "managed"}},
		{"proxy", "/api/v1/settings/proxy", proxyAPI{Name: "proxy", Type: "http", Hostname: "proxy.example.test", Port: 8080}},
		{"custom filter", "/api/v1/customfilter", customFilterAPI{Type: "author", Label: "managed", Filters: []any{map[string]any{"key": "monitored"}}}},
		{"custom format", "/api/v1/customformat", customFormatAPI{Name: "preferred", AppliesTo: "both", Specifications: []map[string]any{{"name": "title", "implementation": "ReleaseTitleSpecification"}}}},
		{"metadata provider", "/api/v1/metadata", metadataAPI{Name: "metadata", Implementation: "Fixture", ConfigContract: "FixtureSettings"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests = append(requests, request.Method+" "+request.URL.Path)
				body, _ := io.ReadAll(request.Body)
				if request.Method != http.MethodDelete && len(body) == 0 {
					t.Fatal("mutation request omitted its JSON payload")
				}
				if request.Method == http.MethodPost {
					writer.WriteHeader(http.StatusCreated)
					_, _ = writer.Write([]byte(`{"id":27}`))
					return
				}
				if request.Method == http.MethodDelete {
					writer.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "crud-route-key", UserAgent: "test/1.0"})
			if err != nil {
				t.Fatal(err)
			}
			var diagnostics diag.Diagnostics
			if id := createProfile(t.Context(), apiClient, test.collection, test.payload, test.name, &diagnostics); id != 27 {
				t.Fatalf("create id = %d, diagnostics=%v", id, diagnostics)
			}
			updateProfile(t.Context(), apiClient, test.collection+"/27", test.payload, test.name, &diagnostics)
			deleteProfile(t.Context(), apiClient, test.collection+"/", types.StringValue("27"), test.name, &diagnostics)
			if diagnostics.HasError() {
				t.Fatalf("lifecycle diagnostics: %v", diagnostics)
			}
			want := []string{"POST " + test.collection, "PUT " + test.collection + "/27", "DELETE " + test.collection + "/27"}
			if strings.Join(requests, ",") != strings.Join(want, ",") {
				t.Fatalf("routes = %v, want %v", requests, want)
			}
		})
	}
}

func TestTagCreateAndUpdateAlwaysRefreshServerState(t *testing.T) {
	t.Parallel()
	var requests []string
	serverLabel := "created-by-server"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v1/tag":
			_, _ = writer.Write([]byte(`{"id":27}`))
		case "PUT /api/v1/tag/27":
			serverLabel = "updated-by-server"
		case "GET /api/v1/tag/27":
			_, _ = writer.Write([]byte(`{"id":27,"label":"` + serverLabel + `"}`))
		default:
			t.Fatalf("unexpected lifecycle request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "tag-lifecycle-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &tagResource{client: apiClient}
	createPlan := stateForResource(t, instance, tagModel{ID: types.StringNull(), Label: types.StringValue("configured-create")})
	createResponse := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan{Raw: createPlan.Raw, Schema: createPlan.Schema}}, createResponse)
	if createResponse.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", createResponse.Diagnostics)
	}
	var created tagModel
	createResponse.Diagnostics.Append(createResponse.State.Get(t.Context(), &created)...)
	if createResponse.Diagnostics.HasError() || created.ID.ValueString() != "27" || created.Label.ValueString() != "created-by-server" {
		t.Fatalf("create did not refresh server state: %#v diagnostics=%v", created, createResponse.Diagnostics)
	}

	updatePlan := stateForResource(t, instance, tagModel{ID: types.StringValue("27"), Label: types.StringValue("configured-update")})
	updateResponse := &resource.UpdateResponse{State: emptyStateForResource(t, instance)}
	instance.Update(t.Context(), resource.UpdateRequest{Plan: tfsdk.Plan{Raw: updatePlan.Raw, Schema: updatePlan.Schema}}, updateResponse)
	if updateResponse.Diagnostics.HasError() {
		t.Fatalf("update failed: %v", updateResponse.Diagnostics)
	}
	var updated tagModel
	updateResponse.Diagnostics.Append(updateResponse.State.Get(t.Context(), &updated)...)
	if updateResponse.Diagnostics.HasError() || updated.Label.ValueString() != "updated-by-server" {
		t.Fatalf("update did not refresh server state: %#v diagnostics=%v", updated, updateResponse.Diagnostics)
	}
	want := "POST /api/v1/tag,GET /api/v1/tag/27,PUT /api/v1/tag/27,GET /api/v1/tag/27"
	if strings.Join(requests, ",") != want {
		t.Fatalf("lifecycle request sequence = %v", requests)
	}
}

func TestCustomizationRefreshCanonicalizesDriftAndDropsSecrets(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("refresh used %s", request.Method)
		}
		responses := map[string]string{
			"/api/v1/tag/27":            `{"id":27,"label":"server-renamed"}`,
			"/api/v1/settings/proxy/27": `{"id":27,"name":"proxy","type":"http","hostname":"proxy.example.test","port":8080,"username":"user","password":"` + customizationSecretSentinel + `"}`,
			"/api/v1/customfilter/27":   `{"id":27,"type":"author","label":"server-filter","filters":[{"value":true,"key":"monitored"}]}`,
			"/api/v1/customformat/27":   `{"id":27,"name":"server-format","includeCustomFormatWhenRenaming":true,"builtInKey":"","appliesTo":"both","specifications":[{"implementation":"ReleaseTitleSpecification","name":"title"}]}`,
			"/api/v1/metadata/27":       `{"id":27,"name":"server-metadata","implementationName":"Fixture","implementation":"Fixture","configContract":"FixtureSettings","enable":true,"tags":[9,3],"fields":[{"name":"baseUrl","value":"https://metadata.example.test"},{"name":"apiToken","privacy":"apiKey","type":"password","value":"` + customizationSecretSentinel + `"}]}`,
		}
		body, ok := responses[request.URL.Path]
		if !ok {
			t.Fatalf("unexpected refresh path %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "refresh-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}

	filter := customFilterModel{ID: types.StringValue("27")}
	filterState := stateForResource(t, &customFilterResource{}, filter)
	var diagnostics diag.Diagnostics
	(&customFilterResource{client: apiClient}).refresh(t.Context(), &filter, &filterState, &diagnostics)
	if diagnostics.HasError() || filter.Label.ValueString() != "server-filter" || filter.FiltersJSON.ValueString() != `[{"key":"monitored","value":true}]` || filter.FiltersSHA256.ValueString() == "" {
		t.Fatalf("custom-filter refresh failed: %#v diagnostics=%v", filter, diagnostics)
	}

	format := customFormatModel{ID: types.StringValue("27")}
	formatState := stateForResource(t, &customFormatResource{}, format)
	(&customFormatResource{client: apiClient}).refresh(t.Context(), &format, &formatState, &diagnostics)
	if diagnostics.HasError() || format.Name.ValueString() != "server-format" || format.SpecificationsSHA256.ValueString() == "" {
		t.Fatalf("custom-format refresh failed: %#v diagnostics=%v", format, diagnostics)
	}

	metadata := metadataModel{ID: types.StringValue("27"), Tags: types.SetNull(types.Int64Type), SecretFields: types.MapNull(types.StringType), ProtectedFieldNames: types.SetNull(types.StringType)}
	metadataState := stateForResource(t, &metadataResource{}, metadata)
	(&metadataResource{client: apiClient}).refresh(t.Context(), &metadata, &metadataState, &diagnostics)
	if diagnostics.HasError() || metadata.Name.ValueString() != "server-metadata" || metadata.FieldValuesJSON.ValueString() != `{"baseUrl":"https://metadata.example.test"}` || metadata.FieldValuesSHA256.ValueString() == "" || !metadata.SecretFields.IsNull() || strings.Contains(metadataState.Raw.String(), customizationSecretSentinel) {
		t.Fatalf("metadata refresh retained protected data or missed drift: %#v diagnostics=%v", metadata, diagnostics)
	}
	var tags []int64
	diagnostics.Append(metadata.Tags.ElementsAs(t.Context(), &tags, false)...)
	if diagnostics.HasError() || len(tags) != 2 {
		t.Fatalf("metadata external tag associations were not refreshed: %v diagnostics=%v", tags, diagnostics)
	}

	proxy := proxyModel{ID: types.StringValue("27"), Password: types.StringNull()}
	proxyState := stateForResource(t, &proxyResource{}, proxy)
	(&proxyResource{client: apiClient}).refresh(t.Context(), &proxy, &proxyState, &diagnostics)
	if diagnostics.HasError() || proxy.Name.ValueString() != "proxy" || !proxy.Password.IsNull() || strings.Contains(proxyState.Raw.String(), customizationSecretSentinel) {
		t.Fatalf("proxy refresh retained password: %#v diagnostics=%v", proxy, diagnostics)
	}

	tag := tagModel{ID: types.StringValue("27")}
	tagState := stateForResource(t, &tagResource{}, tag)
	(&tagResource{client: apiClient}).refresh(t.Context(), &tag, &tagState, &diagnostics)
	if diagnostics.HasError() || tag.Label.ValueString() != "server-renamed" {
		t.Fatalf("tag refresh missed server drift: %#v diagnostics=%v", tag, diagnostics)
	}
}

func TestCustomizationRead404RemovesStateAndImportsAreNumeric(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "not-found-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	state := stateForResource(t, &tagResource{}, tagModel{ID: types.StringValue("27"), Label: types.StringValue("gone")})
	model := tagModel{ID: types.StringValue("27")}
	var diagnostics diag.Diagnostics
	(&tagResource{client: apiClient}).refresh(t.Context(), &model, &state, &diagnostics)
	if diagnostics.HasError() || !state.Raw.IsNull() {
		t.Fatalf("404 did not remove resource state: %v diagnostics=%v", state.Raw, diagnostics)
	}

	resources := []resource.ResourceWithImportState{&tagResource{}, &proxyResource{}, &customFilterResource{}, &customFormatResource{}, &metadataResource{}}
	for _, instance := range resources {
		response := &resource.ImportStateResponse{State: emptyStateForResource(t, instance)}
		instance.ImportState(t.Context(), resource.ImportStateRequest{ID: "27"}, response)
		var id types.String
		response.Diagnostics.Append(response.State.GetAttribute(t.Context(), path.Root("id"), &id)...)
		if response.Diagnostics.HasError() || id.ValueString() != "27" {
			t.Fatalf("%T import failed: %v", instance, response.Diagnostics)
		}
	}
}

func TestTagDetailsUsesExactReadOnlyRoutesAndTypedAssociations(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/tag/detail/27" {
			t.Fatalf("unexpected tag-details request %s %s", request.Method, request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"id":27,"label":"managed","delayProfileIds":[4],"authorIds":[9,2]}`))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "tag-details-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &tagDetailsDataSource{client: apiClient}
	request, response := dataSourceRequest(t, instance, map[string]tftypes.Value{"tag_id": tftypes.NewValue(tftypes.Number, big.NewFloat(27))})
	instance.Read(t.Context(), request, response)
	var tags types.List
	response.Diagnostics.Append(response.State.GetAttribute(t.Context(), path.Root("tags"), &tags)...)
	if response.Diagnostics.HasError() || len(tags.Elements()) != 1 || strings.Contains(response.State.Raw.String(), "tag-details-key") {
		t.Fatalf("unexpected tag-details state: %v diagnostics=%v", response.State.Raw, response.Diagnostics)
	}
}

func stateForResource(t *testing.T, instance resource.Resource, model any) tfsdk.State {
	t.Helper()
	state := emptyStateForResource(t, instance)
	diagnostics := state.Set(t.Context(), model)
	if diagnostics.HasError() {
		t.Fatalf("unable to construct resource state: %v", diagnostics)
	}
	return state
}

func emptyStateForResource(t *testing.T, instance resource.Resource) tfsdk.State {
	t.Helper()
	response := &resource.SchemaResponse{}
	instance.Schema(t.Context(), resource.SchemaRequest{}, response)
	typeValue := response.Schema.Type().TerraformType(t.Context())
	return tfsdk.State{Raw: tftypes.NewValue(typeValue, tftypes.UnknownValue), Schema: response.Schema}
}
