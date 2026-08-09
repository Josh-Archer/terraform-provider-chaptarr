package provider

import (
	"context"
	"io"
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
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestLibrarySchemasValidate(t *testing.T) {
	t.Parallel()
	for _, instance := range []resource.Resource{&authorResource{}, &seriesResource{}} {
		response := &resource.SchemaResponse{}
		instance.Schema(t.Context(), resource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%T schema invalid: %v", instance, diagnostics)
		}
	}
	for _, instance := range []datasource.DataSource{newAuthorLookupDataSource(), newSeriesLookupDataSource()} {
		response := &datasource.SchemaResponse{}
		instance.Schema(t.Context(), datasource.SchemaRequest{}, response)
		if diagnostics := response.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%T schema invalid: %v", instance, diagnostics)
		}
	}
}

func TestAuthorLifecycleDefaultsDangerousFlagsFalse(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, request.Method+" "+request.URL.RequestURI()+" "+string(body))
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v1/author":
			if request.URL.Query().Get("queueIfUnavailable") != "false" || strings.Contains(string(body), `"searchForMissingBooks":true`) {
				t.Fatal("create enabled background work")
			}
			_, _ = writer.Write([]byte(`41`))
		case "GET /api/v1/author/41":
			_, _ = writer.Write([]byte(`{"id":41,"foreignAuthorId":"hc:191785","authorName":"Fixture","monitored":true,"audiobookMonitorExisting":1,"audiobookMonitorFuture":true,"ebookMonitorExisting":0,"ebookMonitorFuture":false,"audiobookRootFolderPath":"/audio","audiobookQualityProfileId":2,"audiobookMetadataProfileId":3,"audiobookTags":[7],"ebookTags":[]}`))
		case "DELETE /api/v1/author/41":
			if request.URL.Query().Get("deleteFiles") != "false" || request.URL.Query().Get("addImportListExclusion") != "false" || request.URL.Query().Get("readdAuthor") != "false" {
				t.Fatal("destroy did not fail closed")
			}
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "author-test-key", UserAgent: "test/1.0"})
	instance := &authorResource{client: apiClient}
	model := authorModel{ID: types.StringUnknown(), ForeignAuthorID: types.StringValue("hc:191785"), AuthorName: types.StringUnknown(), Monitored: types.BoolValue(true), AudiobookMonitorExisting: types.Int64Value(1), AudiobookMonitorFuture: types.BoolValue(true), EbookMonitorExisting: types.Int64Value(0), EbookMonitorFuture: types.BoolValue(false), AudiobookRootFolderPath: types.StringValue("/audio"), EbookRootFolderPath: types.StringNull(), AudiobookQualityProfileID: types.Int64Value(2), EbookQualityProfileID: types.Int64Null(), AudiobookMetadataProfileID: types.Int64Value(3), EbookMetadataProfileID: types.Int64Null(), AudiobookTags: testInt64Set(t, 7), EbookTags: testInt64Set(t), SearchForMissingBooks: types.BoolNull(), MoveFilesOnUpdate: types.BoolNull(), DeleteFilesOnDestroy: types.BoolNull(), AddImportListExclusion: types.BoolNull()}
	plan := stateForResource(t, instance, model)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", created.Diagnostics)
	}
	var state authorModel
	created.Diagnostics.Append(created.State.Get(t.Context(), &state)...)
	if created.Diagnostics.HasError() || state.SearchForMissingBooks.ValueBool() || state.MoveFilesOnUpdate.ValueBool() || state.DeleteFilesOnDestroy.ValueBool() || state.AddImportListExclusion.ValueBool() {
		t.Fatalf("unsafe author state: %#v diagnostics=%v", state, created.Diagnostics)
	}
	deleted := &resource.DeleteResponse{}
	instance.Delete(t.Context(), resource.DeleteRequest{State: created.State}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatalf("delete failed: %v", deleted.Diagnostics)
	}
	for _, value := range requests {
		if strings.Contains(value, "/downloadmedia") || strings.Contains(value, "moveFiles=true") || strings.Contains(value, "deleteFiles=true") {
			t.Fatalf("unsafe request: %s", value)
		}
	}
}

func TestSeriesIntentCreateRefreshAndDestroyNeverCallsAction(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, request.Method+" "+request.URL.RequestURI()+" "+string(body))
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v1/series/add":
			if strings.Contains(string(body), "search") || strings.Contains(string(body), "delete") || strings.Contains(string(body), "move") {
				t.Fatalf("unsafe payload: %s", body)
			}
			_, _ = writer.Write([]byte(`{"success":true,"addedAuthors":[{"id":7}]}`))
		case "GET /api/v1/series":
			_, _ = writer.Write([]byte(`[{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"ebook"}]`))
		case "GET /api/v1/series/12":
			_, _ = writer.Write([]byte(`{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"ebook"}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
	instance := &seriesResource{client: apiClient}
	book, diagnostics := types.ObjectValue(seriesBookType().AttrTypes, map[string]attr.Value{"foreign_book_id": types.StringValue("hc:book-3"), "foreign_author_id": types.StringValue("hc:author-4")})
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	model := seriesModel{ID: types.StringUnknown(), ForeignSeriesID: types.StringValue("hc:series-2"), Title: types.StringUnknown(), MediaType: types.StringValue("ebook"), SelectedBooks: types.SetValueMust(seriesBookType(), []attr.Value{book}), MonitorExisting: types.StringValue("select"), MonitorFuture: types.BoolValue(false), RootFolderPath: types.StringValue("/ebooks"), QualityProfileID: types.Int64Value(2), MetadataProfileID: types.Int64Value(3), Tags: testInt64Set(t, 5)}
	plan := stateForResource(t, instance, model)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", created.Diagnostics)
	}
	deleted := &resource.DeleteResponse{}
	instance.Delete(t.Context(), resource.DeleteRequest{State: created.State}, deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatalf("destroy failed: %v", deleted.Diagnostics)
	}
	for _, value := range requests {
		if strings.HasPrefix(value, "DELETE ") || strings.Contains(value, "download") || strings.Contains(value, "search") {
			t.Fatalf("unsafe series request: %s", value)
		}
	}
}

func TestAuthorUpdateOverlayPreservesServerOwnedMetadata(t *testing.T) {
	t.Parallel()
	payload := map[string]any{"overview": "server-owned", "images": []any{"cover"}, "path": "/existing/path"}
	model := authorModel{ForeignAuthorID: types.StringValue("gr:77"), Monitored: types.BoolValue(false), AudiobookMonitorExisting: types.Int64Value(0), AudiobookMonitorFuture: types.BoolValue(false), EbookMonitorExisting: types.Int64Value(0), EbookMonitorFuture: types.BoolValue(false), AudiobookRootFolderPath: types.StringNull(), EbookRootFolderPath: types.StringNull(), AudiobookQualityProfileID: types.Int64Null(), EbookQualityProfileID: types.Int64Null(), AudiobookMetadataProfileID: types.Int64Null(), EbookMetadataProfileID: types.Int64Null(), AudiobookTags: testInt64Set(t), EbookTags: testInt64Set(t), SearchForMissingBooks: types.BoolValue(false)}
	var diagnostics diag.Diagnostics
	overlayAuthorPayload(t.Context(), payload, model, &diagnostics)
	if diagnostics.HasError() || payload["overview"] != "server-owned" || payload["path"] != "/existing/path" {
		t.Fatalf("overlay lost server metadata: %#v diagnostics=%v", payload, diagnostics)
	}
	options := payload["addOptions"].(map[string]any)
	if options["searchForMissingBooks"] != false {
		t.Fatalf("overlay enabled search: %#v", options)
	}
}

func TestLibraryLookupsAreGETOnlyAndIdentifiersHideQueries(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		if request.Method != http.MethodGet {
			t.Fatalf("lookup mutated with %s", request.Method)
		}
		_, _ = writer.Write([]byte(`[{"foreignAuthorId":"hc:result"}]`))
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "lookup-key", UserAgent: "test/1.0"})
	author := &libraryLookupDataSource{client: apiClient, kind: "author"}
	req, resp := dataSourceRequest(t, author, map[string]tftypes.Value{"term": tftypes.NewValue(tftypes.String, "private-looking author")})
	author.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("author lookup failed: %v", resp.Diagnostics)
	}
	var authorState authorLookupModel
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &authorState)...)
	if strings.Contains(authorState.ID.ValueString(), "private-looking") {
		t.Fatal("lookup ID retained raw query")
	}
	series := &libraryLookupDataSource{client: apiClient, kind: "series"}
	req, resp = dataSourceRequest(t, series, map[string]tftypes.Value{"foreign_series_id": tftypes.NewValue(tftypes.String, "hc:private-series"), "metadata_provider": tftypes.NewValue(tftypes.String, "hardcover")})
	series.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("series lookup failed: %v", resp.Diagnostics)
	}
	var seriesState seriesLookupModel
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &seriesState)...)
	if strings.Contains(seriesState.ID.ValueString(), "private-series") || len(requests) != 2 {
		t.Fatalf("unsafe lookup state/requests: %#v %#v", seriesState, requests)
	}
}

func TestLibraryImportsUseOnlyLocalNumericIdentifiers(t *testing.T) {
	t.Parallel()
	for _, instance := range []resource.Resource{&authorResource{}, &seriesResource{}} {
		response := &resource.ImportStateResponse{State: emptyStateForResource(t, instance)}
		instance.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: "17"}, response)
		if response.Diagnostics.HasError() {
			t.Fatalf("%T import failed: %v", instance, response.Diagnostics)
		}
		var id types.String
		response.Diagnostics.Append(response.State.GetAttribute(t.Context(), path.Root("id"), &id)...)
		if id.ValueString() != "17" {
			t.Fatalf("%T imported id=%q", instance, id.ValueString())
		}
	}
	if got := libraryMutationError(&client.APIError{StatusCode: http.StatusConflict}, "author", "hc:7"); !strings.Contains(got, "Import") || !strings.Contains(got, "provider-prefixed") {
		t.Fatalf("conflict diagnostic is not actionable: %s", got)
	}
}

func testInt64Set(t *testing.T, values ...int64) types.Set {
	t.Helper()
	result, diagnostics := types.SetValueFrom(t.Context(), types.Int64Type, values)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return result
}
