package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const readOnlySyntheticKey = "read-only-api-key-sentinel-84ea"

func TestReadOnlyDefinitionsAreUniqueAndNonMutating(t *testing.T) {
	t.Parallel()

	want := []string{
		"api_info", "author_statistics", "blocklist", "calendar", "calendar_feed", "commands", "database_status", "disk_space", "file_system", "health",
		"languages", "library_search", "localization", "media_cover", "parse",
		"remote_path_mapping_suggestions", "remote_path_mappings", "root_folders", "search", "system_routes",
		"system_statistics", "system_status", "tasks", "updates",
	}
	definitions := readOnlyDefinitions()
	got := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		got = append(got, definition.name)
		if definition.request == nil || definition.decode == nil {
			t.Fatalf("definition %q is incomplete", definition.name)
		}
		if definition.name == "bookshelf" {
			t.Fatal("the mutating bookshelf POST must not be registered as a data source")
		}
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("data source names = %v, want %v", got, want)
	}
}

func TestHealthDecoderReturnsOnlyAggregateCheckFields(t *testing.T) {
	t.Parallel()

	state, err := decodeHealth(jsonResponse(`[
		{"source":"DownloadClient","type":"error","message":"internal path and detail"},
		{"source":"Indexer","type":"warning","message":"another internal detail"},
		{"source":"Update","type":"ok","message":"safe"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"has_errors": true, "has_warnings": true, "error_count": int64(1), "warning_count": int64(1)}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("health state = %#v, want %#v", state, want)
	}
	for _, forbidden := range []string{"source", "message", "path", "detail"} {
		if strings.Contains(strings.ToLower(stateString(state)), forbidden) {
			t.Fatalf("health state unexpectedly contains %q", forbidden)
		}
	}
}

func TestSystemStatusDecoderWhitelistsCapabilityFields(t *testing.T) {
	t.Parallel()

	state, err := decodeSystemStatus(jsonResponse(`{
		"appName":"Chaptarr","version":"0.9.925","branch":"develop","databaseType":"sqlite",
		"authentication":"forms","mode":"console","osName":"linux","runtimeVersion":"9.0",
		"installationId":"private-installation-id","startupPath":"/private/start","appData":"/private/data","urlBase":"/secret-base"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 8 || state["version"] != "0.9.925" {
		t.Fatalf("unexpected system state: %#v", state)
	}
	serialized := stateString(state)
	for _, forbidden := range []string{"private-installation-id", "/private/start", "/private/data", "/secret-base"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("system state leaked excluded value %q", forbidden)
		}
	}
}

func TestDatabaseStatusDecoder(t *testing.T) {
	t.Parallel()

	state, err := decodeDatabaseStatus(jsonResponse(`{
		"appName":"Chaptarr","version":"0.9.925","branch":"main","databaseType":"postgres",
		"databaseVersion":"15.4"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if state["database_type"] != "postgres" || state["database_version"] != "15.4" || state["is_postgres"] != true || state["is_healthy"] != true {
		t.Fatalf("unexpected database status state: %#v", state)
	}
}

func TestJSONDecoderCanonicalizesAndRejectsInvalidData(t *testing.T) {
	t.Parallel()

	state, err := jsonDecode(jsonResponse("{\n  \"name\": \"value\", \"count\": 2\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if state["result_json"] != `{"count":2,"name":"value"}` {
		t.Fatalf("result_json = %q", state["result_json"])
	}
	if _, err := jsonDecode(jsonResponse("not-json")); err == nil {
		t.Fatal("invalid JSON must be rejected")
	}
}

func TestContentDefinitionsStoreOnlyMetadata(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"media_cover", "calendar_feed", "system_routes"} {
		definition := definitionByName(t, name)
		state, err := definition.decode(&client.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
			Body:       []byte("private-library-content"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := state["content"]; exists {
			t.Fatalf("%s must not store raw content", name)
		}
		if state["sha256"] == "" || state["content_length"] != int64(23) {
			t.Fatalf("unexpected %s metadata: %#v", name, state)
		}
		if strings.Contains(stateString(state), "private-library-content") {
			t.Fatalf("%s leaked raw content", name)
		}
	}
}

func TestQueryIdentifierDoesNotStoreQueryValues(t *testing.T) {
	t.Parallel()

	requestPath, identifier := queryPath("/api/v1/search", url.Values{"term": []string{"private library title"}})
	if !strings.Contains(requestPath, "private+library+title") {
		t.Fatalf("request path did not contain encoded query: %q", requestPath)
	}
	if strings.Contains(identifier, "private") || strings.Contains(identifier, "title") {
		t.Fatalf("identifier leaked query value: %q", identifier)
	}
}

func TestDerivedIdentifiersDoNotStoreFilenamesOrTags(t *testing.T) {
	t.Parallel()

	for name, identifier := range map[string]string{
		"media_cover":   fingerprintID("media-cover", "book", "42", "private-title.jpg"),
		"calendar_feed": fingerprintID("calendar-feed", "chaptarr", "tags=private-library"),
	} {
		if strings.Contains(identifier, "private") || strings.Contains(identifier, "title") || strings.Contains(identifier, "library") {
			t.Fatalf("%s identifier leaked source values: %q", name, identifier)
		}
	}
}

func TestCalendarFeedExposesPinnedLegacyTagListContract(t *testing.T) {
	t.Parallel()

	definition := definitionByName(t, "calendar_feed")
	if _, ok := definition.attributes["tags"]; !ok {
		t.Fatal("calendar_feed is missing tags")
	}
	if _, ok := definition.attributes["legacy_tag_list"]; !ok {
		t.Fatal("calendar_feed is missing legacy_tag_list for upstream tagList")
	}
}

func TestMediaCoverRequestEscapesFilenameAndHashesIdentifier(t *testing.T) {
	t.Parallel()

	definition := definitionByName(t, "media_cover")
	requestPath, identifier, diagnostics := buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":             unknownString(),
		"kind":           tftypes.NewValue(tftypes.String, "book"),
		"object_id":      tftypes.NewValue(tftypes.Number, int64(42)),
		"filename":       tftypes.NewValue(tftypes.String, "private title.jpg"),
		"content_type":   unknownString(),
		"content_length": unknownNumber(),
		"sha256":         unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if requestPath != "/api/v1/mediacover/book/42/private%20title.jpg" {
		t.Fatalf("request path = %q", requestPath)
	}
	if strings.Contains(identifier, "private") || strings.Contains(identifier, "title") {
		t.Fatalf("identifier leaked filename: %q", identifier)
	}
}

func TestCalendarFeedRequestEscapesBothTagContractsAndHashesIdentifier(t *testing.T) {
	t.Parallel()

	definition := definitionByName(t, "calendar_feed")
	requestPath, identifier, diagnostics := buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":              unknownString(),
		"format":          tftypes.NewValue(tftypes.String, "chaptarr"),
		"past_days":       tftypes.NewValue(tftypes.Number, nil),
		"future_days":     tftypes.NewValue(tftypes.Number, nil),
		"tags":            tftypes.NewValue(tftypes.String, "private current"),
		"legacy_tag_list": tftypes.NewValue(tftypes.String, "private legacy"),
		"unmonitored":     tftypes.NewValue(tftypes.Bool, nil),
		"content_type":    unknownString(),
		"content_length":  unknownNumber(),
		"sha256":          unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if !strings.Contains(requestPath, "tags=private+current") || !strings.Contains(requestPath, "tagList=private+legacy") {
		t.Fatalf("request path did not preserve escaped tag contracts: %q", requestPath)
	}
	if strings.Contains(identifier, "private") || strings.Contains(identifier, "current") || strings.Contains(identifier, "legacy") {
		t.Fatalf("identifier leaked tag filters: %q", identifier)
	}
}

func TestAllReadOnlyDataSourcesExposeNoCredentialAttributes(t *testing.T) {
	t.Parallel()

	for _, definition := range readOnlyDefinitions() {
		instance := &readOnlyDataSource{definition: definition}
		response := &datasource.SchemaResponse{}
		instance.Schema(t.Context(), datasource.SchemaRequest{}, response)
		for name := range response.Schema.Attributes {
			if isCredentialAttributeName(name) {
				t.Fatalf("data source %s exposes credential-like attribute %s", definition.name, name)
			}
		}
	}
}

func TestHealthDataSourcePerformsAuthenticatedGETAndStoresOnlyAggregates(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotKey = r.Method, r.URL.Path, r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"type":"error","message":"private diagnostic detail"}]`))
	}))
	defer server.Close()

	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: readOnlySyntheticKey, UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	definition := definitionByName(t, "health")
	instance := &readOnlyDataSource{definition: definition, client: apiClient}
	schemaResponse := &datasource.SchemaResponse{}
	instance.Schema(t.Context(), datasource.SchemaRequest{}, schemaResponse)
	typeValue := schemaResponse.Schema.Type().TerraformType(context.Background())
	raw := tftypes.NewValue(typeValue, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"has_errors":    tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
		"has_warnings":  tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
		"error_count":   tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		"warning_count": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
	})
	readResponse := &datasource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: schemaResponse.Schema}}
	instance.Read(t.Context(), datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema}}, readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", readResponse.Diagnostics)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/health" || gotKey != readOnlySyntheticKey {
		t.Fatalf("request = %s %s key=%q", gotMethod, gotPath, gotKey)
	}
	var hasErrors types.Bool
	readResponse.Diagnostics.Append(readResponse.State.GetAttribute(t.Context(), path.Root("has_errors"), &hasErrors)...)
	if readResponse.Diagnostics.HasError() || !hasErrors.ValueBool() {
		t.Fatalf("has_errors was not stored: %v", readResponse.Diagnostics)
	}
	if strings.Contains(readResponse.State.Raw.String(), "private diagnostic detail") || strings.Contains(readResponse.State.Raw.String(), readOnlySyntheticKey) {
		t.Fatal("state leaked a health message or API key")
	}
}

func TestCommandsRequest(t *testing.T) {
	t.Parallel()

	definition := definitionByName(t, "commands")

	// Without command_id
	requestPath, identifier, diagnostics := buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":          unknownString(),
		"command_id":  tftypes.NewValue(tftypes.Number, nil),
		"result_json": unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if requestPath != "/api/v1/command" || identifier != "/api/v1/command" {
		t.Fatalf("commands requestPath = %q, identifier = %q", requestPath, identifier)
	}

	// With command_id
	requestPath, identifier, diagnostics = buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":          unknownString(),
		"command_id":  tftypes.NewValue(tftypes.Number, int64(42)),
		"result_json": unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if requestPath != "/api/v1/command/42" || identifier != "/api/v1/command/42" {
		t.Fatalf("commands requestPath = %q, identifier = %q", requestPath, identifier)
	}
}

func TestBlocklistRequest(t *testing.T) {
	t.Parallel()

	definition := definitionByName(t, "blocklist")

	// Empty parameters
	requestPath, identifier, diagnostics := buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":             unknownString(),
		"page":           tftypes.NewValue(tftypes.Number, nil),
		"page_size":      tftypes.NewValue(tftypes.Number, nil),
		"sort_key":       tftypes.NewValue(tftypes.String, nil),
		"sort_direction": tftypes.NewValue(tftypes.String, nil),
		"result_json":    unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if requestPath != "/api/v1/blocklist" || identifier != "/api/v1/blocklist" {
		t.Fatalf("blocklist requestPath = %q, identifier = %q", requestPath, identifier)
	}

	// With query parameters
	requestPath, identifier, diagnostics = buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":             unknownString(),
		"page":           tftypes.NewValue(tftypes.Number, int64(2)),
		"page_size":      tftypes.NewValue(tftypes.Number, int64(25)),
		"sort_key":       tftypes.NewValue(tftypes.String, "date"),
		"sort_direction": tftypes.NewValue(tftypes.String, "ascending"),
		"result_json":    unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if !strings.Contains(requestPath, "page=2") || !strings.Contains(requestPath, "pageSize=25") || !strings.Contains(requestPath, "sortKey=date") || !strings.Contains(requestPath, "sortDirection=ascending") {
		t.Fatalf("blocklist requestPath = %q missing expected query params", requestPath)
	}
	if strings.Contains(identifier, "page") || strings.Contains(identifier, "date") || strings.Contains(identifier, "ascending") {
		t.Fatalf("blocklist identifier leaked query values: %q", identifier)
	}
}

func TestRemotePathMappingSuggestionsRequest(t *testing.T) {
	t.Parallel()

	definition := definitionByName(t, "remote_path_mapping_suggestions")

	// Empty parameters
	requestPath, identifier, diagnostics := buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":                 unknownString(),
		"download_client_id": tftypes.NewValue(tftypes.Number, nil),
		"host":               tftypes.NewValue(tftypes.String, nil),
		"result_json":        unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if requestPath != "/api/v1/remotepathmapping/suggestions" || identifier != "/api/v1/remotepathmapping/suggestions" {
		t.Fatalf("suggestions requestPath = %q, identifier = %q", requestPath, identifier)
	}

	// With query parameters
	requestPath, identifier, diagnostics = buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":                 unknownString(),
		"download_client_id": tftypes.NewValue(tftypes.Number, int64(3)),
		"host":               tftypes.NewValue(tftypes.String, "client.lan"),
		"result_json":        unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if !strings.Contains(requestPath, "downloadClientId=3") || !strings.Contains(requestPath, "host=client.lan") {
		t.Fatalf("suggestions requestPath = %q missing expected query params", requestPath)
	}
	if strings.Contains(identifier, "client.lan") {
		t.Fatalf("suggestions identifier leaked host value: %q", identifier)
	}
}

func TestAuthorStatisticsRequestAndDecoder(t *testing.T) {
	t.Parallel()

	definition := definitionByName(t, "author_statistics")

	// Request builder
	requestPath, identifier, diagnostics := buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":           unknownString(),
		"author_id":    tftypes.NewValue(tftypes.Number, int64(12)),
		"media_type":   tftypes.NewValue(tftypes.String, "audiobook"),
		"size_on_disk": unknownNumber(),
		"result_json":  unknownString(),
	})
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if requestPath != "/api/v1/author/12/size/audiobook" || identifier != "/api/v1/author/12/size/audiobook" {
		t.Fatalf("author_statistics requestPath = %q, identifier = %q", requestPath, identifier)
	}

	// Missing author_id error
	_, _, diagnostics = buildDefinitionRequest(t, definition, map[string]tftypes.Value{
		"id":           unknownString(),
		"author_id":    tftypes.NewValue(tftypes.Number, nil),
		"media_type":   tftypes.NewValue(tftypes.String, "audiobook"),
		"size_on_disk": unknownNumber(),
		"result_json":  unknownString(),
	})
	if !diagnostics.HasError() {
		t.Fatal("expected diagnostics error when author_id is missing")
	}

	// Decoder success
	state, err := decodeAuthorStatistics(jsonResponse("10485760"))
	if err != nil {
		t.Fatalf("decodeAuthorStatistics failed: %v", err)
	}
	if state["size_on_disk"] != int64(10485760) || state["result_json"] != "10485760" {
		t.Fatalf("unexpected author statistics state: %#v", state)
	}

	// Decoder invalid JSON
	if _, err := decodeAuthorStatistics(jsonResponse("not-a-number")); err == nil {
		t.Fatal("expected decodeAuthorStatistics to fail on invalid JSON")
	}
}

func TestCommandsDataSourceEndToEnd(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotKey = r.Method, r.URL.Path, r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"name":"RescanFolders","status":"completed"}]`))
	}))
	defer server.Close()

	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: readOnlySyntheticKey, UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	definition := definitionByName(t, "commands")
	instance := &readOnlyDataSource{definition: definition, client: apiClient}
	schemaResponse := &datasource.SchemaResponse{}
	instance.Schema(t.Context(), datasource.SchemaRequest{}, schemaResponse)
	typeValue := schemaResponse.Schema.Type().TerraformType(context.Background())
	raw := tftypes.NewValue(typeValue, map[string]tftypes.Value{
		"id":          tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"command_id":  tftypes.NewValue(tftypes.Number, nil),
		"result_json": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})
	readResponse := &datasource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: schemaResponse.Schema}}
	instance.Read(t.Context(), datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema}}, readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", readResponse.Diagnostics)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/command" || gotKey != readOnlySyntheticKey {
		t.Fatalf("request = %s %s key=%q", gotMethod, gotPath, gotKey)
	}
	var resultJSON types.String
	readResponse.Diagnostics.Append(readResponse.State.GetAttribute(t.Context(), path.Root("result_json"), &resultJSON)...)
	if readResponse.Diagnostics.HasError() || !strings.Contains(resultJSON.ValueString(), "RescanFolders") {
		t.Fatalf("result_json was not stored properly: %s", resultJSON.ValueString())
	}
}

func TestAuthorStatisticsDataSourceEndToEnd(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotKey = r.Method, r.URL.Path, r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`52428800`))
	}))
	defer server.Close()

	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: readOnlySyntheticKey, UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	definition := definitionByName(t, "author_statistics")
	instance := &readOnlyDataSource{definition: definition, client: apiClient}
	schemaResponse := &datasource.SchemaResponse{}
	instance.Schema(t.Context(), datasource.SchemaRequest{}, schemaResponse)
	typeValue := schemaResponse.Schema.Type().TerraformType(context.Background())
	raw := tftypes.NewValue(typeValue, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"author_id":    tftypes.NewValue(tftypes.Number, int64(5)),
		"media_type":   tftypes.NewValue(tftypes.String, "audiobook"),
		"size_on_disk": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		"result_json":  tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})
	readResponse := &datasource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: schemaResponse.Schema}}
	instance.Read(t.Context(), datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema}}, readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", readResponse.Diagnostics)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/author/5/size/audiobook" || gotKey != readOnlySyntheticKey {
		t.Fatalf("request = %s %s key=%q", gotMethod, gotPath, gotKey)
	}
	var sizeOnDisk types.Int64
	readResponse.Diagnostics.Append(readResponse.State.GetAttribute(t.Context(), path.Root("size_on_disk"), &sizeOnDisk)...)
	if readResponse.Diagnostics.HasError() || sizeOnDisk.ValueInt64() != 52428800 {
		t.Fatalf("size_on_disk was not stored: %v", sizeOnDisk.ValueInt64())
	}
}

func definitionByName(t *testing.T, name string) readOnlyDefinition {
	t.Helper()
	for _, definition := range readOnlyDefinitions() {
		if definition.name == name {
			return definition
		}
	}
	t.Fatalf("definition %q not found", name)
	return readOnlyDefinition{}
}

func buildDefinitionRequest(t *testing.T, definition readOnlyDefinition, values map[string]tftypes.Value) (string, string, diag.Diagnostics) {
	t.Helper()
	instance := &readOnlyDataSource{definition: definition}
	schemaResponse := &datasource.SchemaResponse{}
	instance.Schema(t.Context(), datasource.SchemaRequest{}, schemaResponse)
	typeValue := schemaResponse.Schema.Type().TerraformType(t.Context())
	raw := tftypes.NewValue(typeValue, values)
	readResponse := &datasource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: schemaResponse.Schema}}
	requestPath, identifier := definition.request(t.Context(), datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema}}, readResponse)
	return requestPath, identifier, readResponse.Diagnostics
}

func unknownString() tftypes.Value {
	return tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
}

func unknownNumber() tftypes.Value {
	return tftypes.NewValue(tftypes.Number, tftypes.UnknownValue)
}

func jsonResponse(body string) *client.Response {
	return &client.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(body)}
}

func stateString(state map[string]any) string {
	var builder strings.Builder
	for key, value := range state {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(reflect.ValueOf(value).String())
		builder.WriteString(";")
	}
	return builder.String()
}
