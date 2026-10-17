package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
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
	ID                            types.String `tfsdk:"id"`
	ForeignSeriesID               types.String `tfsdk:"foreign_series_id"`
	Title                         types.String `tfsdk:"title"`
	MediaType                     types.String `tfsdk:"media_type"`
	SelectedBooks                 types.Set    `tfsdk:"selected_books"`
	MonitorExisting               types.String `tfsdk:"monitor_existing"`
	MonitorFuture                 types.Bool   `tfsdk:"monitor_future"`
	RootFolderPath                types.String `tfsdk:"root_folder_path"`
	QualityProfileID              types.Int64  `tfsdk:"quality_profile_id"`
	MetadataProfileID             types.Int64  `tfsdk:"metadata_profile_id"`
	Tags                          types.Set    `tfsdk:"tags"`
	PreferredNarrator             types.String `tfsdk:"preferred_narrator"`
	ApplyNarratorToBooks          types.Bool   `tfsdk:"apply_narrator_to_books"`
	OverrideExistingBookNarrators types.Bool   `tfsdk:"override_existing_book_narrators"`
	NarratorAnalysis              types.Object `tfsdk:"narrator_analysis"`
	NarratorInheritance           types.Object `tfsdk:"narrator_inheritance"`
	Variants                      types.List   `tfsdk:"variants"`
	RemoveVariantIDs              types.Set    `tfsdk:"remove_variant_ids"`
}
type seriesAPI struct {
	ID               int64  `json:"id"`
	ForeignSeriesID  string `json:"foreignSeriesId"`
	LocalSeriesID    string `json:"localSeriesId"`
	Title            string `json:"title"`
	WorkCount        int64  `json:"workCount"`
	PrimaryWorkCount int64  `json:"primaryWorkCount"`
	MediaType        string `json:"mediaType"`
	Narrator         string `json:"narrator"`
}
type seriesNarratorIntent struct {
	managed  bool
	narrator string
	apply    bool
	override bool
}
type narratorAnalysisModel struct {
	SeriesID                   types.Int64  `tfsdk:"series_id"`
	SeriesTitle                types.String `tfsdk:"series_title"`
	PreferredNarrator          types.String `tfsdk:"preferred_narrator"`
	Narrators                  types.List   `tfsdk:"narrators"`
	SequentialOverlapNarrators types.Set    `tfsdk:"sequential_overlap_narrators"`
	BooksWithoutNarratorCount  types.Int64  `tfsdk:"books_without_narrator_count"`
	TotalBooksInSeries         types.Int64  `tfsdk:"total_books_in_series"`
}
type narratorAnalysisItemModel struct {
	Narrator             types.String  `tfsdk:"narrator"`
	BookCount            types.Int64   `tfsdk:"book_count"`
	Percentage           types.Float64 `tfsdk:"percentage"`
	HasSequentialOverlap types.Bool    `tfsdk:"has_sequential_overlap"`
	IsMainNarrator       types.Bool    `tfsdk:"is_main_narrator"`
	IsSporadicNarrator   types.Bool    `tfsdk:"is_sporadic_narrator"`
}
type narratorInheritanceModel struct {
	SeriesID                types.Int64  `tfsdk:"series_id"`
	SeriesTitle             types.String `tfsdk:"series_title"`
	PreferredSeriesNarrator types.String `tfsdk:"preferred_series_narrator"`
	CanApplyInheritance     types.Bool   `tfsdk:"can_apply_inheritance"`
	BooksWithoutNarrator    types.List   `tfsdk:"books_without_narrator"`
}
type inheritanceBookModel struct {
	BookID               types.Int64  `tfsdk:"book_id"`
	BookTitle            types.String `tfsdk:"book_title"`
	CurrentNarrator      types.String `tfsdk:"current_narrator"`
	CanInheritFromSeries types.Bool   `tfsdk:"can_inherit_from_series"`
}
type seriesVariantModel struct {
	ID               types.Int64  `tfsdk:"id"`
	ForeignSeriesID  types.String `tfsdk:"foreign_series_id"`
	LocalSeriesID    types.String `tfsdk:"local_series_id"`
	Title            types.String `tfsdk:"title"`
	MediaType        types.String `tfsdk:"media_type"`
	Narrator         types.String `tfsdk:"narrator"`
	WorkCount        types.Int64  `tfsdk:"work_count"`
	PrimaryWorkCount types.Int64  `tfsdk:"primary_work_count"`
}
type seriesNarratorAnalysisAPI struct {
	SeriesID                   int64                     `json:"seriesId"`
	SeriesTitle                string                    `json:"seriesTitle"`
	PreferredNarrator          string                    `json:"preferredNarrator"`
	NarratorAnalysis           []narratorAnalysisItemAPI `json:"narratorAnalysis"`
	SequentialOverlapNarrators []string                  `json:"sequentialOverlapNarrators"`
	BooksWithoutNarratorCount  int64                     `json:"booksWithoutNarratorCount"`
	TotalBooksInSeries         int64                     `json:"totalBooksInSeries"`
}
type narratorAnalysisItemAPI struct {
	Narrator             string  `json:"narrator"`
	BookCount            int64   `json:"bookCount"`
	Percentage           float64 `json:"percentage"`
	HasSequentialOverlap bool    `json:"hasSequentialOverlap"`
	IsMainNarrator       bool    `json:"isMainNarrator"`
	IsSporadicNarrator   bool    `json:"isSporadicNarrator"`
}
type seriesNarratorInheritanceAPI struct {
	SeriesID                int64                `json:"seriesId"`
	SeriesTitle             string               `json:"seriesTitle"`
	PreferredSeriesNarrator string               `json:"preferredSeriesNarrator"`
	BooksWithoutNarrator    []inheritanceBookAPI `json:"booksWithoutNarrator"`
	CanApplyInheritance     bool                 `json:"canApplyInheritance"`
}
type inheritanceBookAPI struct {
	BookID               int64  `json:"bookId"`
	BookTitle            string `json:"bookTitle"`
	CurrentNarrator      string `json:"currentNarrator"`
	CanInheritFromSeries bool   `json:"canInheritFromSeries"`
}

func seriesBookType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"foreign_book_id": types.StringType, "foreign_author_id": types.StringType}}
}
func newSeriesResource() resource.Resource { return &seriesResource{} }
func (r *seriesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_series"
}
func (r *seriesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage safe Chaptarr series monitoring intent, an optional preferred narrator, and explicit narrator-variant removal. Refresh only GETs the series, narrator analysis, narrator inheritance, and variants. Discover and search are never called. Destroy forgets Terraform ownership because Chaptarr exposes no general series-delete endpoint.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "foreign_series_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}, "title": schema.StringAttribute{Computed: true}, "media_type": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}},
		"selected_books": schema.SetNestedAttribute{Optional: true, Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"foreign_book_id": schema.StringAttribute{Required: true}, "foreign_author_id": schema.StringAttribute{Required: true}}}, MarkdownDescription: "Required for create/update. Import cannot reconstruct the original selected-book intent, so it begins empty."}, "monitor_existing": schema.StringAttribute{Optional: true, Computed: true, Validators: []validator.String{stringvalidator.OneOf("none", "all", "select")}}, "monitor_future": schema.BoolAttribute{Optional: true, Computed: true}, "root_folder_path": schema.StringAttribute{Optional: true, Computed: true}, "quality_profile_id": schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "metadata_profile_id": schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "tags": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type},
		"preferred_narrator":               schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Narrator stored on the series. Set it to call PUT /narrators/preferred during create and update. Omit it to observe the server value without changing it. Refresh decodes this from the series document."},
		"apply_narrator_to_books":          schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "When true and preferred_narrator is set, create/update also applies that narrator to books in the series. Defaults to false and never runs during refresh or destroy."},
		"override_existing_book_narrators": schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "When true, apply_narrator_to_books replaces narrators that books already have. Requires apply_narrator_to_books. Defaults to false."},
		"narrator_analysis":                schema.SingleNestedAttribute{Computed: true, Attributes: narratorAnalysisAttributes()},
		"narrator_inheritance":             schema.SingleNestedAttribute{Computed: true, Attributes: narratorInheritanceAttributes()},
		"variants":                         schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: seriesVariantAttributes()}, MarkdownDescription: "Narrator variants returned for this series. Images, links, descriptions, and book payloads are omitted."},
		"remove_variant_ids":               schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type, Validators: []validator.Set{setvalidator.ValueInt64sAre(int64validator.AtLeast(1))}, MarkdownDescription: "Variant IDs to delete during create/update. Empty deletes nothing. IDs that are not current variants of this series are left untouched. The managed series ID is rejected. Already-absent variants are success. Destroy does not delete variants."},
	}}
}
func (r *seriesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *seriesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan seriesModel
	var config seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	normalizeSeriesControls(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, config, &resp.State, &resp.Diagnostics)
}
func (r *seriesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	normalizeSeriesControls(ctx, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
}
func (r *seriesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan seriesModel
	var config seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	normalizeSeriesControls(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, config, &resp.State, &resp.Diagnostics)
}
func (r *seriesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.AddWarning("Series retained in Chaptarr", "Chaptarr has no general series-delete endpoint. Terraform ownership was removed without deleting the series, narrator variants, authors, books, files, or monitoring state.")
	}
}
func (r *seriesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "series", &resp.State, &resp.Diagnostics)
}
func (r *seriesResource) apply(ctx context.Context, m *seriesModel, config seriesModel, target *tfsdk.State, d *diag.Diagnostics) {
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
	intent := seriesNarratorIntentFromConfig(config, d)
	variantIDs := seriesVariantIDs(ctx, m.RemoveVariantIDs, d)
	if d.HasError() {
		return
	}
	if existingID, ok := positiveModelID(m.ID); ok && variantIDListContains(variantIDs, existingID) {
		d.AddAttributeError(path.Root("remove_variant_ids"), "Managed series cannot be deleted", "remove_variant_ids must not include the managed series ID. Chaptarr has no general series-delete endpoint, and this provider will not delete the series it owns.")
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
	if variantIDListContains(variantIDs, id) {
		d.AddAttributeError(path.Root("remove_variant_ids"), "Managed series cannot be deleted", "remove_variant_ids must not include the managed series ID. Chaptarr has no general series-delete endpoint, and this provider will not delete the series it owns.")
		return
	}
	m.ID = types.StringValue(strconv.FormatInt(id, 10))
	if intent.managed && !r.setPreferredNarrator(ctx, id, intent, d) {
		return
	}
	if !r.removeSeriesVariants(ctx, id, variantIDs, d) {
		return
	}
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
	state.PreferredNarrator = trimmedString(c.Narrator)
	if !r.loadSeriesNarratorState(ctx, c.ID, state, d) {
		return
	}
	normalizeSeriesControls(ctx, state, d)
	if d.HasError() {
		return
	}
	d.Append(target.Set(ctx, state)...)
}

func seriesNarratorIntentFromConfig(config seriesModel, d *diag.Diagnostics) seriesNarratorIntent {
	intent := seriesNarratorIntent{apply: configuredBool(config.ApplyNarratorToBooks, "apply_narrator_to_books", d), override: configuredBool(config.OverrideExistingBookNarrators, "override_existing_book_narrators", d)}
	if config.PreferredNarrator.IsUnknown() {
		d.AddAttributeError(path.Root("preferred_narrator"), "Preferred narrator must be known", "preferred_narrator must be known during apply. Omit it to leave the series narrator unchanged.")
		return intent
	}
	if !config.PreferredNarrator.IsNull() {
		intent.narrator = strings.TrimSpace(config.PreferredNarrator.ValueString())
		if intent.narrator == "" {
			d.AddAttributeError(path.Root("preferred_narrator"), "Preferred narrator required", "Set a non-empty narrator name, or omit preferred_narrator to leave the series narrator unchanged.")
			return intent
		}
		intent.managed = true
	}
	if (intent.apply || intent.override) && !intent.managed {
		d.AddAttributeError(path.Root("preferred_narrator"), "Preferred narrator required", "apply_narrator_to_books and override_existing_book_narrators require preferred_narrator. They default to false and do not change book narrators on their own.")
	}
	if intent.override && !intent.apply {
		d.AddAttributeError(path.Root("override_existing_book_narrators"), "Book narrator override requires apply", "Set apply_narrator_to_books to true before override_existing_book_narrators can replace narrators already stored on books.")
	}
	return intent
}
func configuredBool(value types.Bool, name string, d *diag.Diagnostics) bool {
	if value.IsUnknown() {
		d.AddAttributeError(path.Root(name), "Value must be known", name+" must be known during apply.")
		return false
	}
	if value.IsNull() {
		return false
	}
	return value.ValueBool()
}
func normalizeSeriesControls(ctx context.Context, m *seriesModel, d *diag.Diagnostics) {
	for _, value := range []*types.Bool{&m.ApplyNarratorToBooks, &m.OverrideExistingBookNarrators} {
		if value.IsNull() || value.IsUnknown() {
			*value = types.BoolValue(false)
		}
	}
	if m.RemoveVariantIDs.IsNull() || m.RemoveVariantIDs.IsUnknown() {
		m.RemoveVariantIDs = setInt64State(ctx, nil, d)
	}
}
func seriesVariantIDs(ctx context.Context, value types.Set, d *diag.Diagnostics) []int64 {
	ids := setInt64Values(ctx, value, d)
	if d.HasError() {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	seen := make(map[int64]struct{}, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id < 1 {
			d.AddAttributeError(path.Root("remove_variant_ids"), "Invalid series variant ID", "Variant IDs must be positive Chaptarr series identifiers.")
			return nil
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
func variantIDListContains(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
func (r *seriesResource) setPreferredNarrator(ctx context.Context, seriesID int64, intent seriesNarratorIntent, d *diag.Diagnostics) bool {
	body, err := json.Marshal(map[string]any{"narrator": intent.narrator, "applyToAllBooks": intent.apply, "overrideExisting": intent.override})
	if err != nil {
		d.AddError("Unable to encode series narrator", "The preferred narrator request could not be encoded.")
		return false
	}
	response, err := r.client.Do(ctx, http.MethodPut, seriesPath(seriesID)+"/narrators/preferred", body)
	if err != nil {
		d.AddAttributeError(path.Root("preferred_narrator"), "Unable to set preferred series narrator", err.Error())
		return false
	}
	var result struct {
		Message        string `json:"message"`
		AppliedToBooks bool   `json:"appliedToBooks"`
	}
	if json.Unmarshal(response.Body, &result) != nil || result.AppliedToBooks != intent.apply {
		d.AddError("Invalid Chaptarr response", "Chaptarr did not confirm whether the preferred narrator was applied to books.")
		return false
	}
	return true
}
func (r *seriesResource) removeSeriesVariants(ctx context.Context, seriesID int64, ids []int64, d *diag.Diagnostics) bool {
	if len(ids) == 0 {
		return true
	}
	body, ok := readSeriesSubresource(ctx, r.client, seriesPath(seriesID)+"/variants", "series variants", d)
	if !ok {
		return false
	}
	present, ok := seriesVariantIDsFromBody(body, d)
	if !ok {
		return false
	}
	for _, variantID := range ids {
		if _, exists := present[variantID]; !exists {
			continue
		}
		_, err := r.client.Do(ctx, http.MethodDelete, seriesPath(seriesID)+"/variants/"+strconv.FormatInt(variantID, 10), nil)
		if err == nil {
			continue
		}
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			continue
		}
		d.AddAttributeError(path.Root("remove_variant_ids"), "Unable to delete series variant", err.Error())
		return false
	}
	return true
}
func (r *seriesResource) loadSeriesNarratorState(ctx context.Context, seriesID int64, state *seriesModel, d *diag.Diagnostics) bool {
	base := seriesPath(seriesID)
	analysisBody, analysisFound := readSeriesLookupSubresource(ctx, r.client, base+"/narrators", "series narrators", d)
	if d.HasError() {
		return false
	}
	analysisFound = analysisFound && !isJSONNull(analysisBody)
	inheritanceBody, inheritanceFound := readSeriesLookupSubresource(ctx, r.client, base+"/narrators/inheritance", "series narrator inheritance", d)
	if d.HasError() {
		return false
	}
	inheritanceFound = inheritanceFound && !isJSONNull(inheritanceBody)
	variantsBody, variantsFound := readSeriesLookupSubresource(ctx, r.client, base+"/variants", "series variants", d)
	if d.HasError() {
		return false
	}
	variantsFound = variantsFound && !isJSONNull(variantsBody)
	if analysisFound {
		analysis, ok := narratorAnalysisState(ctx, seriesID, analysisBody, d)
		if !ok {
			return false
		}
		state.NarratorAnalysis = analysis
	} else {
		state.NarratorAnalysis = types.ObjectNull(narratorAnalysisAttrTypes())
	}
	if inheritanceFound {
		inheritance, ok := narratorInheritanceState(ctx, seriesID, inheritanceBody, d)
		if !ok {
			return false
		}
		state.NarratorInheritance = inheritance
	} else {
		state.NarratorInheritance = types.ObjectNull(narratorInheritanceAttrTypes())
	}
	if variantsFound {
		variants, ok := seriesVariantsState(ctx, variantsBody, d)
		if !ok {
			return false
		}
		state.Variants = variants
	} else {
		state.Variants = types.ListValueMust(seriesVariantType(), []attr.Value{})
	}
	return true
}
func narratorAnalysisState(ctx context.Context, seriesID int64, body []byte, d *diag.Diagnostics) (types.Object, bool) {
	var payload seriesNarratorAnalysisAPI
	if json.Unmarshal(body, &payload) != nil || (payload.SeriesID != 0 && payload.SeriesID != seriesID) {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned invalid series narrator analysis.")
		return types.ObjectNull(narratorAnalysisAttrTypes()), false
	}
	if payload.SeriesID == 0 {
		payload.SeriesID = seriesID
	}
	items := make([]narratorAnalysisItemModel, 0, len(payload.NarratorAnalysis))
	for _, item := range payload.NarratorAnalysis {
		items = append(items, narratorAnalysisItemModel{Narrator: trimmedString(item.Narrator), BookCount: types.Int64Value(item.BookCount), Percentage: types.Float64Value(item.Percentage), HasSequentialOverlap: types.BoolValue(item.HasSequentialOverlap), IsMainNarrator: types.BoolValue(item.IsMainNarrator), IsSporadicNarrator: types.BoolValue(item.IsSporadicNarrator)})
	}
	model := narratorAnalysisModel{SeriesID: types.Int64Value(payload.SeriesID), SeriesTitle: trimmedString(payload.SeriesTitle), PreferredNarrator: trimmedString(payload.PreferredNarrator), Narrators: listObjectState(ctx, narratorAnalysisItemType(), items, d), SequentialOverlapNarrators: setStringState(ctx, payload.SequentialOverlapNarrators, d), BooksWithoutNarratorCount: types.Int64Value(payload.BooksWithoutNarratorCount), TotalBooksInSeries: types.Int64Value(payload.TotalBooksInSeries)}
	if d.HasError() {
		return types.ObjectNull(narratorAnalysisAttrTypes()), false
	}
	return objectState(ctx, narratorAnalysisAttrTypes(), model, d)
}
func narratorInheritanceState(ctx context.Context, seriesID int64, body []byte, d *diag.Diagnostics) (types.Object, bool) {
	var payload seriesNarratorInheritanceAPI
	if json.Unmarshal(body, &payload) != nil || (payload.SeriesID != 0 && payload.SeriesID != seriesID) {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned invalid series narrator inheritance.")
		return types.ObjectNull(narratorInheritanceAttrTypes()), false
	}
	if payload.SeriesID == 0 {
		payload.SeriesID = seriesID
	}
	books := make([]inheritanceBookModel, 0, len(payload.BooksWithoutNarrator))
	for _, book := range payload.BooksWithoutNarrator {
		if book.BookID < 1 {
			d.AddError("Invalid Chaptarr response", "Chaptarr returned narrator inheritance for a book without a positive id.")
			return types.ObjectNull(narratorInheritanceAttrTypes()), false
		}
		books = append(books, inheritanceBookModel{BookID: types.Int64Value(book.BookID), BookTitle: trimmedString(book.BookTitle), CurrentNarrator: trimmedString(book.CurrentNarrator), CanInheritFromSeries: types.BoolValue(book.CanInheritFromSeries)})
	}
	model := narratorInheritanceModel{SeriesID: types.Int64Value(payload.SeriesID), SeriesTitle: trimmedString(payload.SeriesTitle), PreferredSeriesNarrator: trimmedString(payload.PreferredSeriesNarrator), CanApplyInheritance: types.BoolValue(payload.CanApplyInheritance), BooksWithoutNarrator: listObjectState(ctx, inheritanceBookType(), books, d)}
	if d.HasError() {
		return types.ObjectNull(narratorInheritanceAttrTypes()), false
	}
	return objectState(ctx, narratorInheritanceAttrTypes(), model, d)
}
func seriesVariantsState(ctx context.Context, body []byte, d *diag.Diagnostics) (types.List, bool) {
	var payload []seriesAPI
	if json.Unmarshal(body, &payload) != nil {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid series variant list.")
		return types.ListNull(seriesVariantType()), false
	}
	variants := make([]seriesVariantModel, 0, len(payload))
	for _, variant := range payload {
		if variant.ID < 1 {
			d.AddError("Invalid Chaptarr response", "Chaptarr returned a series variant without a positive id.")
			return types.ListNull(seriesVariantType()), false
		}
		variants = append(variants, seriesVariantFromAPI(variant))
	}
	return listObjectState(ctx, seriesVariantType(), variants, d), !d.HasError()
}
func seriesVariantIDsFromBody(body []byte, d *diag.Diagnostics) (map[int64]struct{}, bool) {
	var payload []seriesAPI
	if json.Unmarshal(body, &payload) != nil {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid series variant list.")
		return nil, false
	}
	ids := make(map[int64]struct{}, len(payload))
	for _, variant := range payload {
		if variant.ID < 1 {
			d.AddError("Invalid Chaptarr response", "Chaptarr returned a series variant without a positive id.")
			return nil, false
		}
		ids[variant.ID] = struct{}{}
	}
	return ids, true
}
func seriesVariantFromAPI(variant seriesAPI) seriesVariantModel {
	return seriesVariantModel{ID: types.Int64Value(variant.ID), ForeignSeriesID: trimmedString(variant.ForeignSeriesID), LocalSeriesID: trimmedString(variant.LocalSeriesID), Title: trimmedString(variant.Title), MediaType: trimmedString(variant.MediaType), Narrator: trimmedString(variant.Narrator), WorkCount: types.Int64Value(variant.WorkCount), PrimaryWorkCount: types.Int64Value(variant.PrimaryWorkCount)}
}
func narratorAnalysisAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"series_id": schema.Int64Attribute{Computed: true}, "series_title": schema.StringAttribute{Computed: true}, "preferred_narrator": schema.StringAttribute{Computed: true, MarkdownDescription: "Preferred narrator reported by narrator analysis. The managed preferred_narrator value is decoded from the series document."},
		"narrators":                    schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"narrator": schema.StringAttribute{Computed: true}, "book_count": schema.Int64Attribute{Computed: true}, "percentage": schema.Float64Attribute{Computed: true}, "has_sequential_overlap": schema.BoolAttribute{Computed: true}, "is_main_narrator": schema.BoolAttribute{Computed: true}, "is_sporadic_narrator": schema.BoolAttribute{Computed: true}}}},
		"sequential_overlap_narrators": schema.SetAttribute{Computed: true, ElementType: types.StringType}, "books_without_narrator_count": schema.Int64Attribute{Computed: true}, "total_books_in_series": schema.Int64Attribute{Computed: true},
	}
}
func narratorInheritanceAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"series_id": schema.Int64Attribute{Computed: true}, "series_title": schema.StringAttribute{Computed: true}, "preferred_series_narrator": schema.StringAttribute{Computed: true}, "can_apply_inheritance": schema.BoolAttribute{Computed: true},
		"books_without_narrator": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"book_id": schema.Int64Attribute{Computed: true}, "book_title": schema.StringAttribute{Computed: true}, "current_narrator": schema.StringAttribute{Computed: true}, "can_inherit_from_series": schema.BoolAttribute{Computed: true}}}},
	}
}
func seriesVariantAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{"id": schema.Int64Attribute{Computed: true}, "foreign_series_id": schema.StringAttribute{Computed: true}, "local_series_id": schema.StringAttribute{Computed: true}, "title": schema.StringAttribute{Computed: true}, "media_type": schema.StringAttribute{Computed: true}, "narrator": schema.StringAttribute{Computed: true}, "work_count": schema.Int64Attribute{Computed: true}, "primary_work_count": schema.Int64Attribute{Computed: true}}
}
func narratorAnalysisItemType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"narrator": types.StringType, "book_count": types.Int64Type, "percentage": types.Float64Type, "has_sequential_overlap": types.BoolType, "is_main_narrator": types.BoolType, "is_sporadic_narrator": types.BoolType}}
}
func narratorAnalysisAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"series_id": types.Int64Type, "series_title": types.StringType, "preferred_narrator": types.StringType, "narrators": types.ListType{ElemType: narratorAnalysisItemType()}, "sequential_overlap_narrators": types.SetType{ElemType: types.StringType}, "books_without_narrator_count": types.Int64Type, "total_books_in_series": types.Int64Type}
}
func inheritanceBookType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"book_id": types.Int64Type, "book_title": types.StringType, "current_narrator": types.StringType, "can_inherit_from_series": types.BoolType}}
}
func narratorInheritanceAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"series_id": types.Int64Type, "series_title": types.StringType, "preferred_series_narrator": types.StringType, "can_apply_inheritance": types.BoolType, "books_without_narrator": types.ListType{ElemType: inheritanceBookType()}}
}
func seriesVariantType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "foreign_series_id": types.StringType, "local_series_id": types.StringType, "title": types.StringType, "media_type": types.StringType, "narrator": types.StringType, "work_count": types.Int64Type, "primary_work_count": types.Int64Type}}
}
func objectState(ctx context.Context, attrTypes map[string]attr.Type, value any, d *diag.Diagnostics) (types.Object, bool) {
	result, converted := types.ObjectValueFrom(ctx, attrTypes, value)
	d.Append(converted...)
	return result, !d.HasError()
}
func seriesPath(seriesID int64) string {
	return "/api/v1/series/" + strconv.FormatInt(seriesID, 10)
}
func readSeriesSubresource(ctx context.Context, apiClient *client.Client, endpoint, label string, d *diag.Diagnostics) ([]byte, bool) {
	response, err := apiClient.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		d.AddError("Unable to read "+label, err.Error())
		return nil, false
	}
	return response.Body, true
}
func readSeriesLookupSubresource(ctx context.Context, apiClient *client.Client, endpoint, label string, d *diag.Diagnostics) ([]byte, bool) {
	response, err := apiClient.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		if seriesLookupNotFound(err) {
			return nil, false
		}
		d.AddError("Unable to read "+label, err.Error())
		return nil, false
	}
	return response.Body, true
}
func seriesLookupNotFound(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
func isJSONNull(body []byte) bool {
	return bytes.Equal(bytes.TrimSpace(body), []byte("null"))
}
func trimmedString(value string) types.String {
	value = strings.TrimSpace(value)
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
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
