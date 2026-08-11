package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &bookResource{}
var _ resource.ResourceWithImportState = &bookResource{}

type bookResource struct{ client *client.Client }
type bookModel struct {
	ID                        types.String `tfsdk:"id"`
	LookupJSON                types.String `tfsdk:"lookup_json"`
	ForeignBookID             types.String `tfsdk:"foreign_book_id"`
	AuthorID                  types.Int64  `tfsdk:"author_id"`
	Title                     types.String `tfsdk:"title"`
	MediaType                 types.String `tfsdk:"media_type"`
	Monitored                 types.Bool   `tfsdk:"monitored"`
	AnyEditionOK              types.Bool   `tfsdk:"any_edition_ok"`
	MonitoredEditionID        types.String `tfsdk:"monitored_edition_id"`
	Narrator                  types.String `tfsdk:"narrator"`
	NarratorNames             types.Set    `tfsdk:"narrator_names"`
	Editions                  types.List   `tfsdk:"editions"`
	SearchForNewBook          types.Bool   `tfsdk:"search_for_new_book"`
	DeleteFilesOnDestroy      types.Bool   `tfsdk:"delete_files_on_destroy"`
	AddImportListExclusion    types.Bool   `tfsdk:"add_import_list_exclusion_on_destroy"`
	ApplyDestroyToBothFormats types.Bool   `tfsdk:"apply_destroy_to_both_formats"`
}
type bookEditionModel struct {
	ID               types.Int64  `tfsdk:"id"`
	ForeignEditionID types.String `tfsdk:"foreign_edition_id"`
	Title            types.String `tfsdk:"title"`
	Format           types.String `tfsdk:"format"`
	Monitored        types.Bool   `tfsdk:"monitored"`
	Narrator         types.String `tfsdk:"narrator"`
	NarratorNames    types.Set    `tfsdk:"narrator_names"`
	DurationSeconds  types.Int64  `tfsdk:"duration_seconds"`
}
type bookAPI struct {
	ID            int64            `json:"id"`
	ForeignBookID string           `json:"foreignBookId"`
	AuthorID      int64            `json:"authorId"`
	Title         string           `json:"title"`
	MediaType     string           `json:"mediaType"`
	Monitored     bool             `json:"monitored"`
	AnyEditionOK  bool             `json:"anyEditionOk"`
	Narrator      *string          `json:"narrator"`
	NarratorNames []string         `json:"narratorNames"`
	Editions      []bookEditionAPI `json:"editions"`
}
type bookEditionAPI struct {
	ID               int64    `json:"id"`
	ForeignEditionID string   `json:"foreignEditionId"`
	Title            string   `json:"title"`
	Format           string   `json:"format"`
	Monitored        bool     `json:"monitored"`
	Narrator         *string  `json:"narrator"`
	NarratorNames    []string `json:"narratorNames"`
	DurationSeconds  *int64   `json:"durationSeconds"`
}

func newBookResource() resource.Resource { return &bookResource{} }
func (r *bookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_book"
}
func bookEditionType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "foreign_edition_id": types.StringType, "title": types.StringType, "format": types.StringType, "monitored": types.BoolType, "narrator": types.StringType, "narrator_names": types.SetType{ElemType: types.StringType}, "duration_seconds": types.Int64Type}}
}
func (r *bookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage one Chaptarr book and its monitored edition without invoking download, rename, retag, or file-move actions.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"lookup_json":     schema.StringAttribute{Optional: true, WriteOnly: true, MarkdownDescription: "Apply-only single BookResource candidate selected from chaptarr_book_lookup. Required for create; never stored in state."},
		"foreign_book_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"author_id":       schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(1)}, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"title":           schema.StringAttribute{Computed: true}, "media_type": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"monitored": schema.BoolAttribute{Required: true}, "any_edition_ok": schema.BoolAttribute{Required: true}, "monitored_edition_id": schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Provider-prefixed foreign edition identity to select. Required when any_edition_ok is false."},
		"narrator": schema.StringAttribute{Computed: true}, "narrator_names": schema.SetAttribute{Computed: true, ElementType: types.StringType},
		"editions":            schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"id": schema.Int64Attribute{Computed: true}, "foreign_edition_id": schema.StringAttribute{Computed: true}, "title": schema.StringAttribute{Computed: true}, "format": schema.StringAttribute{Computed: true}, "monitored": schema.BoolAttribute{Computed: true}, "narrator": schema.StringAttribute{Computed: true}, "narrator_names": schema.SetAttribute{Computed: true, ElementType: types.StringType}, "duration_seconds": schema.Int64Attribute{Computed: true}}}},
		"search_for_new_book": schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Explicitly permit a search during create. Defaults to false."}, "delete_files_on_destroy": schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Explicitly delete media files during destroy. Defaults to false."}, "add_import_list_exclusion_on_destroy": schema.BoolAttribute{Optional: true, Computed: true}, "apply_destroy_to_both_formats": schema.BoolAttribute{Optional: true, Computed: true}}}
}
func (r *bookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func normalizeBookControls(m *bookModel) {
	for _, v := range []*types.Bool{&m.SearchForNewBook, &m.DeleteFilesOnDestroy, &m.AddImportListExclusion, &m.ApplyDestroyToBothFormats} {
		if v.IsNull() || v.IsUnknown() {
			*v = types.BoolValue(false)
		}
	}
}
func (r *bookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("lookup_json"), &plan.LookupJSON)...)
	normalizeBookControls(&plan)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := bookCreatePayload(plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/book?mediaType="+url.QueryEscape(plan.MediaType.ValueString()), payload, "book", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	plan.LookupJSON = types.StringNull()
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *bookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	normalizeBookControls(&state)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *bookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan bookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	normalizeBookControls(&plan)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/book/"+strconv.FormatInt(id, 10), "book", &resp.State, &resp.Diagnostics)
	if !found || resp.Diagnostics.HasError() {
		return
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid book document.")
		return
	}
	overlayBookIntent(payload, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/book/"+strconv.FormatInt(id, 10), payload, "book", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}
func (r *bookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	normalizeBookControls(&state)
	id, ok := positiveModelID(state.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	values := url.Values{"deleteFiles": []string{strconv.FormatBool(state.DeleteFilesOnDestroy.ValueBool())}, "addImportListExclusion": []string{strconv.FormatBool(state.AddImportListExclusion.ValueBool())}, "applyToBothFormats": []string{strconv.FormatBool(state.ApplyDestroyToBothFormats.ValueBool())}}
	_, err := r.client.Do(ctx, http.MethodDelete, "/api/v1/book/"+strconv.FormatInt(id, 10)+"?"+values.Encode(), nil)
	if err != nil {
		var apiErr *client.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			resp.Diagnostics.AddError("Unable to delete book", err.Error())
		}
	}
}
func (r *bookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "book", &resp.State, &resp.Diagnostics)
}
func bookCreatePayload(m bookModel, d *diag.Diagnostics) map[string]any {
	if m.LookupJSON.IsNull() || m.LookupJSON.IsUnknown() || strings.TrimSpace(m.LookupJSON.ValueString()) == "" {
		d.AddAttributeError(path.Root("lookup_json"), "Book lookup candidate required", "Select one complete BookResource object from chaptarr_book_lookup and pass it as lookup_json for create.")
		return nil
	}
	var payload map[string]any
	if json.Unmarshal([]byte(m.LookupJSON.ValueString()), &payload) != nil {
		d.AddAttributeError(path.Root("lookup_json"), "Invalid book lookup candidate", "lookup_json must contain one JSON object, not the entire lookup result array.")
		return nil
	}
	candidateID, _ := payload["foreignBookId"].(string)
	if !strings.EqualFold(strings.TrimSpace(candidateID), strings.TrimSpace(m.ForeignBookID.ValueString())) {
		d.AddAttributeError(path.Root("lookup_json"), "Lookup identity mismatch", "The selected lookup candidate foreignBookId must exactly match foreign_book_id. Choose one candidate without combining provider identities.")
		return nil
	}
	payload["foreignBookId"] = strings.TrimSpace(m.ForeignBookID.ValueString())
	payload["authorId"] = m.AuthorID.ValueInt64()
	author, ok := payload["author"].(map[string]any)
	if !ok {
		author = map[string]any{}
	}
	author["id"] = m.AuthorID.ValueInt64()
	payload["author"] = author
	payload["mediaType"] = m.MediaType.ValueString()
	overlayBookIntent(payload, m, d)
	payload["addOptions"] = map[string]any{"searchForNewBook": m.SearchForNewBook.ValueBool()}
	return payload
}
func overlayBookIntent(payload map[string]any, m bookModel, d *diag.Diagnostics) {
	if !validProviderID(m.ForeignBookID.ValueString()) {
		d.AddAttributeError(path.Root("foreign_book_id"), "Provider-prefixed book identifier required", "Use book lookup to select an explicit provider-prefixed work identity.")
		return
	}
	payload["monitored"] = m.Monitored.ValueBool()
	payload["anyEditionOk"] = m.AnyEditionOK.ValueBool()
	selected := strings.TrimSpace(m.MonitoredEditionID.ValueString())
	if m.AnyEditionOK.ValueBool() {
		return
	}
	if !m.AnyEditionOK.ValueBool() && !validProviderID(selected) {
		d.AddAttributeError(path.Root("monitored_edition_id"), "Monitored edition required", "When any_edition_ok is false, select a provider-prefixed foreign edition identity.")
		return
	}
	raw, ok := payload["editions"].([]any)
	if !ok || len(raw) == 0 {
		d.AddError("Book editions required", "The Chaptarr book document contains no editions to track.")
		return
	}
	matched := false
	for _, item := range raw {
		edition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		foreign, _ := edition["foreignEditionId"].(string)
		isSelected := selected != "" && strings.EqualFold(strings.TrimSpace(foreign), selected)
		edition["monitored"] = isSelected
		if isSelected {
			matched = true
		}
	}
	if selected != "" && !matched {
		d.AddAttributeError(path.Root("monitored_edition_id"), "Edition not found", "The selected foreign edition identity is not present in the lookup/current book document.")
	}
}
func (r *bookResource) refresh(ctx context.Context, state *bookModel, target *tfsdk.State, d *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		d.AddError("Invalid book state", "The book has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/book/"+strconv.FormatInt(id, 10), "book", target, d)
	if !found || d.HasError() {
		return
	}
	var c bookAPI
	if json.Unmarshal(body, &c) != nil || c.ID < 1 || !validProviderID(c.ForeignBookID) {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid book document.")
		return
	}
	editions := make([]bookEditionModel, 0, len(c.Editions))
	selected := types.StringNull()
	for _, edition := range c.Editions {
		if edition.Monitored {
			if !selected.IsNull() {
				d.AddError("Invalid monitored edition state", "Chaptarr returned more than one monitored edition.")
				return
			}
			if strings.TrimSpace(edition.ForeignEditionID) != "" {
				selected = types.StringValue(edition.ForeignEditionID)
			}
		}
		editions = append(editions, bookEditionModel{ID: types.Int64Value(edition.ID), ForeignEditionID: types.StringValue(edition.ForeignEditionID), Title: types.StringValue(edition.Title), Format: types.StringValue(edition.Format), Monitored: types.BoolValue(edition.Monitored), Narrator: nullableString(edition.Narrator), NarratorNames: setStringState(ctx, edition.NarratorNames, d), DurationSeconds: nullableInt(edition.DurationSeconds)})
	}
	controls := []types.Bool{state.SearchForNewBook, state.DeleteFilesOnDestroy, state.AddImportListExclusion, state.ApplyDestroyToBothFormats}
	state.ID = types.StringValue(strconv.FormatInt(c.ID, 10))
	state.LookupJSON = types.StringNull()
	state.ForeignBookID = types.StringValue(c.ForeignBookID)
	state.AuthorID = types.Int64Value(c.AuthorID)
	state.Title = types.StringValue(c.Title)
	state.MediaType = types.StringValue(c.MediaType)
	state.Monitored = types.BoolValue(c.Monitored)
	state.AnyEditionOK = types.BoolValue(c.AnyEditionOK)
	state.MonitoredEditionID = selected
	state.Narrator = nullableString(c.Narrator)
	state.NarratorNames = setStringState(ctx, c.NarratorNames, d)
	state.Editions = listObjectState(ctx, bookEditionType(), editions, d)
	state.SearchForNewBook = controls[0]
	state.DeleteFilesOnDestroy = controls[1]
	state.AddImportListExclusion = controls[2]
	state.ApplyDestroyToBothFormats = controls[3]
	normalizeBookControls(state)
	d.Append(target.Set(ctx, state)...)
}
