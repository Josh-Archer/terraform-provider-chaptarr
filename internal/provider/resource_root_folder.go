package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &rootFolderResource{}
	_ resource.ResourceWithImportState = &rootFolderResource{}
)

type rootFolderResource struct{ client *client.Client }

type rootFolderModel struct {
	ID                                types.String `tfsdk:"id"`
	Name                              types.String `tfsdk:"name"`
	Path                              types.String `tfsdk:"path"`
	FolderType                        types.String `tfsdk:"folder_type"`
	DefaultTags                       types.Set    `tfsdk:"default_tags"`
	IsCalibreLibrary                  types.Bool   `tfsdk:"is_calibre_library"`
	Host                              types.String `tfsdk:"host"`
	Port                              types.Int64  `tfsdk:"port"`
	URLBase                           types.String `tfsdk:"url_base"`
	Username                          types.String `tfsdk:"username"`
	Password                          types.String `tfsdk:"password"`
	Library                           types.String `tfsdk:"library"`
	OutputFormat                      types.String `tfsdk:"output_format"`
	OutputProfile                     types.String `tfsdk:"output_profile"`
	UseSSL                            types.Bool   `tfsdk:"use_ssl"`
	PlaceEbooksWithAudiobooks         types.Bool   `tfsdk:"place_ebooks_with_audiobooks"`
	DefaultSyncMonitoredAcrossFormats types.Bool   `tfsdk:"default_sync_monitored_across_formats"`
	AudiobookMonitorExisting          types.Int64  `tfsdk:"audiobook_monitor_existing"`
	AudiobookMonitorFuture            types.Bool   `tfsdk:"audiobook_monitor_future"`
	AudiobookQualityProfileID         types.Int64  `tfsdk:"audiobook_quality_profile_id"`
	AudiobookMetadataProfileID        types.Int64  `tfsdk:"audiobook_metadata_profile_id"`
	AudiobookWriteMetadataJSON        types.Bool   `tfsdk:"audiobook_write_metadata_json"`
	AudiobookWriteCover               types.Bool   `tfsdk:"audiobook_write_cover"`
	AudiobookTags                     types.Set    `tfsdk:"audiobook_tags"`
	EbookMonitorExisting              types.Int64  `tfsdk:"ebook_monitor_existing"`
	EbookMonitorFuture                types.Bool   `tfsdk:"ebook_monitor_future"`
	EbookQualityProfileID             types.Int64  `tfsdk:"ebook_quality_profile_id"`
	EbookMetadataProfileID            types.Int64  `tfsdk:"ebook_metadata_profile_id"`
	EbookWriteMetadataJSON            types.Bool   `tfsdk:"ebook_write_metadata_json"`
	EbookWriteCover                   types.Bool   `tfsdk:"ebook_write_cover"`
	EbookTags                         types.Set    `tfsdk:"ebook_tags"`
	Accessible                        types.Bool   `tfsdk:"accessible"`
	FreeSpace                         types.Int64  `tfsdk:"free_space"`
	TotalSpace                        types.Int64  `tfsdk:"total_space"`
	IsEffectiveDefaultAudiobook       types.Bool   `tfsdk:"is_effective_default_audiobook"`
	IsEffectiveDefaultEbook           types.Bool   `tfsdk:"is_effective_default_ebook"`
	AllowDestroy                      types.Bool   `tfsdk:"allow_destroy"`
}

type rootFolderAPI struct {
	ID                                int64   `json:"id,omitempty"`
	Name                              string  `json:"name"`
	Path                              string  `json:"path"`
	DefaultTags                       []int64 `json:"defaultTags"`
	IsCalibreLibrary                  bool    `json:"isCalibreLibrary"`
	Host                              *string `json:"host,omitempty"`
	Port                              int64   `json:"port"`
	URLBase                           *string `json:"urlBase,omitempty"`
	Username                          *string `json:"username,omitempty"`
	Password                          *string `json:"password,omitempty"`
	Library                           *string `json:"library,omitempty"`
	OutputFormat                      *string `json:"outputFormat,omitempty"`
	OutputProfile                     *string `json:"outputProfile,omitempty"`
	UseSSL                            bool    `json:"useSsl"`
	FolderType                        int64   `json:"folderType"`
	PlaceEbooksWithAudiobooks         bool    `json:"placeEbooksWithAudiobooks"`
	DefaultSyncMonitoredAcrossFormats *bool   `json:"defaultSyncMonitoredAcrossFormats,omitempty"`
	AudiobookMonitorExisting          *int64  `json:"audiobookMonitorExisting,omitempty"`
	AudiobookMonitorFuture            *bool   `json:"audiobookMonitorFuture,omitempty"`
	AudiobookQualityProfileID         *int64  `json:"audiobookQualityProfileId,omitempty"`
	AudiobookMetadataProfileID        *int64  `json:"audiobookMetadataProfileId,omitempty"`
	AudiobookWriteMetadataJSON        *bool   `json:"audiobookWriteAudioBookShelfMetadataJson,omitempty"`
	AudiobookWriteCover               *bool   `json:"audiobookWriteAudioBookShelfCover,omitempty"`
	AudiobookTags                     []int64 `json:"audiobookTags,omitempty"`
	EbookMonitorExisting              *int64  `json:"ebookMonitorExisting,omitempty"`
	EbookMonitorFuture                *bool   `json:"ebookMonitorFuture,omitempty"`
	EbookQualityProfileID             *int64  `json:"ebookQualityProfileId,omitempty"`
	EbookMetadataProfileID            *int64  `json:"ebookMetadataProfileId,omitempty"`
	EbookWriteMetadataJSON            *bool   `json:"ebookWriteAudioBookShelfMetadataJson,omitempty"`
	EbookWriteCover                   *bool   `json:"ebookWriteAudioBookShelfCover,omitempty"`
	EbookTags                         []int64 `json:"ebookTags,omitempty"`
	Accessible                        bool    `json:"accessible"`
	FreeSpace                         *int64  `json:"freeSpace"`
	TotalSpace                        *int64  `json:"totalSpace"`
	IsEffectiveDefaultAudiobook       bool    `json:"isEffectiveDefaultAudiobook"`
	IsEffectiveDefaultEbook           bool    `json:"isEffectiveDefaultEbook"`
}

func newRootFolderResource() resource.Resource { return &rootFolderResource{} }

func (r *rootFolderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_root_folder"
}

func (r *rootFolderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	optionalString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: description}
	}
	optionalBool := func(description string) schema.BoolAttribute {
		return schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: description}
	}
	optionalInt := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, Computed: true, MarkdownDescription: description}
	}
	profileID := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}, MarkdownDescription: description}
	}
	monitorExisting := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.Between(0, 2)}, MarkdownDescription: description}
	}
	tagSet := func(description string) schema.SetAttribute {
		return schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type, MarkdownDescription: description}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Chaptarr library root-folder registration. Create queues Chaptarr's initial scan. Delete removes only the registration and related ingest-queue rows, never library files, and requires allow_destroy=true.",
		Attributes: map[string]schema.Attribute{
			"id":                 schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":               schema.StringAttribute{Required: true},
			"path":               schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Existing readable and writable path. Chaptarr cannot edit a root-folder path in place."},
			"folder_type":        schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("mixed", "audiobook", "ebook")}},
			"default_tags":       tagSet("Default tag identifiers."),
			"is_calibre_library": optionalBool("Whether this root is a Calibre library."),
			"host":               optionalString("Calibre host."), "port": optionalInt("Calibre port."), "url_base": optionalString("Calibre URL base."),
			"username": optionalString("Calibre username."),
			"password": schema.StringAttribute{Optional: true, Sensitive: true, WriteOnly: true, MarkdownDescription: "Calibre password. It is sent only when explicitly configured and is never stored in state."},
			"library":  optionalString("Calibre library name."), "output_format": optionalString("Calibre output formats."), "output_profile": optionalString("Calibre output profile."), "use_ssl": optionalBool("Use TLS for Calibre."),
			"place_ebooks_with_audiobooks":          optionalBool("For mixed roots, place ebooks with audiobooks."),
			"default_sync_monitored_across_formats": optionalBool("Default cross-format monitoring behavior."),
			"audiobook_monitor_existing":            monitorExisting("Audiobook existing monitoring: 0 none, 1 all, 2 selected."),
			"audiobook_monitor_future":              optionalBool("Monitor future audiobooks."),
			"audiobook_quality_profile_id":          profileID("Audiobook quality profile identifier."),
			"audiobook_metadata_profile_id":         profileID("Audiobook metadata profile identifier."),
			"audiobook_write_metadata_json":         optionalBool("Write Audiobookshelf metadata JSON for audiobooks."),
			"audiobook_write_cover":                 optionalBool("Write Audiobookshelf covers for audiobooks."),
			"audiobook_tags":                        tagSet("Audiobook tag identifiers."),
			"ebook_monitor_existing":                monitorExisting("Ebook existing monitoring: 0 none, 1 all, 2 selected."),
			"ebook_monitor_future":                  optionalBool("Monitor future ebooks."),
			"ebook_quality_profile_id":              profileID("Ebook quality profile identifier."),
			"ebook_metadata_profile_id":             profileID("Ebook metadata profile identifier."),
			"ebook_write_metadata_json":             optionalBool("Write Audiobookshelf metadata JSON for ebooks."),
			"ebook_write_cover":                     optionalBool("Write Audiobookshelf covers for ebooks."),
			"ebook_tags":                            tagSet("Ebook tag identifiers."),
			"accessible":                            schema.BoolAttribute{Computed: true}, "free_space": schema.Int64Attribute{Computed: true}, "total_space": schema.Int64Attribute{Computed: true},
			"is_effective_default_audiobook": schema.BoolAttribute{Computed: true}, "is_effective_default_ebook": schema.BoolAttribute{Computed: true},
			"allow_destroy": schema.BoolAttribute{Optional: true, MarkdownDescription: "Must be true before Terraform may remove the Chaptarr registration. Chaptarr does not delete library files."},
		},
	}
}

func (r *rootFolderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	r.client = apiClient
}

func (r *rootFolderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan rootFolderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadRootFolderWriteOnlyConfig(ctx, req.Config, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !validateRootFolderModel(plan, true, &resp.Diagnostics) {
		return
	}
	payload, diagnostics := rootFolderPayload(ctx, plan, 0)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode root folder", "The request could not be encoded.")
		return
	}
	response, err := r.client.Do(ctx, http.MethodPost, "/api/v1/rootfolder", body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create root folder", err.Error())
		return
	}
	id, err := createdIdentifier(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", err.Error())
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *rootFolderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state rootFolderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
}

func (r *rootFolderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan rootFolderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadRootFolderWriteOnlyConfig(ctx, req.Config, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !validateRootFolderModel(plan, false, &resp.Diagnostics) {
		return
	}
	id, ok := positiveModelID(plan.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid root-folder state", "The root folder has no valid numeric identifier.")
		return
	}
	payload, diagnostics := rootFolderPayload(ctx, plan, id)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode root folder", "The request could not be encoded.")
		return
	}
	if _, err := r.client.Do(ctx, http.MethodPut, "/api/v1/rootfolder/"+strconv.FormatInt(id, 10), body); err != nil {
		resp.Diagnostics.AddError("Unable to update root folder", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func loadRootFolderWriteOnlyConfig(ctx context.Context, config tfsdk.Config, plan *rootFolderModel, diagnostics *diag.Diagnostics) {
	diagnostics.Append(config.GetAttribute(ctx, path.Root("password"), &plan.Password)...)
}

func (r *rootFolderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state rootFolderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.AllowDestroy.IsNull() || state.AllowDestroy.IsUnknown() || !state.AllowDestroy.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("allow_destroy"), "Root-folder destroy is disabled", "Set `allow_destroy = true` and apply before removing this resource. Chaptarr removes only its registration and ingest-queue rows; it does not delete library files.")
		return
	}
	id, ok := positiveModelID(state.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid root-folder state", "The root folder has no valid numeric identifier.")
		return
	}
	if _, err := r.client.Do(ctx, http.MethodDelete, "/api/v1/rootfolder/"+strconv.FormatInt(id, 10), nil); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Unable to delete root folder", err.Error())
	}
}

func (r *rootFolderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if err != nil || id < 1 {
		resp.Diagnostics.AddError("Invalid import identifier", "Use the positive numeric Chaptarr root-folder identifier.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strconv.FormatInt(id, 10))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("allow_destroy"), false)...)
}

func validateRootFolderModel(model rootFolderModel, creating bool, diagnostics *diag.Diagnostics) bool {
	typeName := model.FolderType.ValueString()
	if typeName == "audiobook" && hasMediaSettings(model, "ebook") {
		diagnostics.AddError("Invalid ebook settings", "An audiobook-only root folder cannot configure ebook settings.")
	}
	if typeName == "ebook" && hasMediaSettings(model, "audiobook") {
		diagnostics.AddError("Invalid audiobook settings", "An ebook-only root folder cannot configure audiobook settings.")
	}
	calibre := !model.IsCalibreLibrary.IsNull() && !model.IsCalibreLibrary.IsUnknown() && model.IsCalibreLibrary.ValueBool()
	if calibre {
		for name, value := range map[string]types.String{"host": model.Host, "library": model.Library, "output_profile": model.OutputProfile} {
			if value.IsNull() || value.IsUnknown() || strings.TrimSpace(value.ValueString()) == "" {
				diagnostics.AddAttributeError(path.Root(name), "Calibre value required", fmt.Sprintf("`%s` is required for a Calibre library.", name))
			}
		}
		if model.Port.IsNull() || model.Port.IsUnknown() || model.Port.ValueInt64() < 1 || model.Port.ValueInt64() > 65535 {
			diagnostics.AddAttributeError(path.Root("port"), "Invalid Calibre port", "Set a Calibre port from 1 through 65535.")
		}
		usernameConfigured := configured(model.Username) && strings.TrimSpace(model.Username.ValueString()) != ""
		passwordConfigured := configured(model.Password) && model.Password.ValueString() != ""
		if passwordConfigured && !usernameConfigured {
			diagnostics.AddAttributeError(path.Root("username"), "Calibre username required", "Set `username` when a Calibre password is configured.")
		}
		if creating && usernameConfigured && !passwordConfigured {
			diagnostics.AddAttributeError(path.Root("password"), "Calibre password required", "Set the write-only `password` when creating an authenticated Calibre library.")
		}
	}
	return !diagnostics.HasError()
}

func hasMediaSettings(model rootFolderModel, media string) bool {
	if media == "ebook" {
		return configured(model.EbookMonitorExisting) || configured(model.EbookMonitorFuture) || configured(model.EbookQualityProfileID) || configured(model.EbookMetadataProfileID) || configured(model.EbookWriteMetadataJSON) || configured(model.EbookWriteCover) || configuredSet(model.EbookTags)
	}
	return configured(model.AudiobookMonitorExisting) || configured(model.AudiobookMonitorFuture) || configured(model.AudiobookQualityProfileID) || configured(model.AudiobookMetadataProfileID) || configured(model.AudiobookWriteMetadataJSON) || configured(model.AudiobookWriteCover) || configuredSet(model.AudiobookTags)
}

func configured(value interface {
	IsNull() bool
	IsUnknown() bool
}) bool {
	return !value.IsNull() && !value.IsUnknown()
}
func configuredSet(value types.Set) bool {
	return !value.IsNull() && !value.IsUnknown() && len(value.Elements()) > 0
}

func rootFolderPayload(ctx context.Context, model rootFolderModel, id int64) (rootFolderAPI, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	payload := rootFolderAPI{ID: id, Name: model.Name.ValueString(), Path: model.Path.ValueString(), FolderType: map[string]int64{"mixed": 0, "audiobook": 1, "ebook": 2}[model.FolderType.ValueString()]}
	payload.DefaultTags = setInt64Values(ctx, model.DefaultTags, &diagnostics)
	payload.AudiobookTags = setInt64Values(ctx, model.AudiobookTags, &diagnostics)
	payload.EbookTags = setInt64Values(ctx, model.EbookTags, &diagnostics)
	if configured(model.IsCalibreLibrary) {
		payload.IsCalibreLibrary = model.IsCalibreLibrary.ValueBool()
	}
	if configured(model.Port) {
		payload.Port = model.Port.ValueInt64()
	}
	if configured(model.UseSSL) {
		payload.UseSSL = model.UseSSL.ValueBool()
	}
	if configured(model.PlaceEbooksWithAudiobooks) {
		payload.PlaceEbooksWithAudiobooks = model.PlaceEbooksWithAudiobooks.ValueBool()
	}
	payload.Host = stringPointer(model.Host)
	payload.URLBase = stringPointer(model.URLBase)
	payload.Username = stringPointer(model.Username)
	payload.Library = stringPointer(model.Library)
	payload.OutputFormat = stringPointer(model.OutputFormat)
	payload.OutputProfile = stringPointer(model.OutputProfile)
	if configured(model.Password) {
		value := model.Password.ValueString()
		payload.Password = &value
	}
	payload.DefaultSyncMonitoredAcrossFormats = boolPointer(model.DefaultSyncMonitoredAcrossFormats)
	payload.AudiobookMonitorExisting = intPointer(model.AudiobookMonitorExisting)
	payload.AudiobookMonitorFuture = boolPointer(model.AudiobookMonitorFuture)
	payload.AudiobookQualityProfileID = intPointer(model.AudiobookQualityProfileID)
	payload.AudiobookMetadataProfileID = intPointer(model.AudiobookMetadataProfileID)
	payload.AudiobookWriteMetadataJSON = boolPointer(model.AudiobookWriteMetadataJSON)
	payload.AudiobookWriteCover = boolPointer(model.AudiobookWriteCover)
	payload.EbookMonitorExisting = intPointer(model.EbookMonitorExisting)
	payload.EbookMonitorFuture = boolPointer(model.EbookMonitorFuture)
	payload.EbookQualityProfileID = intPointer(model.EbookQualityProfileID)
	payload.EbookMetadataProfileID = intPointer(model.EbookMetadataProfileID)
	payload.EbookWriteMetadataJSON = boolPointer(model.EbookWriteMetadataJSON)
	payload.EbookWriteCover = boolPointer(model.EbookWriteCover)
	return payload, diagnostics
}

func (r *rootFolderResource) refresh(ctx context.Context, state *rootFolderModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid root-folder state", "The root folder has no valid numeric identifier.")
		return
	}
	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/rootfolder/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			target.RemoveResource(ctx)
			return
		}
		diagnostics.AddError("Unable to read root folder", err.Error())
		return
	}
	var current rootFolderAPI
	if err := json.Unmarshal(response.Body, &current); err != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid root-folder document.")
		return
	}
	state.ID = types.StringValue(strconv.FormatInt(current.ID, 10))
	state.Name = types.StringValue(current.Name)
	state.Path = types.StringValue(current.Path)
	folderType, validFolderType := map[int64]string{0: "mixed", 1: "audiobook", 2: "ebook"}[current.FolderType]
	if !validFolderType {
		diagnostics.AddError("Invalid Chaptarr response", fmt.Sprintf("Chaptarr returned unsupported root-folder type %d.", current.FolderType))
		return
	}
	state.FolderType = types.StringValue(folderType)
	state.DefaultTags = setInt64State(ctx, current.DefaultTags, diagnostics)
	state.IsCalibreLibrary = types.BoolValue(current.IsCalibreLibrary)
	state.Host = nullableString(current.Host)
	state.Port = types.Int64Value(current.Port)
	state.URLBase = nullableString(current.URLBase)
	state.Username = nullableString(current.Username)
	state.Password = types.StringNull()
	state.Library = nullableString(current.Library)
	state.OutputFormat = nullableString(current.OutputFormat)
	state.OutputProfile = nullableString(current.OutputProfile)
	state.UseSSL = types.BoolValue(current.UseSSL)
	state.PlaceEbooksWithAudiobooks = types.BoolValue(current.PlaceEbooksWithAudiobooks)
	state.DefaultSyncMonitoredAcrossFormats = nullableBool(current.DefaultSyncMonitoredAcrossFormats)
	state.AudiobookMonitorExisting = nullableInt(current.AudiobookMonitorExisting)
	state.AudiobookMonitorFuture = nullableBool(current.AudiobookMonitorFuture)
	state.AudiobookQualityProfileID = nullableInt(current.AudiobookQualityProfileID)
	state.AudiobookMetadataProfileID = nullableInt(current.AudiobookMetadataProfileID)
	state.AudiobookWriteMetadataJSON = nullableBool(current.AudiobookWriteMetadataJSON)
	state.AudiobookWriteCover = nullableBool(current.AudiobookWriteCover)
	state.AudiobookTags = setInt64State(ctx, current.AudiobookTags, diagnostics)
	state.EbookMonitorExisting = nullableInt(current.EbookMonitorExisting)
	state.EbookMonitorFuture = nullableBool(current.EbookMonitorFuture)
	state.EbookQualityProfileID = nullableInt(current.EbookQualityProfileID)
	state.EbookMetadataProfileID = nullableInt(current.EbookMetadataProfileID)
	state.EbookWriteMetadataJSON = nullableBool(current.EbookWriteMetadataJSON)
	state.EbookWriteCover = nullableBool(current.EbookWriteCover)
	state.EbookTags = setInt64State(ctx, current.EbookTags, diagnostics)
	state.Accessible = types.BoolValue(current.Accessible)
	state.FreeSpace = nullableInt(current.FreeSpace)
	state.TotalSpace = nullableInt(current.TotalSpace)
	state.IsEffectiveDefaultAudiobook = types.BoolValue(current.IsEffectiveDefaultAudiobook)
	state.IsEffectiveDefaultEbook = types.BoolValue(current.IsEffectiveDefaultEbook)
	if state.AllowDestroy.IsNull() || state.AllowDestroy.IsUnknown() {
		state.AllowDestroy = types.BoolValue(false)
	}
	diagnostics.Append(target.Set(ctx, state)...)
}

func setInt64Values(ctx context.Context, value types.Set, diagnostics *diag.Diagnostics) []int64 {
	if value.IsNull() || value.IsUnknown() {
		return []int64{}
	}
	var result []int64
	diagnostics.Append(value.ElementsAs(ctx, &result, false)...)
	return result
}
func setInt64State(ctx context.Context, values []int64, diagnostics *diag.Diagnostics) types.Set {
	if values == nil {
		values = []int64{}
	}
	result, diags := types.SetValueFrom(ctx, types.Int64Type, values)
	diagnostics.Append(diags...)
	return result
}
func stringPointer(value types.String) *string {
	if !configured(value) {
		return nil
	}
	result := value.ValueString()
	return &result
}
func boolPointer(value types.Bool) *bool {
	if !configured(value) {
		return nil
	}
	result := value.ValueBool()
	return &result
}
func intPointer(value types.Int64) *int64 {
	if !configured(value) {
		return nil
	}
	result := value.ValueInt64()
	return &result
}
func nullableString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}
func nullableBool(value *bool) types.Bool {
	if value == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*value)
}
func nullableInt(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}
