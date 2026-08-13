package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &downloadClientResource{}
	_ resource.ResourceWithImportState = &downloadClientResource{}
)

type downloadClientResource struct {
	client *client.Client
}

type downloadClientModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	Enable                   types.Bool   `tfsdk:"enable"`
	Protocol                 types.String `tfsdk:"protocol"`
	Priority                 types.Int64  `tfsdk:"priority"`
	Implementation           types.String `tfsdk:"implementation"`
	ConfigContract           types.String `tfsdk:"config_contract"`
	RemoveCompletedDownloads types.Bool   `tfsdk:"remove_completed_downloads"`
	RemoveFailedDownloads    types.Bool   `tfsdk:"remove_failed_downloads"`
	Fields                   []fieldModel `tfsdk:"field"`
}

type fieldModel struct {
	Name           types.String `tfsdk:"name"`
	Value          types.String `tfsdk:"value"`
	SensitiveValue types.String `tfsdk:"sensitive_value"`
}

type downloadClientAPI struct {
	ID                       int64      `json:"id,omitempty"`
	Name                     string     `json:"name"`
	Enable                   bool       `json:"enable"`
	Protocol                 string     `json:"protocol"`
	Priority                 int64      `json:"priority"`
	Implementation           string     `json:"implementation"`
	ConfigContract           string     `json:"configContract"`
	RemoveCompletedDownloads bool       `json:"removeCompletedDownloads"`
	RemoveFailedDownloads    bool       `json:"removeFailedDownloads"`
	Fields                   []fieldAPI `json:"fields"`
}

type fieldAPI struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value,omitempty"`
}

func newDownloadClientResource() resource.Resource {
	return &downloadClientResource{}
}

func (r *downloadClientResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_download_client"
}

func (r *downloadClientResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Chaptarr download client registration (e.g. Transmission, SABnzbd, qBittorrent).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Download client identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Download client display name.",
			},
			"enable": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the download client is active.",
			},
			"protocol": schema.StringAttribute{
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf("torrent", "usenet")},
				MarkdownDescription: "Protocol ('torrent' or 'usenet').",
			},
			"priority": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Download priority order (default 1).",
			},
			"implementation": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Implementation engine (e.g. 'Transmission', 'Sabnzbd', 'QBittorrent').",
			},
			"config_contract": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Config contract name (e.g. 'TransmissionSettings', 'SabnzbdSettings', 'QBittorrentSettings').",
			},
			"remove_completed_downloads": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Automatically remove completed downloads from client list.",
			},
			"remove_failed_downloads": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Automatically remove failed downloads from client list.",
			},
		},
		Blocks: map[string]schema.Block{
			"field": schema.SetNestedBlock{
				MarkdownDescription: "Name/value setting fields for the download client.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Setting name (e.g. 'host', 'port', 'password').",
						},
						"value": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Setting non-sensitive value.",
						},
						"sensitive_value": schema.StringAttribute{
							Optional:            true,
							Sensitive:           true,
							WriteOnly:           true,
							MarkdownDescription: "Setting sensitive value (passwords, tokens, keys). Never stored in state.",
						},
					},
				},
			},
		},
	}
}

func (r *downloadClientResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *downloadClientResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan downloadClientModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := r.modelToAPI(&plan)
	body, err := json.Marshal(payload)
	if err != nil {
		resp.Diagnostics.AddError("Encode Request Failed", err.Error())
		return
	}

	response, err := r.client.Do(ctx, http.MethodPost, "/api/v1/downloadclient", body)
	if err != nil {
		resp.Diagnostics.AddError("Create Download Client Failed", err.Error())
		return
	}

	var created downloadClientAPI
	if err := json.Unmarshal(response.Body, &created); err != nil {
		resp.Diagnostics.AddError("Unmarshal Response Failed", err.Error())
		return
	}

	r.apiToModel(&created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *downloadClientResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state downloadClientModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/downloadclient/"+state.ID.ValueString(), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read Download Client Failed", err.Error())
		return
	}

	var existing downloadClientAPI
	if err := json.Unmarshal(response.Body, &existing); err != nil {
		resp.Diagnostics.AddError("Unmarshal Response Failed", err.Error())
		return
	}

	r.apiToModel(&existing, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *downloadClientResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan downloadClientModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := r.modelToAPI(&plan)
	id, _ := strconv.ParseInt(plan.ID.ValueString(), 10, 64)
	payload.ID = id

	body, err := json.Marshal(payload)
	if err != nil {
		resp.Diagnostics.AddError("Encode Request Failed", err.Error())
		return
	}

	response, err := r.client.Do(ctx, http.MethodPut, "/api/v1/downloadclient/"+plan.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update Download Client Failed", err.Error())
		return
	}

	var updated downloadClientAPI
	if err := json.Unmarshal(response.Body, &updated); err != nil {
		resp.Diagnostics.AddError("Unmarshal Response Failed", err.Error())
		return
	}

	r.apiToModel(&updated, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *downloadClientResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state downloadClientModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.Do(ctx, http.MethodDelete, "/api/v1/downloadclient/"+state.ID.ValueString(), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Delete Download Client Failed", err.Error())
	}
}

func (r *downloadClientResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *downloadClientResource) modelToAPI(m *downloadClientModel) downloadClientAPI {
	enable := true
	if !m.Enable.IsNull() {
		enable = m.Enable.ValueBool()
	}
	priority := int64(1)
	if !m.Priority.IsNull() {
		priority = m.Priority.ValueInt64()
	}
	removeCompleted := true
	if !m.RemoveCompletedDownloads.IsNull() {
		removeCompleted = m.RemoveCompletedDownloads.ValueBool()
	}
	removeFailed := true
	if !m.RemoveFailedDownloads.IsNull() {
		removeFailed = m.RemoveFailedDownloads.ValueBool()
	}

	apiFields := make([]fieldAPI, 0, len(m.Fields))
	for _, f := range m.Fields {
		val := f.Value.ValueString()
		if !f.SensitiveValue.IsNull() && f.SensitiveValue.ValueString() != "" {
			val = f.SensitiveValue.ValueString()
		}
		var jsonVal interface{} = val
		if num, err := strconv.ParseInt(val, 10, 64); err == nil {
			jsonVal = num
		} else if b, err := strconv.ParseBool(val); err == nil {
			jsonVal = b
		}
		apiFields = append(apiFields, fieldAPI{
			Name:  f.Name.ValueString(),
			Value: jsonVal,
		})
	}

	return downloadClientAPI{
		Name:                     m.Name.ValueString(),
		Enable:                   enable,
		Protocol:                 m.Protocol.ValueString(),
		Priority:                 priority,
		Implementation:           m.Implementation.ValueString(),
		ConfigContract:           m.ConfigContract.ValueString(),
		RemoveCompletedDownloads: removeCompleted,
		RemoveFailedDownloads:    removeFailed,
		Fields:                   apiFields,
	}
}

func (r *downloadClientResource) apiToModel(api *downloadClientAPI, m *downloadClientModel) {
	m.ID = types.StringValue(strconv.FormatInt(api.ID, 10))
	m.Name = types.StringValue(api.Name)
	m.Enable = types.BoolValue(api.Enable)
	m.Protocol = types.StringValue(api.Protocol)
	m.Priority = types.Int64Value(api.Priority)
	m.Implementation = types.StringValue(api.Implementation)
	m.ConfigContract = types.StringValue(api.ConfigContract)
	m.RemoveCompletedDownloads = types.BoolValue(api.RemoveCompletedDownloads)
	m.RemoveFailedDownloads = types.BoolValue(api.RemoveFailedDownloads)

	existingFields := make(map[string]fieldModel)
	for _, f := range m.Fields {
		existingFields[f.Name.ValueString()] = f
	}

	m.Fields = make([]fieldModel, 0, len(api.Fields))
	for _, f := range api.Fields {
		strVal := fmt.Sprintf("%v", f.Value)
		fm := fieldModel{
			Name:  types.StringValue(f.Name),
			Value: types.StringValue(strVal),
		}
		if ef, ok := existingFields[f.Name]; ok && !ef.SensitiveValue.IsNull() {
			fm.SensitiveValue = ef.SensitiveValue
		}
		m.Fields = append(m.Fields, fm)
	}
}
