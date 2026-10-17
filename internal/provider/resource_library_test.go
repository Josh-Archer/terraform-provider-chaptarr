package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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
			_, _ = writer.Write([]byte(`[{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"ebook","narrator":"Decoded Narrator","localSeriesId":"12","workCount":4,"primaryWorkCount":3}]`))
		case "GET /api/v1/series/12":
			_, _ = writer.Write([]byte(`{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"ebook","narrator":"Decoded Narrator","localSeriesId":"12","workCount":4,"primaryWorkCount":3}`))
		case "GET /api/v1/series/12/narrators":
			_, _ = writer.Write([]byte(`{"seriesId":12,"seriesTitle":"Fixture Series","preferredNarrator":"Analysis Narrator","narratorAnalysis":[{"narrator":"Analysis Narrator","bookCount":2,"percentage":66.5,"hasSequentialOverlap":true,"isMainNarrator":true,"isSporadicNarrator":false}],"sequentialOverlapNarrators":["Analysis Narrator"],"booksWithoutNarratorCount":1,"totalBooksInSeries":4}`))
		case "GET /api/v1/series/12/narrators/inheritance":
			_, _ = writer.Write([]byte(`{"seriesId":12,"seriesTitle":"Fixture Series","preferredSeriesNarrator":"Decoded Narrator","booksWithoutNarrator":[{"bookId":9,"bookTitle":"Missing Narrator","currentNarrator":null,"canInheritFromSeries":true}],"canApplyInheritance":true}`))
		case "GET /api/v1/series/12/variants":
			_, _ = writer.Write([]byte(`[{"id":44,"foreignSeriesId":"hc:series-2","localSeriesId":"44","title":"Fixture Series (Narrated by Analysis Narrator)","mediaType":"audiobook","narrator":"Analysis Narrator","workCount":4,"primaryWorkCount":3,"description":"secret description","images":[{"url":"https://covers.example.test/secret-cover"}],"books":[{"title":"Secret Book"}]}]`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
	instance := &seriesResource{client: apiClient}
	model := seriesTestModel(t)
	plan := stateForResource(t, instance, model)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", created.Diagnostics)
	}
	var createdState seriesModel
	created.Diagnostics.Append(created.State.Get(t.Context(), &createdState)...)
	if created.Diagnostics.HasError() || createdState.PreferredNarrator.ValueString() != "Decoded Narrator" || createdState.ApplyNarratorToBooks.ValueBool() || createdState.OverrideExistingBookNarrators.ValueBool() || len(createdState.RemoveVariantIDs.Elements()) != 0 {
		t.Fatalf("series narrator controls were not observed safely: %#v diagnostics=%v", createdState, created.Diagnostics)
	}
	assertSeriesNarratorObservation(t, createdState)
	if strings.Contains(created.State.Raw.String(), "covers.example.test") || strings.Contains(created.State.Raw.String(), "secret description") || strings.Contains(created.State.Raw.String(), "Secret Book") {
		t.Fatal("series state retained variant images, description, or book payload")
	}
	read := &resource.ReadResponse{State: created.State}
	beforeRead := len(requests)
	instance.Read(t.Context(), resource.ReadRequest{State: created.State}, read)
	if read.Diagnostics.HasError() {
		t.Fatalf("refresh failed: %v", read.Diagnostics)
	}
	for _, value := range requests[beforeRead:] {
		if !strings.HasPrefix(value, "GET ") {
			t.Fatalf("refresh mutated series narrators or variants: %s", value)
		}
	}
	deleted := &resource.DeleteResponse{}
	beforeDelete := len(requests)
	instance.Delete(t.Context(), resource.DeleteRequest{State: created.State}, deleted)
	if deleted.Diagnostics.HasError() || len(requests) != beforeDelete {
		t.Fatalf("destroy changed Chaptarr: diagnostics=%v requests=%v", deleted.Diagnostics, requests[beforeDelete:])
	}
	for _, value := range requests {
		if strings.HasPrefix(value, "DELETE ") || strings.HasPrefix(value, "PUT ") || strings.Contains(value, "/narrators/discover") || strings.Contains(value, "/narrators/search") || strings.Contains(value, "download") {
			t.Fatalf("unsafe series request: %s", value)
		}
	}
}

func TestSeriesPreferredNarratorAndVariantRemoval(t *testing.T) {
	t.Parallel()
	var preferred string
	variants := map[int64]bool{44: true, 99: true}
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, request.Method+" "+request.URL.Path+" "+string(body))
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v1/series/add":
			_, _ = writer.Write([]byte(`{"success":true,"addedAuthors":[{"id":7}]}`))
		case "GET /api/v1/series":
			_, _ = writer.Write([]byte(`[{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"audiobook"}]`))
		case "PUT /api/v1/series/12/narrators/preferred":
			if string(body) != `{"applyToAllBooks":true,"narrator":"Narrator One","overrideExisting":true}` {
				t.Fatalf("preferred narrator payload = %s", body)
			}
			preferred = "Narrator One"
			_, _ = writer.Write([]byte(`{"message":"updated","appliedToBooks":true}`))
		case "GET /api/v1/series/12/variants":
			_, _ = writer.Write([]byte(seriesVariantFixture(variants)))
		case "DELETE /api/v1/series/12/variants/44":
			delete(variants, 44)
			_, _ = writer.Write([]byte(`{"message":"deleted"}`))
		case "DELETE /api/v1/series/12/variants/99":
			delete(variants, 99)
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":"missing"}`))
		case "GET /api/v1/series/12":
			_, _ = writer.Write([]byte(`{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"audiobook","narrator":"` + preferred + `"}`))
		case "GET /api/v1/series/12/narrators":
			_, _ = writer.Write([]byte(`{"seriesId":12,"seriesTitle":"Fixture Series","preferredNarrator":"` + preferred + `","narratorAnalysis":[],"sequentialOverlapNarrators":[],"booksWithoutNarratorCount":0,"totalBooksInSeries":1}`))
		case "GET /api/v1/series/12/narrators/inheritance":
			_, _ = writer.Write([]byte(`{"seriesId":12,"seriesTitle":"Fixture Series","preferredSeriesNarrator":"` + preferred + `","booksWithoutNarrator":[],"canApplyInheritance":false}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
	instance := &seriesResource{client: apiClient}
	model := seriesTestModel(t)
	model.MediaType = types.StringValue("audiobook")
	model.PreferredNarrator = types.StringValue("  Narrator One  ")
	model.ApplyNarratorToBooks = types.BoolValue(true)
	model.OverrideExistingBookNarrators = types.BoolValue(true)
	model.RemoveVariantIDs = testInt64Set(t, 99, 44)
	plan := stateForResource(t, instance, model)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", created.Diagnostics)
	}
	var state seriesModel
	created.Diagnostics.Append(created.State.Get(t.Context(), &state)...)
	var stored []seriesVariantModel
	created.Diagnostics.Append(state.Variants.ElementsAs(t.Context(), &stored, false)...)
	if created.Diagnostics.HasError() || state.PreferredNarrator.ValueString() != "Narrator One" || !state.ApplyNarratorToBooks.ValueBool() || !state.OverrideExistingBookNarrators.ValueBool() || len(stored) != 0 || len(state.RemoveVariantIDs.Elements()) != 2 {
		t.Fatalf("preferred narrator or variant removal was not stored: %#v variants=%#v diagnostics=%v", state, stored, created.Diagnostics)
	}
	deletedVariant := false
	missingVariant := false
	for _, value := range requests {
		if strings.Contains(value, "DELETE /api/v1/series/12/variants/44") {
			deletedVariant = true
		}
		if strings.Contains(value, "DELETE /api/v1/series/12/variants/99") {
			missingVariant = true
		}
		if strings.Contains(value, "/narrators/discover") || strings.Contains(value, "/narrators/search") {
			t.Fatalf("narrator discovery ran during apply: %s", value)
		}
	}
	if !deletedVariant || !missingVariant {
		t.Fatalf("variant deletion was not applied: %v", requests)
	}
	read := &resource.ReadResponse{State: created.State}
	beforeRead := len(requests)
	instance.Read(t.Context(), resource.ReadRequest{State: created.State}, read)
	if read.Diagnostics.HasError() {
		t.Fatalf("refresh failed: %v", read.Diagnostics)
	}
	for _, value := range requests[beforeRead:] {
		if !strings.HasPrefix(value, "GET ") {
			t.Fatalf("refresh repeated narrator or variant mutation: %s", value)
		}
	}
	deleted := &resource.DeleteResponse{}
	beforeDelete := len(requests)
	instance.Delete(t.Context(), resource.DeleteRequest{State: created.State}, deleted)
	if deleted.Diagnostics.HasError() || len(requests) != beforeDelete {
		t.Fatalf("destroy deleted narrator state: diagnostics=%v requests=%v", deleted.Diagnostics, requests[beforeDelete:])
	}
}

func TestSeriesUpdateLeavesObservedNarratorUnmanaged(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, request.Method+" "+request.URL.Path+" "+string(body))
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v1/series/add":
			_, _ = writer.Write([]byte(`{"success":true,"addedAuthors":[{"id":7}]}`))
		case "GET /api/v1/series":
			_, _ = writer.Write([]byte(`[{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"ebook"}]`))
		case "GET /api/v1/series/12":
			_, _ = writer.Write([]byte(`{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"ebook","narrator":"Server Narrator"}`))
		case "GET /api/v1/series/12/narrators":
			_, _ = writer.Write([]byte(`{"seriesId":12,"seriesTitle":"Fixture Series","preferredNarrator":"Server Narrator","narratorAnalysis":[],"sequentialOverlapNarrators":[],"booksWithoutNarratorCount":0,"totalBooksInSeries":1}`))
		case "GET /api/v1/series/12/narrators/inheritance":
			_, _ = writer.Write([]byte(`{"seriesId":12,"seriesTitle":"Fixture Series","preferredSeriesNarrator":"Server Narrator","booksWithoutNarrator":[],"canApplyInheritance":false}`))
		case "GET /api/v1/series/12/variants":
			_, _ = writer.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
	instance := &seriesResource{client: apiClient}
	model := seriesTestModel(t)
	plan := stateForResource(t, instance, model)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create failed: %v", created.Diagnostics)
	}
	var createdState seriesModel
	created.Diagnostics.Append(created.State.Get(t.Context(), &createdState)...)
	if created.Diagnostics.HasError() || createdState.PreferredNarrator.ValueString() != "Server Narrator" {
		t.Fatalf("observed narrator = %#v diagnostics=%v", createdState.PreferredNarrator, created.Diagnostics)
	}
	config := seriesTestModel(t)
	config.ID = createdState.ID
	config.Title = createdState.Title
	config.PreferredNarrator = types.StringNull()
	configPlan := stateForResource(t, instance, config)
	updated := &resource.UpdateResponse{State: emptyStateForResource(t, instance)}
	instance.Update(t.Context(), resource.UpdateRequest{Plan: tfsdk.Plan(created.State), Config: tfsdk.Config(configPlan)}, updated)
	if updated.Diagnostics.HasError() {
		t.Fatalf("update failed: %v", updated.Diagnostics)
	}
	for _, value := range requests {
		if strings.HasPrefix(value, "PUT ") || strings.HasPrefix(value, "DELETE ") || strings.Contains(value, "/narrators/discover") || strings.Contains(value, "/narrators/search") {
			t.Fatalf("unmanaged narrator update mutated series: %s", value)
		}
	}
}

func TestSeriesNarratorControlsFailClosed(t *testing.T) {
	t.Parallel()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v1/series/add":
			_, _ = writer.Write([]byte(`{"success":true,"addedAuthors":[{"id":7}]}`))
		case "GET /api/v1/series":
			_, _ = writer.Write([]byte(`[{"id":12,"foreignSeriesId":"hc:series-2","title":"Fixture Series","mediaType":"ebook"}]`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	apiClient, _ := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
	instance := &seriesResource{client: apiClient}
	for _, mutate := range []func(*seriesModel){
		func(model *seriesModel) { model.OverrideExistingBookNarrators = types.BoolValue(true) },
		func(model *seriesModel) { model.ApplyNarratorToBooks = types.BoolValue(true) },
		func(model *seriesModel) { model.PreferredNarrator = types.StringValue("   ") },
	} {
		requests = nil
		model := seriesTestModel(t)
		mutate(&model)
		plan := stateForResource(t, instance, model)
		created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
		instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, created)
		if !created.Diagnostics.HasError() || len(requests) != 0 {
			t.Fatalf("invalid narrator control reached Chaptarr: diagnostics=%v requests=%v", created.Diagnostics, requests)
		}
	}
	model := seriesTestModel(t)
	model.RemoveVariantIDs = testInt64Set(t, 12)
	plan := stateForResource(t, instance, model)
	created := &resource.CreateResponse{State: emptyStateForResource(t, instance)}
	instance.Create(t.Context(), resource.CreateRequest{Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}, created)
	if !created.Diagnostics.HasError() {
		t.Fatal("managed series id was accepted for variant removal")
	}
	for _, value := range requests {
		if strings.HasPrefix(value, "PUT ") || strings.HasPrefix(value, "DELETE ") || strings.Contains(value, "/variants") {
			t.Fatalf("self variant removal mutated series: %s", value)
		}
	}
}

func TestSeriesSubresourceNotFoundIsNoData(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		http.Error(writer, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	state := seriesTestModel(t)
	var diagnostics diag.Diagnostics
	if ok := (&seriesResource{client: apiClient}).loadSeriesNarratorState(t.Context(), 12, &state, &diagnostics); !ok || diagnostics.HasError() {
		t.Fatalf("404 subresources should be treated as no data: ok=%v diagnostics=%v", ok, diagnostics)
	}
	if !state.NarratorAnalysis.IsNull() || !state.NarratorInheritance.IsNull() || state.Variants.IsNull() || len(state.Variants.Elements()) != 0 {
		t.Fatalf("404 subresource state should be null/empty: analysis=%#v inheritance=%#v variants=%#v", state.NarratorAnalysis, state.NarratorInheritance, state.Variants)
	}
}

func TestSeriesSubresourceNullResponsesAreNoData(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		_, _ = writer.Write([]byte("null"))
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	state := seriesTestModel(t)
	var diagnostics diag.Diagnostics
	if ok := (&seriesResource{client: apiClient}).loadSeriesNarratorState(t.Context(), 12, &state, &diagnostics); !ok || diagnostics.HasError() {
		t.Fatalf("null subresources should be treated as no data: ok=%v diagnostics=%v", ok, diagnostics)
	}
	if !state.NarratorAnalysis.IsNull() || !state.NarratorInheritance.IsNull() || state.Variants.IsNull() || len(state.Variants.Elements()) != 0 {
		t.Fatalf("null subresource state should be null/empty: analysis=%#v inheritance=%#v variants=%#v", state.NarratorAnalysis, state.NarratorInheritance, state.Variants)
	}
}

func TestSeriesSubresourceOtherErrorsStillFail(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "500 endpoint not found", statusCode: http.StatusInternalServerError, body: `{"message":"Endpoint not found"}`},
		{name: "401 auth endpoint not found", statusCode: http.StatusUnauthorized, body: `{"message":"auth endpoint not found"}`},
		{name: "500 temporary server failure", statusCode: http.StatusInternalServerError, body: "temporary server failure"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				http.Error(writer, testCase.body, testCase.statusCode)
			}))
			defer server.Close()
			apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
			if err != nil {
				t.Fatal(err)
			}
			state := seriesTestModel(t)
			var diagnostics diag.Diagnostics
			if ok := (&seriesResource{client: apiClient}).loadSeriesNarratorState(t.Context(), 12, &state, &diagnostics); ok || !diagnostics.HasError() {
				t.Fatalf("non-404 subresource errors must fail refresh: ok=%v diagnostics=%v", ok, diagnostics)
			}
		})
	}
}

func TestSeriesSubresourceDroppedConnectionFails(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack connection: %v", err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: "series-key", UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	state := seriesTestModel(t)
	var diagnostics diag.Diagnostics
	if ok := (&seriesResource{client: apiClient}).loadSeriesNarratorState(t.Context(), 12, &state, &diagnostics); ok || !diagnostics.HasError() {
		t.Fatalf("dropped connection must fail refresh: ok=%v diagnostics=%v", ok, diagnostics)
	}
}

func seriesTestModel(t *testing.T) seriesModel {
	t.Helper()
	book, diagnostics := types.ObjectValue(seriesBookType().AttrTypes, map[string]attr.Value{"foreign_book_id": types.StringValue("hc:book-3"), "foreign_author_id": types.StringValue("hc:author-4")})
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	return seriesModel{ID: types.StringUnknown(), ForeignSeriesID: types.StringValue("hc:series-2"), Title: types.StringUnknown(), MediaType: types.StringValue("ebook"), SelectedBooks: types.SetValueMust(seriesBookType(), []attr.Value{book}), MonitorExisting: types.StringValue("select"), MonitorFuture: types.BoolValue(false), RootFolderPath: types.StringValue("/ebooks"), QualityProfileID: types.Int64Value(2), MetadataProfileID: types.Int64Value(3), Tags: testInt64Set(t, 5), PreferredNarrator: types.StringNull(), ApplyNarratorToBooks: types.BoolNull(), OverrideExistingBookNarrators: types.BoolNull(), NarratorAnalysis: types.ObjectNull(narratorAnalysisAttrTypes()), NarratorInheritance: types.ObjectNull(narratorInheritanceAttrTypes()), Variants: types.ListNull(seriesVariantType()), RemoveVariantIDs: types.SetNull(types.Int64Type)}
}

func assertSeriesNarratorObservation(t *testing.T, state seriesModel) {
	t.Helper()
	var analysis narratorAnalysisModel
	var inheritance narratorInheritanceModel
	diagnostics := state.NarratorAnalysis.As(t.Context(), &analysis, basetypes.ObjectAsOptions{})
	diagnostics.Append(state.NarratorInheritance.As(t.Context(), &inheritance, basetypes.ObjectAsOptions{})...)
	var narrators []narratorAnalysisItemModel
	var books []inheritanceBookModel
	var variants []seriesVariantModel
	diagnostics.Append(analysis.Narrators.ElementsAs(t.Context(), &narrators, false)...)
	diagnostics.Append(inheritance.BooksWithoutNarrator.ElementsAs(t.Context(), &books, false)...)
	diagnostics.Append(state.Variants.ElementsAs(t.Context(), &variants, false)...)
	if diagnostics.HasError() || analysis.PreferredNarrator.ValueString() != "Analysis Narrator" || analysis.TotalBooksInSeries.ValueInt64() != 4 || len(narrators) != 1 || narrators[0].Percentage.ValueFloat64() != 66.5 || !narrators[0].IsMainNarrator.ValueBool() || len(analysis.SequentialOverlapNarrators.Elements()) != 1 || !inheritance.CanApplyInheritance.ValueBool() || len(books) != 1 || books[0].BookID.ValueInt64() != 9 || !books[0].CurrentNarrator.IsNull() || len(variants) != 1 || variants[0].ID.ValueInt64() != 44 || variants[0].Narrator.ValueString() != "Analysis Narrator" || variants[0].WorkCount.ValueInt64() != 4 || variants[0].PrimaryWorkCount.ValueInt64() != 3 {
		t.Fatalf("narrator observation = analysis:%#v narrators:%#v inheritance:%#v books:%#v variants:%#v diagnostics=%v", analysis, narrators, inheritance, books, variants, diagnostics)
	}
}

func seriesVariantFixture(variants map[int64]bool) string {
	parts := make([]string, 0, len(variants))
	for id := range variants {
		parts = append(parts, `{"id":`+strconv.FormatInt(id, 10)+`,"foreignSeriesId":"hc:series-2","localSeriesId":"`+strconv.FormatInt(id, 10)+`","title":"Variant","mediaType":"audiobook","narrator":"Narrator One","workCount":1,"primaryWorkCount":1}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
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
