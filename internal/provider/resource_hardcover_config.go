package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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

type hardcoverConfigResource struct {
	client *client.Client
}

type hardcoverConfigModel struct {
	ID        types.String `tfsdk:"id"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	Token     types.String `tfsdk:"token"`
	HasToken  types.Bool   `tfsdk:"has_token"`
	Username  types.String `tfsdk:"username"`
	AvatarURL types.String `tfsdk:"avatar_url"`
}

type hardcoverConfigResponse struct {
	Enabled   bool    `json:"enabled"`
	HasToken  bool    `json:"hasToken"`
	Username  *string `json:"username"`
	AvatarURL *string `json:"avatarUrl"`
}

func newHardcoverConfigResource() resource.Resource {
	return &hardcoverConfigResource{}
}

func (r *hardcoverConfigResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hardcover_config"
}

func (r *hardcoverConfigResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage the singleton Hardcover connection. The token is write-only and never stored in state. Setting `enabled = false` is the only operation that disconnects Hardcover; destroy only relinquishes Terraform ownership.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Stable singleton identifier.",
			},
			"enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the Hardcover connection is enabled.",
			},
			"token": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				WriteOnly:           true,
				MarkdownDescription: "Hardcover token used only when explicitly configured. It is never read back or stored in state.",
			},
			"has_token": schema.BoolAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Whether Chaptarr reports that a Hardcover token is configured.",
			},
			"username": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Connected Hardcover username.",
			},
			"avatar_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Connected Hardcover avatar URL.",
			},
		},
	}
}

func (r *hardcoverConfigResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	r.client = apiClient
}

func (r *hardcoverConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hardcoverConfigModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.apply(ctx, plan, &resp.Diagnostics) {
		return
	}
	r.refresh(ctx, &resp.State, &resp.Diagnostics)
}

func (r *hardcoverConfigResource) Read(ctx context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	r.refresh(ctx, &resp.State, &resp.Diagnostics)
}

func (r *hardcoverConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan hardcoverConfigModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.apply(ctx, plan, &resp.Diagnostics) {
		return
	}
	r.refresh(ctx, &resp.State, &resp.Diagnostics)
}

func (r *hardcoverConfigResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
	// Disconnecting an external service is an explicit enabled=false operation.
}

func (r *hardcoverConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import identifier", "Use `hardcover` as the import identifier.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "hardcover")...)
}

func (r *hardcoverConfigResource) apply(ctx context.Context, plan hardcoverConfigModel, diagnostics *diag.Diagnostics) bool {
	current, err := r.read(ctx)
	if err != nil {
		diagnostics.AddError("Unable to read Hardcover configuration", err.Error())
		return false
	}

	tokenConfigured := !plan.Token.IsNull() && !plan.Token.IsUnknown()
	enabledConfigured := !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown()
	if enabledConfigured && !plan.Enabled.ValueBool() && tokenConfigured {
		diagnostics.AddError("Conflicting Hardcover configuration", "`token` cannot be supplied when `enabled` is false.")
		return false
	}

	wantEnabled := current.Enabled
	if enabledConfigured {
		wantEnabled = plan.Enabled.ValueBool()
	} else if tokenConfigured {
		wantEnabled = true
	}

	switch {
	case !wantEnabled && current.Enabled:
		if _, err := r.client.Do(ctx, http.MethodDelete, "/api/v1/config/hardcover", nil); err != nil {
			diagnostics.AddError("Unable to disable Hardcover", err.Error())
			return false
		}
	case wantEnabled && tokenConfigured:
		body, err := json.Marshal(map[string]string{"token": plan.Token.ValueString()})
		if err != nil {
			diagnostics.AddError("Unable to encode Hardcover configuration", "The token payload could not be encoded.")
			return false
		}
		if _, err := r.client.Do(ctx, http.MethodPost, "/api/v1/config/hardcover", body); err != nil {
			diagnostics.AddError("Unable to configure Hardcover", err.Error())
			return false
		}
	case wantEnabled && !current.HasToken:
		diagnostics.AddError("Hardcover token required", "Chaptarr has no Hardcover token. Configure the write-only `token` attribute to enable the connection.")
		return false
	}

	return !diagnostics.HasError()
}

func (r *hardcoverConfigResource) read(ctx context.Context) (hardcoverConfigResponse, error) {
	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/config/hardcover", nil)
	if err != nil {
		return hardcoverConfigResponse{}, err
	}
	var current hardcoverConfigResponse
	if err := json.Unmarshal(response.Body, &current); err != nil {
		return hardcoverConfigResponse{}, errors.New("Chaptarr returned an invalid Hardcover configuration document")
	}
	return current, nil
}

func (r *hardcoverConfigResource) refresh(ctx context.Context, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	current, err := r.read(ctx)
	if err != nil {
		diagnostics.AddError("Unable to read Hardcover configuration", err.Error())
		return
	}

	model := hardcoverConfigModel{
		ID:       types.StringValue("hardcover"),
		Enabled:  types.BoolValue(current.Enabled),
		Token:    types.StringNull(),
		HasToken: types.BoolValue(current.HasToken),
	}
	if current.Username == nil {
		model.Username = types.StringNull()
	} else {
		model.Username = types.StringValue(*current.Username)
	}
	if current.AvatarURL == nil {
		model.AvatarURL = types.StringNull()
	} else {
		model.AvatarURL = types.StringValue(*current.AvatarURL)
	}
	diagnostics.Append(state.Set(ctx, &model)...)
}
