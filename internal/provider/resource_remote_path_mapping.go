package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                = &remotePathMappingResource{}
	_ resource.ResourceWithImportState = &remotePathMappingResource{}
)

type remotePathMappingResource struct {
	client *client.Client
}

type remotePathMappingModel struct {
	ID                           types.String `tfsdk:"id"`
	DownloadClientID             types.Int64  `tfsdk:"download_client_id"`
	Host                         types.String `tfsdk:"host"`
	RemotePath                   types.String `tfsdk:"remote_path"`
	LocalPath                    types.String `tfsdk:"local_path"`
	TestBeforeApply              types.Bool   `tfsdk:"test_before_apply"`
	LastTestIsMapped             types.Bool   `tfsdk:"last_test_is_mapped"`
	LastTestLocalPathExists      types.Bool   `tfsdk:"last_test_local_path_exists"`
	LastTestLocalPathWritable    types.Bool   `tfsdk:"last_test_local_path_writable"`
	LastTestDownloadClientProbed types.Bool   `tfsdk:"last_test_download_client_probed"`
	LastTestError                types.String `tfsdk:"last_test_error"`
}

type remotePathMappingAPI struct {
	ID               int64  `json:"id,omitempty"`
	DownloadClientID int64  `json:"downloadClientId"`
	Host             string `json:"host"`
	RemotePath       string `json:"remotePath"`
	LocalPath        string `json:"localPath"`
}

type remotePathMappingTestAPI struct {
	DownloadClientID          int64  `json:"downloadClientId"`
	Host                      string `json:"host"`
	RemotePath                string `json:"remotePath"`
	LocalPath                 string `json:"localPath"`
	IsMapped                  bool   `json:"isMapped"`
	LocalPathExists           bool   `json:"localPathExists"`
	LocalPathWritable         bool   `json:"localPathWritable"`
	DownloadClientPathChecked bool   `json:"downloadClientPathChecked"`
	DownloadClientTestError   string `json:"downloadClientTestError"`
}

func newRemotePathMappingResource() resource.Resource { return &remotePathMappingResource{} }

func (r *remotePathMappingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_remote_path_mapping"
}

func (r *remotePathMappingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Chaptarr remote-path mapping. Optional connection testing runs only during create/update apply, never during plan, refresh, or destroy.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"download_client_id": schema.Int64Attribute{
				Optional: true, Computed: true,
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
				MarkdownDescription: "Configured download-client identifier, or zero when matching by host.",
			},
			"host":        schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Download-client host. Required when download_client_id is zero."},
			"remote_path": schema.StringAttribute{Required: true, MarkdownDescription: "Remote download-client path."},
			"local_path":  schema.StringAttribute{Required: true, MarkdownDescription: "Existing local Chaptarr path."},
			"test_before_apply": schema.BoolAttribute{
				Optional: true, MarkdownDescription: "Opt in to Chaptarr's network/filesystem connection probe before create or update. Defaults to false.",
			},
			"last_test_is_mapped":              schema.BoolAttribute{Computed: true},
			"last_test_local_path_exists":      schema.BoolAttribute{Computed: true},
			"last_test_local_path_writable":    schema.BoolAttribute{Computed: true},
			"last_test_download_client_probed": schema.BoolAttribute{Computed: true},
			"last_test_error":                  schema.StringAttribute{Computed: true, MarkdownDescription: "Sanitized generic error returned by the optional probe."},
		},
	}
}

func (r *remotePathMappingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *remotePathMappingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan remotePathMappingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !r.validateModel(plan, &resp.Diagnostics) {
		return
	}
	if !r.test(ctx, &plan, &resp.Diagnostics) {
		return
	}
	payload := mappingPayload(plan, 0)
	body, err := json.Marshal(payload)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode remote-path mapping", "The request could not be encoded.")
		return
	}
	response, err := r.client.Do(ctx, http.MethodPost, "/api/v1/remotepathmapping", body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create remote-path mapping", err.Error())
		return
	}
	id, err := createdIdentifier(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", err.Error())
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *remotePathMappingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state remotePathMappingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
}

func (r *remotePathMappingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan remotePathMappingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !r.validateModel(plan, &resp.Diagnostics) {
		return
	}
	if !r.test(ctx, &plan, &resp.Diagnostics) {
		return
	}
	id, ok := positiveModelID(plan.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid remote-path mapping state", "The mapping has no valid numeric identifier.")
		return
	}
	body, err := json.Marshal(mappingPayload(plan, id))
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode remote-path mapping", "The request could not be encoded.")
		return
	}
	if _, err := r.client.Do(ctx, http.MethodPut, "/api/v1/remotepathmapping/"+strconv.FormatInt(id, 10), body); err != nil {
		resp.Diagnostics.AddError("Unable to update remote-path mapping", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *remotePathMappingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state remotePathMappingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, ok := positiveModelID(state.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid remote-path mapping state", "The mapping has no valid numeric identifier.")
		return
	}
	if _, err := r.client.Do(ctx, http.MethodDelete, "/api/v1/remotepathmapping/"+strconv.FormatInt(id, 10), nil); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Unable to delete remote-path mapping", err.Error())
	}
}

func (r *remotePathMappingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if err != nil || id < 1 {
		resp.Diagnostics.AddError("Invalid import identifier", "Use the positive numeric Chaptarr mapping identifier.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strconv.FormatInt(id, 10))...)
}

func (r *remotePathMappingResource) validateModel(model remotePathMappingModel, diagnostics *diag.Diagnostics) bool {
	downloadClientID := int64(0)
	if !model.DownloadClientID.IsNull() && !model.DownloadClientID.IsUnknown() {
		downloadClientID = model.DownloadClientID.ValueInt64()
	}
	host := ""
	if !model.Host.IsNull() && !model.Host.IsUnknown() {
		host = strings.TrimSpace(model.Host.ValueString())
	}
	if downloadClientID == 0 && host == "" {
		diagnostics.AddAttributeError(path.Root("host"), "Host required", "Set `host` when `download_client_id` is zero.")
	}
	return !diagnostics.HasError()
}

func (r *remotePathMappingResource) test(ctx context.Context, model *remotePathMappingModel, diagnostics *diag.Diagnostics) bool {
	if model.TestBeforeApply.IsNull() || model.TestBeforeApply.IsUnknown() || !model.TestBeforeApply.ValueBool() {
		model.TestBeforeApply = types.BoolValue(false)
		model.LastTestIsMapped = types.BoolNull()
		model.LastTestLocalPathExists = types.BoolNull()
		model.LastTestLocalPathWritable = types.BoolNull()
		model.LastTestDownloadClientProbed = types.BoolNull()
		model.LastTestError = types.StringNull()
		return true
	}
	payload := mappingPayload(*model, 0)
	body, err := json.Marshal(payload)
	if err != nil {
		diagnostics.AddError("Unable to encode remote-path test", "The test request could not be encoded.")
		return false
	}
	response, err := r.client.Do(ctx, http.MethodPost, "/api/v1/remotepathmapping/test", body)
	if err != nil {
		diagnostics.AddError("Remote-path test failed", err.Error())
		return false
	}
	var result remotePathMappingTestAPI
	if err := json.Unmarshal(response.Body, &result); err != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid remote-path test document.")
		return false
	}
	model.LastTestIsMapped = types.BoolValue(result.IsMapped)
	model.LastTestLocalPathExists = types.BoolValue(result.LocalPathExists)
	model.LastTestLocalPathWritable = types.BoolValue(result.LocalPathWritable)
	model.LastTestDownloadClientProbed = types.BoolValue(result.DownloadClientPathChecked)
	if result.DownloadClientTestError == "" {
		model.LastTestError = types.StringNull()
	} else {
		model.LastTestError = types.StringValue(result.DownloadClientTestError)
	}
	downloadClientProbeFailed := payload.DownloadClientID > 0 && !result.DownloadClientPathChecked
	if !result.IsMapped || !result.LocalPathExists || !result.LocalPathWritable || downloadClientProbeFailed || result.DownloadClientTestError != "" {
		diagnostics.AddError(
			"Remote-path test failed",
			fmt.Sprintf("Chaptarr reported is_mapped=%t, local_path_exists=%t, local_path_writable=%t, and download_client_probed=%t. See the access-controlled Chaptarr logs for further diagnostics.", result.IsMapped, result.LocalPathExists, result.LocalPathWritable, result.DownloadClientPathChecked),
		)
		return false
	}
	return true
}

func (r *remotePathMappingResource) refresh(ctx context.Context, state *remotePathMappingModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid remote-path mapping state", "The mapping has no valid numeric identifier.")
		return
	}
	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/remotepathmapping/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			target.RemoveResource(ctx)
			return
		}
		diagnostics.AddError("Unable to read remote-path mapping", err.Error())
		return
	}
	var current remotePathMappingAPI
	if err := json.Unmarshal(response.Body, &current); err != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid remote-path mapping document.")
		return
	}
	state.ID = types.StringValue(strconv.FormatInt(current.ID, 10))
	state.DownloadClientID = types.Int64Value(current.DownloadClientID)
	state.Host = types.StringValue(current.Host)
	state.RemotePath = types.StringValue(current.RemotePath)
	state.LocalPath = types.StringValue(current.LocalPath)
	if state.TestBeforeApply.IsNull() || state.TestBeforeApply.IsUnknown() {
		state.TestBeforeApply = types.BoolValue(false)
	}
	if state.LastTestIsMapped.IsUnknown() {
		state.LastTestIsMapped = types.BoolNull()
	}
	if state.LastTestLocalPathExists.IsUnknown() {
		state.LastTestLocalPathExists = types.BoolNull()
	}
	if state.LastTestLocalPathWritable.IsUnknown() {
		state.LastTestLocalPathWritable = types.BoolNull()
	}
	if state.LastTestDownloadClientProbed.IsUnknown() {
		state.LastTestDownloadClientProbed = types.BoolNull()
	}
	if state.LastTestError.IsUnknown() {
		state.LastTestError = types.StringNull()
	}
	diagnostics.Append(target.Set(ctx, state)...)
}

func mappingPayload(model remotePathMappingModel, id int64) remotePathMappingAPI {
	payload := remotePathMappingAPI{ID: id, RemotePath: model.RemotePath.ValueString(), LocalPath: model.LocalPath.ValueString()}
	if !model.DownloadClientID.IsNull() && !model.DownloadClientID.IsUnknown() {
		payload.DownloadClientID = model.DownloadClientID.ValueInt64()
	}
	if !model.Host.IsNull() && !model.Host.IsUnknown() {
		payload.Host = strings.TrimSpace(model.Host.ValueString())
	}
	return payload
}

func positiveModelID(value types.String) (int64, bool) {
	if value.IsNull() || value.IsUnknown() {
		return 0, false
	}
	id, err := strconv.ParseInt(value.ValueString(), 10, 64)
	return id, err == nil && id > 0
}

func createdIdentifier(body []byte) (int64, error) {
	var object struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(body, &object) == nil && object.ID > 0 {
		return object.ID, nil
	}
	var number json.Number
	if json.Unmarshal(body, &number) == nil {
		if id, err := number.Int64(); err == nil && id > 0 {
			return id, nil
		}
	}
	return 0, errors.New("chaptarr did not return a positive numeric identifier")
}
