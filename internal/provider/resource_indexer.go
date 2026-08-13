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
	_ resource.Resource                = &indexerResource{}
	_ resource.ResourceWithImportState = &indexerResource{}
)

type indexerResource struct {
	client *client.Client
}

type indexerModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Enable         types.Bool   `tfsdk:"enable"`
	Protocol       types.String `tfsdk:"protocol"`
	Priority       types.Int64  `tfsdk:"priority"`
	Implementation types.String `tfsdk:"implementation"`
	ConfigContract types.String `tfsdk:"config_contract"`
	AppProfileID   types.Int64  `tfsdk:"app_profile_id"`
	Fields         []fieldModel `tfsdk:"field"`
}

type indexerAPI struct {
	ID             int64      `json:"id,omitempty"`
	Name           string     `json:"name"`
	Enable         bool       `json:"enable"`
	Protocol       string     `json:"protocol"`
	Priority       int64      `json:"priority"`
	Implementation string     `json:"implementation"`
	ConfigContract string     `json:"configContract"`
	AppProfileID   int64      `json:"appProfileId,omitempty"`
	Fields         []fieldAPI `json:"fields"`
}

func newIndexerResource() resource.Resource {
	return &indexerResource{}
}

func (r *indexerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_indexer"
}

func (r *indexerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Chaptarr indexer registration (e.g. Torznab, Newznab, Prowlarr).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Indexer identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Indexer display name.",
			},
			"enable": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the indexer is active.",
			},
			"protocol": schema.StringAttribute{
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf("torrent", "usenet")},
				MarkdownDescription: "Protocol ('torrent' or 'usenet').",
			},
			"priority": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Indexer priority order (default 25).",
			},
			"implementation": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Implementation engine (e.g. 'Torznab', 'Newznab').",
			},
			"config_contract": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Config contract name (e.g. 'TorznabSettings', 'NewznabSettings').",
			},
			"app_profile_id": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Associated application profile ID (default 1).",
			},
		},
		Blocks: map[string]schema.Block{
			"field": schema.SetNestedBlock{
				MarkdownDescription: "Name/value setting fields for the indexer.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Setting name (e.g. 'baseUrl', 'apiKey', 'categories').",
						},
						"value": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Setting non-sensitive value.",
						},
						"sensitive_value": schema.StringAttribute{
							Optional:            true,
							Sensitive:           true,
							WriteOnly:           true,
							MarkdownDescription: "Setting sensitive value (apiKeys, passkeys, tokens). Never stored in state.",
						},
					},
				},
			},
		},
	}
}

func (r *indexerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *indexerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan indexerModel
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

	response, err := r.client.Do(ctx, http.MethodPost, "/api/v1/indexer", body)
	if err != nil {
		resp.Diagnostics.AddError("Create Indexer Failed", err.Error())
		return
	}

	var created indexerAPI
	if err := json.Unmarshal(response.Body, &created); err != nil {
		resp.Diagnostics.AddError("Unmarshal Response Failed", err.Error())
		return
	}

	r.apiToModel(&created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *indexerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state indexerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/indexer/"+state.ID.ValueString(), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read Indexer Failed", err.Error())
		return
	}

	var existing indexerAPI
	if err := json.Unmarshal(response.Body, &existing); err != nil {
		resp.Diagnostics.AddError("Unmarshal Response Failed", err.Error())
		return
	}

	r.apiToModel(&existing, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *indexerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan indexerModel
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

	response, err := r.client.Do(ctx, http.MethodPut, "/api/v1/indexer/"+plan.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update Indexer Failed", err.Error())
		return
	}

	var updated indexerAPI
	if err := json.Unmarshal(response.Body, &updated); err != nil {
		resp.Diagnostics.AddError("Unmarshal Response Failed", err.Error())
		return
	}

	r.apiToModel(&updated, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *indexerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state indexerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.Do(ctx, http.MethodDelete, "/api/v1/indexer/"+state.ID.ValueString(), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Delete Indexer Failed", err.Error())
	}
}

func (r *indexerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *indexerResource) modelToAPI(m *indexerModel) indexerAPI {
	enable := true
	if !m.Enable.IsNull() {
		enable = m.Enable.ValueBool()
	}
	priority := int64(25)
	if !m.Priority.IsNull() {
		priority = m.Priority.ValueInt64()
	}
	appProfileID := int64(1)
	if !m.AppProfileID.IsNull() {
		appProfileID = m.AppProfileID.ValueInt64()
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

	return indexerAPI{
		Name:           m.Name.ValueString(),
		Enable:         enable,
		Protocol:       m.Protocol.ValueString(),
		Priority:       priority,
		Implementation: m.Implementation.ValueString(),
		ConfigContract: m.ConfigContract.ValueString(),
		AppProfileID:   appProfileID,
		Fields:         apiFields,
	}
}

func (r *indexerResource) apiToModel(api *indexerAPI, m *indexerModel) {
	m.ID = types.StringValue(strconv.FormatInt(api.ID, 10))
	m.Name = types.StringValue(api.Name)
	m.Enable = types.BoolValue(api.Enable)
	m.Protocol = types.StringValue(api.Protocol)
	m.Priority = types.Int64Value(api.Priority)
	m.Implementation = types.StringValue(api.Implementation)
	m.ConfigContract = types.StringValue(api.ConfigContract)
	m.AppProfileID = types.Int64Value(api.AppProfileID)

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
