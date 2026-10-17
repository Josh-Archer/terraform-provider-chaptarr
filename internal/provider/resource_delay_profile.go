package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
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
	_ resource.ResourceWithModifyPlan  = &delayProfileResource{}
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
	AfterID                        types.Int64  `tfsdk:"after_id"`
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
		MarkdownDescription: "Manage a Chaptarr delay profile. Set `after_id` to place the profile after another profile (`0` places it first); `order` is observed from the server. The global profile (ID 1) cannot be destroyed or reordered.",
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
			"after_id":                            schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(0)}, MarkdownDescription: "Place this profile after the given delay-profile ID. Use `0` to move it to the first position. Omit to leave ordering unchanged. Cannot be set on the global profile (ID 1)."},
			"tags":                                schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type},
		},
	}
}

func (r *delayProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *delayProfileResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan delayProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.AfterID.IsNull() || plan.AfterID.IsUnknown() {
		return
	}
	id := int64(0)
	var state delayProfileModel
	hasState := false
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		id, _ = positiveModelID(state.ID)
		hasState = true
	}
	if id == 0 && !plan.ID.IsNull() && !plan.ID.IsUnknown() {
		id, _ = positiveModelID(plan.ID)
	}
	if !validateDelayProfilePosition(id, plan.AfterID, &resp.Diagnostics) {
		return
	}
	if r.client != nil {
		targetWasDeletedInUI := hasState && !state.AfterID.IsNull() && !state.AfterID.IsUnknown() && state.AfterID.Equal(plan.AfterID)
		validateDelayProfileTarget(ctx, r.client, id, plan.AfterID, targetWasDeletedInUI, &resp.Diagnostics)
	}
}

func (r *delayProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan delayProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	payload := delayProfilePayload(ctx, plan, 0, &resp.Diagnostics)
	if !validateDelayProfile(payload, false, &resp.Diagnostics) || !validateDelayProfileTarget(ctx, r.client, 0, plan.AfterID, false, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/delayprofile", payload, "delay profile", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	var reorderDiagnostics diag.Diagnostics
	if !reorderDelayProfile(ctx, r.client, id, plan.AfterID, &reorderDiagnostics) {
		for _, err := range reorderDiagnostics.Errors() {
			resp.Diagnostics.AddWarning(
				"Delay profile created but initial reorder failed",
				fmt.Sprintf("Delay profile %d was created, but initial reordering failed: %s. Ordering remains unadjusted; the next apply will retry reordering without recreating the profile.", id, err.Detail()),
			)
		}
		for _, warn := range reorderDiagnostics.Warnings() {
			resp.Diagnostics.AddWarning(warn.Summary(), warn.Detail())
		}
		setDelayProfileReorderPending(ctx, resp.Private, true, &resp.Diagnostics)
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
		return
	}
	resp.Diagnostics.Append(reorderDiagnostics...)
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *delayProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state delayProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if isDelayProfileReorderPending(ctx, req.Private) {
		state.AfterID = types.Int64Null()
	}
	r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
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
	if !validateDelayProfilePosition(id, plan.AfterID, &resp.Diagnostics) {
		return
	}
	var prior delayProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	reorderNeeded := (!plan.AfterID.Equal(prior.AfterID) || isDelayProfileReorderPending(ctx, req.Private)) && !plan.AfterID.IsNull() && !plan.AfterID.IsUnknown()
	if reorderNeeded {
		if !validateDelayProfileTarget(ctx, r.client, id, plan.AfterID, false, &resp.Diagnostics) {
			return
		}
	}
	updateProfile(ctx, r.client, "/api/v1/delayprofile/"+strconv.FormatInt(id, 10), payload, "delay profile", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		if reorderNeeded {
			if !reorderDelayProfile(ctx, r.client, id, plan.AfterID, &resp.Diagnostics) {
				plan.AfterID = types.Int64Null()
				if !prior.AfterID.IsUnknown() {
					plan.AfterID = prior.AfterID
				}
				var refreshDiagnostics diag.Diagnostics
				r.refresh(ctx, &plan, &resp.State, &refreshDiagnostics)
				resp.Diagnostics.Append(refreshDiagnostics...)
				return
			}
			setDelayProfileReorderPending(ctx, resp.Private, false, &resp.Diagnostics)
		} else if plan.AfterID.IsNull() {
			setDelayProfileReorderPending(ctx, resp.Private, false, &resp.Diagnostics)
		}
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

func validateDelayProfilePosition(id int64, afterID types.Int64, diagnostics *diag.Diagnostics) bool {
	if afterID.IsNull() || afterID.IsUnknown() {
		return !diagnostics.HasError()
	}
	if id == 1 {
		diagnostics.AddAttributeError(path.Root("after_id"), "Invalid delay-profile position", "The global delay profile (ID 1) cannot be reordered.")
	}
	if afterID.ValueInt64() < 0 {
		diagnostics.AddAttributeError(path.Root("after_id"), "Invalid delay-profile position", "after_id must be zero or a positive delay-profile ID.")
	} else if afterID.ValueInt64() == 1 {
		diagnostics.AddAttributeError(path.Root("after_id"), "Invalid delay-profile position", "The global delay profile (ID 1) cannot be used as an after_id target.")
	} else if id > 0 && afterID.ValueInt64() == id {
		diagnostics.AddAttributeError(path.Root("after_id"), "Invalid delay-profile position", "A delay profile cannot be placed after itself.")
	}
	return !diagnostics.HasError()
}

func validateDelayProfileTarget(ctx context.Context, apiClient *client.Client, id int64, afterID types.Int64, warnIfMissing bool, diagnostics *diag.Diagnostics) bool {
	if apiClient == nil || afterID.IsNull() || afterID.IsUnknown() || afterID.ValueInt64() == 0 || diagnostics.HasError() {
		return !diagnostics.HasError()
	}
	if !validateDelayProfilePosition(id, afterID, diagnostics) {
		return false
	}
	response, err := apiClient.Do(ctx, http.MethodGet, "/api/v1/delayprofile", nil)
	if err != nil {
		if warnIfMissing {
			diagnostics.AddAttributeWarning(path.Root("after_id"), "Unable to verify delay-profile position", fmt.Sprintf("Could not verify that delay profile %d exists: %s", afterID.ValueInt64(), err))
			return true
		}
		diagnostics.AddAttributeError(path.Root("after_id"), "Unable to verify delay-profile position", fmt.Sprintf("Could not verify that delay profile %d exists: %s", afterID.ValueInt64(), err))
		return false
	}
	var profiles []delayProfileAPI
	if err := json.Unmarshal(response.Body, &profiles); err != nil {
		if warnIfMissing {
			diagnostics.AddAttributeWarning(path.Root("after_id"), "Unable to verify delay-profile position", "Chaptarr returned an invalid delay-profile list while checking after_id.")
			return true
		}
		diagnostics.AddAttributeError(path.Root("after_id"), "Unable to verify delay-profile position", "Chaptarr returned an invalid delay-profile list while checking after_id.")
		return false
	}
	for _, profile := range profiles {
		if profile.ID == afterID.ValueInt64() {
			return true
		}
	}
	if warnIfMissing {
		diagnostics.AddAttributeWarning(path.Root("after_id"), "Delay-profile target missing", fmt.Sprintf("Delay profile %d does not exist in Chaptarr (it may have been deleted in the UI). The profile's position will not be updated unless after_id changes.", afterID.ValueInt64()))
		return true
	}
	diagnostics.AddAttributeError(path.Root("after_id"), "Invalid delay-profile position", fmt.Sprintf("Delay profile %d does not exist.", afterID.ValueInt64()))
	return false
}

const delayProfileReorderPendingKey = "reorder_pending"

type privateStateGetter interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

type privateStateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

func isNilPrivateState(v any) bool {
	if v == nil {
		return true
	}
	val := reflect.ValueOf(v)
	return val.Kind() == reflect.Pointer && val.IsNil()
}

func isDelayProfileReorderPending(ctx context.Context, private privateStateGetter) bool {
	if isNilPrivateState(private) {
		return false
	}
	data, diags := private.GetKey(ctx, delayProfileReorderPendingKey)
	if diags.HasError() || len(data) == 0 {
		return false
	}
	var pending bool
	if err := json.Unmarshal(data, &pending); err == nil {
		return pending
	}
	return string(data) == "true"
}

func setDelayProfileReorderPending(ctx context.Context, private privateStateSetter, pending bool, diagnostics *diag.Diagnostics) {
	if isNilPrivateState(private) {
		return
	}
	if !pending {
		diagnostics.Append(private.SetKey(ctx, delayProfileReorderPendingKey, nil)...)
		return
	}
	diagnostics.Append(private.SetKey(ctx, delayProfileReorderPendingKey, []byte("true"))...)
}

func reorderDelayProfile(ctx context.Context, apiClient *client.Client, id int64, afterID types.Int64, diagnostics *diag.Diagnostics) bool {
	if afterID.IsNull() || afterID.IsUnknown() {
		return !diagnostics.HasError()
	}
	if !validateDelayProfilePosition(id, afterID, diagnostics) {
		return false
	}
	values := url.Values{}
	if afterID.ValueInt64() != 0 {
		values.Set("afterId", strconv.FormatInt(afterID.ValueInt64(), 10))
	}
	endpoint := "/api/v1/delayprofile/reorder/" + strconv.FormatInt(id, 10)
	if query := values.Encode(); query != "" {
		endpoint += "?" + query
	}
	if _, err := apiClient.Do(ctx, http.MethodPut, endpoint, nil); err != nil {
		diagnostics.AddError("Unable to reorder delay profile", err.Error())
		return false
	}
	return true
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
