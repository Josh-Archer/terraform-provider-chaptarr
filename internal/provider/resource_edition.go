package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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

var _ resource.Resource = &editionResource{}
var _ resource.ResourceWithImportState = &editionResource{}

type editionResource struct{ client *client.Client }
type editionModel struct {
	ID               types.String `tfsdk:"id"`
	BookID           types.Int64  `tfsdk:"book_id"`
	EditionID        types.Int64  `tfsdk:"edition_id"`
	ForeignEditionID types.String `tfsdk:"foreign_edition_id"`
	Title            types.String `tfsdk:"title"`
	Format           types.String `tfsdk:"format"`
	Monitored        types.Bool   `tfsdk:"monitored"`
	Narrator         types.String `tfsdk:"narrator"`
	NarratorNames    types.Set    `tfsdk:"narrator_names"`
	DurationSeconds  types.Int64  `tfsdk:"duration_seconds"`
}

func newEditionResource() resource.Resource { return &editionResource{} }
func (r *editionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_edition"
}
func (r *editionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	positive := []validator.Int64{int64validator.AtLeast(1)}
	resp.Schema = schema.Schema{MarkdownDescription: "Select one existing Chaptarr edition as the monitored edition for its book. Apply uses the full Book PUT; refresh is GET-only; destroy forgets selection ownership without changing Chaptarr or files.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "book_id": schema.Int64Attribute{Required: true, Validators: positive, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}}, "edition_id": schema.Int64Attribute{Required: true, Validators: positive, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}}, "foreign_edition_id": schema.StringAttribute{Computed: true}, "title": schema.StringAttribute{Computed: true}, "format": schema.StringAttribute{Computed: true}, "monitored": schema.BoolAttribute{Required: true, MarkdownDescription: "Must be true. Resource ownership represents selecting this edition; remove the resource to stop managing the selection without changing it."}, "narrator": schema.StringAttribute{Computed: true}, "narrator_names": schema.SetAttribute{Computed: true, ElementType: types.StringType}, "duration_seconds": schema.Int64Attribute{Computed: true}}}
}
func (r *editionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *editionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan editionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *editionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state editionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *editionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan editionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *editionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state editionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.AddWarning("Edition selection retained in Chaptarr", "Destroy removed Terraform ownership without selecting another edition, changing monitoring, moving files, or deleting media.")
	}
}
func (r *editionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(strings.TrimSpace(req.ID), "/")
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid edition import identifier", "Use `<book_id>/<edition_id>` with positive local numeric identifiers.")
		return
	}
	bookID, e1 := strconv.ParseInt(parts[0], 10, 64)
	editionID, e2 := strconv.ParseInt(parts[1], 10, 64)
	if e1 != nil || e2 != nil || bookID < 1 || editionID < 1 {
		resp.Diagnostics.AddError("Invalid edition import identifier", "Use `<book_id>/<edition_id>` with positive local numeric identifiers.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%d/%d", bookID, editionID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("book_id"), bookID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("edition_id"), editionID)...)
}
func (r *editionResource) apply(ctx context.Context, m *editionModel, target *tfsdk.State, d *diag.Diagnostics) {
	if m.Monitored.IsNull() || m.Monitored.IsUnknown() || !m.Monitored.ValueBool() {
		d.AddAttributeError(path.Root("monitored"), "Edition selection must be monitored", "Set monitored=true. To stop managing the selection, remove the resource; the provider will not silently select a replacement edition.")
		return
	}
	bookID := m.BookID.ValueInt64()
	editionID := m.EditionID.ValueInt64()
	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/book/"+strconv.FormatInt(bookID, 10), nil)
	if err != nil {
		d.AddError("Unable to read book", err.Error())
		return
	}
	var payload map[string]any
	if json.Unmarshal(response.Body, &payload) != nil {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid book document.")
		return
	}
	raw, ok := payload["editions"].([]any)
	if !ok {
		d.AddError("Book editions required", "The Chaptarr book document contains no editions.")
		return
	}
	matched := false
	for _, item := range raw {
		edition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, ok := edition["id"].(float64)
		selected := ok && int64(id) == editionID
		edition["monitored"] = selected
		matched = matched || selected
	}
	if !matched {
		d.AddAttributeError(path.Root("edition_id"), "Edition not found", "The edition is not part of the configured local book.")
		return
	}
	payload["anyEditionOk"] = false
	updateProfile(ctx, r.client, "/api/v1/book/"+strconv.FormatInt(bookID, 10), payload, "edition selection", d)
	if d.HasError() {
		return
	}
	m.ID = types.StringValue(fmt.Sprintf("%d/%d", bookID, editionID))
	r.refresh(ctx, m, target, d)
}
func (r *editionResource) refresh(ctx context.Context, state *editionModel, target *tfsdk.State, d *diag.Diagnostics) {
	bookID := state.BookID.ValueInt64()
	editionID := state.EditionID.ValueInt64()
	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/edition?"+url.Values{"bookId": []string{strconv.FormatInt(bookID, 10)}}.Encode(), nil)
	if err != nil {
		d.AddError("Unable to read editions", err.Error())
		return
	}
	var values []bookEditionAPI
	if json.Unmarshal(response.Body, &values) != nil {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid edition list.")
		return
	}
	for _, current := range values {
		if current.ID != editionID {
			continue
		}
		state.ID = types.StringValue(fmt.Sprintf("%d/%d", bookID, editionID))
		state.ForeignEditionID = types.StringValue(current.ForeignEditionID)
		state.Title = types.StringValue(current.Title)
		state.Format = types.StringValue(current.Format)
		state.Monitored = types.BoolValue(current.Monitored)
		state.Narrator = nullableString(current.Narrator)
		state.NarratorNames = setStringState(ctx, current.NarratorNames, d)
		state.DurationSeconds = nullableInt(current.DurationSeconds)
		d.Append(target.Set(ctx, state)...)
		return
	}
	target.RemoveResource(ctx)
}
