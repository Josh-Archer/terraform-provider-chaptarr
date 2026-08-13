package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestBookSchemaAndReadOnlyBookSchemasValidate(t *testing.T) {
	t.Parallel()
	for _, instance := range []resource.Resource{&bookResource{}, &editionResource{}} {
		response := &resource.SchemaResponse{}
		instance.Schema(t.Context(), resource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%T schema invalid: %v", instance, diagnostics)
		}
	}
	for _, definition := range bookReadOnlyDefinitions() {
		dataSource := newReadOnlyDataSource(definition)()
		response := &datasource.SchemaResponse{}
		dataSource.Schema(t.Context(), datasource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%s schema invalid: %v", definition.name, diagnostics)
		}
	}
}

func TestEditionSelectionLifecycleNormalizesNarratorAndNeverTouchesFiles(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, request.Method+" "+request.URL.RequestURI()+" "+string(body))
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v1/book/51":
			_, _ = writer.Write([]byte(`{"id":51,"foreignBookId":"hc:11","authorId":7,"title":"Book","mediaType":"audiobook","monitored":true,"anyEditionOk":true,"editions":[{"id":61,"foreignEditionId":"hc:edition:22","monitored":false},{"id":62,"foreignEditionId":"hc:edition:23","monitored":true}]}`))
		case "PUT /api/v1/book/51":
			text := string(body)
			if !strings.Contains(text, `"id":61`) || !strings.Contains(text, `"anyEditionOk":false`) {
				t.Fatalf("incorrect edition selection: %s", text)
			}
		case "GET /api/v1/edition":
			if request.URL.Query().Get("bookId") != "51" {
				t.Fatalf("wrong edition query")
			}
			_, _ = writer.Write([]byte(`[{"id":61,"foreignEditionId":"hc:edition:22","title":"Audio","format":"Audible","monitored":true,"narrator":"Narrator One","narratorNames":["Narrator One","Narrator Two"],"durationSeconds":3600}]`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "edition-key", UserAgent: "test/1.0"})
	instance := &editionResource{client: apiClient}
	model := editionModel{ID: types.StringUnknown(), BookID: types.Int64Value(51), EditionID: types.Int64Value(61), ForeignEditionID: types.StringUnknown(), Title: types.StringUnknown(), Format: types.StringUnknown(), Monitored: types.BoolValue(true), Narrator: types.StringUnknown(), NarratorNames: types.SetUnknown(types.StringType), DurationSeconds: types.Int64Unknown()}
	plan := stateForResource(t, instance, model)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("edition create failed: %v", created.Diagnostics)
	}
	var state editionModel
	created.Diagnostics.Append(created.State.Get(t.Context(), &state)...)
	if created.Diagnostics.HasError() || state.ID.ValueString() != "51/61" || state.ForeignEditionID.ValueString() != "hc:edition:22" || state.Narrator.ValueString() != "Narrator One" || state.DurationSeconds.ValueInt64() != 3600 {
		t.Fatalf("unexpected edition state %#v diagnostics=%v", state, created.Diagnostics)
	}
	before := len(requests)
	deleted := &resource.DeleteResponse{}
	instance.Delete(t.Context(), resource.DeleteRequest{State: created.State}, deleted)
	if deleted.Diagnostics.HasError() || len(requests) != before {
		t.Fatalf("edition destroy mutated upstream: requests=%v diagnostics=%v", requests, deleted.Diagnostics)
	}
	for _, value := range requests {
		if strings.Contains(value, "download") || strings.Contains(value, "rename") || strings.Contains(value, "retag") || strings.HasPrefix(value, "DELETE ") {
			t.Fatalf("unsafe edition request: %s", value)
		}
	}
}

func TestBookLifecycleTracksEditionAndNarratorsWithoutFileActions(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, request.Method+" "+request.URL.RequestURI()+" "+string(body))
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v1/book":
			text := string(body)
			if request.URL.Query().Get("mediaType") != "audiobook" || !strings.Contains(text, `"foreignEditionId":"hc:edition:22"`) || !strings.Contains(text, `"monitored":true`) || strings.Contains(text, `"searchForNewBook":true`) {
				t.Fatalf("unsafe/incorrect create: %s %s", request.URL.RequestURI(), text)
			}
			_, _ = writer.Write([]byte(`51`))
		case "GET /api/v1/book/51":
			_, _ = writer.Write([]byte(`{"id":51,"foreignBookId":"hc:11","authorId":7,"title":"Fixture Book","mediaType":"audiobook","monitored":true,"anyEditionOk":false,"narrator":"Narrator One","narratorNames":["Narrator One","Narrator Two"],"editions":[{"id":61,"foreignEditionId":"hc:edition:22","title":"Audio","format":"Audible","monitored":true,"narrator":"Narrator One","narratorNames":["Narrator One","Narrator Two"],"durationSeconds":3600},{"id":62,"foreignEditionId":"hc:edition:23","title":"Other","format":"EPUB","monitored":false,"narratorNames":[]}]}`))
		case "DELETE /api/v1/book/51":
			if request.URL.Query().Get("deleteFiles") != "false" || request.URL.Query().Get("addImportListExclusion") != "false" || request.URL.Query().Get("applyToBothFormats") != "false" {
				t.Fatal("book destroy did not fail closed")
			}
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "book-test-key", UserAgent: "test/1.0"})
	instance := &bookResource{client: apiClient}
	lookup := `{"foreignBookId":"hc:11","title":"Lookup Book","author":{"id":0,"foreignAuthorId":"hc:author:9"},"editions":[{"id":0,"foreignEditionId":"hc:edition:22","title":"Audio","monitored":false},{"id":0,"foreignEditionId":"hc:edition:23","title":"Other","monitored":true}]}`
	model := bookModel{ID: types.StringUnknown(), LookupJSON: types.StringNull(), ForeignBookID: types.StringValue("hc:11"), AuthorID: types.Int64Value(7), Title: types.StringUnknown(), MediaType: types.StringValue("audiobook"), Monitored: types.BoolValue(true), AnyEditionOK: types.BoolValue(false), MonitoredEditionID: types.StringValue("hc:edition:22"), Narrator: types.StringUnknown(), NarratorNames: types.SetUnknown(types.StringType), Editions: types.ListUnknown(bookEditionType()), SearchForNewBook: types.BoolNull(), DeleteFilesOnDestroy: types.BoolNull(), AddImportListExclusion: types.BoolNull(), ApplyDestroyToBothFormats: types.BoolNull()}
	plan := stateForResource(t, instance, model)
	config := configWithStringAttribute(t, instance, "lookup_json", lookup, model)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: config}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", created.Diagnostics)
	}
	var state bookModel
	created.Diagnostics.Append(created.State.Get(t.Context(), &state)...)
	var editions []bookEditionModel
	created.Diagnostics.Append(state.Editions.ElementsAs(t.Context(), &editions, false)...)
	if created.Diagnostics.HasError() || state.LookupJSON.IsUnknown() || !state.LookupJSON.IsNull() || state.MonitoredEditionID.ValueString() != "hc:edition:22" || state.Narrator.ValueString() != "Narrator One" || len(editions) != 2 || editions[0].DurationSeconds.ValueInt64() != 3600 {
		t.Fatalf("unexpected book state: %#v editions=%#v diagnostics=%v", state, editions, created.Diagnostics)
	}
	deleted := &resource.DeleteResponse{}
	instance.Delete(t.Context(), resource.DeleteRequest{State: created.State}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatalf("delete failed: %v", deleted.Diagnostics)
	}
	for _, request := range requests {
		if strings.Contains(request, "/downloadmedia") || strings.Contains(request, "/rename") || strings.Contains(request, "/retag") || strings.Contains(request, "deleteFiles=true") {
			t.Fatalf("unsafe request: %s", request)
		}
	}
}

func TestBookReadOnlyDefinitionsUseOnlyGETAndHashQueries(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("mutating data source request: %s", request.Method)
		}
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "book-read-key", UserAgent: "test/1.0"})
	cases := []struct {
		name      string
		overrides map[string]tftypes.Value
	}{{"book_lookup", map[string]tftypes.Value{"term": tftypes.NewValue(tftypes.String, "private-looking title"), "media_type": tftypes.NewValue(tftypes.String, "ebook")}}, {"editions", map[string]tftypes.Value{"book_id": tftypes.NewValue(tftypes.Number, 9)}}, {"book_file", map[string]tftypes.Value{"book_id": tftypes.NewValue(tftypes.Number, 9)}}, {"rename_book_preview", map[string]tftypes.Value{"book_id": tftypes.NewValue(tftypes.Number, 9)}}, {"retag_book_preview", map[string]tftypes.Value{"book_id": tftypes.NewValue(tftypes.Number, 9)}}}
	definitions := map[string]readOnlyDefinition{}
	for _, definition := range bookReadOnlyDefinitions() {
		definitions[definition.name] = definition
	}
	for _, test := range cases {
		definition := definitions[test.name]
		instance := &readOnlyDataSource{client: apiClient, definition: definition}
		req, resp := dataSourceRequest(t, instance, test.overrides)
		instance.Read(t.Context(), req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s read failed: %v", test.name, resp.Diagnostics)
		}
		var id types.String
		resp.Diagnostics.Append(resp.State.GetAttribute(t.Context(), path.Root("id"), &id)...)
		if strings.Contains(id.ValueString(), "private-looking") {
			t.Fatalf("%s ID retained query", test.name)
		}
	}
}

func TestBookUpdateOverlayPreservesMetadataAndAnyEditionSelection(t *testing.T) {
	t.Parallel()
	var payload map[string]any
	_ = json.Unmarshal([]byte(`{"id":51,"overview":"server-owned","images":[{"url":"cover"}],"editions":[{"id":61,"foreignEditionId":"hc:edition:22","monitored":true},{"id":62,"foreignEditionId":"hc:edition:23","monitored":false}]}`), &payload)
	model := bookModel{ForeignBookID: types.StringValue("hc:11"), Monitored: types.BoolValue(true), AnyEditionOK: types.BoolValue(true), MonitoredEditionID: types.StringNull()}
	var diagnostics diag.Diagnostics
	overlayBookIntent(payload, model, &diagnostics)
	if diagnostics.HasError() || payload["overview"] != "server-owned" {
		t.Fatalf("overlay lost metadata: %#v diagnostics=%v", payload, diagnostics)
	}
	editions := payload["editions"].([]any)
	first := editions[0].(map[string]any)
	if first["monitored"] != true {
		t.Fatalf("any-edition update erased current selection: %#v", editions)
	}
}

func TestBookAndEditionImportsAreLocalOnly(t *testing.T) {
	t.Parallel()
	book := &bookResource{}
	bookResponse := &resource.ImportStateResponse{State: emptyStateForResource(t, book)}
	book.ImportState(t.Context(), resource.ImportStateRequest{ID: "51"}, bookResponse)
	if bookResponse.Diagnostics.HasError() {
		t.Fatalf("book import failed: %v", bookResponse.Diagnostics)
	}
	edition := &editionResource{}
	editionResponse := &resource.ImportStateResponse{State: emptyStateForResource(t, edition)}
	edition.ImportState(t.Context(), resource.ImportStateRequest{ID: "51/61"}, editionResponse)
	if editionResponse.Diagnostics.HasError() {
		t.Fatalf("edition import failed: %v", editionResponse.Diagnostics)
	}
	var state editionModel
	editionResponse.Diagnostics.Append(editionResponse.State.Get(t.Context(), &state)...)
	if editionResponse.Diagnostics.HasError() || state.BookID.ValueInt64() != 51 || state.EditionID.ValueInt64() != 61 || state.ID.ValueString() != "51/61" {
		t.Fatalf("unexpected edition import: %#v diagnostics=%v", state, editionResponse.Diagnostics)
	}
}

func configWithStringAttribute(t *testing.T, instance resource.Resource, name, value string, model any) tfsdk.Config {
	t.Helper()
	state := stateForResource(t, instance, model)
	terraformType := state.Schema.Type().TerraformType(t.Context())
	raw := map[string]tftypes.Value{}
	if err := state.Raw.As(&raw); err != nil {
		t.Fatalf("resource state did not expose an object value: %v", err)
	}
	raw[name] = tftypes.NewValue(tftypes.String, value)
	return tfsdk.Config{Raw: tftypes.NewValue(terraformType, raw), Schema: state.Schema}
}
