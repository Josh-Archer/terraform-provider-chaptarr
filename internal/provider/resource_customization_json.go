package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type customFilterResource struct{ client *client.Client }
type customFilterModel struct {
	ID            types.String `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	Label         types.String `tfsdk:"label"`
	FiltersJSON   types.String `tfsdk:"filters_json"`
	FiltersSHA256 types.String `tfsdk:"filters_sha256"`
}
type customFilterAPI struct {
	ID      int64  `json:"id,omitempty"`
	Type    string `json:"type"`
	Label   string `json:"label"`
	Filters []any  `json:"filters"`
}

type customFormatResource struct{ client *client.Client }
type customFormatModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	IncludeWhenRenaming  types.Bool   `tfsdk:"include_when_renaming"`
	BuiltInKey           types.String `tfsdk:"built_in_key"`
	AppliesTo            types.String `tfsdk:"applies_to"`
	SpecificationsJSON   types.String `tfsdk:"specifications_json"`
	SpecificationsSHA256 types.String `tfsdk:"specifications_sha256"`
}
type customFormatAPI struct {
	ID                  int64            `json:"id,omitempty"`
	Name                string           `json:"name"`
	IncludeWhenRenaming *bool            `json:"includeCustomFormatWhenRenaming"`
	BuiltInKey          string           `json:"builtInKey,omitempty"`
	AppliesTo           string           `json:"appliesTo"`
	Specifications      []map[string]any `json:"specifications"`
}

var _ resource.ResourceWithImportState = &customFilterResource{}
var _ resource.ResourceWithImportState = &customFormatResource{}

func newCustomFilterResource() resource.Resource { return &customFilterResource{} }
func (r *customFilterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_filter"
}
func (r *customFilterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr UI custom filter. The API's open-ended filter objects are stored as canonical JSON with a stable SHA-256 drift fingerprint.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "type": schema.StringAttribute{Required: true}, "label": schema.StringAttribute{Required: true}, "filters_json": schema.StringAttribute{Required: true}, "filters_sha256": schema.StringAttribute{Computed: true},
	}}
}
func (r *customFilterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *customFilterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan customFilterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	payload := customFilterPayload(plan, 0, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/customfilter", payload, "custom filter", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *customFilterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state customFilterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *customFilterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan customFilterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	payload := customFilterPayload(plan, id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/customfilter/"+strconv.FormatInt(id, 10), payload, "custom filter", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}
func (r *customFilterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state customFilterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/customfilter/", state.ID, "custom filter", &resp.Diagnostics)
	}
}
func (r *customFilterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "custom filter", &resp.State, &resp.Diagnostics)
}
func customFilterPayload(model customFilterModel, id int64, diagnostics *diag.Diagnostics) customFilterAPI {
	if strings.TrimSpace(model.Type.ValueString()) == "" || strings.TrimSpace(model.Label.ValueString()) == "" {
		diagnostics.AddError("Invalid custom filter", "Both type and label must be non-empty.")
	}
	canonical, decoded := canonicalArray(model.FiltersJSON.ValueString(), "filters_json", diagnostics)
	if diagnostics.HasError() {
		return customFilterAPI{}
	}
	model.FiltersJSON = types.StringValue(canonical)
	return customFilterAPI{ID: id, Type: strings.TrimSpace(model.Type.ValueString()), Label: strings.TrimSpace(model.Label.ValueString()), Filters: decoded}
}
func (r *customFilterResource) refresh(ctx context.Context, state *customFilterModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/customfilter/"+strconv.FormatInt(id, 10), "custom filter", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current customFilterAPI
	if json.Unmarshal(body, &current) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid custom-filter document.")
		return
	}
	canonical, hash := canonicalValue(current.Filters)
	state.ID, state.Type, state.Label = types.StringValue(strconv.FormatInt(current.ID, 10)), types.StringValue(current.Type), types.StringValue(current.Label)
	state.FiltersJSON, state.FiltersSHA256 = types.StringValue(canonical), types.StringValue(hash)
	diagnostics.Append(target.Set(ctx, state)...)
}

func newCustomFormatResource() resource.Resource { return &customFormatResource{} }
func (r *customFormatResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_format"
}
func (r *customFormatResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr custom format. Dynamic specification fields are canonical JSON and receive a stable SHA-256 drift fingerprint; top-level media and rename policy remain typed.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "name": schema.StringAttribute{Required: true}, "include_when_renaming": schema.BoolAttribute{Optional: true, Computed: true}, "built_in_key": schema.StringAttribute{Computed: true}, "applies_to": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("both", "audiobook", "ebook")}}, "specifications_json": schema.StringAttribute{Required: true}, "specifications_sha256": schema.StringAttribute{Computed: true},
	}}
}
func (r *customFormatResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *customFormatResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan customFormatModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	payload := customFormatPayload(plan, 0, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/customformat", payload, "custom format", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *customFormatResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state customFormatModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *customFormatResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan customFormatModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	payload := customFormatPayload(plan, id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/customformat/"+strconv.FormatInt(id, 10), payload, "custom format", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}
func (r *customFormatResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state customFormatModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/customformat/", state.ID, "custom format", &resp.Diagnostics)
	}
}
func (r *customFormatResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "custom format", &resp.State, &resp.Diagnostics)
}
func customFormatPayload(model customFormatModel, id int64, diagnostics *diag.Diagnostics) customFormatAPI {
	canonical, decoded := canonicalObjectArray(model.SpecificationsJSON.ValueString(), "specifications_json", diagnostics)
	_ = canonical
	if len(decoded) == 0 && !diagnostics.HasError() {
		diagnostics.AddError("Custom-format specification required", "Configure at least one specification.")
	}
	for _, spec := range decoded {
		if strings.TrimSpace(fmt.Sprint(spec["name"])) == "" || strings.TrimSpace(fmt.Sprint(spec["implementation"])) == "" {
			diagnostics.AddError("Invalid custom-format specification", "Every specification requires non-empty name and implementation values.")
		}
	}
	return customFormatAPI{ID: id, Name: strings.TrimSpace(model.Name.ValueString()), IncludeWhenRenaming: boolPointer(model.IncludeWhenRenaming), BuiltInKey: model.BuiltInKey.ValueString(), AppliesTo: model.AppliesTo.ValueString(), Specifications: decoded}
}
func (r *customFormatResource) refresh(ctx context.Context, state *customFormatModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/customformat/"+strconv.FormatInt(id, 10), "custom format", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current customFormatAPI
	if json.Unmarshal(body, &current) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid custom-format document.")
		return
	}
	canonical, hash := canonicalValue(current.Specifications)
	state.ID, state.Name, state.BuiltInKey, state.AppliesTo = types.StringValue(strconv.FormatInt(current.ID, 10)), types.StringValue(current.Name), types.StringValue(current.BuiltInKey), types.StringValue(current.AppliesTo)
	state.IncludeWhenRenaming = nullableBool(current.IncludeWhenRenaming)
	state.SpecificationsJSON, state.SpecificationsSHA256 = types.StringValue(canonical), types.StringValue(hash)
	diagnostics.Append(target.Set(ctx, state)...)
}

func canonicalArray(raw, name string, diagnostics *diag.Diagnostics) (string, []any) {
	var value []any
	if json.Unmarshal([]byte(raw), &value) != nil {
		diagnostics.AddError("Invalid "+name, "Provide a valid JSON array.")
		return "", nil
	}
	canonical, _ := json.Marshal(value)
	return string(canonical), value
}
func canonicalObjectArray(raw, name string, diagnostics *diag.Diagnostics) (string, []map[string]any) {
	var value []map[string]any
	if json.Unmarshal([]byte(raw), &value) != nil {
		diagnostics.AddError("Invalid "+name, "Provide a valid JSON array of objects.")
		return "", nil
	}
	canonical, _ := json.Marshal(value)
	return string(canonical), value
}
func canonicalValue(value any) (string, string) {
	canonical, _ := json.Marshal(value)
	sum := sha256.Sum256(canonical)
	return string(canonical), hex.EncodeToString(sum[:])
}
