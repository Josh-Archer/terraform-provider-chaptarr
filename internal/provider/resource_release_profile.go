package provider

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &releaseProfileResource{}
	_ resource.ResourceWithImportState = &releaseProfileResource{}
)

type releaseProfileResource struct{ client *client.Client }

type releaseProfileModel struct {
	ID        types.String `tfsdk:"id"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	Required  types.List   `tfsdk:"required_terms"`
	Ignored   types.List   `tfsdk:"ignored_terms"`
	IndexerID types.Int64  `tfsdk:"indexer_id"`
	Tags      types.Set    `tfsdk:"tags"`
}

type releaseProfileAPI struct {
	ID        int64    `json:"id,omitempty"`
	Enabled   bool     `json:"enabled"`
	Required  []string `json:"required"`
	Ignored   []string `json:"ignored"`
	IndexerID int64    `json:"indexerId"`
	Tags      []int64  `json:"tags"`
}

func newReleaseProfileResource() resource.Resource { return &releaseProfileResource{} }

func (r *releaseProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_release_profile"
}

func (r *releaseProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Chaptarr release profile. Required and ignored terms are ordered and are preserved exactly across refresh.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"enabled":        schema.BoolAttribute{Required: true},
			"required_terms": schema.ListAttribute{Optional: true, Computed: true, ElementType: types.StringType},
			"ignored_terms":  schema.ListAttribute{Optional: true, Computed: true, ElementType: types.StringType},
			"indexer_id":     schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(0)}},
			"tags":           schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type},
		},
	}
}

func (r *releaseProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *releaseProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan releaseProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	payload := releaseProfilePayload(ctx, plan, 0, &resp.Diagnostics)
	if !validateReleaseProfile(payload, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/releaseprofile", payload, "release profile", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *releaseProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state releaseProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}

func (r *releaseProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan releaseProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Invalid release-profile state", "The profile has no valid numeric identifier.")
		}
		return
	}
	payload := releaseProfilePayload(ctx, plan, id, &resp.Diagnostics)
	if !validateReleaseProfile(payload, &resp.Diagnostics) {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/releaseprofile/"+strconv.FormatInt(id, 10), payload, "release profile", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}

func (r *releaseProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state releaseProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/releaseprofile/", state.ID, "release profile", &resp.Diagnostics)
	}
}

func (r *releaseProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "release profile", &resp.State, &resp.Diagnostics)
}

func releaseProfilePayload(ctx context.Context, model releaseProfileModel, id int64, diagnostics *diag.Diagnostics) releaseProfileAPI {
	return releaseProfileAPI{ID: id, Enabled: valueBool(model.Enabled), Required: listStringValues(ctx, model.Required, diagnostics), Ignored: listStringValues(ctx, model.Ignored, diagnostics), IndexerID: valueInt64(model.IndexerID), Tags: setInt64Values(ctx, model.Tags, diagnostics)}
}

func validateReleaseProfile(payload releaseProfileAPI, diagnostics *diag.Diagnostics) bool {
	if len(payload.Required) == 0 && len(payload.Ignored) == 0 {
		diagnostics.AddError("Release terms required", "Configure at least one required_terms or ignored_terms entry.")
	}
	return !diagnostics.HasError()
}

func (r *releaseProfileResource) refresh(ctx context.Context, state *releaseProfileModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid release-profile state", "The profile has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/releaseprofile/"+strconv.FormatInt(id, 10), "release profile", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current releaseProfileAPI
	if json.Unmarshal(body, &current) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid release-profile document.")
		return
	}
	state.ID, state.Enabled, state.IndexerID = types.StringValue(strconv.FormatInt(current.ID, 10)), types.BoolValue(current.Enabled), types.Int64Value(current.IndexerID)
	state.Required = listStringState(ctx, current.Required, diagnostics)
	state.Ignored = listStringState(ctx, current.Ignored, diagnostics)
	state.Tags = setInt64State(ctx, current.Tags, diagnostics)
	diagnostics.Append(target.Set(ctx, state)...)
}

func listStringValues(ctx context.Context, value types.List, diagnostics *diag.Diagnostics) []string {
	if value.IsNull() || value.IsUnknown() {
		return []string{}
	}
	var values []string
	diagnostics.Append(value.ElementsAs(ctx, &values, false)...)
	return values
}

func listStringState(ctx context.Context, values []string, diagnostics *diag.Diagnostics) types.List {
	result, converted := types.ListValueFrom(ctx, types.StringType, values)
	diagnostics.Append(converted...)
	return result
}
