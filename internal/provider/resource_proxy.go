package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
	_ resource.Resource                = &proxyResource{}
	_ resource.ResourceWithImportState = &proxyResource{}
)

type proxyResource struct{ client *client.Client }
type proxyModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Type     types.String `tfsdk:"type"`
	Hostname types.String `tfsdk:"hostname"`
	Port     types.Int64  `tfsdk:"port"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
}
type proxyAPI struct {
	ID       int64  `json:"id,omitempty"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Hostname string `json:"hostname"`
	Port     int64  `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func newProxyResource() resource.Resource { return &proxyResource{} }
func (r *proxyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_proxy"
}
func (r *proxyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr HTTP, SOCKS4, or SOCKS5 proxy. Password is apply-only and Chaptarr preserves the existing password when it is omitted during update. Connectivity testing remains an explicit operational action and is not run during plan or refresh.", Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name": schema.StringAttribute{Required: true}, "type": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("http", "socks4", "socks5")}},
		"hostname": schema.StringAttribute{Required: true}, "port": schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.Between(1, 65535)}},
		"username": schema.StringAttribute{Optional: true, Computed: true},
		"password": schema.StringAttribute{Optional: true, Sensitive: true, WriteOnly: true, MarkdownDescription: "Apply-only proxy password. Omit on update to preserve Chaptarr's existing password."},
	}}
}
func (r *proxyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *proxyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan proxyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadProxyPassword(ctx, req.Config, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !validateProxy(plan, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/settings/proxy", proxyPayload(plan, 0), "proxy", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *proxyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state proxyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *proxyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan proxyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadProxyPassword(ctx, req.Config, &plan, &resp.Diagnostics)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok || !validateProxy(plan, &resp.Diagnostics) {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/settings/proxy/"+strconv.FormatInt(id, 10), proxyPayload(plan, id), "proxy", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}
func (r *proxyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state proxyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/settings/proxy/", state.ID, "proxy", &resp.Diagnostics)
	}
}
func (r *proxyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "proxy", &resp.State, &resp.Diagnostics)
}
func loadProxyPassword(ctx context.Context, config tfsdk.Config, plan *proxyModel, diagnostics *diag.Diagnostics) {
	diagnostics.Append(config.GetAttribute(ctx, path.Root("password"), &plan.Password)...)
}
func validateProxy(model proxyModel, diagnostics *diag.Diagnostics) bool {
	if strings.TrimSpace(model.Name.ValueString()) == "" {
		diagnostics.AddError("Proxy name required", "Set a non-empty proxy name.")
	}
	if strings.TrimSpace(model.Hostname.ValueString()) == "" {
		diagnostics.AddError("Proxy hostname required", "Set a non-empty proxy hostname.")
	}
	if configured(model.Password) && model.Password.ValueString() != "" && strings.TrimSpace(model.Username.ValueString()) == "" {
		diagnostics.AddAttributeError(path.Root("username"), "Proxy username required", "Set a username when configuring a proxy password.")
	}
	return !diagnostics.HasError()
}
func proxyPayload(model proxyModel, id int64) proxyAPI {
	return proxyAPI{ID: id, Name: strings.TrimSpace(model.Name.ValueString()), Type: model.Type.ValueString(), Hostname: strings.TrimSpace(model.Hostname.ValueString()), Port: model.Port.ValueInt64(), Username: model.Username.ValueString(), Password: model.Password.ValueString()}
}
func (r *proxyResource) refresh(ctx context.Context, state *proxyModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid proxy state", "The proxy has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/settings/proxy/"+strconv.FormatInt(id, 10), "proxy", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current proxyAPI
	if json.Unmarshal(body, &current) != nil || current.ID < 1 {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid proxy document.")
		return
	}
	if current.Type != "http" && current.Type != "socks4" && current.Type != "socks5" {
		diagnostics.AddError("Invalid Chaptarr response", fmt.Sprintf("Chaptarr returned unsupported proxy type %q.", current.Type))
		return
	}
	state.ID, state.Name, state.Type, state.Hostname, state.Port = types.StringValue(strconv.FormatInt(current.ID, 10)), types.StringValue(current.Name), types.StringValue(current.Type), types.StringValue(current.Hostname), types.Int64Value(current.Port)
	state.Username = types.StringValue(current.Username)
	state.Password = types.StringNull()
	diagnostics.Append(target.Set(ctx, state)...)
}
