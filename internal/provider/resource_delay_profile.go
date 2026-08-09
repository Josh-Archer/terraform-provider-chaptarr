package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

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
	_ resource.Resource                = &delayProfileResource{}
	_ resource.ResourceWithImportState = &delayProfileResource{}
)

type delayProfileResource struct{ client *client.Client }

type delayProfileModel struct {
	ID                             types.String `tfsdk:"id"`
	EnableUsenet                   types.Bool   `tfsdk:"enable_usenet"`
	EnableTorrent                  types.Bool   `tfsdk:"enable_torrent"`
	PreferredProtocol              types.String `tfsdk:"preferred_protocol"`
	UsenetDelay                    types.Int64  `tfsdk:"usenet_delay_minutes"`
	TorrentDelay                   types.Int64  `tfsdk:"torrent_delay_minutes"`
	BypassIfHighestQuality         types.Bool   `tfsdk:"bypass_if_highest_quality"`
	BypassIfAboveCustomFormatScore types.Bool   `tfsdk:"bypass_if_above_custom_format_score"`
	MinimumCustomFormatScore       types.Int64  `tfsdk:"minimum_custom_format_score"`
	Order                          types.Int64  `tfsdk:"order"`
	Tags                           types.Set    `tfsdk:"tags"`
}

type delayProfileAPI struct {
	ID                             int64   `json:"id,omitempty"`
	EnableUsenet                   bool    `json:"enableUsenet"`
	EnableTorrent                  bool    `json:"enableTorrent"`
	PreferredProtocol              string  `json:"preferredProtocol"`
	UsenetDelay                    int64   `json:"usenetDelay"`
	TorrentDelay                   int64   `json:"torrentDelay"`
	BypassIfHighestQuality         bool    `json:"bypassIfHighestQuality"`
	BypassIfAboveCustomFormatScore bool    `json:"bypassIfAboveCustomFormatScore"`
	MinimumCustomFormatScore       int64   `json:"minimumCustomFormatScore"`
	Order                          int64   `json:"order"`
	Tags                           []int64 `json:"tags"`
}

func newDelayProfileResource() resource.Resource { return &delayProfileResource{} }

func (r *delayProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_delay_profile"
}

func (r *delayProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Chaptarr delay profile. The server owns profile ordering; `order` is observed and the global profile (ID 1) cannot be destroyed.",
		Attributes: map[string]schema.Attribute{
			"id":                                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"enable_usenet":                       schema.BoolAttribute{Required: true},
			"enable_torrent":                      schema.BoolAttribute{Required: true},
			"preferred_protocol":                  schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("unknown", "usenet", "torrent")}},
			"usenet_delay_minutes":                schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(0)}},
			"torrent_delay_minutes":               schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(0)}},
			"bypass_if_highest_quality":           schema.BoolAttribute{Required: true},
			"bypass_if_above_custom_format_score": schema.BoolAttribute{Required: true},
			"minimum_custom_format_score":         schema.Int64Attribute{Required: true},
			"order":                               schema.Int64Attribute{Computed: true},
			"tags":                                schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type},
		},
	}
}

func (r *delayProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *delayProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan delayProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	payload := delayProfilePayload(ctx, plan, 0, &resp.Diagnostics)
	if !validateDelayProfile(payload, false, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/delayprofile", payload, "delay profile", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *delayProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state delayProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}

func (r *delayProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan delayProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Invalid delay-profile state", "The profile has no valid numeric identifier.")
		}
		return
	}
	payload := delayProfilePayload(ctx, plan, id, &resp.Diagnostics)
	if !validateDelayProfile(payload, id == 1, &resp.Diagnostics) {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/delayprofile/"+strconv.FormatInt(id, 10), payload, "delay profile", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}

func (r *delayProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state delayProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	id, ok := positiveModelID(state.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	if id == 1 {
		resp.Diagnostics.AddAttributeError(path.Root("id"), "Global delay profile cannot be deleted", "Chaptarr reserves delay-profile ID 1. Remove it from Terraform state instead of destroying it in Chaptarr.")
		return
	}
	deleteProfile(ctx, r.client, "/api/v1/delayprofile/", state.ID, "delay profile", &resp.Diagnostics)
}

func (r *delayProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "delay profile", &resp.State, &resp.Diagnostics)
}

func delayProfilePayload(ctx context.Context, model delayProfileModel, id int64, diagnostics *diag.Diagnostics) delayProfileAPI {
	return delayProfileAPI{ID: id, EnableUsenet: valueBool(model.EnableUsenet), EnableTorrent: valueBool(model.EnableTorrent), PreferredProtocol: model.PreferredProtocol.ValueString(), UsenetDelay: valueInt64(model.UsenetDelay), TorrentDelay: valueInt64(model.TorrentDelay), BypassIfHighestQuality: valueBool(model.BypassIfHighestQuality), BypassIfAboveCustomFormatScore: valueBool(model.BypassIfAboveCustomFormatScore), MinimumCustomFormatScore: valueInt64(model.MinimumCustomFormatScore), Order: valueInt64(model.Order), Tags: setInt64Values(ctx, model.Tags, diagnostics)}
}

func validateDelayProfile(payload delayProfileAPI, global bool, diagnostics *diag.Diagnostics) bool {
	if !payload.EnableUsenet && !payload.EnableTorrent {
		diagnostics.AddError("Download protocol required", "Enable at least one of Usenet or Torrent.")
	}
	if global && len(payload.Tags) != 0 {
		diagnostics.AddError("Invalid global delay-profile tags", "The global delay profile (ID 1) cannot have tags.")
	}
	if !global && len(payload.Tags) == 0 {
		diagnostics.AddError("Delay-profile tags required", "Non-global delay profiles must have at least one tag.")
	}
	return !diagnostics.HasError()
}

func (r *delayProfileResource) refresh(ctx context.Context, state *delayProfileModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid delay-profile state", "The profile has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/delayprofile/"+strconv.FormatInt(id, 10), "delay profile", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current delayProfileAPI
	if json.Unmarshal(body, &current) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid delay-profile document.")
		return
	}
	if current.PreferredProtocol != "unknown" && current.PreferredProtocol != "usenet" && current.PreferredProtocol != "torrent" {
		diagnostics.AddError("Invalid Chaptarr response", fmt.Sprintf("Chaptarr returned unsupported download protocol %q.", current.PreferredProtocol))
		return
	}
	state.ID = types.StringValue(strconv.FormatInt(current.ID, 10))
	state.EnableUsenet, state.EnableTorrent, state.PreferredProtocol = types.BoolValue(current.EnableUsenet), types.BoolValue(current.EnableTorrent), types.StringValue(current.PreferredProtocol)
	state.UsenetDelay, state.TorrentDelay = types.Int64Value(current.UsenetDelay), types.Int64Value(current.TorrentDelay)
	state.BypassIfHighestQuality, state.BypassIfAboveCustomFormatScore = types.BoolValue(current.BypassIfHighestQuality), types.BoolValue(current.BypassIfAboveCustomFormatScore)
	state.MinimumCustomFormatScore, state.Order = types.Int64Value(current.MinimumCustomFormatScore), types.Int64Value(current.Order)
	state.Tags = setInt64State(ctx, current.Tags, diagnostics)
	diagnostics.Append(target.Set(ctx, state)...)
}
