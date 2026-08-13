package provider

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &tagResource{}
	_ resource.ResourceWithImportState = &tagResource{}
)

type tagResource struct{ client *client.Client }
type tagModel struct {
	ID    types.String `tfsdk:"id"`
	Label types.String `tfsdk:"label"`
}
type tagAPI struct {
	ID    int64  `json:"id,omitempty"`
	Label string `json:"label"`
}

func newTagResource() resource.Resource { return &tagResource{} }
func (r *tagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}
func (r *tagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr tag. Associations to other objects are observed separately through the tag-details data source and are not mutated by this resource.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "label": schema.StringAttribute{Required: true},
	}}
}
func (r *tagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *tagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || strings.TrimSpace(plan.Label.ValueString()) == "" {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Tag label required", "Set a non-empty tag label.")
		}
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/tag", tagAPI{Label: strings.TrimSpace(plan.Label.ValueString())}, "tag", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *tagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *tagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan tagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	if strings.TrimSpace(plan.Label.ValueString()) == "" {
		resp.Diagnostics.AddError("Tag label required", "Set a non-empty tag label.")
		return
	}
	updateProfile(ctx, r.client, "/api/v1/tag/"+strconv.FormatInt(id, 10), tagAPI{ID: id, Label: strings.TrimSpace(plan.Label.ValueString())}, "tag", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}
func (r *tagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/tag/", state.ID, "tag", &resp.Diagnostics)
	}
}
func (r *tagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "tag", &resp.State, &resp.Diagnostics)
}
func (r *tagResource) refresh(ctx context.Context, state *tagModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid tag state", "The tag has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/tag/"+strconv.FormatInt(id, 10), "tag", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current tagAPI
	if json.Unmarshal(body, &current) != nil || current.ID < 1 || strings.TrimSpace(current.Label) == "" {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid tag document.")
		return
	}
	state.ID, state.Label = types.StringValue(strconv.FormatInt(current.ID, 10)), types.StringValue(current.Label)
	diagnostics.Append(target.Set(ctx, state)...)
}
