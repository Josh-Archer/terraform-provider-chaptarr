package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithImportState = &metadataResource{}

type metadataResource struct{ client *client.Client }
type metadataModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	ImplementationName  types.String `tfsdk:"implementation_name"`
	Implementation      types.String `tfsdk:"implementation"`
	ConfigContract      types.String `tfsdk:"config_contract"`
	Enable              types.Bool   `tfsdk:"enable"`
	Tags                types.Set    `tfsdk:"tags"`
	FieldValuesJSON     types.String `tfsdk:"field_values_json"`
	FieldValuesSHA256   types.String `tfsdk:"field_values_sha256"`
	SecretFields        types.Map    `tfsdk:"secret_fields"`
	ProtectedFieldNames types.Set    `tfsdk:"protected_field_names"`
}
type providerFieldAPI struct {
	Order    int64  `json:"order,omitempty"`
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Privacy  string `json:"privacy,omitempty"`
	Value    any    `json:"value"`
	Type     string `json:"type,omitempty"`
	Advanced bool   `json:"advanced,omitempty"`
}
type metadataAPI struct {
	ID                 int64              `json:"id,omitempty"`
	Name               string             `json:"name"`
	Fields             []providerFieldAPI `json:"fields"`
	ImplementationName string             `json:"implementationName,omitempty"`
	Implementation     string             `json:"implementation"`
	ConfigContract     string             `json:"configContract"`
	Tags               []int64            `json:"tags"`
	Enable             bool               `json:"enable"`
}

func newMetadataResource() resource.Resource { return &metadataResource{} }
func (r *metadataResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metadata"
}
func (r *metadataResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr library metadata provider. Non-secret dynamic settings are canonical JSON. Password/API-key fields are accepted only through the Sensitive+WriteOnly secret_fields map and never stored. Enabling a provider makes Chaptarr validate/test it during apply; no tests run during plan or refresh.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "name": schema.StringAttribute{Required: true}, "implementation_name": schema.StringAttribute{Computed: true}, "implementation": schema.StringAttribute{Required: true}, "config_contract": schema.StringAttribute{Required: true}, "enable": schema.BoolAttribute{Required: true}, "tags": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type, MarkdownDescription: "Omit to preserve tags already managed outside Terraform; configure explicitly to replace the association set."}, "field_values_json": schema.StringAttribute{Required: true, MarkdownDescription: "Canonical JSON object mapping non-secret field names to values."}, "field_values_sha256": schema.StringAttribute{Computed: true}, "secret_fields": schema.MapAttribute{Optional: true, Sensitive: true, WriteOnly: true, ElementType: types.StringType, MarkdownDescription: "Apply-only map of password/API-key field names to values."}, "protected_field_names": schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Names of password/API-key fields advertised by Chaptarr; values are never retained."},
	}}
}
func (r *metadataResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *metadataResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan metadataModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadMetadataSecrets(ctx, req.Config, &plan, &resp.Diagnostics)
	payload := metadataPayload(ctx, plan, 0, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !r.validateFieldPrivacy(ctx, payload, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/metadata", payload, "metadata provider", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}
func (r *metadataResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state metadataModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}
func (r *metadataResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan metadataModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	loadMetadataSecrets(ctx, req.Config, &plan, &resp.Diagnostics)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	payload := metadataPayload(ctx, plan, id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !r.validateFieldPrivacy(ctx, payload, &resp.Diagnostics) {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/metadata/"+strconv.FormatInt(id, 10), payload, "metadata provider", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}
func (r *metadataResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state metadataModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/metadata/", state.ID, "metadata provider", &resp.Diagnostics)
	}
}
func (r *metadataResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "metadata provider", &resp.State, &resp.Diagnostics)
}
func loadMetadataSecrets(ctx context.Context, config tfsdk.Config, plan *metadataModel, diagnostics *diag.Diagnostics) {
	diagnostics.Append(config.GetAttribute(ctx, path.Root("secret_fields"), &plan.SecretFields)...)
}
func metadataPayload(ctx context.Context, model metadataModel, id int64, diagnostics *diag.Diagnostics) metadataAPI {
	values := map[string]any{}
	if json.Unmarshal([]byte(model.FieldValuesJSON.ValueString()), &values) != nil {
		diagnostics.AddError("Invalid field_values_json", "Provide a valid JSON object of non-secret metadata field values.")
		return metadataAPI{}
	}
	fields := make([]providerFieldAPI, 0, len(values))
	for name, value := range values {
		if strings.TrimSpace(name) == "" {
			diagnostics.AddError("Invalid metadata field", "Field names must not be empty.")
			continue
		}
		fields = append(fields, providerFieldAPI{Name: name, Value: value})
	}
	if !model.SecretFields.IsNull() && !model.SecretFields.IsUnknown() {
		secrets := map[string]string{}
		diagnostics.Append(model.SecretFields.ElementsAs(ctx, &secrets, false)...)
		for name, value := range secrets {
			if strings.TrimSpace(name) == "" {
				diagnostics.AddError("Invalid secret field", "Secret field names must not be empty.")
				continue
			}
			fields = append(fields, providerFieldAPI{Name: name, Value: value, Privacy: "password", Type: "password"})
		}
	}
	sortProviderFields(fields)
	return metadataAPI{ID: id, Name: strings.TrimSpace(model.Name.ValueString()), Fields: fields, Implementation: model.Implementation.ValueString(), ConfigContract: model.ConfigContract.ValueString(), Tags: setInt64Values(ctx, model.Tags, diagnostics), Enable: valueBool(model.Enable)}
}
func (r *metadataResource) refresh(ctx context.Context, state *metadataModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/metadata/"+strconv.FormatInt(id, 10), "metadata provider", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current metadataAPI
	if json.Unmarshal(body, &current) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid metadata-provider document.")
		return
	}
	values := map[string]any{}
	secretNames := []string{}
	for _, field := range current.Fields {
		if sensitiveProviderField(field) {
			secretNames = append(secretNames, field.Name)
			continue
		}
		values[field.Name] = field.Value
	}
	canonical, hash := canonicalValue(values)
	state.ID, state.Name = types.StringValue(strconv.FormatInt(current.ID, 10)), types.StringValue(current.Name)
	state.ImplementationName, state.Implementation, state.ConfigContract = types.StringValue(current.ImplementationName), types.StringValue(current.Implementation), types.StringValue(current.ConfigContract)
	state.Enable = types.BoolValue(current.Enable)
	state.Tags = setInt64State(ctx, current.Tags, diagnostics)
	state.FieldValuesJSON, state.FieldValuesSHA256 = types.StringValue(canonical), types.StringValue(hash)
	state.SecretFields = types.MapNull(types.StringType)
	state.ProtectedFieldNames = setStringState(ctx, secretNames, diagnostics)
	diagnostics.Append(target.Set(ctx, state)...)
}
func sensitiveProviderField(field providerFieldAPI) bool {
	return field.Privacy == "password" || field.Privacy == "apiKey" || field.Type == "password"
}
func (r *metadataResource) validateFieldPrivacy(ctx context.Context, payload metadataAPI, diagnostics *diag.Diagnostics) bool {
	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/metadata/schema", nil)
	if err != nil {
		diagnostics.AddError("Unable to validate metadata fields", err.Error())
		return false
	}
	var templates []metadataAPI
	if json.Unmarshal(response.Body, &templates) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid metadata-provider schema.")
		return false
	}
	var template *metadataAPI
	for index := range templates {
		if templates[index].Implementation == payload.Implementation && templates[index].ConfigContract == payload.ConfigContract {
			template = &templates[index]
			break
		}
	}
	if template == nil {
		diagnostics.AddError("Unsupported metadata provider", "No current Chaptarr metadata schema matches the configured implementation and config_contract.")
		return false
	}
	privacy := make(map[string]bool, len(template.Fields))
	for _, field := range template.Fields {
		privacy[field.Name] = sensitiveProviderField(field)
	}
	for _, field := range payload.Fields {
		expectedSensitive, exists := privacy[field.Name]
		if !exists {
			diagnostics.AddError("Unknown metadata field", fmt.Sprintf("The current Chaptarr schema does not advertise field %q for this provider.", field.Name))
			continue
		}
		providedSensitive := sensitiveProviderField(field)
		if expectedSensitive && !providedSensitive {
			diagnostics.AddError("Sensitive metadata field misplaced", fmt.Sprintf("Move field %q from field_values_json to the write-only secret_fields map.", field.Name))
		}
		if !expectedSensitive && providedSensitive {
			diagnostics.AddError("Non-secret metadata field misplaced", fmt.Sprintf("Move field %q from secret_fields to field_values_json.", field.Name))
		}
	}
	return !diagnostics.HasError()
}
func sortProviderFields(fields []providerFieldAPI) {
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
}
