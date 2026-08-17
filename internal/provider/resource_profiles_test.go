package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestQualityProfilePayloadSupportsAudiobookAndEbook(t *testing.T) {
	t.Parallel()

	for _, mediaType := range []string{"audiobook", "ebook"} {
		t.Run(mediaType, func(t *testing.T) {
			t.Parallel()
			qualityID, secondQualityID, groupID := int64(12), int64(10), int64(100)
			if mediaType == "ebook" {
				qualityID, secondQualityID, groupID = 3, 2, 200
			}
			children := []qualityLeafModel{
				{ID: types.Int64Value(0), Name: types.StringValue(""), QualityID: types.Int64Value(qualityID), QualityName: types.StringValue("first"), QualityIsConversionTarget: types.BoolValue(true), Allowed: types.BoolValue(true)},
				{ID: types.Int64Value(0), Name: types.StringValue(""), QualityID: types.Int64Value(secondQualityID), QualityName: types.StringValue("second"), QualityIsConversionTarget: types.BoolValue(false), Allowed: types.BoolValue(true)},
			}
			var diagnostics diag.Diagnostics
			item := qualityItemModel{ID: types.Int64Value(groupID), Name: types.StringValue(mediaType + " group"), QualityID: types.Int64Null(), QualityName: types.StringNull(), QualityIsConversionTarget: types.BoolNull(), Allowed: types.BoolValue(true), Items: listObjectState(t.Context(), qualityLeafType(), children, &diagnostics)}
			formatOne := formatItemModel{FormatID: types.Int64Value(3), BuiltInKey: types.StringValue("first"), Name: types.StringValue("First"), Score: types.Int64Value(20)}
			formatTwo := formatItemModel{FormatID: types.Int64Value(4), BuiltInKey: types.StringValue("second"), Name: types.StringValue("Second"), Score: types.Int64Value(10)}
			model := qualityProfileModel{Name: types.StringValue("Profile"), ProfileType: types.StringValue(mediaType), UpgradeAllowed: types.BoolValue(true), PreferCustomFormatsOverQuality: types.BoolValue(mediaType == "audiobook"), ConvertToQualityID: types.Int64Value(qualityID), Cutoff: types.Int64Value(qualityID), MinimumFormatScore: types.Int64Value(0), CutoffFormatScore: types.Int64Value(0)}
			model.Items = listObjectState(t.Context(), qualityItemType(), []qualityItemModel{item}, &diagnostics)
			model.FormatItems = listObjectState(t.Context(), formatItemType(), []formatItemModel{formatOne, formatTwo}, &diagnostics)
			payload := qualityProfilePayload(t.Context(), model, 0, &diagnostics)
			if diagnostics.HasError() || !validateQualityProfile(payload, &diagnostics) {
				t.Fatalf("%s payload diagnostics: %v", mediaType, diagnostics)
			}
			if payload.ProfileType != mediaType || payload.ConvertToQualityID == nil || *payload.ConvertToQualityID != qualityID || len(payload.Items) != 1 || payload.Items[0].ID != groupID || payload.Items[0].Name != mediaType+" group" || len(payload.Items[0].Items) != 2 {
				t.Fatalf("unexpected %s payload: %#v", mediaType, payload)
			}
			if len(payload.FormatItems) != 2 || payload.FormatItems[0].Format != 3 || payload.FormatItems[1].Format != 4 {
				t.Fatal("ordered custom-format items were not preserved")
			}
			payload.ID = 44
			roundTripState, ok := qualityProfileState(t.Context(), payload, &diagnostics)
			if !ok {
				t.Fatalf("%s state conversion failed: %v", mediaType, diagnostics)
			}
			roundTrip := qualityProfilePayload(t.Context(), roundTripState, 44, &diagnostics)
			if diagnostics.HasError() || roundTrip.Items[0].ID != groupID || roundTrip.Items[0].Name != mediaType+" group" || roundTrip.Items[0].Items[0].Quality.ID != qualityID || roundTrip.Items[0].Items[1].Quality.ID != secondQualityID {
				t.Fatalf("%s ordered group identity did not round trip: %#v diagnostics=%v", mediaType, roundTrip.Items, diagnostics)
			}
		})
	}
}

func TestQualityProfileValidationRejectsEbookAudiobookPreference(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	payload := qualityProfileAPI{ProfileType: "ebook", PreferCustomFormatsOverQuality: true, Items: []qualityProfileItemAPI{{Quality: &qualityReference{ID: 14}}}}
	if validateQualityProfile(payload, &diagnostics) || !diagnostics.HasError() {
		t.Fatal("expected ebook custom-format preference validation error")
	}
}

func TestQualityProfilePayloadAllowsUnknownTextAndEmptyFormatItems(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	unknownText := qualityItemModel{QualityID: types.Int64Value(0), QualityName: types.StringNull(), QualityIsConversionTarget: types.BoolNull(), Allowed: types.BoolValue(true), Items: types.ListNull(qualityLeafType())}
	azw3 := qualityItemModel{QualityID: types.Int64Value(4), QualityName: types.StringNull(), QualityIsConversionTarget: types.BoolNull(), Allowed: types.BoolValue(true), Items: types.ListNull(qualityLeafType())}
	model := qualityProfileModel{Name: types.StringValue("E-Book"), ProfileType: types.StringValue("ebook"), UpgradeAllowed: types.BoolValue(true), PreferCustomFormatsOverQuality: types.BoolValue(false), Cutoff: types.Int64Value(4), MinimumFormatScore: types.Int64Value(0), CutoffFormatScore: types.Int64Value(0)}
	model.Items = listObjectState(t.Context(), qualityItemType(), []qualityItemModel{unknownText, azw3}, &diagnostics)
	model.FormatItems = listObjectState(t.Context(), formatItemType(), []formatItemModel{}, &diagnostics)
	payload := qualityProfilePayload(t.Context(), model, 1, &diagnostics)
	if diagnostics.HasError() || !validateQualityProfile(payload, &diagnostics) {
		t.Fatalf("unknown-text ebook payload rejected: %v", diagnostics)
	}
	if payload.FormatItems == nil {
		t.Fatal("empty format_items must serialize as an empty JSON array, not null")
	}
	if len(payload.FormatItems) != 0 || len(payload.Items) != 2 || payload.Items[0].Quality == nil || payload.Items[0].Quality.ID != 0 || payload.Items[0].Items == nil {
		t.Fatalf("unexpected unknown-text payload: %#v", payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil || !strings.Contains(string(encoded), `"formatItems":[]`) || !strings.Contains(string(encoded), `"id":0`) {
		t.Fatalf("ebook payload JSON must include empty formatItems and quality 0: %s err=%v", encoded, err)
	}
}

func TestQualityProfileValidationRejectsNegativeQualityID(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	payload := qualityProfileAPI{ProfileType: "ebook", Items: []qualityProfileItemAPI{{Quality: &qualityReference{ID: -1}, Allowed: true, Items: []qualityProfileItemAPI{}}}}
	if validateQualityProfile(payload, &diagnostics) || !diagnostics.HasError() {
		t.Fatal("expected negative quality ID validation error")
	}
}

func TestMergeQualityProfileServerOwnedFillsEmptyNames(t *testing.T) {
	t.Parallel()
	current := qualityProfileAPI{
		ID: 2, Name: "Audiobook", ProfileType: "audiobook", UpgradeAllowed: true, Cutoff: 11,
		Items: []qualityProfileItemAPI{
			{Quality: &qualityReference{ID: 13, Name: "Unknown Audio"}, Allowed: true, Items: []qualityProfileItemAPI{}},
			{Quality: &qualityReference{ID: 12, Name: "M4B", IsConversionTarget: true}, Allowed: true, Items: []qualityProfileItemAPI{}},
		},
		FormatItems: []formatItemAPI{
			{Format: 2, BuiltInKey: "preferred-narrator", Name: "Selected Audiobook Narrators", Score: 50},
			{Format: 1, BuiltInKey: "dramatized-full-cast-audio", Name: "Dramatized / Full-Cast Audio", Score: 0},
		},
	}
	payload := qualityProfileAPI{
		ID: 2, Name: "Audiobook", ProfileType: "audiobook", UpgradeAllowed: true, Cutoff: 11,
		Items: []qualityProfileItemAPI{
			{Quality: &qualityReference{ID: 13}, Allowed: true},
			{Quality: &qualityReference{ID: 12}, Allowed: true},
		},
		FormatItems: []formatItemAPI{{Format: 2, Score: 50}, {Format: 1, Score: 0}},
	}
	mergeQualityProfileServerOwned(current, &payload)
	if payload.Items[0].Quality.Name != "Unknown Audio" || payload.Items[1].Quality.Name != "M4B" || !payload.Items[1].Quality.IsConversionTarget {
		t.Fatalf("quality names were not merged: %#v", payload.Items)
	}
	if payload.FormatItems[0].Name != "Selected Audiobook Narrators" || payload.FormatItems[0].BuiltInKey != "preferred-narrator" || payload.FormatItems[1].Name != "Dramatized / Full-Cast Audio" {
		t.Fatalf("format names were not merged: %#v", payload.FormatItems)
	}
}

func TestQualityProfileUpdateMergesServerOwnedNamesBeforePut(t *testing.T) {
	t.Parallel()
	currentJSON := `{"id":2,"name":"Audiobook","profileType":"audiobook","upgradeAllowed":true,"preferCustomFormatsOverQuality":false,"cutoff":11,"items":[{"quality":{"id":13,"name":"Unknown Audio","isConversionTarget":false},"items":[],"allowed":true},{"quality":{"id":11,"name":"FLAC","isConversionTarget":false},"items":[],"allowed":true}],"minFormatScore":0,"cutoffFormatScore":0,"formatItems":[{"format":2,"builtInKey":"preferred-narrator","name":"Selected Audiobook Narrators","score":50},{"format":1,"builtInKey":"dramatized-full-cast-audio","name":"Dramatized / Full-Cast Audio","score":0}]}`
	var putBody qualityProfileAPI
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/qualityprofile/2":
			gets++
			_, _ = writer.Write([]byte(currentJSON))
		case request.Method == http.MethodPut && request.URL.Path == "/api/v1/qualityprofile/2":
			if err := json.NewDecoder(request.Body).Decode(&putBody); err != nil {
				t.Fatalf("decode put: %v", err)
			}
			_, _ = writer.Write([]byte(currentJSON))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "quality-profile-update-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics diag.Diagnostics
	unknownAudio := qualityItemModel{QualityID: types.Int64Value(13), Allowed: types.BoolValue(true), Items: types.ListNull(qualityLeafType())}
	flac := qualityItemModel{QualityID: types.Int64Value(11), Allowed: types.BoolValue(true), Items: types.ListNull(qualityLeafType())}
	model := qualityProfileModel{ID: types.StringValue("2"), Name: types.StringValue("Audiobook"), ProfileType: types.StringValue("audiobook"), UpgradeAllowed: types.BoolValue(true), PreferCustomFormatsOverQuality: types.BoolValue(false), Cutoff: types.Int64Value(11), MinimumFormatScore: types.Int64Value(0), CutoffFormatScore: types.Int64Value(0)}
	model.Items = listObjectState(t.Context(), qualityItemType(), []qualityItemModel{unknownAudio, flac}, &diagnostics)
	model.FormatItems = listObjectState(t.Context(), formatItemType(), []formatItemModel{{FormatID: types.Int64Value(2), Score: types.Int64Value(50)}, {FormatID: types.Int64Value(1), Score: types.Int64Value(0)}}, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("plan construction failed: %v", diagnostics)
	}
	instance := &qualityProfileResource{client: apiClient}
	plan := stateForResource(t, instance, model)
	response := &resource.UpdateResponse{State: emptyStateForResource(t, instance)}
	instance.Update(t.Context(), resource.UpdateRequest{Plan: tfsdk.Plan(plan), State: plan}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("update failed: %v", response.Diagnostics)
	}
	if gets < 1 {
		t.Fatal("update must GET the current profile before PUT")
	}
	if len(putBody.Items) != 2 || putBody.Items[0].Quality == nil || putBody.Items[0].Quality.Name != "Unknown Audio" || putBody.Items[1].Quality.Name != "FLAC" {
		t.Fatalf("PUT omitted quality names: %#v", putBody.Items)
	}
	if len(putBody.FormatItems) != 2 || putBody.FormatItems[0].Name != "Selected Audiobook Narrators" || putBody.FormatItems[0].BuiltInKey != "preferred-narrator" || putBody.FormatItems[1].Name != "Dramatized / Full-Cast Audio" {
		t.Fatalf("PUT omitted format names: %#v", putBody.FormatItems)
	}
}

func TestQualityProfileStateRejectsUnsupportedDeepNesting(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	_, ok := qualityItemState(t.Context(), []qualityProfileItemAPI{{Items: []qualityProfileItemAPI{{Quality: &qualityReference{ID: 1}, Items: []qualityProfileItemAPI{{Quality: &qualityReference{ID: 2}}}}}}}, &diagnostics)
	if ok || !diagnostics.HasError() {
		t.Fatal("deep quality nesting must produce a deterministic compatibility diagnostic")
	}
}

func TestMetadataProfileSetNormalizationIsStable(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	state := setStringState(t.Context(), []string{" French ", "english", "ENGLISH", "null"}, &diagnostics)
	values := setStringValues(t.Context(), state, &diagnostics)
	if diagnostics.HasError() || strings.Join(values, ",") != "english,French,null" {
		t.Fatalf("unexpected normalized languages %v diagnostics=%v", values, diagnostics)
	}
	model := metadataProfileModel{Name: types.StringValue("Books"), ProfileType: types.StringValue("ebook"), AllowedLanguages: state, Ignored: types.SetNull(types.StringType)}
	payload := metadataProfilePayload(t.Context(), model, 0, &diagnostics)
	if payload.ProfileType != 2 || payload.AllowedLanguages != "english,French,null" {
		t.Fatalf("unexpected metadata payload: %#v", payload)
	}
}

func TestMetadataProfilePayloadNormalizesRawConfiguredSets(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	raw, converted := types.SetValueFrom(t.Context(), types.StringType, []string{" French ", "english", "ENGLISH", "  ", "null"})
	diagnostics.Append(converted...)
	values := setStringValues(t.Context(), raw, &diagnostics)
	if diagnostics.HasError() || strings.Join(values, ",") != "english,French,null" {
		t.Fatalf("unexpected raw configured languages %v diagnostics=%v", values, diagnostics)
	}
	model := metadataProfileModel{Name: types.StringValue("Books"), ProfileType: types.StringValue("ebook"), AllowedLanguages: raw, Ignored: types.SetNull(types.StringType)}
	payload := metadataProfilePayload(t.Context(), model, 0, &diagnostics)
	if diagnostics.HasError() || payload.AllowedLanguages != "english,French,null" {
		t.Fatalf("unexpected normalized metadata payload: %#v diagnostics=%v", payload, diagnostics)
	}
}

func TestReleaseProfilePreservesOrderedTerms(t *testing.T) {
	t.Parallel()
	var diagnostics diag.Diagnostics
	model := releaseProfileModel{Enabled: types.BoolValue(true), Required: listStringState(t.Context(), []string{"first", "second"}, &diagnostics), Ignored: listStringState(t.Context(), []string{"third"}, &diagnostics), Tags: types.SetNull(types.Int64Type)}
	payload := releaseProfilePayload(t.Context(), model, 0, &diagnostics)
	if diagnostics.HasError() || strings.Join(payload.Required, ",") != "first,second" || strings.Join(payload.Ignored, ",") != "third" {
		t.Fatalf("ordered release terms changed: %#v diagnostics=%v", payload, diagnostics)
	}
}

func TestDelayProfileValidationMatchesGlobalRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload delayProfileAPI
		global  bool
	}{
		{name: "no transport", payload: delayProfileAPI{Tags: []int64{1}}, global: false},
		{name: "non-global without tags", payload: delayProfileAPI{EnableUsenet: true}, global: false},
		{name: "global with tags", payload: delayProfileAPI{EnableUsenet: true, Tags: []int64{1}}, global: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var diagnostics diag.Diagnostics
			if validateDelayProfile(test.payload, test.global, &diagnostics) || !diagnostics.HasError() {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestQualityDefinitionAdoptionPreservesServerOwnedFields(t *testing.T) {
	t.Parallel()
	minimum, maximum := 1.0, 10.0
	current := qualityDefinitionAPI{ID: 7, Quality: qualityReference{ID: 12, Name: "M4B"}, Title: "Server title", GroupName: "Audio", GroupWeight: 3, Weight: 4, MinimumSize: &minimum, MaximumSize: &maximum}
	plan := qualityDefinitionModel{MinimumSize: types.Float64Value(2.5), MaximumSize: types.Float64Null()}
	mergeQualityDefinitionPlan(plan, &current)
	if current.Title != "Server title" || current.GroupName != "Audio" || current.MinimumSize == nil || *current.MinimumSize != 2.5 || current.MaximumSize == nil || *current.MaximumSize != 10 {
		t.Fatalf("adoption overwrote server-owned/default values: %#v", current)
	}
}

func TestQualityDefinitionIdentityRequiresReplacement(t *testing.T) {
	t.Parallel()
	response := &resource.SchemaResponse{}
	(&qualityDefinitionResource{}).Schema(t.Context(), resource.SchemaRequest{}, response)
	attribute, ok := response.Schema.Attributes["quality_id"].(resourceschema.Int64Attribute)
	if !ok || len(attribute.PlanModifiers) == 0 {
		t.Fatal("quality_id must have an identity replacement plan modifier")
	}
	modifierResponse := &planmodifier.Int64Response{}
	attribute.PlanModifiers[0].PlanModifyInt64(t.Context(), planmodifier.Int64Request{
		State: tfsdk.State{Raw: tftypes.NewValue(tftypes.String, "existing")}, StateValue: types.Int64Value(12),
		Plan: tfsdk.Plan{Raw: tftypes.NewValue(tftypes.String, "planned")}, PlanValue: types.Int64Value(3),
	}, modifierResponse)
	if modifierResponse.Diagnostics.HasError() || !modifierResponse.RequiresReplace {
		t.Fatalf("changing quality_id must require replacement: %v", modifierResponse.Diagnostics)
	}
}

func TestProfileSchemasExposeNoSensitiveAttributes(t *testing.T) {
	t.Parallel()
	resources := []interface {
		Schema(context.Context, resource.SchemaRequest, *resource.SchemaResponse)
	}{&qualityProfileResource{}, &metadataProfileResource{}, &releaseProfileResource{}, &delayProfileResource{}, &qualityDefinitionResource{}}
	for _, instance := range resources {
		response := &resource.SchemaResponse{}
		instance.Schema(t.Context(), resource.SchemaRequest{}, response)
		diagnostics := response.Schema.ValidateImplementation(t.Context())
		if diagnostics.HasError() {
			t.Fatalf("invalid resource schema: %v", diagnostics)
		}
		for name, attribute := range response.Schema.Attributes {
			if attribute.IsSensitive() {
				t.Fatalf("profile attribute %s is unexpectedly sensitive; profile schemas must contain no credentials", name)
			}
		}
	}
}

func TestProfileDataSourceSchemasAreValid(t *testing.T) {
	t.Parallel()
	dataSources := []datasource.DataSource{&qualityProfileSchemaDataSource{}, &metadataProfileSchemaDataSource{}}
	for _, instance := range dataSources {
		response := &datasource.SchemaResponse{}
		instance.Schema(t.Context(), datasource.SchemaRequest{}, response)
		diagnostics := response.Schema.ValidateImplementation(t.Context())
		if diagnostics.HasError() {
			t.Fatalf("invalid data-source schema: %v", diagnostics)
		}
	}
}

func TestQualityProfileSchemaDataSourceUsesExactMediaTypeAndTypedState(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/qualityprofile/schema" || request.URL.Query().Get("mediaType") != "ebook" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.String())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":0,"name":"New E-Book Profile","profileType":"ebook","upgradeAllowed":true,"preferCustomFormatsOverQuality":false,"convertToQualityId":3,"cutoff":3,"items":[{"id":3,"name":"EPUB","quality":{"id":3,"name":"EPUB","isConversionTarget":true},"items":[],"allowed":true}],"minFormatScore":0,"cutoffFormatScore":0,"formatItems":[{"format":3,"builtInKey":"ebook","name":"Ebook","score":0}]}`))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "profile-schema-test-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &qualityProfileSchemaDataSource{client: apiClient}
	request, response := dataSourceRequest(t, instance, map[string]tftypes.Value{"media_type": tftypes.NewValue(tftypes.String, "ebook")})
	instance.Read(t.Context(), request, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", response.Diagnostics)
	}
	var profileType types.String
	var items types.List
	response.Diagnostics.Append(response.State.GetAttribute(t.Context(), path.Root("profile_type"), &profileType)...)
	response.Diagnostics.Append(response.State.GetAttribute(t.Context(), path.Root("items"), &items)...)
	if response.Diagnostics.HasError() || profileType.ValueString() != "ebook" || len(items.Elements()) != 1 {
		t.Fatalf("unexpected typed schema state: type=%s items=%d diagnostics=%v", profileType.ValueString(), len(items.Elements()), response.Diagnostics)
	}
}

func TestMetadataProfileSchemaDataSourceCombinesDefaultsAndLanguages(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/metadataprofile/schema":
			_, _ = writer.Write([]byte(`{"minPopularity":1.5,"minPages":50,"skipMissingDate":true}`))
		case "/api/v1/metadataprofile/languages":
			_, _ = writer.Write([]byte(`{"languages":[{"name":"English","code":"eng"}],"note":"private server note must not enter state","specialValues":[{"name":"Unknown","code":"null"}]}`))
		default:
			t.Fatalf("unexpected request path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "metadata-schema-test-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &metadataProfileSchemaDataSource{client: apiClient}
	request, response := dataSourceRequest(t, instance, nil)
	instance.Read(t.Context(), request, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", response.Diagnostics)
	}
	if strings.Contains(response.State.Raw.String(), "private server note") || strings.Contains(response.State.Raw.String(), "metadata-schema-test-key") {
		t.Fatal("metadata schema state leaked an API message or credential")
	}
	var languages types.List
	response.Diagnostics.Append(response.State.GetAttribute(t.Context(), path.Root("languages"), &languages)...)
	if response.Diagnostics.HasError() || len(languages.Elements()) != 1 {
		t.Fatalf("unexpected language state: %v", response.Diagnostics)
	}
}

func dataSourceRequest(t *testing.T, instance datasource.DataSource, overrides map[string]tftypes.Value) (datasource.ReadRequest, *datasource.ReadResponse) {
	t.Helper()
	schemaResponse := &datasource.SchemaResponse{}
	instance.Schema(t.Context(), datasource.SchemaRequest{}, schemaResponse)
	typeValue := schemaResponse.Schema.Type().TerraformType(t.Context())
	objectType := typeValue.(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, tftypes.UnknownValue)
	}
	for name, value := range overrides {
		values[name] = value
	}
	raw := tftypes.NewValue(typeValue, values)
	return datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema}}, &datasource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: schemaResponse.Schema}}
}
