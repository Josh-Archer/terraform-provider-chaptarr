package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
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
	_ resource.Resource                = &authorResource{}
	_ resource.ResourceWithImportState = &authorResource{}
	_ resource.Resource                = &seriesResource{}
	_ resource.ResourceWithImportState = &seriesResource{}
	_ datasource.DataSource            = &libraryLookupDataSource{}
)

type authorResource struct{ client *client.Client }
type authorModel struct {
	ID                         types.String `tfsdk:"id"`
	ForeignAuthorID            types.String `tfsdk:"foreign_author_id"`
	AuthorName                 types.String `tfsdk:"author_name"`
	Monitored                  types.Bool   `tfsdk:"monitored"`
	AudiobookMonitorExisting   types.Int64  `tfsdk:"audiobook_monitor_existing"`
	AudiobookMonitorFuture     types.Bool   `tfsdk:"audiobook_monitor_future"`
	EbookMonitorExisting       types.Int64  `tfsdk:"ebook_monitor_existing"`
	EbookMonitorFuture         types.Bool   `tfsdk:"ebook_monitor_future"`
	AudiobookRootFolderPath    types.String `tfsdk:"audiobook_root_folder_path"`
	EbookRootFolderPath        types.String `tfsdk:"ebook_root_folder_path"`
	AudiobookQualityProfileID  types.Int64  `tfsdk:"audiobook_quality_profile_id"`
	EbookQualityProfileID      types.Int64  `tfsdk:"ebook_quality_profile_id"`
	AudiobookMetadataProfileID types.Int64  `tfsdk:"audiobook_metadata_profile_id"`
	EbookMetadataProfileID     types.Int64  `tfsdk:"ebook_metadata_profile_id"`
	AudiobookTags              types.Set    `tfsdk:"audiobook_tags"`
	EbookTags                  types.Set    `tfsdk:"ebook_tags"`
	SearchForMissingBooks      types.Bool   `tfsdk:"search_for_missing_books"`
	MoveFilesOnUpdate          types.Bool   `tfsdk:"move_files_on_update"`
	DeleteFilesOnDestroy       types.Bool   `tfsdk:"delete_files_on_destroy"`
	AddImportListExclusion     types.Bool   `tfsdk:"add_import_list_exclusion_on_destroy"`
}

type authorAPI struct {
	ID                         int64             `json:"id,omitempty"`
	ForeignAuthorID            string            `json:"foreignAuthorId,omitempty"`
	AuthorName                 string            `json:"authorName,omitempty"`
	Monitored                  bool              `json:"monitored"`
	AudiobookMonitorExisting   *int64            `json:"audiobookMonitorExisting,omitempty"`
	AudiobookMonitorFuture     *bool             `json:"audiobookMonitorFuture,omitempty"`
	EbookMonitorExisting       *int64            `json:"ebookMonitorExisting,omitempty"`
	EbookMonitorFuture         *bool             `json:"ebookMonitorFuture,omitempty"`
	AudiobookRootFolderPath    *string           `json:"audiobookRootFolderPath,omitempty"`
	EbookRootFolderPath        *string           `json:"ebookRootFolderPath,omitempty"`
	AudiobookQualityProfileID  *int64            `json:"audiobookQualityProfileId,omitempty"`
	EbookQualityProfileID      *int64            `json:"ebookQualityProfileId,omitempty"`
	AudiobookMetadataProfileID *int64            `json:"audiobookMetadataProfileId,omitempty"`
	EbookMetadataProfileID     *int64            `json:"ebookMetadataProfileId,omitempty"`
	AudiobookTags              []int64           `json:"audiobookTags"`
	EbookTags                  []int64           `json:"ebookTags"`
	AddOptions                 *authorAddOptions `json:"addOptions,omitempty"`
}
type authorAddOptions struct {
	SearchForMissingBooks bool `json:"searchForMissingBooks"`
}

func newAuthorResource() resource.Resource { return &authorResource{} }
func (r *authorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_author"
}
func (r *authorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	positive := []validator.Int64{int64validator.AtLeast(1)}
	monitorMode := []validator.Int64{int64validator.Between(0, 2)}
	resp.Schema = schema.Schema{MarkdownDescription: "Manage Chaptarr author collection intent. Refresh is GET-only. Search, file moves, and file deletion are disabled unless their explicit controls are true.", Attributes: map[string]schema.Attribute{
		"id":                         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"foreign_author_id":          schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Provider-prefixed identity such as `hc:191785`; use chaptarr_author_lookup to choose it."},
		"author_name":                schema.StringAttribute{Computed: true},
		"monitored":                  schema.BoolAttribute{Required: true},
		"audiobook_monitor_existing": schema.Int64Attribute{Required: true, Validators: monitorMode},
		"audiobook_monitor_future":   schema.BoolAttribute{Required: true},
		"ebook_monitor_existing":     schema.Int64Attribute{Required: true, Validators: monitorMode},
		"ebook_monitor_future":       schema.BoolAttribute{Required: true},
		"audiobook_root_folder_path": schema.StringAttribute{Optional: true}, "ebook_root_folder_path": schema.StringAttribute{Optional: true},
		"audiobook_quality_profile_id": schema.Int64Attribute{Optional: true, Validators: positive}, "ebook_quality_profile_id": schema.Int64Attribute{Optional: true, Validators: positive},
		"audiobook_metadata_profile_id": schema.Int64Attribute{Optional: true, Validators: positive}, "ebook_metadata_profile_id": schema.Int64Attribute{Optional: true, Validators: positive},
		"audiobook_tags": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type}, "ebook_tags": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type},
		"search_for_missing_books":             schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Explicitly allow a missing-book search during create. Defaults to false."},
		"move_files_on_update":                 schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Explicitly allow Chaptarr to queue file moves during update. Defaults to false."},
		"delete_files_on_destroy":              schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Explicitly allow deletion of media files during destroy. Defaults to false."},
		"add_import_list_exclusion_on_destroy": schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Explicitly add an import-list exclusion during destroy. Defaults to false."},
	}}
}
func (r *authorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func normalizeAuthorControls(m *authorModel) {
	for _, v := range []*types.Bool{&m.SearchForMissingBooks, &m.MoveFilesOnUpdate, &m.DeleteFilesOnDestroy, &m.AddImportListExclusion} {
		if v.IsNull() || v.IsUnknown() {
			*v = types.BoolValue(false)
		}
	}
}
func (r *authorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan authorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	normalizeAuthorControls(&plan)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := authorPayload(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/author?queueIfUnavailable=false", payload, "author", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *authorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state authorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	normalizeAuthorControls(&state)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *authorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan authorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	normalizeAuthorControls(&plan)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	current, found := readProfile(ctx, r.client, "/api/v1/author/"+strconv.FormatInt(id, 10), "author", &resp.State, &resp.Diagnostics)
	if !found || resp.Diagnostics.HasError() {
		return
	}
	var payload map[string]any
	if json.Unmarshal(current, &payload) != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid author document.")
		return
	}
	overlayAuthorPayload(ctx, payload, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	body, _ := json.Marshal(payload)
	endpoint := "/api/v1/author/" + strconv.FormatInt(id, 10) + "?moveFiles=" + strconv.FormatBool(plan.MoveFilesOnUpdate.ValueBool())
	if _, err := r.client.Do(ctx, http.MethodPut, endpoint, body); err != nil {
		resp.Diagnostics.AddError("Unable to update author", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *authorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state authorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	normalizeAuthorControls(&state)
	id, ok := positiveModelID(state.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	values := url.Values{}
	values.Set("deleteFiles", strconv.FormatBool(state.DeleteFilesOnDestroy.ValueBool()))
	values.Set("addImportListExclusion", strconv.FormatBool(state.AddImportListExclusion.ValueBool()))
	values.Set("readdAuthor", "false")
	_, err := r.client.Do(ctx, http.MethodDelete, "/api/v1/author/"+strconv.FormatInt(id, 10)+"?"+values.Encode(), nil)
	if err != nil {
		var apiErr *client.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			resp.Diagnostics.AddError("Unable to delete author", err.Error())
		}
	}
}
func (r *authorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "author", &resp.State, &resp.Diagnostics)
}
func authorPayload(ctx context.Context, m authorModel, d *diag.Diagnostics) authorAPI {
	if m.ForeignAuthorID.IsNull() || m.ForeignAuthorID.IsUnknown() || !validProviderID(m.ForeignAuthorID.ValueString()) {
		d.AddAttributeError(path.Root("foreign_author_id"), "Provider-prefixed author identifier required", "Use author lookup to choose an explicit provider-prefixed identifier such as `hc:191785`.")
		return authorAPI{}
	}
	if (m.AudiobookRootFolderPath.IsNull() || strings.TrimSpace(m.AudiobookRootFolderPath.ValueString()) == "") && (m.EbookRootFolderPath.IsNull() || strings.TrimSpace(m.EbookRootFolderPath.ValueString()) == "") {
		d.AddError("Author root folder required", "Configure at least one audiobook_root_folder_path or ebook_root_folder_path.")
		return authorAPI{}
	}
	p := authorAPI{}
	b, _ := json.Marshal(map[string]any{})
	_ = b
	raw := map[string]any{}
	overlayAuthorPayload(ctx, raw, m, d)
	encoded, _ := json.Marshal(raw)
	_ = json.Unmarshal(encoded, &p)
	return p
}
func overlayAuthorPayload(ctx context.Context, p map[string]any, m authorModel, d *diag.Diagnostics) {
	p["foreignAuthorId"] = strings.TrimSpace(m.ForeignAuthorID.ValueString())
	p["monitored"] = m.Monitored.ValueBool()
	p["audiobookMonitorExisting"] = m.AudiobookMonitorExisting.ValueInt64()
	p["audiobookMonitorFuture"] = m.AudiobookMonitorFuture.ValueBool()
	p["ebookMonitorExisting"] = m.EbookMonitorExisting.ValueInt64()
	p["ebookMonitorFuture"] = m.EbookMonitorFuture.ValueBool()
	setOptionalString(p, "audiobookRootFolderPath", m.AudiobookRootFolderPath)
	setOptionalString(p, "ebookRootFolderPath", m.EbookRootFolderPath)
	setOptionalInt(p, "audiobookQualityProfileId", m.AudiobookQualityProfileID)
	setOptionalInt(p, "ebookQualityProfileId", m.EbookQualityProfileID)
	setOptionalInt(p, "audiobookMetadataProfileId", m.AudiobookMetadataProfileID)
	setOptionalInt(p, "ebookMetadataProfileId", m.EbookMetadataProfileID)
	p["audiobookTags"] = setInt64Values(ctx, m.AudiobookTags, d)
	p["ebookTags"] = setInt64Values(ctx, m.EbookTags, d)
	p["addOptions"] = map[string]any{"searchForMissingBooks": m.SearchForMissingBooks.ValueBool()}
}
func setOptionalString(p map[string]any, k string, v types.String) {
	if v.IsNull() || v.IsUnknown() {
		p[k] = nil
	} else {
		p[k] = v.ValueString()
	}
}
func setOptionalInt(p map[string]any, k string, v types.Int64) {
	if v.IsNull() || v.IsUnknown() {
		p[k] = nil
	} else {
		p[k] = v.ValueInt64()
	}
}
func (r *authorResource) refresh(ctx context.Context, state *authorModel, target *tfsdk.State, d *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		d.AddError("Invalid author state", "The author has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/author/"+strconv.FormatInt(id, 10), "author", target, d)
	if !found || d.HasError() {
		return
	}
	var c authorAPI
	if json.Unmarshal(body, &c) != nil || c.ID < 1 || strings.TrimSpace(c.ForeignAuthorID) == "" {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid author document.")
		return
	}
	controls := []types.Bool{state.SearchForMissingBooks, state.MoveFilesOnUpdate, state.DeleteFilesOnDestroy, state.AddImportListExclusion}
	*state = authorModel{ID: types.StringValue(strconv.FormatInt(c.ID, 10)), ForeignAuthorID: types.StringValue(c.ForeignAuthorID), AuthorName: types.StringValue(c.AuthorName), Monitored: types.BoolValue(c.Monitored), AudiobookMonitorExisting: nullableInt(c.AudiobookMonitorExisting), AudiobookMonitorFuture: nullableBool(c.AudiobookMonitorFuture), EbookMonitorExisting: nullableInt(c.EbookMonitorExisting), EbookMonitorFuture: nullableBool(c.EbookMonitorFuture), AudiobookRootFolderPath: nullableString(c.AudiobookRootFolderPath), EbookRootFolderPath: nullableString(c.EbookRootFolderPath), AudiobookQualityProfileID: nullableInt(c.AudiobookQualityProfileID), EbookQualityProfileID: nullableInt(c.EbookQualityProfileID), AudiobookMetadataProfileID: nullableInt(c.AudiobookMetadataProfileID), EbookMetadataProfileID: nullableInt(c.EbookMetadataProfileID), AudiobookTags: setInt64State(ctx, c.AudiobookTags, d), EbookTags: setInt64State(ctx, c.EbookTags, d), SearchForMissingBooks: controls[0], MoveFilesOnUpdate: controls[1], DeleteFilesOnDestroy: controls[2], AddImportListExclusion: controls[3]}
	for _, value := range []*types.Int64{&state.AudiobookMonitorExisting, &state.EbookMonitorExisting} {
		if value.IsNull() || value.IsUnknown() {
			*value = types.Int64Value(0)
		}
	}
	for _, value := range []*types.Bool{&state.AudiobookMonitorFuture, &state.EbookMonitorFuture} {
		if value.IsNull() || value.IsUnknown() {
			*value = types.BoolValue(false)
		}
	}
	normalizeAuthorControls(state)
	d.Append(target.Set(ctx, state)...)
}

type selectedSeriesBookModel struct {
	ForeignBookID   types.String `tfsdk:"foreign_book_id"`
	ForeignAuthorID types.String `tfsdk:"foreign_author_id"`
}
type seriesResource struct{ client *client.Client }
type seriesModel struct {
	ID                types.String `tfsdk:"id"`
	ForeignSeriesID   types.String `tfsdk:"foreign_series_id"`
	Title             types.String `tfsdk:"title"`
	MediaType         types.String `tfsdk:"media_type"`
	SelectedBooks     types.Set    `tfsdk:"selected_books"`
	MonitorExisting   types.String `tfsdk:"monitor_existing"`
	MonitorFuture     types.Bool   `tfsdk:"monitor_future"`
	RootFolderPath    types.String `tfsdk:"root_folder_path"`
	QualityProfileID  types.Int64  `tfsdk:"quality_profile_id"`
	MetadataProfileID types.Int64  `tfsdk:"metadata_profile_id"`
	Tags              types.Set    `tfsdk:"tags"`
}
type seriesAPI struct {
	ID              int64  `json:"id"`
	ForeignSeriesID string `json:"foreignSeriesId"`
	Title           string `json:"title"`
	MediaType       string `json:"mediaType"`
}

func seriesBookType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"foreign_book_id": types.StringType, "foreign_author_id": types.StringType}}
}
func newSeriesResource() resource.Resource { return &seriesResource{} }
func (r *seriesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_series"
}
func (r *seriesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage safe Chaptarr series monitoring intent. Create/update may change monitoring only; refresh is GET-only. Destroy forgets Terraform ownership because Chaptarr exposes no general series-delete endpoint.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "foreign_series_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}, "title": schema.StringAttribute{Computed: true}, "media_type": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}},
		"selected_books": schema.SetNestedAttribute{Optional: true, Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"foreign_book_id": schema.StringAttribute{Required: true}, "foreign_author_id": schema.StringAttribute{Required: true}}}, MarkdownDescription: "Required for create/update. Import cannot reconstruct the original selected-book intent, so it begins empty."}, "monitor_existing": schema.StringAttribute{Optional: true, Computed: true, Validators: []validator.String{stringvalidator.OneOf("none", "all", "select")}}, "monitor_future": schema.BoolAttribute{Optional: true, Computed: true}, "root_folder_path": schema.StringAttribute{Optional: true, Computed: true}, "quality_profile_id": schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "metadata_profile_id": schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "tags": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type}}}
}
func (r *seriesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *seriesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *seriesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *seriesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *seriesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.AddWarning("Series retained in Chaptarr", "Chaptarr has no general series-delete endpoint. Terraform ownership was removed without deleting media, authors, books, or monitoring state.")
	}
}
func (r *seriesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "series", &resp.State, &resp.Diagnostics)
}
func (r *seriesResource) apply(ctx context.Context, m *seriesModel, target *tfsdk.State, d *diag.Diagnostics) {
	if m.ForeignSeriesID.IsNull() || m.ForeignSeriesID.IsUnknown() || !validProviderID(m.ForeignSeriesID.ValueString()) {
		d.AddAttributeError(path.Root("foreign_series_id"), "Provider-prefixed series identifier required", "Use lookup to select an explicit provider-prefixed identifier such as `hc:123`.")
		return
	}
	if m.MediaType.IsNull() || m.MediaType.IsUnknown() || m.MonitorExisting.IsNull() || m.MonitorExisting.IsUnknown() || m.MonitorFuture.IsNull() || m.MonitorFuture.IsUnknown() || m.RootFolderPath.IsNull() || m.RootFolderPath.IsUnknown() || m.QualityProfileID.IsNull() || m.QualityProfileID.IsUnknown() || m.MetadataProfileID.IsNull() || m.MetadataProfileID.IsUnknown() {
		d.AddError("Incomplete series intent", "Create and update require media_type, monitor_existing, monitor_future, root_folder_path, quality_profile_id, and metadata_profile_id. Imported series must be configured with these values before update.")
		return
	}
	var books []selectedSeriesBookModel
	d.Append(m.SelectedBooks.ElementsAs(ctx, &books, false)...)
	if len(books) == 0 {
		d.AddAttributeError(path.Root("selected_books"), "Selected books required", "Select at least one series book.")
		return
	}
	selected := make([]map[string]string, 0, len(books))
	for _, b := range books {
		bookID := strings.TrimSpace(b.ForeignBookID.ValueString())
		authorID := strings.TrimSpace(b.ForeignAuthorID.ValueString())
		if !validProviderID(bookID) || !validProviderID(authorID) {
			d.AddAttributeError(path.Root("selected_books"), "Provider-prefixed identifiers required", "Every selected book and author must use an explicit provider-prefixed identifier such as `hc:123`.")
			return
		}
		selected = append(selected, map[string]string{"foreignBookId": bookID, "foreignAuthorId": authorID})
	}
	payload := map[string]any{"foreignSeriesId": strings.TrimSpace(m.ForeignSeriesID.ValueString()), "selectedMediaType": m.MediaType.ValueString(), "selectedBooks": selected, "monitorExisting": m.MonitorExisting.ValueString(), "monitorFuture": m.MonitorFuture.ValueBool(), "tags": setInt64Values(ctx, m.Tags, d)}
	if m.MediaType.ValueString() == "audiobook" {
		payload["audiobookRootFolderPath"] = m.RootFolderPath.ValueString()
		payload["audiobookQualityProfileId"] = m.QualityProfileID.ValueInt64()
		payload["audiobookMetadataProfileId"] = m.MetadataProfileID.ValueInt64()
	} else {
		payload["ebookRootFolderPath"] = m.RootFolderPath.ValueString()
		payload["ebookQualityProfileId"] = m.QualityProfileID.ValueInt64()
		payload["ebookMetadataProfileId"] = m.MetadataProfileID.ValueInt64()
	}
	body, _ := json.Marshal(payload)
	response, err := r.client.Do(ctx, http.MethodPost, "/api/v1/series/add", body)
	if err != nil {
		d.AddError("Unable to apply series intent", libraryMutationError(err, "series", m.ForeignSeriesID.ValueString()))
		return
	}
	var result struct {
		Success      bool `json:"success"`
		AddedAuthors []struct {
			ID int64 `json:"id"`
		} `json:"addedAuthors"`
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(response.Body, &result) != nil || !result.Success {
		d.AddError("Invalid Chaptarr response", "Chaptarr did not confirm that the series monitoring intent was applied.")
		return
	}
	id, err := r.findSeries(ctx, m.ForeignSeriesID.ValueString(), result.AddedAuthors)
	if err != nil {
		d.AddError("Unable to resolve series", err.Error())
		return
	}
	m.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, m, target, d)
}
func (r *seriesResource) findSeries(ctx context.Context, foreign string, authors []struct {
	ID int64 `json:"id"`
}) (int64, error) {
	for _, a := range authors {
		if a.ID < 1 {
			continue
		}
		response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/series?authorId="+strconv.FormatInt(a.ID, 10), nil)
		if err != nil {
			return 0, err
		}
		var values []seriesAPI
		if json.Unmarshal(response.Body, &values) != nil {
			return 0, fmt.Errorf("chaptarr returned an invalid series list")
		}
		for _, v := range values {
			if strings.EqualFold(v.ForeignSeriesID, foreign) {
				return v.ID, nil
			}
		}
	}
	return 0, fmt.Errorf("chaptarr applied the request but did not return a local series matching %q", foreign)
}
func (r *seriesResource) refresh(ctx context.Context, state *seriesModel, target *tfsdk.State, d *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		d.AddError("Invalid series state", "The series has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/series/"+strconv.FormatInt(id, 10), "series", target, d)
	if !found || d.HasError() {
		return
	}
	var c seriesAPI
	if json.Unmarshal(body, &c) != nil || c.ID < 1 || strings.TrimSpace(c.ForeignSeriesID) == "" {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid series document.")
		return
	}
	state.ID = types.StringValue(strconv.FormatInt(c.ID, 10))
	state.ForeignSeriesID = types.StringValue(c.ForeignSeriesID)
	state.Title = types.StringValue(c.Title)
	if c.MediaType != "" {
		state.MediaType = types.StringValue(c.MediaType)
	}
	if state.SelectedBooks.IsNull() || state.SelectedBooks.IsUnknown() {
		state.SelectedBooks = types.SetValueMust(seriesBookType(), []attr.Value{})
	}
	if state.MonitorExisting.IsNull() || state.MonitorExisting.IsUnknown() {
		state.MonitorExisting = types.StringValue("none")
	}
	if state.MonitorFuture.IsNull() || state.MonitorFuture.IsUnknown() {
		state.MonitorFuture = types.BoolValue(false)
	}
	if state.RootFolderPath.IsUnknown() {
		state.RootFolderPath = types.StringNull()
	}
	if state.QualityProfileID.IsUnknown() {
		state.QualityProfileID = types.Int64Null()
	}
	if state.MetadataProfileID.IsUnknown() {
		state.MetadataProfileID = types.Int64Null()
	}
	if state.Tags.IsNull() || state.Tags.IsUnknown() {
		state.Tags = setInt64State(ctx, nil, d)
	}
	d.Append(target.Set(ctx, state)...)
}

type libraryLookupDataSource struct {
	client *client.Client
	kind   string
}
type authorLookupModel struct {
	ID         types.String `tfsdk:"id"`
	Term       types.String `tfsdk:"term"`
	ResultJSON types.String `tfsdk:"result_json"`
}
type seriesLookupModel struct {
	ID               types.String `tfsdk:"id"`
	ForeignSeriesID  types.String `tfsdk:"foreign_series_id"`
	MetadataProvider types.String `tfsdk:"metadata_provider"`
	ResultJSON       types.String `tfsdk:"result_json"`
}

func newAuthorLookupDataSource() datasource.DataSource {
	return &libraryLookupDataSource{kind: "author"}
}
func newSeriesLookupDataSource() datasource.DataSource {
	return &libraryLookupDataSource{kind: "series"}
}
func (d *libraryLookupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind + "_lookup"
}
func (d *libraryLookupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]dsschema.Attribute{"id": dsschema.StringAttribute{Computed: true}, "result_json": dsschema.StringAttribute{Computed: true, MarkdownDescription: "Canonical bounded lookup response; inspect provider-prefixed IDs before creating a resource."}}
	if d.kind == "author" {
		attrs["term"] = dsschema.StringAttribute{Required: true}
	} else {
		attrs["foreign_series_id"] = dsschema.StringAttribute{Required: true}
		attrs["metadata_provider"] = dsschema.StringAttribute{Optional: true, MarkdownDescription: "Optional upstream provider filter mapped to Chaptarr's provider query parameter."}
	}
	resp.Schema = dsschema.Schema{MarkdownDescription: "GET-only Chaptarr " + d.kind + " lookup.", Attributes: attrs}
}
func (d *libraryLookupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (d *libraryLookupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	values := url.Values{}
	if d.kind == "author" {
		var state authorLookupModel
		resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		values.Set("term", strings.TrimSpace(state.Term.ValueString()))
		if values.Get("term") == "" {
			resp.Diagnostics.AddAttributeError(path.Root("term"), "Lookup term required", "Set a non-empty author lookup term.")
			return
		}
		endpoint := "/api/v1/author/lookup?" + values.Encode()
		response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			resp.Diagnostics.AddError("Unable to lookup author", err.Error())
			return
		}
		decoded, err := jsonDecode(response)
		if err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", err.Error())
			return
		}
		state.ID = types.StringValue(fingerprintID("/api/v1/author/lookup", values.Encode()))
		state.ResultJSON = types.StringValue(decoded["result_json"].(string))
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	} else {
		var state seriesLookupModel
		resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		values.Set("foreignSeriesId", strings.TrimSpace(state.ForeignSeriesID.ValueString()))
		if !validProviderID(values.Get("foreignSeriesId")) {
			resp.Diagnostics.AddAttributeError(path.Root("foreign_series_id"), "Provider-prefixed series identifier required", "Use an explicit provider-prefixed identifier such as `hc:123`.")
			return
		}
		if !state.MetadataProvider.IsNull() {
			values.Set("provider", strings.TrimSpace(state.MetadataProvider.ValueString()))
		}
		endpoint := "/api/v1/series/lookup?" + values.Encode()
		response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			resp.Diagnostics.AddError("Unable to lookup series", err.Error())
			return
		}
		decoded, err := jsonDecode(response)
		if err != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", err.Error())
			return
		}
		state.ID = types.StringValue(fingerprintID("/api/v1/series/lookup", values.Encode()))
		state.ResultJSON = types.StringValue(decoded["result_json"].(string))
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}
func validProviderID(value string) bool {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
func libraryMutationError(err error, kind, foreign string) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 409 {
		return "Chaptarr reported an existing " + kind + " or provider ambiguity for " + foreign + ". Import the unique local numeric identifier, or use lookup and choose an explicit provider-prefixed ID."
	}
	return err.Error()
}
