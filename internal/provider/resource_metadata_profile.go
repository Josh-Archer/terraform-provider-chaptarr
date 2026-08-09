package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
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
	_ resource.Resource                = &metadataProfileResource{}
	_ resource.ResourceWithImportState = &metadataProfileResource{}
)

type metadataProfileResource struct{ client *client.Client }

type metadataProfileModel struct {
	ID                           types.String  `tfsdk:"id"`
	Name                         types.String  `tfsdk:"name"`
	ProfileType                  types.String  `tfsdk:"profile_type"`
	MinPopularity                types.Float64 `tfsdk:"minimum_popularity"`
	MinPages                     types.Int64   `tfsdk:"minimum_pages"`
	SkipMissingDate              types.Bool    `tfsdk:"skip_missing_date"`
	SkipMissingISBN              types.Bool    `tfsdk:"skip_missing_isbn"`
	SkipPartsAndSets             types.Bool    `tfsdk:"skip_parts_and_sets"`
	SkipSeriesSecondary          types.Bool    `tfsdk:"skip_secondary_series"`
	SkipMissingIdentifierOmnibus types.Bool    `tfsdk:"skip_omnibus_without_identifier"`
	SkipOmnibus                  types.Bool    `tfsdk:"skip_omnibus"`
	SkipMissingASIN              types.Bool    `tfsdk:"skip_missing_asin"`
	AllowedLanguages             types.Set     `tfsdk:"allowed_languages"`
	Ignored                      types.Set     `tfsdk:"ignored_terms"`
}

type metadataProfileAPI struct {
	ID                           int64    `json:"id,omitempty"`
	Name                         string   `json:"name"`
	ProfileType                  int64    `json:"profileType"`
	MinPopularity                float64  `json:"minPopularity"`
	SkipMissingDate              bool     `json:"skipMissingDate"`
	SkipMissingISBN              bool     `json:"skipMissingIsbn"`
	SkipPartsAndSets             bool     `json:"skipPartsAndSets"`
	SkipSeriesSecondary          bool     `json:"skipSeriesSecondary"`
	SkipMissingIdentifierOmnibus bool     `json:"skipMissingIdentifierOmnibus"`
	SkipOmnibus                  bool     `json:"skipOmnibus"`
	SkipMissingASIN              bool     `json:"skipMissingAsin"`
	AllowedLanguages             string   `json:"allowedLanguages"`
	MinPages                     int64    `json:"minPages"`
	Ignored                      []string `json:"ignored"`
}

func newMetadataProfileResource() resource.Resource { return &metadataProfileResource{} }

func (r *metadataProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metadata_profile"
}

func (r *metadataProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Chaptarr metadata profile. Filter changes can queue an author metadata refresh during apply; name-only edits do not.",
		Attributes: map[string]schema.Attribute{
			"id":                              schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":                            schema.StringAttribute{Required: true},
			"profile_type":                    schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("general", "audiobook", "ebook")}},
			"minimum_popularity":              schema.Float64Attribute{Optional: true, Computed: true, Validators: []validator.Float64{float64validator.AtLeast(0)}},
			"minimum_pages":                   schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(0)}},
			"skip_missing_date":               schema.BoolAttribute{Optional: true, Computed: true},
			"skip_missing_isbn":               schema.BoolAttribute{Optional: true, Computed: true},
			"skip_parts_and_sets":             schema.BoolAttribute{Optional: true, Computed: true},
			"skip_secondary_series":           schema.BoolAttribute{Optional: true, Computed: true},
			"skip_omnibus_without_identifier": schema.BoolAttribute{Optional: true, Computed: true},
			"skip_omnibus":                    schema.BoolAttribute{Optional: true, Computed: true},
			"skip_missing_asin":               schema.BoolAttribute{Optional: true, Computed: true},
			"allowed_languages":               schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, MarkdownDescription: "Case-insensitive language names, ISO codes, or `null` for unknown language. Stored in deterministic sorted order."},
			"ignored_terms":                   schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, MarkdownDescription: "Case-insensitive ignored metadata terms."},
		},
	}
}

func (r *metadataProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *metadataProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan metadataProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	payload := metadataProfilePayload(ctx, plan, 0, &resp.Diagnostics)
	id := createProfile(ctx, r.client, "/api/v1/metadataprofile", payload, "metadata profile", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *metadataProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state metadataProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}

func (r *metadataProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan metadataProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Invalid metadata-profile state", "The profile has no valid numeric identifier.")
		}
		return
	}
	payload := metadataProfilePayload(ctx, plan, id, &resp.Diagnostics)
	updateProfile(ctx, r.client, "/api/v1/metadataprofile/"+strconv.FormatInt(id, 10), payload, "metadata profile", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}

func (r *metadataProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state metadataProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/metadataprofile/", state.ID, "metadata profile", &resp.Diagnostics)
	}
}

func (r *metadataProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "metadata profile", &resp.State, &resp.Diagnostics)
}

func metadataProfilePayload(ctx context.Context, model metadataProfileModel, id int64, diagnostics *diag.Diagnostics) metadataProfileAPI {
	profileTypes := map[string]int64{"general": 0, "audiobook": 1, "ebook": 2}
	return metadataProfileAPI{
		ID: id, Name: model.Name.ValueString(), ProfileType: profileTypes[model.ProfileType.ValueString()],
		MinPopularity: valueFloat64(model.MinPopularity), MinPages: valueInt64(model.MinPages),
		SkipMissingDate: valueBool(model.SkipMissingDate), SkipMissingISBN: valueBool(model.SkipMissingISBN),
		SkipPartsAndSets: valueBool(model.SkipPartsAndSets), SkipSeriesSecondary: valueBool(model.SkipSeriesSecondary),
		SkipMissingIdentifierOmnibus: valueBool(model.SkipMissingIdentifierOmnibus), SkipOmnibus: valueBool(model.SkipOmnibus), SkipMissingASIN: valueBool(model.SkipMissingASIN),
		AllowedLanguages: strings.Join(setStringValues(ctx, model.AllowedLanguages, diagnostics), ","), Ignored: setStringValues(ctx, model.Ignored, diagnostics),
	}
}

func (r *metadataProfileResource) refresh(ctx context.Context, state *metadataProfileModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid metadata-profile state", "The profile has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/metadataprofile/"+strconv.FormatInt(id, 10), "metadata profile", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current metadataProfileAPI
	if json.Unmarshal(body, &current) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid metadata-profile document.")
		return
	}
	profileType, valid := map[int64]string{0: "general", 1: "audiobook", 2: "ebook"}[current.ProfileType]
	if !valid {
		diagnostics.AddError("Invalid Chaptarr response", fmt.Sprintf("Chaptarr returned unsupported metadata profile type %d.", current.ProfileType))
		return
	}
	state.ID, state.Name, state.ProfileType = types.StringValue(strconv.FormatInt(current.ID, 10)), types.StringValue(current.Name), types.StringValue(profileType)
	state.MinPopularity, state.MinPages = types.Float64Value(current.MinPopularity), types.Int64Value(current.MinPages)
	state.SkipMissingDate, state.SkipMissingISBN = types.BoolValue(current.SkipMissingDate), types.BoolValue(current.SkipMissingISBN)
	state.SkipPartsAndSets, state.SkipSeriesSecondary = types.BoolValue(current.SkipPartsAndSets), types.BoolValue(current.SkipSeriesSecondary)
	state.SkipMissingIdentifierOmnibus, state.SkipOmnibus, state.SkipMissingASIN = types.BoolValue(current.SkipMissingIdentifierOmnibus), types.BoolValue(current.SkipOmnibus), types.BoolValue(current.SkipMissingASIN)
	state.AllowedLanguages = setStringState(ctx, splitCanonicalCSV(current.AllowedLanguages), diagnostics)
	state.Ignored = setStringState(ctx, current.Ignored, diagnostics)
	diagnostics.Append(target.Set(ctx, state)...)
}

func configuredResourceClient(data any, diagnostics *diag.Diagnostics) *client.Client {
	if data == nil {
		return nil
	}
	apiClient, ok := data.(*client.Client)
	if !ok {
		diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", data))
		return nil
	}
	return apiClient
}

func createProfile(ctx context.Context, apiClient *client.Client, endpoint string, payload any, label string, diagnostics *diag.Diagnostics) int64 {
	body, err := json.Marshal(payload)
	if err != nil {
		diagnostics.AddError("Unable to encode "+label, "The request could not be encoded.")
		return 0
	}
	response, err := apiClient.Do(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		diagnostics.AddError("Unable to create "+label, err.Error())
		return 0
	}
	id, err := createdIdentifier(response.Body)
	if err != nil {
		diagnostics.AddError("Invalid Chaptarr response", err.Error())
		return 0
	}
	return id
}

func updateProfile(ctx context.Context, apiClient *client.Client, endpoint string, payload any, label string, diagnostics *diag.Diagnostics) {
	body, err := json.Marshal(payload)
	if err != nil {
		diagnostics.AddError("Unable to encode "+label, "The request could not be encoded.")
		return
	}
	if _, err := apiClient.Do(ctx, http.MethodPut, endpoint, body); err != nil {
		diagnostics.AddError("Unable to update "+label, err.Error())
	}
}

func deleteProfile(ctx context.Context, apiClient *client.Client, endpointPrefix string, value types.String, label string, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(value)
	if !ok {
		diagnostics.AddError("Invalid "+label+" state", "The resource has no valid numeric identifier.")
		return
	}
	if _, err := apiClient.Do(ctx, http.MethodDelete, endpointPrefix+strconv.FormatInt(id, 10), nil); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		diagnostics.AddError("Unable to delete "+label, err.Error())
	}
}

func readProfile(ctx context.Context, apiClient *client.Client, endpoint, label string, target *tfsdk.State, diagnostics *diag.Diagnostics) ([]byte, bool) {
	response, err := apiClient.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			target.RemoveResource(ctx)
			return nil, false
		}
		diagnostics.AddError("Unable to read "+label, err.Error())
		return nil, false
	}
	return response.Body, true
}

func importNumericProfile(ctx context.Context, rawID, label string, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
	if err != nil || id < 1 {
		diagnostics.AddError("Invalid import identifier", "Use the positive numeric Chaptarr "+label+" identifier.")
		return
	}
	diagnostics.Append(state.SetAttribute(ctx, path.Root("id"), strconv.FormatInt(id, 10))...)
}

func setStringValues(ctx context.Context, value types.Set, diagnostics *diag.Diagnostics) []string {
	if value.IsNull() || value.IsUnknown() {
		return []string{}
	}
	var values []string
	diagnostics.Append(value.ElementsAs(ctx, &values, false)...)
	for index := range values {
		values[index] = strings.TrimSpace(values[index])
	}
	sort.Slice(values, func(i, j int) bool { return strings.ToLower(values[i]) < strings.ToLower(values[j]) })
	return values
}

func setStringState(ctx context.Context, values []string, diagnostics *diag.Diagnostics) types.Set {
	normalized := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := strings.ToLower(trimmed)
		if trimmed != "" {
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				normalized = append(normalized, trimmed)
			}
		}
	}
	sort.Slice(normalized, func(i, j int) bool { return strings.ToLower(normalized[i]) < strings.ToLower(normalized[j]) })
	result, converted := types.SetValueFrom(ctx, types.StringType, normalized)
	diagnostics.Append(converted...)
	return result
}

func splitCanonicalCSV(value string) []string { return strings.Split(value, ",") }
func valueBool(value types.Bool) bool {
	return !value.IsNull() && !value.IsUnknown() && value.ValueBool()
}
func valueInt64(value types.Int64) int64 {
	if value.IsNull() || value.IsUnknown() {
		return 0
	}
	return value.ValueInt64()
}
func valueFloat64(value types.Float64) float64 {
	if value.IsNull() || value.IsUnknown() {
		return 0
	}
	return value.ValueFloat64()
}
