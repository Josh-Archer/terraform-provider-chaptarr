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

var _ resource.ResourceWithImportState = &downloadClientResource{}

type downloadClientResource struct{ client *client.Client }
type downloadClientModel struct {
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
	Protocol            types.String `tfsdk:"protocol"`
	Priority            types.Int64  `tfsdk:"priority"`
	AudiobookTags       types.Set    `tfsdk:"audiobook_tags"`
	EbookTags           types.Set    `tfsdk:"ebook_tags"`
	RemoveCompleted     types.Bool   `tfsdk:"remove_completed_downloads"`
	RemoveFailed        types.Bool   `tfsdk:"remove_failed_downloads"`
	CopyUnmanaged       types.Bool   `tfsdk:"copy_unmanaged_downloads"`
}
type downloadClientAPI struct {
	integrationBaseAPI
	Protocol        string  `json:"protocol"`
	Priority        int64   `json:"priority"`
	AudiobookTags   []int64 `json:"audiobookTags"`
	EbookTags       []int64 `json:"ebookTags"`
	RemoveCompleted bool    `json:"removeCompletedDownloads"`
	RemoveFailed    bool    `json:"removeFailedDownloads"`
	CopyUnmanaged   bool    `json:"copyUnmanagedDownloads"`
}

func newDownloadClientResource() resource.Resource { return &downloadClientResource{} }
func (r *downloadClientResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_download_client"
}
func (r *downloadClientResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	a := integrationBaseAttributes()
	a["protocol"] = schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("usenet", "torrent")}}
	a["priority"] = schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.Between(1, 50)}}
	a["audiobook_tags"] = schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type, MarkdownDescription: "Omit to preserve audiobook tag routing managed outside Terraform."}
	a["ebook_tags"] = schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type, MarkdownDescription: "Omit to preserve ebook tag routing managed outside Terraform."}
	a["tags"] = schema.SetAttribute{Computed: true, ElementType: types.Int64Type, MarkdownDescription: "Computed union of audiobook_tags and ebook_tags."}
	a["remove_completed_downloads"] = schema.BoolAttribute{Required: true}
	a["remove_failed_downloads"] = schema.BoolAttribute{Required: true}
	a["copy_unmanaged_downloads"] = schema.BoolAttribute{Required: true}
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr download client. Dynamic settings are schema-validated; credentials are apply-only. Connectivity tests and downloads are never invoked.", Attributes: a}
}
func (r *downloadClientResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *downloadClientResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan downloadClientModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadIntegrationSecrets(ctx, req.Config, &plan.SecretFields, &resp.Diagnostics)
	payload := downloadClientPayload(ctx, plan, 0, &resp.Diagnostics)
	if !validateIntegrationActivation(plan.Enable, plan.TestOnApply, &resp.Diagnostics) || resp.Diagnostics.HasError() || !validateIntegrationFields(ctx, r.client, "/api/v1/downloadclient/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/downloadclient", payload, "download client", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *downloadClientResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state downloadClientModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *downloadClientResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan downloadClientModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadIntegrationSecrets(ctx, req.Config, &plan.SecretFields, &resp.Diagnostics)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	payload := downloadClientPayload(ctx, plan, id, &resp.Diagnostics)
	if !validateIntegrationActivation(plan.Enable, plan.TestOnApply, &resp.Diagnostics) || resp.Diagnostics.HasError() || !validateIntegrationFields(ctx, r.client, "/api/v1/downloadclient/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &resp.Diagnostics) {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/downloadclient/"+strconv.FormatInt(id, 10), payload, "download client", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}
func (r *downloadClientResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state downloadClientModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/downloadclient/", state.ID, "download client", &resp.Diagnostics)
	}
}
func (r *downloadClientResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "download client", &resp.State, &resp.Diagnostics)
}
func downloadClientPayload(ctx context.Context, plan downloadClientModel, id int64, diagnostics *diag.Diagnostics) downloadClientAPI {
	base := integrationBasePayload(ctx, id, plan.Name, plan.Implementation, plan.ConfigContract, plan.Enable, plan.Tags, plan.FieldValuesJSON, plan.SecretFields, diagnostics)
	audiobookTags := setInt64Values(ctx, plan.AudiobookTags, diagnostics)
	ebookTags := setInt64Values(ctx, plan.EbookTags, diagnostics)
	base.Tags = append(append([]int64{}, audiobookTags...), ebookTags...)
	return downloadClientAPI{integrationBaseAPI: base, Protocol: plan.Protocol.ValueString(), Priority: plan.Priority.ValueInt64(), AudiobookTags: audiobookTags, EbookTags: ebookTags, RemoveCompleted: valueBool(plan.RemoveCompleted), RemoveFailed: valueBool(plan.RemoveFailed), CopyUnmanaged: valueBool(plan.CopyUnmanaged)}
}
func (r *downloadClientResource) refresh(ctx context.Context, state *downloadClientModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/downloadclient/"+strconv.FormatInt(id, 10), "download client", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current downloadClientAPI
	if json.Unmarshal(body, &current) != nil || current.ID < 1 || strings.TrimSpace(current.Name) == "" {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid download-client document.")
		return
	}
	setIntegrationBaseState(ctx, current.integrationBaseAPI, &state.ID, &state.Name, &state.ImplementationName, &state.Implementation, &state.ConfigContract, &state.Enable, &state.Tags, &state.FieldValuesJSON, &state.FieldValuesSHA256, &state.SecretFields, &state.ProtectedFieldNames, diagnostics)
	normalizeIntegrationTestAuthorization(&state.TestOnApply)
	state.Protocol, state.Priority = types.StringValue(current.Protocol), types.Int64Value(current.Priority)
	state.AudiobookTags, state.EbookTags = setInt64State(ctx, current.AudiobookTags, diagnostics), setInt64State(ctx, current.EbookTags, diagnostics)
	state.RemoveCompleted, state.RemoveFailed, state.CopyUnmanaged = types.BoolValue(current.RemoveCompleted), types.BoolValue(current.RemoveFailed), types.BoolValue(current.CopyUnmanaged)
	diagnostics.Append(target.Set(ctx, state)...)
}
