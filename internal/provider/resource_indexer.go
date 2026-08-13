package provider

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithImportState = &indexerResource{}

type indexerResource struct{ client *client.Client }
type indexerModel struct {
	ID                      types.String `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	ImplementationName      types.String `tfsdk:"implementation_name"`
	Implementation          types.String `tfsdk:"implementation"`
	ConfigContract          types.String `tfsdk:"config_contract"`
	Enable                  types.Bool   `tfsdk:"enable"`
	TestOnApply             types.Bool   `tfsdk:"test_on_apply"`
	Tags                    types.Set    `tfsdk:"tags"`
	FieldValuesJSON         types.String `tfsdk:"field_values_json"`
	FieldValuesSHA256       types.String `tfsdk:"field_values_sha256"`
	SecretFields            types.Map    `tfsdk:"secret_fields"`
	ProtectedFieldNames     types.Set    `tfsdk:"protected_field_names"`
	EnableRSS               types.Bool   `tfsdk:"enable_rss"`
	EnableAutomaticSearch   types.Bool   `tfsdk:"enable_automatic_search"`
	EnableInteractiveSearch types.Bool   `tfsdk:"enable_interactive_search"`
	SupportsRSS             types.Bool   `tfsdk:"supports_rss"`
	SupportsSearch          types.Bool   `tfsdk:"supports_search"`
	Protocol                types.String `tfsdk:"protocol"`
	Priority                types.Int64  `tfsdk:"priority"`
	DownloadClientID        types.Int64  `tfsdk:"download_client_id"`
	ProxyID                 types.Int64  `tfsdk:"proxy_id"`
}
type indexerAPI struct {
	integrationBaseAPI
	EnableRSS               bool   `json:"enableRss"`
	EnableAutomaticSearch   bool   `json:"enableAutomaticSearch"`
	EnableInteractiveSearch bool   `json:"enableInteractiveSearch"`
	SupportsRSS             bool   `json:"supportsRss"`
	SupportsSearch          bool   `json:"supportsSearch"`
	Protocol                string `json:"protocol"`
	Priority                int64  `json:"priority"`
	DownloadClientID        int64  `json:"downloadClientId"`
	ProxyID                 *int64 `json:"proxyId"`
}

func newIndexerResource() resource.Resource { return &indexerResource{} }
func (r *indexerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_indexer"
}
func (r *indexerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	a := integrationBaseAttributes()
	a["enable_rss"] = schema.BoolAttribute{Required: true}
	a["enable_automatic_search"] = schema.BoolAttribute{Required: true}
	a["enable_interactive_search"] = schema.BoolAttribute{Required: true}
	a["supports_rss"] = schema.BoolAttribute{Computed: true}
	a["supports_search"] = schema.BoolAttribute{Computed: true}
	a["protocol"] = schema.StringAttribute{Computed: true}
	a["priority"] = schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.Between(1, 50)}}
	a["download_client_id"] = schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(0)}}
	a["proxy_id"] = schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr indexer. Dynamic settings are schema-validated; credentials are apply-only. Test and release-push actions are never invoked.", Attributes: a}
}
func (r *indexerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *indexerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var p indexerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	loadIntegrationSecrets(ctx, req.Config, &p.SecretFields, &resp.Diagnostics)
	payload := indexerPayload(ctx, p, 0, &resp.Diagnostics)
	if !validateIntegrationActivation(p.Enable, p.TestOnApply, &resp.Diagnostics) || resp.Diagnostics.HasError() || !validateIntegrationFields(ctx, r.client, "/api/v1/indexer/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/indexer", payload, "indexer", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	p.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &p, &resp.State, &resp.Diagnostics)
}
func (r *indexerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var s indexerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &s, &resp.State, &resp.Diagnostics)
	}
}
func (r *indexerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var p indexerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	loadIntegrationSecrets(ctx, req.Config, &p.SecretFields, &resp.Diagnostics)
	id, ok := positiveModelID(p.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	payload := indexerPayload(ctx, p, id, &resp.Diagnostics)
	if !validateIntegrationActivation(p.Enable, p.TestOnApply, &resp.Diagnostics) || resp.Diagnostics.HasError() || !validateIntegrationFields(ctx, r.client, "/api/v1/indexer/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &resp.Diagnostics) {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/indexer/"+strconv.FormatInt(id, 10), payload, "indexer", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &p, &resp.State, &resp.Diagnostics)
	}
}
func (r *indexerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var s indexerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/indexer/", s.ID, "indexer", &resp.Diagnostics)
	}
}
func (r *indexerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "indexer", &resp.State, &resp.Diagnostics)
}
func indexerPayload(ctx context.Context, p indexerModel, id int64, d *diag.Diagnostics) indexerAPI {
	base := integrationBasePayload(ctx, id, p.Name, p.Implementation, p.ConfigContract, p.Enable, p.Tags, p.FieldValuesJSON, p.SecretFields, d)
	var proxy *int64
	if !p.ProxyID.IsNull() && !p.ProxyID.IsUnknown() {
		v := p.ProxyID.ValueInt64()
		proxy = &v
	}
	return indexerAPI{integrationBaseAPI: base, EnableRSS: valueBool(p.EnableRSS), EnableAutomaticSearch: valueBool(p.EnableAutomaticSearch), EnableInteractiveSearch: valueBool(p.EnableInteractiveSearch), Priority: p.Priority.ValueInt64(), DownloadClientID: valueInt64(p.DownloadClientID), ProxyID: proxy}
}
func (r *indexerResource) refresh(ctx context.Context, s *indexerModel, target *tfsdk.State, d *diag.Diagnostics) {
	id, ok := positiveModelID(s.ID)
	if !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/indexer/"+strconv.FormatInt(id, 10), "indexer", target, d)
	if !found || d.HasError() {
		return
	}
	var current indexerAPI
	if json.Unmarshal(body, &current) != nil || current.ID < 1 || strings.TrimSpace(current.Name) == "" {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid indexer document.")
		return
	}
	setIntegrationBaseState(ctx, current.integrationBaseAPI, &s.ID, &s.Name, &s.ImplementationName, &s.Implementation, &s.ConfigContract, &s.Enable, &s.Tags, &s.FieldValuesJSON, &s.FieldValuesSHA256, &s.SecretFields, &s.ProtectedFieldNames, d)
	normalizeIntegrationTestAuthorization(&s.TestOnApply)
	s.EnableRSS = types.BoolValue(current.EnableRSS)
	s.EnableAutomaticSearch = types.BoolValue(current.EnableAutomaticSearch)
	s.EnableInteractiveSearch = types.BoolValue(current.EnableInteractiveSearch)
	s.SupportsRSS = types.BoolValue(current.SupportsRSS)
	s.SupportsSearch = types.BoolValue(current.SupportsSearch)
	s.Protocol = types.StringValue(current.Protocol)
	s.Priority = types.Int64Value(current.Priority)
	s.DownloadClientID = types.Int64Value(current.DownloadClientID)
	if current.ProxyID == nil {
		s.ProxyID = types.Int64Null()
	} else {
		s.ProxyID = types.Int64Value(*current.ProxyID)
	}
	d.Append(target.Set(ctx, s)...)
}
