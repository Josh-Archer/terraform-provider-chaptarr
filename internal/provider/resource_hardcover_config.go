package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &hardcoverConfigResource{}
	_ resource.ResourceWithImportState = &hardcoverConfigResource{}
)

type hardcoverConfigResource struct{ client *client.Client }

type hardcoverConfigModel struct {
	ID                      types.String `tfsdk:"id"`
	Enabled                 types.Bool   `tfsdk:"enabled"`
	Token                   types.String `tfsdk:"token"`
	HasToken                types.Bool   `tfsdk:"has_token"`
	Username                types.String `tfsdk:"username"`
	AvatarURL               types.String `tfsdk:"avatar_url"`
	AllowExternalValidation types.Bool   `tfsdk:"allow_external_validation"`
	ObserveServer           types.Bool   `tfsdk:"observe_server"`
}

type hardcoverConfigResponse struct {
	Enabled   bool    `json:"enabled"`
	HasToken  bool    `json:"hasToken"`
	Username  *string `json:"username"`
	AvatarURL *string `json:"avatarUrl"`
}

func newHardcoverConfigResource() resource.Resource { return &hardcoverConfigResource{} }

func (r *hardcoverConfigResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hardcover_config"
}

func (r *hardcoverConfigResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage the singleton Hardcover connection. Tokens are write-only. Chaptarr validates submitted tokens externally, and its GET may backfill profile data, so both network behaviors require explicit opt-in. Destroy only relinquishes Terraform ownership; set enabled=false to disconnect.",
		Attributes: map[string]schema.Attribute{
			"id":                        schema.StringAttribute{Computed: true},
			"enabled":                   schema.BoolAttribute{Optional: true, Computed: true},
			"token":                     schema.StringAttribute{Optional: true, Sensitive: true, WriteOnly: true},
			"has_token":                 schema.BoolAttribute{Computed: true, Sensitive: true},
			"username":                  schema.StringAttribute{Computed: true},
			"avatar_url":                schema.StringAttribute{Computed: true},
			"allow_external_validation": schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Authorize Chaptarr to submit a configured token to Hardcover during create or rotation."},
			"observe_server":            schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Authorize GET observation, which may cause Chaptarr to contact Hardcover to backfill profile fields."},
		},
	}
}

func (r *hardcoverConfigResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}

func loadHardcoverToken(ctx context.Context, config tfsdk.Config, model *hardcoverConfigModel, diagnostics *diag.Diagnostics) {
	diagnostics.Append(config.GetAttribute(ctx, path.Root("token"), &model.Token)...)
}

func validateHardcoverWrite(model hardcoverConfigModel, diagnostics *diag.Diagnostics) bool {
	if model.Token.IsNull() || model.Token.IsUnknown() || strings.TrimSpace(model.Token.ValueString()) == "" {
		diagnostics.AddAttributeError(path.Root("token"), "Hardcover token required", "Configure a non-empty ephemeral token for create or rotation.")
		return false
	}
	if !valueBool(model.AllowExternalValidation) {
		diagnostics.AddAttributeError(path.Root("allow_external_validation"), "External validation authorization required", "Chaptarr submits this token to Hardcover before saving it. Set allow_external_validation=true to authorize that apply-time external call.")
		return false
	}
	return true
}

func (r *hardcoverConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hardcoverConfigModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadHardcoverToken(ctx, req.Config, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Token.IsNull() && !plan.Token.IsUnknown() {
		if !validateHardcoverWrite(plan, &resp.Diagnostics) || !r.postToken(ctx, plan.Token.ValueString(), &resp.Diagnostics) {
			return
		}
		plan.Enabled = types.BoolValue(true)
		plan.HasToken = types.BoolValue(true)
	} else if valueBool(plan.Enabled) && !valueBool(plan.ObserveServer) {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Hardcover token required", "Configure token with allow_external_validation=true, or set observe_server=true to adopt an existing connection.")
		return
	}
	normalizeHardcoverControls(&plan)
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *hardcoverConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hardcoverConfigModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}

func (r *hardcoverConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan hardcoverConfigModel
	var configuredEnabled types.Bool
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadHardcoverToken(ctx, req.Config, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("enabled"), &configuredEnabled)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Token.IsNull() && !plan.Token.IsUnknown() {
		if !validateHardcoverWrite(plan, &resp.Diagnostics) || !r.postToken(ctx, plan.Token.ValueString(), &resp.Diagnostics) {
			return
		}
		plan.Enabled = types.BoolValue(true)
		plan.HasToken = types.BoolValue(true)
	} else if !configuredEnabled.IsNull() && !configuredEnabled.IsUnknown() && !configuredEnabled.ValueBool() {
		if _, err := r.client.Do(ctx, http.MethodDelete, "/api/v1/config/hardcover", nil); err != nil && !hardcoverNotFound(err) {
			resp.Diagnostics.AddError("Unable to disable Hardcover", err.Error())
			return
		}
		plan.HasToken = types.BoolValue(false)
		plan.Username = types.StringNull()
		plan.AvatarURL = types.StringNull()
	}
	normalizeHardcoverControls(&plan)
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *hardcoverConfigResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
	// Disconnecting an external account is an explicit enabled=false operation.
}

func (r *hardcoverConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.TrimSpace(req.ID) != "hardcover" {
		resp.Diagnostics.AddError("Invalid Hardcover import identifier", "Use the literal identifier hardcover.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "hardcover")...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("allow_external_validation"), false)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("observe_server"), false)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), false)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("has_token"), false)...)
}

func (r *hardcoverConfigResource) postToken(ctx context.Context, token string, diagnostics *diag.Diagnostics) bool {
	body, err := json.Marshal(map[string]string{"token": strings.TrimSpace(token)})
	if err != nil {
		diagnostics.AddError("Unable to encode Hardcover configuration", "The token payload could not be encoded.")
		return false
	}
	if _, err := r.client.Do(ctx, http.MethodPost, "/api/v1/config/hardcover", body); err != nil {
		diagnostics.AddError("Unable to configure Hardcover", err.Error())
		return false
	}
	return true
}

func hardcoverNotFound(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func normalizeHardcoverControls(model *hardcoverConfigModel) {
	if model.AllowExternalValidation.IsNull() || model.AllowExternalValidation.IsUnknown() {
		model.AllowExternalValidation = types.BoolValue(false)
	}
	if model.ObserveServer.IsNull() || model.ObserveServer.IsUnknown() {
		model.ObserveServer = types.BoolValue(false)
	}
	model.Token = types.StringNull()
	if model.ID.IsNull() || model.ID.IsUnknown() {
		model.ID = types.StringValue("hardcover")
	}
	if model.Enabled.IsNull() || model.Enabled.IsUnknown() {
		model.Enabled = types.BoolValue(false)
	}
	if model.HasToken.IsNull() || model.HasToken.IsUnknown() {
		model.HasToken = types.BoolValue(false)
	}
	if model.Username.IsUnknown() {
		model.Username = types.StringNull()
	}
	if model.AvatarURL.IsUnknown() {
		model.AvatarURL = types.StringNull()
	}
}

func (r *hardcoverConfigResource) refresh(ctx context.Context, state *hardcoverConfigModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	normalizeHardcoverControls(state)
	if !valueBool(state.ObserveServer) {
		diagnostics.Append(target.Set(ctx, state)...)
		return
	}
	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/config/hardcover", nil)
	if err != nil {
		if hardcoverNotFound(err) {
			target.RemoveResource(ctx)
			return
		}
		diagnostics.AddError("Unable to observe Hardcover configuration", err.Error())
		return
	}
	var current hardcoverConfigResponse
	if json.Unmarshal(response.Body, &current) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid Hardcover configuration.")
		return
	}
	state.Enabled = types.BoolValue(current.Enabled)
	state.HasToken = types.BoolValue(current.HasToken)
	state.Username = nullableString(current.Username)
	state.AvatarURL = nullableString(current.AvatarURL)
	state.Token = types.StringNull()
	diagnostics.Append(target.Set(ctx, state)...)
}
