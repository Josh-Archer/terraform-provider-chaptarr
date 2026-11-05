package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var historyEventTypes = []string{
	"unknown",
	"grabbed",
	"bookFileImported",
	"downloadFailed",
	"bookFileDeleted",
	"bookFileRenamed",
	"bookImportIncomplete",
	"downloadImported",
	"bookFileRetagged",
	"downloadIgnored",
	"bookFileConverted",
	"bookFileConversionFailed",
}

func configuredDataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return nil
	}
	return apiClient
}

func requireDataSourceClient(apiClient *client.Client, resp *datasource.ReadResponse) bool {
	if apiClient == nil {
		resp.Diagnostics.AddError("Provider not configured", "Configure the Chaptarr provider before reading this data source.")
		return false
	}
	return true
}

func safeEntryName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, "://") || strings.HasPrefix(lower, "magnet:") {
		return ""
	}
	trimmed = strings.TrimRight(trimmed, `/\`)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndexAny(trimmed, `/\`); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return ""
	}
	if len(trimmed) == 2 && trimmed[1] == ':' {
		return ""
	}
	if containsCredentialQueryMarker(trimmed) {
		return "[REDACTED]"
	}
	return trimmed
}

func sanitizeReleaseTitle(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, "://") || strings.HasPrefix(lower, "magnet:") {
		return ""
	}
	if strings.ContainsAny(trimmed, `/\`) || (len(trimmed) >= 2 && trimmed[1] == ':') {
		return safeEntryName(trimmed)
	}
	if containsCredentialQueryMarker(trimmed) {
		return "[REDACTED]"
	}
	return trimmed
}

func containsCredentialQueryMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"apikey=", "api_key=", "passkey=", "token=", "secret=", "password=", "authorization=", "bearer "} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func optionalPathSHA256(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return publicFingerprint(trimmed)
}

func knownStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func operationalQueryPath(base string, values url.Values) string {
	encoded := values.Encode()
	if encoded == "" {
		return base
	}
	return base + "?" + encoded
}

// ---------------------------------------------------------------------------
// Tag: Backup (chaptarr_backups)
// ---------------------------------------------------------------------------

type backupsDataSource struct {
	client *client.Client
}

type backupsState struct {
	ID         types.String `tfsdk:"id"`
	TotalCount types.Int64  `tfsdk:"total_count"`
	Backups    types.List   `tfsdk:"backups"`
}

type backupItemModel struct {
	ID   types.Int64  `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Type types.String `tfsdk:"type"`
	Size types.Int64  `tfsdk:"size"`
	Time types.String `tfsdk:"time"`
}

type backupAPI struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Time string `json:"time"`
}

func newBackupsDataSource() datasource.DataSource { return &backupsDataSource{} }

func (d *backupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backups"
}

func (d *backupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read Chaptarr's backup archive inventory without storing server-side backup filesystem paths in Terraform state.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "Deterministic SHA-256 query identifier."},
			"total_count": schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of backup archives returned by Chaptarr."},
			"backups": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Redacted backup archive summaries ordered by numeric ID.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":   schema.Int64Attribute{Computed: true},
						"name": schema.StringAttribute{Computed: true, MarkdownDescription: "Sanitized archive filename without directory paths."},
						"type": schema.StringAttribute{Computed: true, MarkdownDescription: "Backup trigger type (`scheduled`, `manual`, or `update`)."},
						"size": schema.Int64Attribute{Computed: true, MarkdownDescription: "Archive size in bytes."},
						"time": schema.StringAttribute{Computed: true, MarkdownDescription: "Backup timestamp reported by Chaptarr."},
					},
				},
			},
		},
	}
}

func (d *backupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredDataSourceClient(req, resp)
}

func (d *backupsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if !requireDataSourceClient(d.client, resp) {
		return
	}
	const endpoint = "/api/v1/system/backup"
	response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read backups", err.Error())
		return
	}
	var values []backupAPI
	if err := json.Unmarshal(response.Body, &values); err != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid backup inventory JSON.")
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	models := make([]backupItemModel, 0, len(values))
	for _, value := range values {
		models = append(models, backupItemModel{
			ID:   types.Int64Value(value.ID),
			Name: types.StringValue(safeEntryName(value.Name)),
			Type: types.StringValue(value.Type),
			Size: types.Int64Value(value.Size),
			Time: types.StringValue(value.Time),
		})
	}
	state := backupsState{
		ID:         types.StringValue(publicFingerprint(endpoint)),
		TotalCount: types.Int64Value(int64(len(models))),
		Backups:    listObjectState(ctx, backupItemObjectType(), models, &resp.Diagnostics),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func backupItemObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":   types.Int64Type,
		"name": types.StringType,
		"type": types.StringType,
		"size": types.Int64Type,
		"time": types.StringType,
	}}
}

// ---------------------------------------------------------------------------
// Tag: SettingsBackup (chaptarr_settings_backups)
// ---------------------------------------------------------------------------

type settingsBackupsDataSource struct {
	client *client.Client
}

type settingsBackupsConfig struct {
	Operation  types.String `tfsdk:"operation"`
	RootFolder types.String `tfsdk:"root_folder"`
}

type settingsBackupsState struct {
	ID         types.String `tfsdk:"id"`
	Operation  types.String `tfsdk:"operation"`
	RootFolder types.String `tfsdk:"root_folder"`
	Files      types.List   `tfsdk:"files"`
	Locations  types.List   `tfsdk:"locations"`
}

type settingsBackupFileModel struct {
	Name             types.String `tfsdk:"name"`
	Size             types.Int64  `tfsdk:"size"`
	LastWriteTimeUTC types.String `tfsdk:"last_write_time_utc"`
	PathSHA256       types.String `tfsdk:"path_sha256"`
}

type settingsBackupLocationModel struct {
	PathSHA256 types.String `tfsdk:"path_sha256"`
	Exists     types.Bool   `tfsdk:"exists"`
	Writable   types.Bool   `tfsdk:"writable"`
	HasWarning types.Bool   `tfsdk:"has_warning"`
}

type settingsBackupFileAPI struct {
	Path             string `json:"path"`
	Name             string `json:"name"`
	Size             int64  `json:"size"`
	LastWriteTimeUTC string `json:"lastWriteTimeUtc"`
}

type settingsBackupLocationAPI struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Writable bool   `json:"writable"`
	Warning  string `json:"warning"`
}

func newSettingsBackupsDataSource() datasource.DataSource { return &settingsBackupsDataSource{} }

func (d *settingsBackupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_backups"
}

func (d *settingsBackupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read Chaptarr settings-backup files and destination locations without storing internal filesystem paths or raw warning messages in Terraform state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Deterministic SHA-256 query identifier."},
			"operation": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Observation scope: `all` (default), `files`, or `locations`.",
				Validators:          []validator.String{stringvalidator.OneOf("all", "files", "locations")},
			},
			"root_folder": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional root-folder filter forwarded as `rootFolder` when reading settings-backup files. Not valid when `operation = \"locations\"`.",
			},
			"files": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Discovered settings-backup file metadata with hashed path fingerprints.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":                schema.StringAttribute{Computed: true, MarkdownDescription: "Sanitized filename without directory components."},
						"size":                schema.Int64Attribute{Computed: true},
						"last_write_time_utc": schema.StringAttribute{Computed: true},
						"path_sha256":         schema.StringAttribute{Computed: true, MarkdownDescription: "SHA-256 digest of the server-side file path."},
					},
				},
			},
			"locations": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Discovered settings-backup location capabilities with hashed path fingerprints.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"path_sha256": schema.StringAttribute{Computed: true, MarkdownDescription: "SHA-256 digest of the server-side location path."},
						"exists":      schema.BoolAttribute{Computed: true},
						"writable":    schema.BoolAttribute{Computed: true},
						"has_warning": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Chaptarr reported a warning for this location."},
					},
				},
			},
		},
	}
}

func (d *settingsBackupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredDataSourceClient(req, resp)
}

func (d *settingsBackupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if !requireDataSourceClient(d.client, resp) {
		return
	}
	var cfg settingsBackupsConfig
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathFor("operation"), &cfg.Operation)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathFor("root_folder"), &cfg.RootFolder)...)
	if resp.Diagnostics.HasError() {
		return
	}

	operation := "all"
	if !cfg.Operation.IsNull() && !cfg.Operation.IsUnknown() && strings.TrimSpace(cfg.Operation.ValueString()) != "" {
		operation = strings.TrimSpace(cfg.Operation.ValueString())
	}
	if operation != "all" && operation != "files" && operation != "locations" {
		resp.Diagnostics.AddAttributeError(pathFor("operation"), "Invalid settings-backup operation", "`operation` must be `all`, `files`, or `locations`.")
		return
	}

	rootFolderState := types.StringNull()
	rootFolderFilter := ""
	if !cfg.RootFolder.IsNull() && !cfg.RootFolder.IsUnknown() {
		trimmed := strings.TrimSpace(cfg.RootFolder.ValueString())
		if trimmed == "" {
			resp.Diagnostics.AddAttributeError(pathFor("root_folder"), "Invalid root_folder filter", "`root_folder` must not be empty when configured.")
			return
		}
		if operation == "locations" {
			resp.Diagnostics.AddAttributeError(pathFor("root_folder"), "Unsupported filter for locations", "`root_folder` cannot be set when `operation = \"locations\"`.")
			return
		}
		rootFolderFilter = trimmed
		rootFolderState = types.StringValue(trimmed)
	}

	fileModels := []settingsBackupFileModel{}
	if operation == "all" || operation == "files" {
		values := url.Values{}
		if rootFolderFilter != "" {
			values.Set("rootFolder", rootFolderFilter)
		}
		filesEndpoint := operationalQueryPath("/api/v1/system/settingsbackup/files", values)
		response, err := d.client.Do(ctx, http.MethodGet, filesEndpoint, nil)
		if err != nil {
			resp.Diagnostics.AddError("Unable to read settings-backup files", err.Error())
			return
		}
		var rawFiles []settingsBackupFileAPI
		if err := json.Unmarshal(response.Body, &rawFiles); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid settings-backup file metadata.")
			return
		}
		fileModels = make([]settingsBackupFileModel, 0, len(rawFiles))
		for _, item := range rawFiles {
			fileModels = append(fileModels, settingsBackupFileModel{
				Name:             types.StringValue(safeEntryName(item.Name)),
				Size:             types.Int64Value(item.Size),
				LastWriteTimeUTC: types.StringValue(item.LastWriteTimeUTC),
				PathSHA256:       types.StringValue(optionalPathSHA256(item.Path)),
			})
		}
		sort.Slice(fileModels, func(i, j int) bool {
			if fileModels[i].Name.ValueString() != fileModels[j].Name.ValueString() {
				return fileModels[i].Name.ValueString() < fileModels[j].Name.ValueString()
			}
			if fileModels[i].LastWriteTimeUTC.ValueString() != fileModels[j].LastWriteTimeUTC.ValueString() {
				return fileModels[i].LastWriteTimeUTC.ValueString() < fileModels[j].LastWriteTimeUTC.ValueString()
			}
			return fileModels[i].PathSHA256.ValueString() < fileModels[j].PathSHA256.ValueString()
		})
	}

	locationModels := []settingsBackupLocationModel{}
	if operation == "all" || operation == "locations" {
		response, err := d.client.Do(ctx, http.MethodGet, "/api/v1/system/settingsbackup/locations", nil)
		if err != nil {
			resp.Diagnostics.AddError("Unable to read settings-backup locations", err.Error())
			return
		}
		var rawLocations []settingsBackupLocationAPI
		if err := json.Unmarshal(response.Body, &rawLocations); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid settings-backup location metadata.")
			return
		}
		locationModels = make([]settingsBackupLocationModel, 0, len(rawLocations))
		for _, item := range rawLocations {
			locationModels = append(locationModels, settingsBackupLocationModel{
				PathSHA256: types.StringValue(optionalPathSHA256(item.Path)),
				Exists:     types.BoolValue(item.Exists),
				Writable:   types.BoolValue(item.Writable),
				HasWarning: types.BoolValue(strings.TrimSpace(item.Warning) != ""),
			})
		}
		sort.Slice(locationModels, func(i, j int) bool {
			return locationModels[i].PathSHA256.ValueString() < locationModels[j].PathSHA256.ValueString()
		})
	}

	state := settingsBackupsState{
		ID:         types.StringValue(publicFingerprint("settingsbackup\x00" + operation + "\x00" + rootFolderFilter)),
		Operation:  types.StringValue(operation),
		RootFolder: rootFolderState,
		Files:      listObjectState(ctx, settingsBackupFileObjectType(), fileModels, &resp.Diagnostics),
		Locations:  listObjectState(ctx, settingsBackupLocationObjectType(), locationModels, &resp.Diagnostics),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func settingsBackupFileObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"name":                types.StringType,
		"size":                types.Int64Type,
		"last_write_time_utc": types.StringType,
		"path_sha256":         types.StringType,
	}}
}

func settingsBackupLocationObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"path_sha256": types.StringType,
		"exists":      types.BoolType,
		"writable":    types.BoolType,
		"has_warning": types.BoolType,
	}}
}

// ---------------------------------------------------------------------------
// Tag: History (chaptarr_history)
// ---------------------------------------------------------------------------

type historyDataSource struct {
	client *client.Client
}

type historyState struct {
	ID            types.String `tfsdk:"id"`
	Mode          types.String `tfsdk:"mode"`
	Page          types.Int64  `tfsdk:"page"`
	PageSize      types.Int64  `tfsdk:"page_size"`
	SortKey       types.String `tfsdk:"sort_key"`
	SortDirection types.String `tfsdk:"sort_direction"`
	EventTypes    types.Set    `tfsdk:"event_types"`
	EventType     types.String `tfsdk:"event_type"`
	AuthorID      types.Int64  `tfsdk:"author_id"`
	BookID        types.Int64  `tfsdk:"book_id"`
	Date          types.String `tfsdk:"date"`
	DownloadID    types.String `tfsdk:"download_id"`
	IncludeAuthor types.Bool   `tfsdk:"include_author"`
	IncludeBook   types.Bool   `tfsdk:"include_book"`
	MediaType     types.String `tfsdk:"media_type"`
	TotalRecords  types.Int64  `tfsdk:"total_records"`
	Records       types.List   `tfsdk:"records"`
}

type historyRecordModel struct {
	ID                  types.Int64  `tfsdk:"id"`
	BookID              types.Int64  `tfsdk:"book_id"`
	AuthorID            types.Int64  `tfsdk:"author_id"`
	EventType           types.String `tfsdk:"event_type"`
	Date                types.String `tfsdk:"date"`
	QualityName         types.String `tfsdk:"quality_name"`
	CustomFormatScore   types.Int64  `tfsdk:"custom_format_score"`
	QualityCutoffNotMet types.Bool   `tfsdk:"quality_cutoff_not_met"`
	DownloadID          types.String `tfsdk:"download_id"`
	AuthorName          types.String `tfsdk:"author_name"`
	BookTitle           types.String `tfsdk:"book_title"`
}

type historyPagingAPI struct {
	TotalRecords int64            `json:"totalRecords"`
	Records      []historyItemAPI `json:"records"`
}

type historyItemAPI struct {
	ID                  int64              `json:"id"`
	BookID              int64              `json:"bookId"`
	AuthorID            int64              `json:"authorId"`
	EventType           string             `json:"eventType"`
	Date                string             `json:"date"`
	CustomFormatScore   int64              `json:"customFormatScore"`
	QualityCutoffNotMet bool               `json:"qualityCutoffNotMet"`
	DownloadID          string             `json:"downloadId"`
	Quality             qualityEnvelopeAPI `json:"quality"`
	Author              *summaryAuthorAPI  `json:"author"`
	Book                *summaryBookAPI    `json:"book"`
}

type qualityEnvelopeAPI struct {
	Quality struct {
		Name string `json:"name"`
	} `json:"quality"`
}

type summaryAuthorAPI struct {
	ID              int64  `json:"id"`
	AuthorName      string `json:"authorName"`
	ForeignAuthorID string `json:"foreignAuthorId"`
}

type summaryBookAPI struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	ForeignBookID string `json:"foreignBookId"`
}

func newHistoryDataSource() datasource.DataSource { return &historyDataSource{} }

func (d *historyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_history"
}

func (d *historyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read Chaptarr activity history (`paged`, `author`, or `since`) with raw event payloads, filesystem paths, and URLs redacted from Terraform state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Deterministic SHA-256 query identifier."},
			"mode": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "History query mode: `paged` (default, `/api/v1/history`), `author` (`/api/v1/history/author`), or `since` (`/api/v1/history/since`).",
				Validators:          []validator.String{stringvalidator.OneOf("paged", "author", "since")},
			},
			"page":           schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"page_size":      schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.Between(1, 1000)}},
			"sort_key":       schema.StringAttribute{Optional: true},
			"sort_direction": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("default", "ascending", "descending")}},
			"event_types":    schema.SetAttribute{Optional: true, ElementType: types.Int64Type, MarkdownDescription: "Numeric event-type filters for `paged` mode."},
			"event_type":     schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf(historyEventTypes...)}, MarkdownDescription: "Named event-type filter for `author` or `since` mode."},
			"author_id":      schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"book_id":        schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"date":           schema.StringAttribute{Optional: true, MarkdownDescription: "ISO-8601 cutoff timestamp required when `mode = \"since\"`."},
			"download_id":    schema.StringAttribute{Optional: true},
			"include_author": schema.BoolAttribute{Optional: true},
			"include_book":   schema.BoolAttribute{Optional: true},
			"media_type":     schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("all", "audiobook", "ebook")}},
			"total_records":  schema.Int64Attribute{Computed: true},
			"records": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Redacted history records omitting `data`, `sourceTitle`, and nested library paths/URLs.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                     schema.Int64Attribute{Computed: true},
						"book_id":                schema.Int64Attribute{Computed: true},
						"author_id":              schema.Int64Attribute{Computed: true},
						"event_type":             schema.StringAttribute{Computed: true},
						"date":                   schema.StringAttribute{Computed: true},
						"quality_name":           schema.StringAttribute{Computed: true},
						"custom_format_score":    schema.Int64Attribute{Computed: true},
						"quality_cutoff_not_met": schema.BoolAttribute{Computed: true},
						"download_id":            schema.StringAttribute{Computed: true},
						"author_name":            schema.StringAttribute{Computed: true},
						"book_title":             schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *historyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredDataSourceClient(req, resp)
}

func (d *historyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if !requireDataSourceClient(d.client, resp) {
		return
	}
	var state historyState
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	mode := "paged"
	if !state.Mode.IsNull() && !state.Mode.IsUnknown() && strings.TrimSpace(state.Mode.ValueString()) != "" {
		mode = strings.TrimSpace(state.Mode.ValueString())
	}
	if mode != "paged" && mode != "author" && mode != "since" {
		resp.Diagnostics.AddAttributeError(pathFor("mode"), "Invalid history mode", "`mode` must be `paged`, `author`, or `since`.")
		return
	}
	state.Mode = types.StringValue(mode)

	endpoint, ok := buildHistoryEndpoint(ctx, mode, &state, &resp.Diagnostics)
	if !ok || resp.Diagnostics.HasError() {
		return
	}

	response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read history", err.Error())
		return
	}

	var rawRecords []historyItemAPI
	var totalRecords int64
	if mode == "paged" {
		var page historyPagingAPI
		if err := json.Unmarshal(response.Body, &page); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid paged history JSON.")
			return
		}
		rawRecords = page.Records
		totalRecords = page.TotalRecords
	} else {
		if err := json.Unmarshal(response.Body, &rawRecords); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid history JSON.")
			return
		}
		totalRecords = int64(len(rawRecords))
	}

	models := make([]historyRecordModel, 0, len(rawRecords))
	for _, item := range rawRecords {
		authorName := ""
		if item.Author != nil {
			authorName = item.Author.AuthorName
		}
		bookTitle := ""
		if item.Book != nil {
			bookTitle = item.Book.Title
		}
		models = append(models, historyRecordModel{
			ID:                  types.Int64Value(item.ID),
			BookID:              types.Int64Value(item.BookID),
			AuthorID:            types.Int64Value(item.AuthorID),
			EventType:           types.StringValue(item.EventType),
			Date:                types.StringValue(item.Date),
			QualityName:         types.StringValue(item.Quality.Quality.Name),
			CustomFormatScore:   types.Int64Value(item.CustomFormatScore),
			QualityCutoffNotMet: types.BoolValue(item.QualityCutoffNotMet),
			DownloadID:          types.StringValue(item.DownloadID),
			AuthorName:          types.StringValue(authorName),
			BookTitle:           types.StringValue(bookTitle),
		})
	}

	state.ID = types.StringValue(publicFingerprint(endpoint))
	state.TotalRecords = types.Int64Value(totalRecords)
	state.Records = listObjectState(ctx, historyRecordObjectType(), models, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func buildHistoryEndpoint(ctx context.Context, mode string, state *historyState, diagnostics *diag.Diagnostics) (string, bool) {
	values := url.Values{}
	if !state.IncludeAuthor.IsNull() && !state.IncludeAuthor.IsUnknown() {
		values.Set("includeAuthor", strconv.FormatBool(state.IncludeAuthor.ValueBool()))
	}
	if !state.IncludeBook.IsNull() && !state.IncludeBook.IsUnknown() {
		values.Set("includeBook", strconv.FormatBool(state.IncludeBook.ValueBool()))
	}
	if !state.MediaType.IsNull() && !state.MediaType.IsUnknown() {
		mediaType := strings.TrimSpace(state.MediaType.ValueString())
		if mediaType != "" {
			values.Set("mediaType", mediaType)
			state.MediaType = types.StringValue(mediaType)
		}
	}

	switch mode {
	case "paged":
		rejectIfSetInt64(state.AuthorID, "author_id", "paged", diagnostics)
		rejectIfSetString(state.Date, "date", "paged", diagnostics)
		rejectIfSetString(state.EventType, "event_type", "paged", diagnostics)
		if !state.Page.IsNull() && !state.Page.IsUnknown() {
			if state.Page.ValueInt64() < 1 {
				diagnostics.AddAttributeError(pathFor("page"), "Invalid page", "`page` must be at least 1.")
			} else {
				values.Set("page", strconv.FormatInt(state.Page.ValueInt64(), 10))
			}
		}
		if !state.PageSize.IsNull() && !state.PageSize.IsUnknown() {
			if state.PageSize.ValueInt64() < 1 || state.PageSize.ValueInt64() > 1000 {
				diagnostics.AddAttributeError(pathFor("page_size"), "Invalid page_size", "`page_size` must be between 1 and 1000.")
			} else {
				values.Set("pageSize", strconv.FormatInt(state.PageSize.ValueInt64(), 10))
			}
		}
		if !state.SortKey.IsNull() && !state.SortKey.IsUnknown() {
			if trimmed := strings.TrimSpace(state.SortKey.ValueString()); trimmed != "" {
				values.Set("sortKey", trimmed)
				state.SortKey = types.StringValue(trimmed)
			}
		}
		if !state.SortDirection.IsNull() && !state.SortDirection.IsUnknown() {
			if trimmed := strings.TrimSpace(state.SortDirection.ValueString()); trimmed != "" {
				values.Set("sortDirection", trimmed)
				state.SortDirection = types.StringValue(trimmed)
			}
		}
		if !state.EventTypes.IsNull() && !state.EventTypes.IsUnknown() {
			var raw []int64
			diagnostics.Append(state.EventTypes.ElementsAs(ctx, &raw, false)...)
			for _, v := range raw {
				if v < 0 {
					diagnostics.AddAttributeError(pathFor("event_types"), "Invalid event type", "`event_types` entries must be non-negative.")
					break
				}
			}
			sort.Slice(raw, func(i, j int) bool { return raw[i] < raw[j] })
			for _, v := range raw {
				values.Add("eventType", strconv.FormatInt(v, 10))
			}
		}
		if !state.BookID.IsNull() && !state.BookID.IsUnknown() {
			if state.BookID.ValueInt64() < 1 {
				diagnostics.AddAttributeError(pathFor("book_id"), "Invalid book_id", "`book_id` must be at least 1.")
			} else {
				values.Set("bookId", strconv.FormatInt(state.BookID.ValueInt64(), 10))
			}
		}
		if !state.DownloadID.IsNull() && !state.DownloadID.IsUnknown() {
			if trimmed := strings.TrimSpace(state.DownloadID.ValueString()); trimmed != "" {
				values.Set("downloadId", trimmed)
				state.DownloadID = types.StringValue(trimmed)
			}
		}
		return operationalQueryPath("/api/v1/history", values), !diagnostics.HasError()

	case "author":
		rejectIfSetInt64(state.Page, "page", "author", diagnostics)
		rejectIfSetInt64(state.PageSize, "page_size", "author", diagnostics)
		rejectIfSetString(state.SortKey, "sort_key", "author", diagnostics)
		rejectIfSetString(state.SortDirection, "sort_direction", "author", diagnostics)
		rejectIfSetSet(state.EventTypes, "event_types", "author", diagnostics)
		rejectIfSetString(state.Date, "date", "author", diagnostics)
		rejectIfSetString(state.DownloadID, "download_id", "author", diagnostics)
		if state.AuthorID.IsNull() || state.AuthorID.IsUnknown() || state.AuthorID.ValueInt64() < 1 {
			diagnostics.AddAttributeError(pathFor("author_id"), "Required author_id", "`author_id` must be set to at least 1 when `mode = \"author\"`.")
		} else {
			values.Set("authorId", strconv.FormatInt(state.AuthorID.ValueInt64(), 10))
		}
		if !state.BookID.IsNull() && !state.BookID.IsUnknown() {
			if state.BookID.ValueInt64() < 1 {
				diagnostics.AddAttributeError(pathFor("book_id"), "Invalid book_id", "`book_id` must be at least 1.")
			} else {
				values.Set("bookId", strconv.FormatInt(state.BookID.ValueInt64(), 10))
			}
		}
		if !state.EventType.IsNull() && !state.EventType.IsUnknown() {
			if trimmed := strings.TrimSpace(state.EventType.ValueString()); trimmed != "" {
				values.Set("eventType", trimmed)
				state.EventType = types.StringValue(trimmed)
			}
		}
		return operationalQueryPath("/api/v1/history/author", values), !diagnostics.HasError()

	default: // "since"
		rejectIfSetInt64(state.Page, "page", "since", diagnostics)
		rejectIfSetInt64(state.PageSize, "page_size", "since", diagnostics)
		rejectIfSetString(state.SortKey, "sort_key", "since", diagnostics)
		rejectIfSetString(state.SortDirection, "sort_direction", "since", diagnostics)
		rejectIfSetSet(state.EventTypes, "event_types", "since", diagnostics)
		rejectIfSetInt64(state.AuthorID, "author_id", "since", diagnostics)
		rejectIfSetInt64(state.BookID, "book_id", "since", diagnostics)
		rejectIfSetString(state.DownloadID, "download_id", "since", diagnostics)
		if state.Date.IsNull() || state.Date.IsUnknown() || strings.TrimSpace(state.Date.ValueString()) == "" {
			diagnostics.AddAttributeError(pathFor("date"), "Required date", "`date` must be non-empty when `mode = \"since\"`.")
		} else {
			trimmed := strings.TrimSpace(state.Date.ValueString())
			values.Set("date", trimmed)
			state.Date = types.StringValue(trimmed)
		}
		if !state.EventType.IsNull() && !state.EventType.IsUnknown() {
			if trimmed := strings.TrimSpace(state.EventType.ValueString()); trimmed != "" {
				values.Set("eventType", trimmed)
				state.EventType = types.StringValue(trimmed)
			}
		}
		return operationalQueryPath("/api/v1/history/since", values), !diagnostics.HasError()
	}
}

func rejectIfSetInt64(v types.Int64, attrName, contextName string, diagnostics *diag.Diagnostics) {
	if !v.IsNull() && !v.IsUnknown() {
		diagnostics.AddAttributeError(pathFor(attrName), "Unsupported query filter", fmt.Sprintf("`%s` is not supported in `%s` mode.", attrName, contextName))
	}
}

func rejectIfSetString(v types.String, attrName, contextName string, diagnostics *diag.Diagnostics) {
	if !v.IsNull() && !v.IsUnknown() {
		diagnostics.AddAttributeError(pathFor(attrName), "Unsupported query filter", fmt.Sprintf("`%s` is not supported in `%s` mode.", attrName, contextName))
	}
}

func rejectIfSetBool(v types.Bool, attrName, contextName string, diagnostics *diag.Diagnostics) {
	if !v.IsNull() && !v.IsUnknown() {
		diagnostics.AddAttributeError(pathFor(attrName), "Unsupported query filter", fmt.Sprintf("`%s` is not supported in `%s` mode.", attrName, contextName))
	}
}

func rejectIfSetSet(v types.Set, attrName, contextName string, diagnostics *diag.Diagnostics) {
	if !v.IsNull() && !v.IsUnknown() {
		diagnostics.AddAttributeError(pathFor(attrName), "Unsupported query filter", fmt.Sprintf("`%s` is not supported in `%s` mode.", attrName, contextName))
	}
}

func historyRecordObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":                     types.Int64Type,
		"book_id":                types.Int64Type,
		"author_id":              types.Int64Type,
		"event_type":             types.StringType,
		"date":                   types.StringType,
		"quality_name":           types.StringType,
		"custom_format_score":    types.Int64Type,
		"quality_cutoff_not_met": types.BoolType,
		"download_id":            types.StringType,
		"author_name":            types.StringType,
		"book_title":             types.StringType,
	}}
}

// ---------------------------------------------------------------------------
// Tag: Ignored (chaptarr_ignored)
// ---------------------------------------------------------------------------

type ignoredDataSource struct {
	client *client.Client
}

type ignoredState struct {
	ID            types.String `tfsdk:"id"`
	Page          types.Int64  `tfsdk:"page"`
	PageSize      types.Int64  `tfsdk:"page_size"`
	SortKey       types.String `tfsdk:"sort_key"`
	SortDirection types.String `tfsdk:"sort_direction"`
	TotalRecords  types.Int64  `tfsdk:"total_records"`
	Records       types.List   `tfsdk:"records"`
}

type ignoredRecordModel struct {
	ID                 types.Int64  `tfsdk:"id"`
	AuthorID           types.Int64  `tfsdk:"author_id"`
	BookID             types.Int64  `tfsdk:"book_id"`
	DownloadID         types.String `tfsdk:"download_id"`
	SourceTitle        types.String `tfsdk:"source_title"`
	Date               types.String `tfsdk:"date"`
	Protocol           types.String `tfsdk:"protocol"`
	DownloadClientID   types.Int64  `tfsdk:"download_client_id"`
	DownloadClient     types.String `tfsdk:"download_client"`
	DownloadClientName types.String `tfsdk:"download_client_name"`
	IsInClient         types.Bool   `tfsdk:"is_in_client"`
}

type ignoredPagingAPI struct {
	TotalRecords int64            `json:"totalRecords"`
	Records      []ignoredItemAPI `json:"records"`
}

type ignoredItemAPI struct {
	ID                 int64  `json:"id"`
	AuthorID           int64  `json:"authorId"`
	BookID             int64  `json:"bookId"`
	DownloadID         string `json:"downloadId"`
	SourceTitle        string `json:"sourceTitle"`
	Date               string `json:"date"`
	Protocol           string `json:"protocol"`
	DownloadClientID   int64  `json:"downloadClientId"`
	DownloadClient     string `json:"downloadClient"`
	DownloadClientName string `json:"downloadClientName"`
	IsInClient         bool   `json:"isInClient"`
}

func newIgnoredDataSource() datasource.DataSource { return &ignoredDataSource{} }

func (d *ignoredDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ignored"
}

func (d *ignoredDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read ignored downloads from Chaptarr without mutating ignored entries or exposing URLs/filesystem paths in Terraform state.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Deterministic SHA-256 query identifier."},
			"page":           schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"page_size":      schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.Between(1, 1000)}},
			"sort_key":       schema.StringAttribute{Optional: true},
			"sort_direction": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("default", "ascending", "descending")}},
			"total_records":  schema.Int64Attribute{Computed: true},
			"records": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                   schema.Int64Attribute{Computed: true},
						"author_id":            schema.Int64Attribute{Computed: true},
						"book_id":              schema.Int64Attribute{Computed: true},
						"download_id":          schema.StringAttribute{Computed: true},
						"source_title":         schema.StringAttribute{Computed: true, MarkdownDescription: "Sanitized release title with paths, URLs, and credential markers stripped."},
						"date":                 schema.StringAttribute{Computed: true},
						"protocol":             schema.StringAttribute{Computed: true},
						"download_client_id":   schema.Int64Attribute{Computed: true},
						"download_client":      schema.StringAttribute{Computed: true},
						"download_client_name": schema.StringAttribute{Computed: true},
						"is_in_client":         schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *ignoredDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredDataSourceClient(req, resp)
}

func (d *ignoredDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if !requireDataSourceClient(d.client, resp) {
		return
	}
	var state ignoredState
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	values := url.Values{}
	if !state.Page.IsNull() && !state.Page.IsUnknown() {
		if state.Page.ValueInt64() < 1 {
			resp.Diagnostics.AddAttributeError(pathFor("page"), "Invalid page", "`page` must be at least 1.")
			return
		}
		values.Set("page", strconv.FormatInt(state.Page.ValueInt64(), 10))
	}
	if !state.PageSize.IsNull() && !state.PageSize.IsUnknown() {
		if state.PageSize.ValueInt64() < 1 || state.PageSize.ValueInt64() > 1000 {
			resp.Diagnostics.AddAttributeError(pathFor("page_size"), "Invalid page_size", "`page_size` must be between 1 and 1000.")
			return
		}
		values.Set("pageSize", strconv.FormatInt(state.PageSize.ValueInt64(), 10))
	}
	if !state.SortKey.IsNull() && !state.SortKey.IsUnknown() {
		if trimmed := strings.TrimSpace(state.SortKey.ValueString()); trimmed != "" {
			values.Set("sortKey", trimmed)
			state.SortKey = types.StringValue(trimmed)
		}
	}
	if !state.SortDirection.IsNull() && !state.SortDirection.IsUnknown() {
		if trimmed := strings.TrimSpace(state.SortDirection.ValueString()); trimmed != "" {
			values.Set("sortDirection", trimmed)
			state.SortDirection = types.StringValue(trimmed)
		}
	}

	endpoint := operationalQueryPath("/api/v1/ignored", values)
	response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read ignored downloads", err.Error())
		return
	}
	var page ignoredPagingAPI
	if err := json.Unmarshal(response.Body, &page); err != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid ignored-downloads JSON.")
		return
	}

	models := make([]ignoredRecordModel, 0, len(page.Records))
	for _, item := range page.Records {
		models = append(models, ignoredRecordModel{
			ID:                 types.Int64Value(item.ID),
			AuthorID:           types.Int64Value(item.AuthorID),
			BookID:             types.Int64Value(item.BookID),
			DownloadID:         types.StringValue(item.DownloadID),
			SourceTitle:        types.StringValue(sanitizeReleaseTitle(item.SourceTitle)),
			Date:               types.StringValue(item.Date),
			Protocol:           types.StringValue(item.Protocol),
			DownloadClientID:   types.Int64Value(item.DownloadClientID),
			DownloadClient:     types.StringValue(item.DownloadClient),
			DownloadClientName: types.StringValue(item.DownloadClientName),
			IsInClient:         types.BoolValue(item.IsInClient),
		})
	}

	state.ID = types.StringValue(publicFingerprint(endpoint))
	state.TotalRecords = types.Int64Value(page.TotalRecords)
	state.Records = listObjectState(ctx, ignoredRecordObjectType(), models, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func ignoredRecordObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":                   types.Int64Type,
		"author_id":            types.Int64Type,
		"book_id":              types.Int64Type,
		"download_id":          types.StringType,
		"source_title":         types.StringType,
		"date":                 types.StringType,
		"protocol":             types.StringType,
		"download_client_id":   types.Int64Type,
		"download_client":      types.StringType,
		"download_client_name": types.StringType,
		"is_in_client":         types.BoolType,
	}}
}

// ---------------------------------------------------------------------------
// Tag: ManualImport (chaptarr_manual_import_candidates)
// ---------------------------------------------------------------------------

type manualImportCandidatesDataSource struct {
	client *client.Client
}

type manualImportCandidatesState struct {
	ID                   types.String `tfsdk:"id"`
	Folder               types.String `tfsdk:"folder"`
	DownloadID           types.String `tfsdk:"download_id"`
	AuthorID             types.Int64  `tfsdk:"author_id"`
	FilterExistingFiles  types.Bool   `tfsdk:"filter_existing_files"`
	ReplaceExistingFiles types.Bool   `tfsdk:"replace_existing_files"`
	MediaType            types.String `tfsdk:"media_type"`
	BookFileIDs          types.Set    `tfsdk:"book_file_ids"`
	TotalCount           types.Int64  `tfsdk:"total_count"`
	Candidates           types.List   `tfsdk:"candidates"`
}

type manualImportCandidateModel struct {
	ID                        types.Int64  `tfsdk:"id"`
	Name                      types.String `tfsdk:"name"`
	PathSHA256                types.String `tfsdk:"path_sha256"`
	Size                      types.Int64  `tfsdk:"size"`
	AuthorID                  types.Int64  `tfsdk:"author_id"`
	AuthorName                types.String `tfsdk:"author_name"`
	ForeignAuthorID           types.String `tfsdk:"foreign_author_id"`
	BookID                    types.Int64  `tfsdk:"book_id"`
	BookTitle                 types.String `tfsdk:"book_title"`
	ForeignBookID             types.String `tfsdk:"foreign_book_id"`
	EditionID                 types.Int64  `tfsdk:"edition_id"`
	ForeignEditionID          types.String `tfsdk:"foreign_edition_id"`
	SuggestedForeignAuthorID  types.String `tfsdk:"suggested_foreign_author_id"`
	SuggestedAuthorName       types.String `tfsdk:"suggested_author_name"`
	SuggestedForeignBookID    types.String `tfsdk:"suggested_foreign_book_id"`
	SuggestedBookTitle        types.String `tfsdk:"suggested_book_title"`
	SuggestedForeignEditionID types.String `tfsdk:"suggested_foreign_edition_id"`
	SuggestedEditionTitle     types.String `tfsdk:"suggested_edition_title"`
	QualityName               types.String `tfsdk:"quality_name"`
	QualityWeight             types.Int64  `tfsdk:"quality_weight"`
	ReleaseGroup              types.String `tfsdk:"release_group"`
	DownloadID                types.String `tfsdk:"download_id"`
	IndexerFlags              types.Int64  `tfsdk:"indexer_flags"`
	HasRejections             types.Bool   `tfsdk:"has_rejections"`
	RejectionCount            types.Int64  `tfsdk:"rejection_count"`
	WarningCount              types.Int64  `tfsdk:"warning_count"`
	AdditionalFile            types.Bool   `tfsdk:"additional_file"`
	ReplaceExistingFiles      types.Bool   `tfsdk:"replace_existing_files"`
	DisableReleaseSwitching   types.Bool   `tfsdk:"disable_release_switching"`
}

type manualImportCandidateAPI struct {
	ID                        int64              `json:"id"`
	Path                      string             `json:"path"`
	Name                      string             `json:"name"`
	Size                      int64              `json:"size"`
	Author                    *summaryAuthorAPI  `json:"author"`
	Book                      *summaryBookAPI    `json:"book"`
	EditionID                 *int64             `json:"editionId"`
	ForeignEditionID          string             `json:"foreignEditionId"`
	SuggestedForeignAuthorID  string             `json:"suggestedForeignAuthorId"`
	SuggestedAuthorName       string             `json:"suggestedAuthorName"`
	SuggestedForeignBookID    string             `json:"suggestedForeignBookId"`
	SuggestedBookTitle        string             `json:"suggestedBookTitle"`
	SuggestedForeignEditionID string             `json:"suggestedForeignEditionId"`
	SuggestedEditionTitle     string             `json:"suggestedEditionTitle"`
	Quality                   qualityEnvelopeAPI `json:"quality"`
	ReleaseGroup              string             `json:"releaseGroup"`
	QualityWeight             int64              `json:"qualityWeight"`
	DownloadID                string             `json:"downloadId"`
	IndexerFlags              int64              `json:"indexerFlags"`
	Rejections                []json.RawMessage  `json:"rejections"`
	Warnings                  []string           `json:"warnings"`
	AdditionalFile            bool               `json:"additionalFile"`
	ReplaceExistingFiles      bool               `json:"replaceExistingFiles"`
	DisableReleaseSwitching   bool               `json:"disableReleaseSwitching"`
}

func newManualImportCandidatesDataSource() datasource.DataSource {
	return &manualImportCandidatesDataSource{}
}

func (d *manualImportCandidatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_manual_import_candidates"
}

func (d *manualImportCandidatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Inspect manual-import candidates without importing files or storing candidate filesystem paths, rejection messages, or embedded file tags in Terraform state.",
		Attributes: map[string]schema.Attribute{
			"id":                     schema.StringAttribute{Computed: true, MarkdownDescription: "Deterministic SHA-256 query identifier."},
			"folder":                 schema.StringAttribute{Optional: true, MarkdownDescription: "Optional folder query parameter passed to `/api/v1/manualimport`."},
			"download_id":            schema.StringAttribute{Optional: true},
			"author_id":              schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"filter_existing_files":  schema.BoolAttribute{Optional: true},
			"replace_existing_files": schema.BoolAttribute{Optional: true},
			"media_type":             schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("all", "audiobook", "ebook")}},
			"book_file_ids":          schema.SetAttribute{Optional: true, ElementType: types.Int64Type},
			"total_count":            schema.Int64Attribute{Computed: true},
			"candidates": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Redacted manual-import candidate summaries.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                           schema.Int64Attribute{Computed: true},
						"name":                         schema.StringAttribute{Computed: true, MarkdownDescription: "Sanitized candidate filename without directory components."},
						"path_sha256":                  schema.StringAttribute{Computed: true, MarkdownDescription: "SHA-256 digest of the server-side candidate path."},
						"size":                         schema.Int64Attribute{Computed: true},
						"author_id":                    schema.Int64Attribute{Computed: true},
						"author_name":                  schema.StringAttribute{Computed: true},
						"foreign_author_id":            schema.StringAttribute{Computed: true},
						"book_id":                      schema.Int64Attribute{Computed: true},
						"book_title":                   schema.StringAttribute{Computed: true},
						"foreign_book_id":              schema.StringAttribute{Computed: true},
						"edition_id":                   schema.Int64Attribute{Computed: true},
						"foreign_edition_id":           schema.StringAttribute{Computed: true},
						"suggested_foreign_author_id":  schema.StringAttribute{Computed: true},
						"suggested_author_name":        schema.StringAttribute{Computed: true},
						"suggested_foreign_book_id":    schema.StringAttribute{Computed: true},
						"suggested_book_title":         schema.StringAttribute{Computed: true},
						"suggested_foreign_edition_id": schema.StringAttribute{Computed: true},
						"suggested_edition_title":      schema.StringAttribute{Computed: true},
						"quality_name":                 schema.StringAttribute{Computed: true},
						"quality_weight":               schema.Int64Attribute{Computed: true},
						"release_group":                schema.StringAttribute{Computed: true},
						"download_id":                  schema.StringAttribute{Computed: true},
						"indexer_flags":                schema.Int64Attribute{Computed: true},
						"has_rejections":               schema.BoolAttribute{Computed: true},
						"rejection_count":              schema.Int64Attribute{Computed: true},
						"warning_count":                schema.Int64Attribute{Computed: true},
						"additional_file":              schema.BoolAttribute{Computed: true},
						"replace_existing_files":       schema.BoolAttribute{Computed: true},
						"disable_release_switching":    schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *manualImportCandidatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredDataSourceClient(req, resp)
}

func (d *manualImportCandidatesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if !requireDataSourceClient(d.client, resp) {
		return
	}
	var state manualImportCandidatesState
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	values := url.Values{}
	if !state.Folder.IsNull() && !state.Folder.IsUnknown() {
		trimmed := strings.TrimSpace(state.Folder.ValueString())
		if trimmed == "" {
			resp.Diagnostics.AddAttributeError(pathFor("folder"), "Invalid folder", "`folder` must not be empty when configured.")
			return
		}
		values.Set("folder", trimmed)
		state.Folder = types.StringValue(trimmed)
	}
	if !state.DownloadID.IsNull() && !state.DownloadID.IsUnknown() {
		if trimmed := strings.TrimSpace(state.DownloadID.ValueString()); trimmed != "" {
			values.Set("downloadId", trimmed)
			state.DownloadID = types.StringValue(trimmed)
		}
	}
	if !state.AuthorID.IsNull() && !state.AuthorID.IsUnknown() {
		if state.AuthorID.ValueInt64() < 1 {
			resp.Diagnostics.AddAttributeError(pathFor("author_id"), "Invalid author_id", "`author_id` must be at least 1.")
			return
		}
		values.Set("authorId", strconv.FormatInt(state.AuthorID.ValueInt64(), 10))
	}
	if !state.FilterExistingFiles.IsNull() && !state.FilterExistingFiles.IsUnknown() {
		values.Set("filterExistingFiles", strconv.FormatBool(state.FilterExistingFiles.ValueBool()))
	}
	if !state.ReplaceExistingFiles.IsNull() && !state.ReplaceExistingFiles.IsUnknown() {
		values.Set("replaceExistingFiles", strconv.FormatBool(state.ReplaceExistingFiles.ValueBool()))
	}
	if !state.MediaType.IsNull() && !state.MediaType.IsUnknown() {
		if trimmed := strings.TrimSpace(state.MediaType.ValueString()); trimmed != "" {
			values.Set("mediaType", trimmed)
			state.MediaType = types.StringValue(trimmed)
		}
	}
	if !state.BookFileIDs.IsNull() && !state.BookFileIDs.IsUnknown() {
		var ids []int64
		resp.Diagnostics.Append(state.BookFileIDs.ElementsAs(ctx, &ids, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		for _, id := range ids {
			if id < 1 {
				resp.Diagnostics.AddAttributeError(pathFor("book_file_ids"), "Invalid book_file_ids entry", "`book_file_ids` entries must be at least 1.")
				return
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			values.Add("bookFileIds", strconv.FormatInt(id, 10))
		}
	}

	endpoint := operationalQueryPath("/api/v1/manualimport", values)
	response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read manual-import candidates", err.Error())
		return
	}
	var rawCandidates []manualImportCandidateAPI
	if err := json.Unmarshal(response.Body, &rawCandidates); err != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid manual-import candidate JSON.")
		return
	}

	models := make([]manualImportCandidateModel, 0, len(rawCandidates))
	for _, item := range rawCandidates {
		var authorID int64
		var authorName, foreignAuthorID string
		if item.Author != nil {
			authorID = item.Author.ID
			authorName = item.Author.AuthorName
			foreignAuthorID = item.Author.ForeignAuthorID
		}
		var bookID int64
		var bookTitle, foreignBookID string
		if item.Book != nil {
			bookID = item.Book.ID
			bookTitle = item.Book.Title
			foreignBookID = item.Book.ForeignBookID
		}
		models = append(models, manualImportCandidateModel{
			ID:                        types.Int64Value(item.ID),
			Name:                      types.StringValue(safeEntryName(item.Name)),
			PathSHA256:                types.StringValue(optionalPathSHA256(item.Path)),
			Size:                      types.Int64Value(item.Size),
			AuthorID:                  types.Int64Value(authorID),
			AuthorName:                types.StringValue(authorName),
			ForeignAuthorID:           types.StringValue(foreignAuthorID),
			BookID:                    types.Int64Value(bookID),
			BookTitle:                 types.StringValue(bookTitle),
			ForeignBookID:             types.StringValue(foreignBookID),
			EditionID:                 nullableInt(item.EditionID),
			ForeignEditionID:          types.StringValue(item.ForeignEditionID),
			SuggestedForeignAuthorID:  types.StringValue(item.SuggestedForeignAuthorID),
			SuggestedAuthorName:       types.StringValue(item.SuggestedAuthorName),
			SuggestedForeignBookID:    types.StringValue(item.SuggestedForeignBookID),
			SuggestedBookTitle:        types.StringValue(item.SuggestedBookTitle),
			SuggestedForeignEditionID: types.StringValue(item.SuggestedForeignEditionID),
			SuggestedEditionTitle:     types.StringValue(item.SuggestedEditionTitle),
			QualityName:               types.StringValue(item.Quality.Quality.Name),
			QualityWeight:             types.Int64Value(item.QualityWeight),
			ReleaseGroup:              types.StringValue(item.ReleaseGroup),
			DownloadID:                types.StringValue(item.DownloadID),
			IndexerFlags:              types.Int64Value(item.IndexerFlags),
			HasRejections:             types.BoolValue(len(item.Rejections) > 0),
			RejectionCount:            types.Int64Value(int64(len(item.Rejections))),
			WarningCount:              types.Int64Value(int64(len(item.Warnings))),
			AdditionalFile:            types.BoolValue(item.AdditionalFile),
			ReplaceExistingFiles:      types.BoolValue(item.ReplaceExistingFiles),
			DisableReleaseSwitching:   types.BoolValue(item.DisableReleaseSwitching),
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].ID.ValueInt64() != models[j].ID.ValueInt64() {
			return models[i].ID.ValueInt64() < models[j].ID.ValueInt64()
		}
		if models[i].PathSHA256.ValueString() != models[j].PathSHA256.ValueString() {
			return models[i].PathSHA256.ValueString() < models[j].PathSHA256.ValueString()
		}
		return models[i].Name.ValueString() < models[j].Name.ValueString()
	})

	state.ID = types.StringValue(publicFingerprint(endpoint))
	state.TotalCount = types.Int64Value(int64(len(models)))
	state.Candidates = listObjectState(ctx, manualImportCandidateObjectType(), models, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func manualImportCandidateObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":                           types.Int64Type,
		"name":                         types.StringType,
		"path_sha256":                  types.StringType,
		"size":                         types.Int64Type,
		"author_id":                    types.Int64Type,
		"author_name":                  types.StringType,
		"foreign_author_id":            types.StringType,
		"book_id":                      types.Int64Type,
		"book_title":                   types.StringType,
		"foreign_book_id":              types.StringType,
		"edition_id":                   types.Int64Type,
		"foreign_edition_id":           types.StringType,
		"suggested_foreign_author_id":  types.StringType,
		"suggested_author_name":        types.StringType,
		"suggested_foreign_book_id":    types.StringType,
		"suggested_book_title":         types.StringType,
		"suggested_foreign_edition_id": types.StringType,
		"suggested_edition_title":      types.StringType,
		"quality_name":                 types.StringType,
		"quality_weight":               types.Int64Type,
		"release_group":                types.StringType,
		"download_id":                  types.StringType,
		"indexer_flags":                types.Int64Type,
		"has_rejections":               types.BoolType,
		"rejection_count":              types.Int64Type,
		"warning_count":                types.Int64Type,
		"additional_file":              types.BoolType,
		"replace_existing_files":       types.BoolType,
		"disable_release_switching":    types.BoolType,
	}}
}

// ---------------------------------------------------------------------------
// Tag: PendingAuthorImport (chaptarr_pending_author_import)
// ---------------------------------------------------------------------------

type pendingAuthorImportDataSource struct {
	client *client.Client
}

type pendingAuthorImportState struct {
	ID             types.String `tfsdk:"id"`
	Operation      types.String `tfsdk:"operation"`
	PendingID      types.Int64  `tfsdk:"pending_id"`
	ProviderID     types.String `tfsdk:"provider_id"`
	Items          types.List   `tfsdk:"items"`
	Existence      types.Object `tfsdk:"existence"`
	ProfileOptions types.Object `tfsdk:"profile_options"`
}

type pendingAuthorImportItemModel struct {
	ID                           types.Int64  `tfsdk:"id"`
	ProviderID                   types.String `tfsdk:"provider_id"`
	ProviderPrefix               types.String `tfsdk:"provider_prefix"`
	AuthorName                   types.String `tfsdk:"author_name"`
	AudiobookStatus              types.String `tfsdk:"audiobook_status"`
	EbookStatus                  types.String `tfsdk:"ebook_status"`
	OverallStatus                types.String `tfsdk:"overall_status"`
	AudiobookMonitored           types.Bool   `tfsdk:"audiobook_monitored"`
	AudiobookMonitorNewItems     types.String `tfsdk:"audiobook_monitor_new_items"`
	AudiobookMonitorExistingMode types.String `tfsdk:"audiobook_monitor_existing_mode"`
	AudiobookQualityProfileID    types.Int64  `tfsdk:"audiobook_quality_profile_id"`
	AudiobookMetadataProfileID   types.Int64  `tfsdk:"audiobook_metadata_profile_id"`
	AudiobookBooksToMonitor      types.List   `tfsdk:"audiobook_books_to_monitor"`
	AudiobookBooksToSearch       types.List   `tfsdk:"audiobook_books_to_search"`
	AudiobookTags                types.Set    `tfsdk:"audiobook_tags"`
	EbookMonitored               types.Bool   `tfsdk:"ebook_monitored"`
	EbookMonitorNewItems         types.String `tfsdk:"ebook_monitor_new_items"`
	EbookMonitorExistingMode     types.String `tfsdk:"ebook_monitor_existing_mode"`
	EbookQualityProfileID        types.Int64  `tfsdk:"ebook_quality_profile_id"`
	EbookMetadataProfileID       types.Int64  `tfsdk:"ebook_metadata_profile_id"`
	EbookBooksToMonitor          types.List   `tfsdk:"ebook_books_to_monitor"`
	EbookBooksToSearch           types.List   `tfsdk:"ebook_books_to_search"`
	EbookTags                    types.Set    `tfsdk:"ebook_tags"`
	Tags                         types.Set    `tfsdk:"tags"`
	SearchForMissingBooks        types.Bool   `tfsdk:"search_for_missing_books"`
	CreatedAt                    types.String `tfsdk:"created_at"`
	UpdatedAt                    types.String `tfsdk:"updated_at"`
	LastAttemptAt                types.String `tfsdk:"last_attempt_at"`
	NextAttemptAt                types.String `tfsdk:"next_attempt_at"`
	AttemptCount                 types.Int64  `tfsdk:"attempt_count"`
	MaxAttempts                  types.Int64  `tfsdk:"max_attempts"`
	HasError                     types.Bool   `tfsdk:"has_error"`
	SourceApplication            types.String `tfsdk:"source_application"`
}

type pendingAuthorExistenceModel struct {
	Exists       types.Bool   `tfsdk:"exists"`
	AuthorID     types.Int64  `tfsdk:"author_id"`
	AuthorName   types.String `tfsdk:"author_name"`
	Pending      types.Bool   `tfsdk:"pending"`
	PendingID    types.Int64  `tfsdk:"pending_id"`
	Status       types.String `tfsdk:"status"`
	NextAttempt  types.String `tfsdk:"next_attempt"`
	AttemptCount types.Int64  `tfsdk:"attempt_count"`
}

type pendingProfileOptionsModel struct {
	Audiobook types.Object `tfsdk:"audiobook"`
	Ebook     types.Object `tfsdk:"ebook"`
	All       types.Object `tfsdk:"all"`
}

type pendingMediaProfileOptionsModel struct {
	QualityProfiles  types.List  `tfsdk:"quality_profiles"`
	MetadataProfiles types.List  `tfsdk:"metadata_profiles"`
	RootFolderCount  types.Int64 `tfsdk:"root_folder_count"`
	RootFolderNames  types.List  `tfsdk:"root_folder_names"`
}

type pendingProfileOptionItemModel struct {
	ID   types.Int64  `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

type pendingAuthorImportItemAPI struct {
	ID                           int64    `json:"id"`
	ProviderID                   string   `json:"providerId"`
	ProviderPrefix               string   `json:"providerPrefix"`
	AuthorName                   string   `json:"authorName"`
	AudiobookStatus              string   `json:"audiobookStatus"`
	EbookStatus                  string   `json:"ebookStatus"`
	OverallStatus                string   `json:"overallStatus"`
	AudiobookMonitored           *bool    `json:"audiobookMonitored"`
	AudiobookMonitorNewItems     string   `json:"audiobookMonitorNewItems"`
	AudiobookMonitorExistingMode string   `json:"audiobookMonitorExistingMode"`
	AudiobookQualityProfileID    *int64   `json:"audiobookQualityProfileId"`
	AudiobookMetadataProfileID   *int64   `json:"audiobookMetadataProfileId"`
	AudiobookBooksToMonitor      []string `json:"audiobookBooksToMonitor"`
	AudiobookBooksToSearch       []string `json:"audiobookBooksToSearch"`
	AudiobookTags                []int64  `json:"audiobookTags"`
	EbookMonitored               *bool    `json:"ebookMonitored"`
	EbookMonitorNewItems         string   `json:"ebookMonitorNewItems"`
	EbookMonitorExistingMode     string   `json:"ebookMonitorExistingMode"`
	EbookQualityProfileID        *int64   `json:"ebookQualityProfileId"`
	EbookMetadataProfileID       *int64   `json:"ebookMetadataProfileId"`
	EbookBooksToMonitor          []string `json:"ebookBooksToMonitor"`
	EbookBooksToSearch           []string `json:"ebookBooksToSearch"`
	EbookTags                    []int64  `json:"ebookTags"`
	Tags                         []int64  `json:"tags"`
	SearchForMissingBooks        bool     `json:"searchForMissingBooks"`
	CreatedAt                    string   `json:"createdAt"`
	UpdatedAt                    *string  `json:"updatedAt"`
	LastAttemptAt                *string  `json:"lastAttemptAt"`
	NextAttemptAt                string   `json:"nextAttemptAt"`
	AttemptCount                 int64    `json:"attemptCount"`
	MaxAttempts                  int64    `json:"maxAttempts"`
	LastError                    string   `json:"lastError"`
	SourceApplication            string   `json:"sourceApplication"`
}

type pendingAuthorExistenceAPI struct {
	Exists       bool    `json:"exists"`
	AuthorID     *int64  `json:"authorId"`
	AuthorName   *string `json:"authorName"`
	Pending      bool    `json:"pending"`
	PendingID    *int64  `json:"pendingId"`
	Status       *string `json:"status"`
	NextAttempt  *string `json:"nextAttempt"`
	AttemptCount *int64  `json:"attemptCount"`
}

type pendingProfileOptionsAPI struct {
	Audiobook pendingMediaProfileOptionsAPI `json:"audiobook"`
	Ebook     pendingMediaProfileOptionsAPI `json:"ebook"`
	All       pendingMediaProfileOptionsAPI `json:"all"`
}

type pendingMediaProfileOptionsAPI struct {
	QualityProfiles  []pendingProfileOptionAPI    `json:"qualityProfiles"`
	MetadataProfiles []pendingProfileOptionAPI    `json:"metadataProfiles"`
	RootFolders      []pendingRootFolderOptionAPI `json:"rootFolders"`
}

type pendingProfileOptionAPI struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type pendingRootFolderOptionAPI struct {
	Name string `json:"name"`
}

func newPendingAuthorImportDataSource() datasource.DataSource {
	return &pendingAuthorImportDataSource{}
}

func (d *pendingAuthorImportDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pending_author_import"
}

func (d *pendingAuthorImportDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	profileListSchema := schema.ListNestedAttribute{
		Computed: true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"id":   schema.Int64Attribute{Computed: true},
				"name": schema.StringAttribute{Computed: true},
			},
		},
	}
	mediaOptionsSchema := schema.SingleNestedAttribute{
		Computed: true,
		Attributes: map[string]schema.Attribute{
			"quality_profiles":  profileListSchema,
			"metadata_profiles": profileListSchema,
			"root_folder_count": schema.Int64Attribute{Computed: true},
			"root_folder_names": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Sanitized root-folder names without directory paths."},
		},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Observe pending author imports, provider-ID existence status, or pending-import profile options without storing server root-folder paths, raw error logs, or caller identity fields in Terraform state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Deterministic SHA-256 query identifier."},
			"operation": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Query operation: `list` (default), `get`, `exists`, or `profile_options`.",
				Validators:          []validator.String{stringvalidator.OneOf("list", "get", "exists", "profile_options")},
			},
			"pending_id":  schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}, MarkdownDescription: "Pending author import ID required when `operation = \"get\"`."},
			"provider_id": schema.StringAttribute{Optional: true, MarkdownDescription: "Upstream provider author ID required when `operation = \"exists\"`."},
			"items": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Redacted pending author import items populated for `list` and `get` operations.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                              schema.Int64Attribute{Computed: true},
						"provider_id":                     schema.StringAttribute{Computed: true},
						"provider_prefix":                 schema.StringAttribute{Computed: true},
						"author_name":                     schema.StringAttribute{Computed: true},
						"audiobook_status":                schema.StringAttribute{Computed: true},
						"ebook_status":                    schema.StringAttribute{Computed: true},
						"overall_status":                  schema.StringAttribute{Computed: true},
						"audiobook_monitored":             schema.BoolAttribute{Computed: true},
						"audiobook_monitor_new_items":     schema.StringAttribute{Computed: true},
						"audiobook_monitor_existing_mode": schema.StringAttribute{Computed: true},
						"audiobook_quality_profile_id":    schema.Int64Attribute{Computed: true},
						"audiobook_metadata_profile_id":   schema.Int64Attribute{Computed: true},
						"audiobook_books_to_monitor":      schema.ListAttribute{Computed: true, ElementType: types.StringType},
						"audiobook_books_to_search":       schema.ListAttribute{Computed: true, ElementType: types.StringType},
						"audiobook_tags":                  schema.SetAttribute{Computed: true, ElementType: types.Int64Type},
						"ebook_monitored":                 schema.BoolAttribute{Computed: true},
						"ebook_monitor_new_items":         schema.StringAttribute{Computed: true},
						"ebook_monitor_existing_mode":     schema.StringAttribute{Computed: true},
						"ebook_quality_profile_id":        schema.Int64Attribute{Computed: true},
						"ebook_metadata_profile_id":       schema.Int64Attribute{Computed: true},
						"ebook_books_to_monitor":          schema.ListAttribute{Computed: true, ElementType: types.StringType},
						"ebook_books_to_search":           schema.ListAttribute{Computed: true, ElementType: types.StringType},
						"ebook_tags":                      schema.SetAttribute{Computed: true, ElementType: types.Int64Type},
						"tags":                            schema.SetAttribute{Computed: true, ElementType: types.Int64Type},
						"search_for_missing_books":        schema.BoolAttribute{Computed: true},
						"created_at":                      schema.StringAttribute{Computed: true},
						"updated_at":                      schema.StringAttribute{Computed: true},
						"last_attempt_at":                 schema.StringAttribute{Computed: true},
						"next_attempt_at":                 schema.StringAttribute{Computed: true},
						"attempt_count":                   schema.Int64Attribute{Computed: true},
						"max_attempts":                    schema.Int64Attribute{Computed: true},
						"has_error":                       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Chaptarr recorded a non-empty `lastError` on the pending import."},
						"source_application":              schema.StringAttribute{Computed: true},
					},
				},
			},
			"existence": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Existence and pending status populated when `operation = \"exists\"`, or null otherwise.",
				Attributes: map[string]schema.Attribute{
					"exists":        schema.BoolAttribute{Computed: true},
					"author_id":     schema.Int64Attribute{Computed: true},
					"author_name":   schema.StringAttribute{Computed: true},
					"pending":       schema.BoolAttribute{Computed: true},
					"pending_id":    schema.Int64Attribute{Computed: true},
					"status":        schema.StringAttribute{Computed: true},
					"next_attempt":  schema.StringAttribute{Computed: true},
					"attempt_count": schema.Int64Attribute{Computed: true},
				},
			},
			"profile_options": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Profile and redacted root-folder options populated when `operation = \"profile_options\"`, or null otherwise.",
				Attributes: map[string]schema.Attribute{
					"audiobook": mediaOptionsSchema,
					"ebook":     mediaOptionsSchema,
					"all":       mediaOptionsSchema,
				},
			},
		},
	}
}

func (d *pendingAuthorImportDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredDataSourceClient(req, resp)
}

func (d *pendingAuthorImportDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if !requireDataSourceClient(d.client, resp) {
		return
	}
	var state pendingAuthorImportState
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathFor("operation"), &state.Operation)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathFor("pending_id"), &state.PendingID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathFor("provider_id"), &state.ProviderID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	operation := "list"
	if !state.Operation.IsNull() && !state.Operation.IsUnknown() && strings.TrimSpace(state.Operation.ValueString()) != "" {
		operation = strings.TrimSpace(state.Operation.ValueString())
	}
	if operation != "list" && operation != "get" && operation != "exists" && operation != "profile_options" {
		resp.Diagnostics.AddAttributeError(pathFor("operation"), "Invalid pending_author_import operation", "`operation` must be `list`, `get`, `exists`, or `profile_options`.")
		return
	}
	state.Operation = types.StringValue(operation)

	var endpoint string
	switch operation {
	case "list":
		rejectIfSetInt64(state.PendingID, "pending_id", "list", &resp.Diagnostics)
		rejectIfSetString(state.ProviderID, "provider_id", "list", &resp.Diagnostics)
		endpoint = "/api/v1/pendingauthorimport"
	case "get":
		rejectIfSetString(state.ProviderID, "provider_id", "get", &resp.Diagnostics)
		if state.PendingID.IsNull() || state.PendingID.IsUnknown() || state.PendingID.ValueInt64() < 1 {
			resp.Diagnostics.AddAttributeError(pathFor("pending_id"), "Required pending_id", "`pending_id` must be at least 1 when `operation = \"get\"`.")
		} else {
			endpoint = "/api/v1/pendingauthorimport/" + strconv.FormatInt(state.PendingID.ValueInt64(), 10)
		}
	case "exists":
		rejectIfSetInt64(state.PendingID, "pending_id", "exists", &resp.Diagnostics)
		if state.ProviderID.IsNull() || state.ProviderID.IsUnknown() || strings.TrimSpace(state.ProviderID.ValueString()) == "" {
			resp.Diagnostics.AddAttributeError(pathFor("provider_id"), "Required provider_id", "`provider_id` must be non-empty when `operation = \"exists\"`.")
		} else {
			providerID := strings.TrimSpace(state.ProviderID.ValueString())
			if strings.ContainsAny(providerID, `/\`) || providerID == "." || providerID == ".." {
				resp.Diagnostics.AddAttributeError(pathFor("provider_id"), "Unsafe provider_id", "`provider_id` must not contain path separators or dot-segments.")
			} else {
				state.ProviderID = types.StringValue(providerID)
				endpoint = "/api/v1/pendingauthorimport/author/exists/" + url.PathEscape(providerID)
			}
		}
	case "profile_options":
		rejectIfSetInt64(state.PendingID, "pending_id", "profile_options", &resp.Diagnostics)
		rejectIfSetString(state.ProviderID, "provider_id", "profile_options", &resp.Diagnostics)
		endpoint = "/api/v1/pendingauthorimport/profiles/options"
	}
	if resp.Diagnostics.HasError() {
		return
	}

	response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read pending author import data", err.Error())
		return
	}

	itemModels := []pendingAuthorImportItemModel{}
	existenceObj := types.ObjectNull(pendingAuthorExistenceAttrTypes())
	profileOptionsObj := types.ObjectNull(pendingProfileOptionsAttrTypes())

	switch operation {
	case "list":
		var rawItems []pendingAuthorImportItemAPI
		if err := json.Unmarshal(response.Body, &rawItems); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid pending author import list JSON.")
			return
		}
		sort.Slice(rawItems, func(i, j int) bool { return rawItems[i].ID < rawItems[j].ID })
		itemModels = make([]pendingAuthorImportItemModel, 0, len(rawItems))
		for _, item := range rawItems {
			itemModels = append(itemModels, buildPendingAuthorImportItemModel(ctx, item, &resp.Diagnostics))
		}
	case "get":
		var one pendingAuthorImportItemAPI
		if err := json.Unmarshal(response.Body, &one); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid pending author import JSON.")
			return
		}
		itemModels = []pendingAuthorImportItemModel{buildPendingAuthorImportItemModel(ctx, one, &resp.Diagnostics)}
	case "exists":
		var raw pendingAuthorExistenceAPI
		if err := json.Unmarshal(response.Body, &raw); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid pending author existence JSON.")
			return
		}
		model := pendingAuthorExistenceModel{
			Exists:       types.BoolValue(raw.Exists),
			AuthorID:     nullableInt(raw.AuthorID),
			AuthorName:   nullableString(raw.AuthorName),
			Pending:      types.BoolValue(raw.Pending),
			PendingID:    nullableInt(raw.PendingID),
			Status:       nullableString(raw.Status),
			NextAttempt:  nullableString(raw.NextAttempt),
			AttemptCount: nullableInt(raw.AttemptCount),
		}
		existenceObj, _ = objectState(ctx, pendingAuthorExistenceAttrTypes(), model, &resp.Diagnostics)
	case "profile_options":
		var raw pendingProfileOptionsAPI
		if err := json.Unmarshal(response.Body, &raw); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid pending import profile options JSON.")
			return
		}
		audiobookObj, _ := objectState(ctx, pendingMediaProfileOptionsAttrTypes(), buildPendingMediaOptionsModel(ctx, raw.Audiobook, &resp.Diagnostics), &resp.Diagnostics)
		ebookObj, _ := objectState(ctx, pendingMediaProfileOptionsAttrTypes(), buildPendingMediaOptionsModel(ctx, raw.Ebook, &resp.Diagnostics), &resp.Diagnostics)
		allObj, _ := objectState(ctx, pendingMediaProfileOptionsAttrTypes(), buildPendingMediaOptionsModel(ctx, raw.All, &resp.Diagnostics), &resp.Diagnostics)
		profileOptionsObj, _ = objectState(ctx, pendingProfileOptionsAttrTypes(), pendingProfileOptionsModel{
			Audiobook: audiobookObj,
			Ebook:     ebookObj,
			All:       allObj,
		}, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(publicFingerprint(endpoint))
	state.Items = listObjectState(ctx, pendingAuthorImportItemObjectType(), itemModels, &resp.Diagnostics)
	state.Existence = existenceObj
	state.ProfileOptions = profileOptionsObj
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func buildPendingAuthorImportItemModel(ctx context.Context, item pendingAuthorImportItemAPI, diagnostics *diag.Diagnostics) pendingAuthorImportItemModel {
	return pendingAuthorImportItemModel{
		ID:                           types.Int64Value(item.ID),
		ProviderID:                   types.StringValue(item.ProviderID),
		ProviderPrefix:               types.StringValue(item.ProviderPrefix),
		AuthorName:                   types.StringValue(item.AuthorName),
		AudiobookStatus:              types.StringValue(item.AudiobookStatus),
		EbookStatus:                  types.StringValue(item.EbookStatus),
		OverallStatus:                types.StringValue(item.OverallStatus),
		AudiobookMonitored:           nullableBool(item.AudiobookMonitored),
		AudiobookMonitorNewItems:     types.StringValue(item.AudiobookMonitorNewItems),
		AudiobookMonitorExistingMode: types.StringValue(item.AudiobookMonitorExistingMode),
		AudiobookQualityProfileID:    nullableInt(item.AudiobookQualityProfileID),
		AudiobookMetadataProfileID:   nullableInt(item.AudiobookMetadataProfileID),
		AudiobookBooksToMonitor:      listStringState(ctx, knownStringSlice(item.AudiobookBooksToMonitor), diagnostics),
		AudiobookBooksToSearch:       listStringState(ctx, knownStringSlice(item.AudiobookBooksToSearch), diagnostics),
		AudiobookTags:                setInt64State(ctx, item.AudiobookTags, diagnostics),
		EbookMonitored:               nullableBool(item.EbookMonitored),
		EbookMonitorNewItems:         types.StringValue(item.EbookMonitorNewItems),
		EbookMonitorExistingMode:     types.StringValue(item.EbookMonitorExistingMode),
		EbookQualityProfileID:        nullableInt(item.EbookQualityProfileID),
		EbookMetadataProfileID:       nullableInt(item.EbookMetadataProfileID),
		EbookBooksToMonitor:          listStringState(ctx, knownStringSlice(item.EbookBooksToMonitor), diagnostics),
		EbookBooksToSearch:           listStringState(ctx, knownStringSlice(item.EbookBooksToSearch), diagnostics),
		EbookTags:                    setInt64State(ctx, item.EbookTags, diagnostics),
		Tags:                         setInt64State(ctx, item.Tags, diagnostics),
		SearchForMissingBooks:        types.BoolValue(item.SearchForMissingBooks),
		CreatedAt:                    types.StringValue(item.CreatedAt),
		UpdatedAt:                    nullableString(item.UpdatedAt),
		LastAttemptAt:                nullableString(item.LastAttemptAt),
		NextAttemptAt:                types.StringValue(item.NextAttemptAt),
		AttemptCount:                 types.Int64Value(item.AttemptCount),
		MaxAttempts:                  types.Int64Value(item.MaxAttempts),
		HasError:                     types.BoolValue(strings.TrimSpace(item.LastError) != ""),
		SourceApplication:            types.StringValue(item.SourceApplication),
	}
}

func buildPendingMediaOptionsModel(ctx context.Context, raw pendingMediaProfileOptionsAPI, diagnostics *diag.Diagnostics) pendingMediaProfileOptionsModel {
	qualitySorted := append([]pendingProfileOptionAPI(nil), raw.QualityProfiles...)
	sort.Slice(qualitySorted, func(i, j int) bool { return qualitySorted[i].ID < qualitySorted[j].ID })
	qualityModels := make([]pendingProfileOptionItemModel, 0, len(qualitySorted))
	for _, q := range qualitySorted {
		qualityModels = append(qualityModels, pendingProfileOptionItemModel{ID: types.Int64Value(q.ID), Name: types.StringValue(q.Name)})
	}

	metadataSorted := append([]pendingProfileOptionAPI(nil), raw.MetadataProfiles...)
	sort.Slice(metadataSorted, func(i, j int) bool { return metadataSorted[i].ID < metadataSorted[j].ID })
	metadataModels := make([]pendingProfileOptionItemModel, 0, len(metadataSorted))
	for _, m := range metadataSorted {
		metadataModels = append(metadataModels, pendingProfileOptionItemModel{ID: types.Int64Value(m.ID), Name: types.StringValue(m.Name)})
	}

	folderNames := make([]string, 0, len(raw.RootFolders))
	for _, rf := range raw.RootFolders {
		if safe := safeEntryName(rf.Name); safe != "" {
			folderNames = append(folderNames, safe)
		}
	}
	sort.Strings(folderNames)

	return pendingMediaProfileOptionsModel{
		QualityProfiles:  listObjectState(ctx, pendingProfileOptionObjectType(), qualityModels, diagnostics),
		MetadataProfiles: listObjectState(ctx, pendingProfileOptionObjectType(), metadataModels, diagnostics),
		RootFolderCount:  types.Int64Value(int64(len(raw.RootFolders))),
		RootFolderNames:  listStringState(ctx, folderNames, diagnostics),
	}
}

func pendingAuthorImportItemObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":                              types.Int64Type,
		"provider_id":                     types.StringType,
		"provider_prefix":                 types.StringType,
		"author_name":                     types.StringType,
		"audiobook_status":                types.StringType,
		"ebook_status":                    types.StringType,
		"overall_status":                  types.StringType,
		"audiobook_monitored":             types.BoolType,
		"audiobook_monitor_new_items":     types.StringType,
		"audiobook_monitor_existing_mode": types.StringType,
		"audiobook_quality_profile_id":    types.Int64Type,
		"audiobook_metadata_profile_id":   types.Int64Type,
		"audiobook_books_to_monitor":      types.ListType{ElemType: types.StringType},
		"audiobook_books_to_search":       types.ListType{ElemType: types.StringType},
		"audiobook_tags":                  types.SetType{ElemType: types.Int64Type},
		"ebook_monitored":                 types.BoolType,
		"ebook_monitor_new_items":         types.StringType,
		"ebook_monitor_existing_mode":     types.StringType,
		"ebook_quality_profile_id":        types.Int64Type,
		"ebook_metadata_profile_id":       types.Int64Type,
		"ebook_books_to_monitor":          types.ListType{ElemType: types.StringType},
		"ebook_books_to_search":           types.ListType{ElemType: types.StringType},
		"ebook_tags":                      types.SetType{ElemType: types.Int64Type},
		"tags":                            types.SetType{ElemType: types.Int64Type},
		"search_for_missing_books":        types.BoolType,
		"created_at":                      types.StringType,
		"updated_at":                      types.StringType,
		"last_attempt_at":                 types.StringType,
		"next_attempt_at":                 types.StringType,
		"attempt_count":                   types.Int64Type,
		"max_attempts":                    types.Int64Type,
		"has_error":                       types.BoolType,
		"source_application":              types.StringType,
	}}
}

func pendingAuthorExistenceAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"exists":        types.BoolType,
		"author_id":     types.Int64Type,
		"author_name":   types.StringType,
		"pending":       types.BoolType,
		"pending_id":    types.Int64Type,
		"status":        types.StringType,
		"next_attempt":  types.StringType,
		"attempt_count": types.Int64Type,
	}
}

func pendingProfileOptionObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":   types.Int64Type,
		"name": types.StringType,
	}}
}

func pendingMediaProfileOptionsAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"quality_profiles":  types.ListType{ElemType: pendingProfileOptionObjectType()},
		"metadata_profiles": types.ListType{ElemType: pendingProfileOptionObjectType()},
		"root_folder_count": types.Int64Type,
		"root_folder_names": types.ListType{ElemType: types.StringType},
	}
}

func pendingProfileOptionsAttrTypes() map[string]attr.Type {
	mediaType := types.ObjectType{AttrTypes: pendingMediaProfileOptionsAttrTypes()}
	return map[string]attr.Type{
		"audiobook": mediaType,
		"ebook":     mediaType,
		"all":       mediaType,
	}
}

// ---------------------------------------------------------------------------
// Tags: Cutoff & Missing (chaptarr_cutoff_wanted & chaptarr_missing)
// ---------------------------------------------------------------------------

type wantedBooksDataSource struct {
	client *client.Client
	kind   string // "cutoff" or "missing"
}

type wantedBooksState struct {
	ID            types.String `tfsdk:"id"`
	BookID        types.Int64  `tfsdk:"book_id"`
	Page          types.Int64  `tfsdk:"page"`
	PageSize      types.Int64  `tfsdk:"page_size"`
	SortKey       types.String `tfsdk:"sort_key"`
	SortDirection types.String `tfsdk:"sort_direction"`
	IncludeAuthor types.Bool   `tfsdk:"include_author"`
	Monitored     types.Bool   `tfsdk:"monitored"`
	MediaType     types.String `tfsdk:"media_type"`
	TotalRecords  types.Int64  `tfsdk:"total_records"`
	Records       types.List   `tfsdk:"records"`
}

type wantedBookModel struct {
	ID              types.Int64  `tfsdk:"id"`
	ForeignBookID   types.String `tfsdk:"foreign_book_id"`
	AuthorID        types.Int64  `tfsdk:"author_id"`
	AuthorName      types.String `tfsdk:"author_name"`
	ForeignAuthorID types.String `tfsdk:"foreign_author_id"`
	Title           types.String `tfsdk:"title"`
	SeriesTitle     types.String `tfsdk:"series_title"`
	MediaType       types.String `tfsdk:"media_type"`
	Monitored       types.Bool   `tfsdk:"monitored"`
	AnyEditionOk    types.Bool   `tfsdk:"any_edition_ok"`
	ReleaseDate     types.String `tfsdk:"release_date"`
	Narrator        types.String `tfsdk:"narrator"`
	HasFiles        types.Bool   `tfsdk:"has_files"`
	Grabbed         types.Bool   `tfsdk:"grabbed"`
}

type wantedBooksPagingAPI struct {
	TotalRecords int64           `json:"totalRecords"`
	Records      []wantedBookAPI `json:"records"`
}

type wantedBookAPI struct {
	ID            int64             `json:"id"`
	ForeignBookID string            `json:"foreignBookId"`
	AuthorID      int64             `json:"authorId"`
	Author        *summaryAuthorAPI `json:"author"`
	Title         string            `json:"title"`
	SeriesTitle   string            `json:"seriesTitle"`
	MediaType     string            `json:"mediaType"`
	Monitored     bool              `json:"monitored"`
	AnyEditionOk  bool              `json:"anyEditionOk"`
	ReleaseDate   string            `json:"releaseDate"`
	Narrator      string            `json:"narrator"`
	HasFiles      bool              `json:"hasFiles"`
	Grabbed       bool              `json:"grabbed"`
}

func newCutoffWantedDataSource() datasource.DataSource {
	return &wantedBooksDataSource{kind: "cutoff"}
}

func newMissingDataSource() datasource.DataSource {
	return &wantedBooksDataSource{kind: "missing"}
}

func (d *wantedBooksDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	if d.kind == "cutoff" {
		resp.TypeName = req.ProviderTypeName + "_cutoff_wanted"
		return
	}
	resp.TypeName = req.ProviderTypeName + "_missing"
}

func (d *wantedBooksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	description := "Read missing wanted books or a single missing book by ID without storing nested filesystem paths or external cover/catalog URLs in Terraform state."
	if d.kind == "cutoff" {
		description = "Read quality-cutoff-unmet wanted books or a single cutoff-unmet book by ID without storing nested filesystem paths or external cover/catalog URLs in Terraform state."
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: description,
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Deterministic SHA-256 query identifier."},
			"book_id":        schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}, MarkdownDescription: "Optional single wanted book ID (`/api/v1/wanted/.../{id}`). Cannot be combined with paging or list filter attributes."},
			"page":           schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"page_size":      schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.Between(1, 1000)}},
			"sort_key":       schema.StringAttribute{Optional: true},
			"sort_direction": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("default", "ascending", "descending")}},
			"include_author": schema.BoolAttribute{Optional: true},
			"monitored":      schema.BoolAttribute{Optional: true},
			"media_type":     schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("all", "audiobook", "ebook")}},
			"total_records":  schema.Int64Attribute{Computed: true},
			"records": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                schema.Int64Attribute{Computed: true},
						"foreign_book_id":   schema.StringAttribute{Computed: true},
						"author_id":         schema.Int64Attribute{Computed: true},
						"author_name":       schema.StringAttribute{Computed: true},
						"foreign_author_id": schema.StringAttribute{Computed: true},
						"title":             schema.StringAttribute{Computed: true},
						"series_title":      schema.StringAttribute{Computed: true},
						"media_type":        schema.StringAttribute{Computed: true},
						"monitored":         schema.BoolAttribute{Computed: true},
						"any_edition_ok":    schema.BoolAttribute{Computed: true},
						"release_date":      schema.StringAttribute{Computed: true},
						"narrator":          schema.StringAttribute{Computed: true},
						"has_files":         schema.BoolAttribute{Computed: true},
						"grabbed":           schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *wantedBooksDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredDataSourceClient(req, resp)
}

func (d *wantedBooksDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if !requireDataSourceClient(d.client, resp) {
		return
	}
	var state wantedBooksState
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	basePath := "/api/v1/wanted/" + d.kind
	singleLookup := !state.BookID.IsNull() && !state.BookID.IsUnknown()
	var endpoint string
	if singleLookup {
		if state.BookID.ValueInt64() < 1 {
			resp.Diagnostics.AddAttributeError(pathFor("book_id"), "Invalid book_id", "`book_id` must be at least 1.")
			return
		}
		rejectIfSetInt64(state.Page, "page", "book_id", &resp.Diagnostics)
		rejectIfSetInt64(state.PageSize, "page_size", "book_id", &resp.Diagnostics)
		rejectIfSetString(state.SortKey, "sort_key", "book_id", &resp.Diagnostics)
		rejectIfSetString(state.SortDirection, "sort_direction", "book_id", &resp.Diagnostics)
		rejectIfSetBool(state.IncludeAuthor, "include_author", "book_id", &resp.Diagnostics)
		rejectIfSetBool(state.Monitored, "monitored", "book_id", &resp.Diagnostics)
		rejectIfSetString(state.MediaType, "media_type", "book_id", &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		endpoint = basePath + "/" + strconv.FormatInt(state.BookID.ValueInt64(), 10)
	} else {
		values := url.Values{}
		if !state.Page.IsNull() && !state.Page.IsUnknown() {
			if state.Page.ValueInt64() < 1 {
				resp.Diagnostics.AddAttributeError(pathFor("page"), "Invalid page", "`page` must be at least 1.")
				return
			}
			values.Set("page", strconv.FormatInt(state.Page.ValueInt64(), 10))
		}
		if !state.PageSize.IsNull() && !state.PageSize.IsUnknown() {
			if state.PageSize.ValueInt64() < 1 || state.PageSize.ValueInt64() > 1000 {
				resp.Diagnostics.AddAttributeError(pathFor("page_size"), "Invalid page_size", "`page_size` must be between 1 and 1000.")
				return
			}
			values.Set("pageSize", strconv.FormatInt(state.PageSize.ValueInt64(), 10))
		}
		if !state.SortKey.IsNull() && !state.SortKey.IsUnknown() {
			if trimmed := strings.TrimSpace(state.SortKey.ValueString()); trimmed != "" {
				values.Set("sortKey", trimmed)
				state.SortKey = types.StringValue(trimmed)
			}
		}
		if !state.SortDirection.IsNull() && !state.SortDirection.IsUnknown() {
			if trimmed := strings.TrimSpace(state.SortDirection.ValueString()); trimmed != "" {
				values.Set("sortDirection", trimmed)
				state.SortDirection = types.StringValue(trimmed)
			}
		}
		if !state.IncludeAuthor.IsNull() && !state.IncludeAuthor.IsUnknown() {
			values.Set("includeAuthor", strconv.FormatBool(state.IncludeAuthor.ValueBool()))
		}
		if !state.Monitored.IsNull() && !state.Monitored.IsUnknown() {
			values.Set("monitored", strconv.FormatBool(state.Monitored.ValueBool()))
		}
		if !state.MediaType.IsNull() && !state.MediaType.IsUnknown() {
			if trimmed := strings.TrimSpace(state.MediaType.ValueString()); trimmed != "" {
				values.Set("mediaType", trimmed)
				state.MediaType = types.StringValue(trimmed)
			}
		}
		endpoint = operationalQueryPath(basePath, values)
	}

	response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read wanted books", err.Error())
		return
	}

	var rawBooks []wantedBookAPI
	var totalRecords int64
	if singleLookup {
		var one wantedBookAPI
		if err := json.Unmarshal(response.Body, &one); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid wanted-book JSON.")
			return
		}
		rawBooks = []wantedBookAPI{one}
		totalRecords = 1
	} else {
		var page wantedBooksPagingAPI
		if err := json.Unmarshal(response.Body, &page); err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid wanted-books page JSON.")
			return
		}
		rawBooks = page.Records
		totalRecords = page.TotalRecords
	}

	models := make([]wantedBookModel, 0, len(rawBooks))
	for _, book := range rawBooks {
		var authorName, foreignAuthorID string
		if book.Author != nil {
			authorName = book.Author.AuthorName
			foreignAuthorID = book.Author.ForeignAuthorID
		}
		models = append(models, wantedBookModel{
			ID:              types.Int64Value(book.ID),
			ForeignBookID:   types.StringValue(book.ForeignBookID),
			AuthorID:        types.Int64Value(book.AuthorID),
			AuthorName:      types.StringValue(authorName),
			ForeignAuthorID: types.StringValue(foreignAuthorID),
			Title:           types.StringValue(book.Title),
			SeriesTitle:     types.StringValue(book.SeriesTitle),
			MediaType:       types.StringValue(book.MediaType),
			Monitored:       types.BoolValue(book.Monitored),
			AnyEditionOk:    types.BoolValue(book.AnyEditionOk),
			ReleaseDate:     types.StringValue(book.ReleaseDate),
			Narrator:        types.StringValue(book.Narrator),
			HasFiles:        types.BoolValue(book.HasFiles),
			Grabbed:         types.BoolValue(book.Grabbed),
		})
	}

	state.ID = types.StringValue(publicFingerprint(endpoint))
	state.TotalRecords = types.Int64Value(totalRecords)
	state.Records = listObjectState(ctx, wantedBookObjectType(), models, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func wantedBookObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":                types.Int64Type,
		"foreign_book_id":   types.StringType,
		"author_id":         types.Int64Type,
		"author_name":       types.StringType,
		"foreign_author_id": types.StringType,
		"title":             types.StringType,
		"series_title":      types.StringType,
		"media_type":        types.StringType,
		"monitored":         types.BoolType,
		"any_edition_ok":    types.BoolType,
		"release_date":      types.StringType,
		"narrator":          types.StringType,
		"has_files":         types.BoolType,
		"grabbed":           types.BoolType,
	}}
}
