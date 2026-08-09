package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type readOnlyDataSource struct {
	client     *client.Client
	definition readOnlyDefinition
}

type readOnlyDefinition struct {
	name        string
	description string
	attributes  map[string]schema.Attribute
	request     func(context.Context, datasource.ReadRequest, *datasource.ReadResponse) (string, string)
	decode      func(*client.Response) (map[string]any, error)
}

func newReadOnlyDataSource(definition readOnlyDefinition) func() datasource.DataSource {
	return func() datasource.DataSource { return &readOnlyDataSource{definition: definition} }
}

func (d *readOnlyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.definition.name
}

func (d *readOnlyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Stable identifier for this read-only query."},
	}
	for name, attribute := range d.definition.attributes {
		attributes[name] = attribute
	}
	resp.Schema = schema.Schema{MarkdownDescription: d.definition.description, Attributes: attributes}
}

func (d *readOnlyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	d.client = apiClient
}

func (d *readOnlyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "Configure the Chaptarr provider before reading this data source.")
		return
	}
	requestPath, identifier := d.definition.request(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := d.client.Do(ctx, http.MethodGet, requestPath, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Chaptarr data", err.Error())
		return
	}
	state, err := d.definition.decode(response)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", err.Error())
		return
	}
	state["id"] = identifier
	for name, value := range state {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), value)...)
	}
}

func jsonDecode(response *client.Response) (map[string]any, error) {
	var value any
	if err := json.Unmarshal(response.Body, &value); err != nil {
		return nil, fmt.Errorf("chaptarr returned invalid JSON")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("chaptarr returned data that could not be encoded")
	}
	return map[string]any{"result_json": string(canonical)}, nil
}

func noQuery(requestPath, identifier string) func(context.Context, datasource.ReadRequest, *datasource.ReadResponse) (string, string) {
	return func(context.Context, datasource.ReadRequest, *datasource.ReadResponse) (string, string) {
		return requestPath, identifier
	}
}

func stringInput(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse, name string, required bool) string {
	var value types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &value)...)
	if resp.Diagnostics.HasError() || value.IsNull() || value.IsUnknown() {
		if required && !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Required query value", fmt.Sprintf("`%s` must be known and non-empty.", name))
		}
		return ""
	}
	trimmed := strings.TrimSpace(value.ValueString())
	if required && trimmed == "" {
		resp.Diagnostics.AddAttributeError(path.Root(name), "Required query value", fmt.Sprintf("`%s` must not be empty.", name))
		return ""
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), trimmed)...)
	return trimmed
}

func intInput(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse, name string) (int64, bool) {
	var value types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &value)...)
	if resp.Diagnostics.HasError() || value.IsNull() || value.IsUnknown() {
		return 0, false
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), value.ValueInt64())...)
	return value.ValueInt64(), true
}

func boolInput(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse, name string) (bool, bool) {
	var value types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &value)...)
	if resp.Diagnostics.HasError() || value.IsNull() || value.IsUnknown() {
		return false, false
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), value.ValueBool())...)
	return value.ValueBool(), true
}

func queryPath(base string, values url.Values) (string, string) {
	encoded := values.Encode()
	if encoded == "" {
		return base, base
	}
	return base + "?" + encoded, fingerprintID(base, encoded)
}

func fingerprintID(prefix string, values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(append([]string{prefix}, values...), "\x00")))
	return prefix + ":" + hex.EncodeToString(digest[:8])
}

func resultJSONAttribute() schema.Attribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical bounded JSON returned by Chaptarr. It refreshes whenever Terraform reads the data source."}
}

func readOnlyDefinitions() []readOnlyDefinition {
	definitions := []readOnlyDefinition{
		{
			name: "api_info", description: "Read API compatibility information for module preconditions without changing Chaptarr.",
			attributes: map[string]schema.Attribute{
				"current":    schema.StringAttribute{Computed: true},
				"deprecated": schema.ListAttribute{Computed: true, ElementType: types.StringType},
			}, request: noQuery("/api", "api-info"), decode: decodeAPIInfo,
		},
		{
			name: "languages", description: "Read the configured language catalog or one language by numeric identifier.",
			attributes: map[string]schema.Attribute{"language_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "result_json": resultJSONAttribute()},
			request:    optionalIDRequest("/api/v1/language", "language_id"), decode: jsonDecode,
		},
		jsonDefinition("localization", "Read the active localization dictionary.", "/api/v1/localization"),
		{
			name: "search", description: "Search external metadata without modifying the library.",
			attributes: map[string]schema.Attribute{
				"term":              schema.StringAttribute{Required: true},
				"metadata_provider": schema.StringAttribute{Optional: true, MarkdownDescription: "Optional upstream metadata-provider filter."},
				"result_json":       resultJSONAttribute(),
			}, request: searchRequest("/api/v1/search", true, false), decode: jsonDecode,
		},
		{
			name: "library_search", description: "Search the current Chaptarr library without modifying it.",
			attributes: map[string]schema.Attribute{
				"term":        schema.StringAttribute{Required: true},
				"limit":       schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.Between(1, 1000)}},
				"result_json": resultJSONAttribute(),
			}, request: searchRequest("/api/v1/library/search", false, true), decode: jsonDecode,
		},
		{
			name: "parse", description: "Parse a release title without mutating Chaptarr.",
			attributes: map[string]schema.Attribute{"title": schema.StringAttribute{Required: true}, "result_json": resultJSONAttribute()},
			request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
				title := stringInput(ctx, req, resp, "title", true)
				return queryPath("/api/v1/parse", url.Values{"title": []string{title}})
			}, decode: jsonDecode,
		},
		{
			name: "calendar", description: "Read calendar entries. Results are refreshed on every Terraform read.",
			attributes: map[string]schema.Attribute{
				"calendar_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
				"start":       schema.StringAttribute{Optional: true}, "end": schema.StringAttribute{Optional: true},
				"unmonitored": schema.BoolAttribute{Optional: true}, "include_author": schema.BoolAttribute{Optional: true},
				"result_json": resultJSONAttribute(),
			}, request: calendarRequest, decode: jsonDecode,
		},
		{
			name: "system_routes", description: "Fingerprint registered or duplicate HTTP routes for development capability discovery without storing route details.",
			attributes: map[string]schema.Attribute{
				"duplicates_only": schema.BoolAttribute{Optional: true}, "content_type": schema.StringAttribute{Computed: true},
				"content_length": schema.Int64Attribute{Computed: true}, "sha256": schema.StringAttribute{Computed: true},
			},
			request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
				duplicates, _ := boolInput(ctx, req, resp, "duplicates_only")
				if duplicates {
					return "/api/v1/system/routes/duplicate", "system-routes-duplicate"
				}
				return "/api/v1/system/routes", "system-routes"
			}, decode: fingerprintDecode,
		},
		{
			name: "tasks", description: "Read scheduled task status without starting or cancelling tasks.",
			attributes: map[string]schema.Attribute{"task_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "result_json": resultJSONAttribute()},
			request:    optionalIDRequest("/api/v1/system/task", "task_id"), decode: jsonDecode,
		},
		jsonDefinition("updates", "Read available update metadata without installing updates.", "/api/v1/update"),
		jsonDefinition("disk_space", "Read current disk-space observations. Values are runtime data, not managed resources.", "/api/v1/diskspace"),
		fileSystemDefinition(),
		mediaCoverDefinition(),
		calendarFeedDefinition(),
		{
			name: "health", description: "Read a secret-free health summary suitable for Terraform checks. Raw logs are never returned.",
			attributes: map[string]schema.Attribute{
				"has_errors": schema.BoolAttribute{Computed: true}, "has_warnings": schema.BoolAttribute{Computed: true},
				"error_count": schema.Int64Attribute{Computed: true}, "warning_count": schema.Int64Attribute{Computed: true},
			}, request: noQuery("/api/v1/health", "health"), decode: decodeHealth,
		},
		{
			name: "system_status", description: "Read a deliberately limited, secret-free system capability summary suitable for Terraform checks.",
			attributes: map[string]schema.Attribute{
				"app_name": schema.StringAttribute{Computed: true}, "version": schema.StringAttribute{Computed: true},
				"branch": schema.StringAttribute{Computed: true}, "database_type": schema.StringAttribute{Computed: true},
				"authentication": schema.StringAttribute{Computed: true}, "mode": schema.StringAttribute{Computed: true},
				"os_name": schema.StringAttribute{Computed: true}, "runtime_version": schema.StringAttribute{Computed: true},
			}, request: noQuery("/api/v1/system/status", "system-status"), decode: decodeSystemStatus,
		},
		{
			name: "system_statistics", description: "Read aggregate system statistics without returning item names, paths, or logs.",
			attributes: map[string]schema.Attribute{
				"media_type":  schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("all", "audiobook", "ebook")}},
				"total_books": schema.Int64Attribute{Computed: true}, "monitored_books": schema.Int64Attribute{Computed: true},
				"file_count": schema.Int64Attribute{Computed: true}, "total_file_size": schema.Int64Attribute{Computed: true},
				"author_count": schema.Int64Attribute{Computed: true},
			}, request: statisticsRequest, decode: decodeStatistics,
		},
	}
	return definitions
}

func jsonDefinition(name, description, requestPath string) readOnlyDefinition {
	return readOnlyDefinition{name: name, description: description, attributes: map[string]schema.Attribute{"result_json": resultJSONAttribute()}, request: noQuery(requestPath, name), decode: jsonDecode}
}

func optionalIDRequest(base, attribute string) func(context.Context, datasource.ReadRequest, *datasource.ReadResponse) (string, string) {
	return func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
		if value, ok := intInput(ctx, req, resp, attribute); ok {
			requestPath := base + "/" + strconv.FormatInt(value, 10)
			return requestPath, requestPath
		}
		return base, base
	}
}

func searchRequest(base string, provider, limit bool) func(context.Context, datasource.ReadRequest, *datasource.ReadResponse) (string, string) {
	return func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
		values := url.Values{"term": []string{stringInput(ctx, req, resp, "term", true)}}
		if provider {
			if value := stringInput(ctx, req, resp, "metadata_provider", false); value != "" {
				values.Set("provider", value)
			}
		}
		if limit {
			if value, ok := intInput(ctx, req, resp, "limit"); ok {
				values.Set("limit", strconv.FormatInt(value, 10))
			}
		}
		return queryPath(base, values)
	}
}

func calendarRequest(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
	if value, ok := intInput(ctx, req, resp, "calendar_id"); ok {
		requestPath := "/api/v1/calendar/" + strconv.FormatInt(value, 10)
		return requestPath, requestPath
	}
	values := url.Values{}
	for _, name := range []string{"start", "end"} {
		if value := stringInput(ctx, req, resp, name, false); value != "" {
			values.Set(name, value)
		}
	}
	for name, queryName := range map[string]string{"unmonitored": "unmonitored", "include_author": "includeAuthor"} {
		if value, ok := boolInput(ctx, req, resp, name); ok {
			values.Set(queryName, strconv.FormatBool(value))
		}
	}
	return queryPath("/api/v1/calendar", values)
}

func fileSystemDefinition() readOnlyDefinition {
	return readOnlyDefinition{
		name: "file_system", description: "Read a bounded filesystem lookup. This observes Chaptarr's view of paths and never creates, moves, or deletes files.",
		attributes: map[string]schema.Attribute{
			"operation": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("contents", "type", "media_files")}},
			"path":      schema.StringAttribute{Required: true}, "include_files": schema.BoolAttribute{Optional: true},
			"allow_folders_without_trailing_slashes": schema.BoolAttribute{Optional: true}, "result_json": resultJSONAttribute(),
		},
		request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
			operation := stringInput(ctx, req, resp, "operation", true)
			lookupPath := stringInput(ctx, req, resp, "path", true)
			endpoint := map[string]string{"contents": "/api/v1/filesystem", "type": "/api/v1/filesystem/type", "media_files": "/api/v1/filesystem/mediafiles"}[operation]
			values := url.Values{"path": []string{lookupPath}}
			includeFiles, hasIncludeFiles := boolInput(ctx, req, resp, "include_files")
			allowFolders, hasAllowFolders := boolInput(ctx, req, resp, "allow_folders_without_trailing_slashes")
			if operation == "contents" {
				if hasIncludeFiles {
					values.Set("includeFiles", strconv.FormatBool(includeFiles))
				}
				if hasAllowFolders {
					values.Set("allowFoldersWithoutTrailingSlashes", strconv.FormatBool(allowFolders))
				}
			}
			return queryPath(endpoint, values)
		}, decode: jsonDecode,
	}
}

func mediaCoverDefinition() readOnlyDefinition {
	return readOnlyDefinition{
		name: "media_cover", description: "Read bounded metadata for an existing cover image. Image bytes are hashed and are not stored in Terraform state.",
		attributes: map[string]schema.Attribute{
			"kind":      schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("author", "book")}},
			"object_id": schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"filename":  schema.StringAttribute{Required: true}, "content_type": schema.StringAttribute{Computed: true},
			"content_length": schema.Int64Attribute{Computed: true}, "sha256": schema.StringAttribute{Computed: true},
		},
		request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
			kind := stringInput(ctx, req, resp, "kind", true)
			filename := stringInput(ctx, req, resp, "filename", true)
			if strings.ContainsAny(filename, `/\\`) || filename == "." || filename == ".." {
				resp.Diagnostics.AddAttributeError(path.Root("filename"), "Unsafe cover filename", "`filename` must be a single filename without path separators.")
			}
			objectID, _ := intInput(ctx, req, resp, "object_id")
			segment := "author"
			if kind == "book" {
				segment = "book"
			}
			requestPath := fmt.Sprintf("/api/v1/mediacover/%s/%d/%s", segment, objectID, url.PathEscape(filename))
			return requestPath, fingerprintID("media-cover", kind, strconv.FormatInt(objectID, 10), filename)
		},
		decode: fingerprintDecode,
	}
}

func calendarFeedDefinition() readOnlyDefinition {
	return readOnlyDefinition{
		name: "calendar_feed", description: "Read a calendar feed fingerprint without storing book titles or feed contents in Terraform state.",
		attributes: map[string]schema.Attribute{
			"format":          schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("chaptarr", "readarr")}},
			"past_days":       schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.Between(0, 3650)}},
			"future_days":     schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.Between(0, 3650)}},
			"tags":            schema.StringAttribute{Optional: true},
			"legacy_tag_list": schema.StringAttribute{Optional: true, MarkdownDescription: "Legacy comma-separated tag filter sent as `tagList`; prefer `tags`."},
			"unmonitored":     schema.BoolAttribute{Optional: true},
			"content_type":    schema.StringAttribute{Computed: true}, "content_length": schema.Int64Attribute{Computed: true}, "sha256": schema.StringAttribute{Computed: true},
		},
		request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
			format := stringInput(ctx, req, resp, "format", false)
			if format == "" {
				format = "chaptarr"
				resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("format"), format)...)
			}
			values := url.Values{}
			for name, queryName := range map[string]string{"past_days": "pastDays", "future_days": "futureDays"} {
				if value, ok := intInput(ctx, req, resp, name); ok {
					values.Set(queryName, strconv.FormatInt(value, 10))
				}
			}
			if value := stringInput(ctx, req, resp, "tags", false); value != "" {
				values.Set("tags", value)
			}
			if value := stringInput(ctx, req, resp, "legacy_tag_list", false); value != "" {
				values.Set("tagList", value)
			}
			if value, ok := boolInput(ctx, req, resp, "unmonitored"); ok {
				values.Set("unmonitored", strconv.FormatBool(value))
			}
			endpoint := "/feed/v1/calendar/chaptarr.ics"
			if format == "readarr" {
				endpoint = "/feed/v1/calendar/readarr.ics"
			}
			requestPath, _ := queryPath(endpoint, values)
			return requestPath, fingerprintID("calendar-feed", format, values.Encode())
		},
		decode: fingerprintDecode,
	}
}

func fingerprintDecode(response *client.Response) (map[string]any, error) {
	digest := sha256.Sum256(response.Body)
	return map[string]any{
		"content_type":   response.Header.Get("Content-Type"),
		"content_length": int64(len(response.Body)),
		"sha256":         hex.EncodeToString(digest[:]),
	}, nil
}

func statisticsRequest(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
	values := url.Values{}
	if value := stringInput(ctx, req, resp, "media_type", false); value != "" {
		values.Set("mediaType", value)
	}
	return queryPath("/api/v1/system/statistics", values)
}

func decodeAPIInfo(response *client.Response) (map[string]any, error) {
	var value struct {
		Current    string   `json:"current"`
		Deprecated []string `json:"deprecated"`
	}
	if err := json.Unmarshal(response.Body, &value); err != nil {
		return nil, fmt.Errorf("chaptarr returned invalid API information")
	}
	if value.Deprecated == nil {
		value.Deprecated = []string{}
	}
	return map[string]any{"current": value.Current, "deprecated": value.Deprecated}, nil
}

func decodeHealth(response *client.Response) (map[string]any, error) {
	var values []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(response.Body, &values); err != nil {
		return nil, fmt.Errorf("chaptarr returned invalid health information")
	}
	var errorsCount, warningsCount int64
	for _, value := range values {
		switch strings.ToLower(value.Type) {
		case "error":
			errorsCount++
		case "warning", "notice":
			warningsCount++
		}
	}
	return map[string]any{"has_errors": errorsCount > 0, "has_warnings": warningsCount > 0, "error_count": errorsCount, "warning_count": warningsCount}, nil
}

func decodeSystemStatus(response *client.Response) (map[string]any, error) {
	var value struct {
		AppName        string `json:"appName"`
		Version        string `json:"version"`
		Branch         string `json:"branch"`
		DatabaseType   string `json:"databaseType"`
		Authentication string `json:"authentication"`
		Mode           string `json:"mode"`
		OSName         string `json:"osName"`
		RuntimeVersion string `json:"runtimeVersion"`
	}
	if err := json.Unmarshal(response.Body, &value); err != nil {
		return nil, fmt.Errorf("chaptarr returned invalid system status")
	}
	return map[string]any{"app_name": value.AppName, "version": value.Version, "branch": value.Branch, "database_type": value.DatabaseType, "authentication": value.Authentication, "mode": value.Mode, "os_name": value.OSName, "runtime_version": value.RuntimeVersion}, nil
}

func decodeStatistics(response *client.Response) (map[string]any, error) {
	var value struct {
		TotalBooks     int64 `json:"totalBooks"`
		MonitoredBooks int64 `json:"monitoredBooks"`
		FileCount      int64 `json:"fileCount"`
		TotalFileSize  int64 `json:"totalFileSize"`
		AuthorCount    int64 `json:"authorCount"`
	}
	if err := json.Unmarshal(response.Body, &value); err != nil {
		return nil, fmt.Errorf("chaptarr returned invalid system statistics")
	}
	return map[string]any{"total_books": value.TotalBooks, "monitored_books": value.MonitoredBooks, "file_count": value.FileCount, "total_file_size": value.TotalFileSize, "author_count": value.AuthorCount}, nil
}
