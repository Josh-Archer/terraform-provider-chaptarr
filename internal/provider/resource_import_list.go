package provider

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithImportState = &importListResource{}

type importListResource struct{ client *client.Client }
type importListModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	ImplementationName  types.String `tfsdk:"implementation_name"`
	Implementation      types.String `tfsdk:"implementation"`
	ConfigContract      types.String `tfsdk:"config_contract"`
	Enable              types.Bool   `tfsdk:"enable"`
	TestOnApply         types.Bool   `tfsdk:"test_on_apply"`
	Tags                types.Set    `tfsdk:"tags"`
	FieldValuesJSON     types.String `tfsdk:"field_values_json"`
	FieldValuesSHA256   types.String `tfsdk:"field_values_sha256"`
	SecretFields        types.Map    `tfsdk:"secret_fields"`
	ProtectedFieldNames types.Set    `tfsdk:"protected_field_names"`
	EnableAutomaticAdd  types.Bool   `tfsdk:"enable_automatic_add"`
	ShouldMonitor       types.String `tfsdk:"should_monitor"`
	ShouldMonitorExist  types.Bool   `tfsdk:"should_monitor_existing"`
	ShouldSearch        types.Bool   `tfsdk:"should_search"`
	RootFolderPath      types.String `tfsdk:"root_folder_path"`
	MonitorNewItems     types.String `tfsdk:"monitor_new_items"`
	QualityProfileID    types.Int64  `tfsdk:"quality_profile_id"`
	MetadataProfileID   types.Int64  `tfsdk:"metadata_profile_id"`
	ListType            types.String `tfsdk:"list_type"`
	MinRefreshInterval  types.String `tfsdk:"minimum_refresh_interval"`
	HardcoverUsername   types.String `tfsdk:"hardcover_username"`
	HardcoverAvatarURL  types.String `tfsdk:"hardcover_avatar_url"`
}
type importListAPI struct {
	integrationBaseAPI
	EnableAutomaticAdd bool   `json:"enableAutomaticAdd"`
	ShouldMonitor      string `json:"shouldMonitor"`
	ShouldMonitorExist bool   `json:"shouldMonitorExisting"`
	ShouldSearch       bool   `json:"shouldSearch"`
	RootFolderPath     string `json:"rootFolderPath"`
	MonitorNewItems    string `json:"monitorNewItems"`
	QualityProfileID   int64  `json:"qualityProfileId"`
	MetadataProfileID  int64  `json:"metadataProfileId"`
	ListType           string `json:"listType"`
	MinRefreshInterval string `json:"minRefreshInterval"`
	HardcoverUsername  string `json:"hardcoverUsername"`
	HardcoverAvatarURL string `json:"hardcoverAvatarUrl"`
}

func newImportListResource() resource.Resource { return &importListResource{} }
func (r *importListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_import_list"
}
func (r *importListResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	a := integrationBaseAttributes()
	a["enable_automatic_add"] = schema.BoolAttribute{Required: true}
	a["should_monitor"] = schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("none", "specificBook", "entireAuthor")}}
	a["should_monitor_existing"] = schema.BoolAttribute{Required: true}
	a["should_search"] = schema.BoolAttribute{Required: true}
	a["root_folder_path"] = schema.StringAttribute{Required: true}
	a["monitor_new_items"] = schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("all", "none", "new")}}
	a["quality_profile_id"] = schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}
	a["metadata_profile_id"] = schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}
	a["list_type"] = schema.StringAttribute{Optional: true, Computed: true, Validators: []validator.String{stringvalidator.OneOf("program", "goodreads", "other")}}
	a["minimum_refresh_interval"] = schema.StringAttribute{Optional: true, Computed: true}
	a["hardcover_username"] = schema.StringAttribute{Computed: true}
	a["hardcover_avatar_url"] = schema.StringAttribute{Computed: true}
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr import list. Dynamic nested settings are canonical and schema-validated; credentials are apply-only. Automatic provider tests require explicit test_on_apply authorization.", Attributes: a}
}
func (r *importListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *importListResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan importListModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadIntegrationSecrets(ctx, req.Config, &plan.SecretFields, &resp.Diagnostics)
	payload := importListPayload(ctx, plan, 0, &resp.Diagnostics)
	if !validateIntegrationActivation(plan.Enable, plan.TestOnApply, &resp.Diagnostics) || resp.Diagnostics.HasError() || !validateIntegrationFields(ctx, r.client, "/api/v1/importlist/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/importlist", payload, "import list", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *importListResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state importListModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *importListResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan importListModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadIntegrationSecrets(ctx, req.Config, &plan.SecretFields, &resp.Diagnostics)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	payload := importListPayload(ctx, plan, id, &resp.Diagnostics)
	if !validateIntegrationActivation(plan.Enable, plan.TestOnApply, &resp.Diagnostics) || resp.Diagnostics.HasError() || !validateIntegrationFields(ctx, r.client, "/api/v1/importlist/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &resp.Diagnostics) {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/importlist/"+strconv.FormatInt(id, 10), payload, "import list", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}
func (r *importListResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state importListModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/importlist/", state.ID, "import list", &resp.Diagnostics)
	}
}
func (r *importListResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "import list", &resp.State, &resp.Diagnostics)
}
func importListPayload(ctx context.Context, p importListModel, id int64, d *diag.Diagnostics) importListAPI {
	if strings.TrimSpace(p.RootFolderPath.ValueString()) == "" {
		d.AddError("Root folder required", "root_folder_path must not be empty.")
	}
	return importListAPI{integrationBaseAPI: integrationBasePayload(ctx, id, p.Name, p.Implementation, p.ConfigContract, p.Enable, p.Tags, p.FieldValuesJSON, p.SecretFields, d), EnableAutomaticAdd: valueBool(p.EnableAutomaticAdd), ShouldMonitor: p.ShouldMonitor.ValueString(), ShouldMonitorExist: valueBool(p.ShouldMonitorExist), ShouldSearch: valueBool(p.ShouldSearch), RootFolderPath: strings.TrimSpace(p.RootFolderPath.ValueString()), MonitorNewItems: p.MonitorNewItems.ValueString(), QualityProfileID: p.QualityProfileID.ValueInt64(), MetadataProfileID: p.MetadataProfileID.ValueInt64(), ListType: p.ListType.ValueString(), MinRefreshInterval: p.MinRefreshInterval.ValueString()}
}
func (r *importListResource) refresh(ctx context.Context, s *importListModel, target *tfsdk.State, d *diag.Diagnostics) {
	id, ok := positiveModelID(s.ID)
	if !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/importlist/"+strconv.FormatInt(id, 10), "import list", target, d)
	if !found || d.HasError() {
		return
	}
	var c importListAPI
	if json.Unmarshal(body, &c) != nil || c.ID < 1 || strings.TrimSpace(c.Name) == "" {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid import-list document.")
		return
	}
	setIntegrationBaseState(ctx, c.integrationBaseAPI, &s.ID, &s.Name, &s.ImplementationName, &s.Implementation, &s.ConfigContract, &s.Enable, &s.Tags, &s.FieldValuesJSON, &s.FieldValuesSHA256, &s.SecretFields, &s.ProtectedFieldNames, d)
	normalizeIntegrationTestAuthorization(&s.TestOnApply)
	s.EnableAutomaticAdd = types.BoolValue(c.EnableAutomaticAdd)
	s.ShouldMonitor = types.StringValue(c.ShouldMonitor)
	s.ShouldMonitorExist = types.BoolValue(c.ShouldMonitorExist)
	s.ShouldSearch = types.BoolValue(c.ShouldSearch)
	s.RootFolderPath = types.StringValue(c.RootFolderPath)
	s.MonitorNewItems = types.StringValue(c.MonitorNewItems)
	s.QualityProfileID = types.Int64Value(c.QualityProfileID)
	s.MetadataProfileID = types.Int64Value(c.MetadataProfileID)
	s.ListType = types.StringValue(c.ListType)
	s.MinRefreshInterval = types.StringValue(c.MinRefreshInterval)
	s.HardcoverUsername = types.StringValue(c.HardcoverUsername)
	s.HardcoverAvatarURL = types.StringValue(c.HardcoverAvatarURL)
	d.Append(target.Set(ctx, s)...)
}
