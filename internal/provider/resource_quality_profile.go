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
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &qualityProfileResource{}
	_ resource.ResourceWithImportState = &qualityProfileResource{}
)

type qualityProfileResource struct{ client *client.Client }

type qualityProfileModel struct {
	ID                             types.String `tfsdk:"id"`
	Name                           types.String `tfsdk:"name"`
	ProfileType                    types.String `tfsdk:"profile_type"`
	UpgradeAllowed                 types.Bool   `tfsdk:"upgrade_allowed"`
	PreferCustomFormatsOverQuality types.Bool   `tfsdk:"prefer_custom_formats_over_quality"`
	ConvertToQualityID             types.Int64  `tfsdk:"convert_to_quality_id"`
	Cutoff                         types.Int64  `tfsdk:"cutoff"`
	Items                          types.List   `tfsdk:"items"`
	MinimumFormatScore             types.Int64  `tfsdk:"minimum_format_score"`
	CutoffFormatScore              types.Int64  `tfsdk:"cutoff_format_score"`
	FormatItems                    types.List   `tfsdk:"format_items"`
}

type qualityItemModel struct {
	ID                        types.Int64  `tfsdk:"id"`
	Name                      types.String `tfsdk:"name"`
	QualityID                 types.Int64  `tfsdk:"quality_id"`
	QualityName               types.String `tfsdk:"quality_name"`
	QualityIsConversionTarget types.Bool   `tfsdk:"quality_is_conversion_target"`
	Allowed                   types.Bool   `tfsdk:"allowed"`
	Items                     types.List   `tfsdk:"items"`
}

type qualityLeafModel struct {
	ID                        types.Int64  `tfsdk:"id"`
	Name                      types.String `tfsdk:"name"`
	QualityID                 types.Int64  `tfsdk:"quality_id"`
	QualityName               types.String `tfsdk:"quality_name"`
	QualityIsConversionTarget types.Bool   `tfsdk:"quality_is_conversion_target"`
	Allowed                   types.Bool   `tfsdk:"allowed"`
}

type formatItemModel struct {
	FormatID   types.Int64  `tfsdk:"format_id"`
	BuiltInKey types.String `tfsdk:"built_in_key"`
	Name       types.String `tfsdk:"name"`
	Score      types.Int64  `tfsdk:"score"`
}

type qualityProfileAPI struct {
	ID                             int64                   `json:"id,omitempty"`
	Name                           string                  `json:"name"`
	ProfileType                    string                  `json:"profileType"`
	UpgradeAllowed                 bool                    `json:"upgradeAllowed"`
	PreferCustomFormatsOverQuality bool                    `json:"preferCustomFormatsOverQuality"`
	ConvertMP3ToM4B                bool                    `json:"convertMp3ToM4b"`
	ConvertToQualityID             *int64                  `json:"convertToQualityId"`
	Cutoff                         int64                   `json:"cutoff"`
	Items                          []qualityProfileItemAPI `json:"items"`
	MinimumFormatScore             int64                   `json:"minFormatScore"`
	CutoffFormatScore              int64                   `json:"cutoffFormatScore"`
	FormatItems                    []formatItemAPI         `json:"formatItems"`
}

type qualityProfileItemAPI struct {
	ID      int64                   `json:"id"`
	Name    string                  `json:"name"`
	Quality *qualityReference       `json:"quality"`
	Items   []qualityProfileItemAPI `json:"items"`
	Allowed bool                    `json:"allowed"`
}

type formatItemAPI struct {
	Format     int64  `json:"format"`
	BuiltInKey string `json:"builtInKey,omitempty"`
	Name       string `json:"name,omitempty"`
	Score      int64  `json:"score"`
}

func newQualityProfileResource() resource.Resource { return &qualityProfileResource{} }

func (r *qualityProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_quality_profile"
}

func qualityLeafAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":                           schema.Int64Attribute{Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
		"name":                         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"quality_id":                   schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(0)}, MarkdownDescription: "Quality identifier. `0` is Chaptarr's Unknown Text leaf."},
		"quality_name":                 schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"quality_is_conversion_target": schema.BoolAttribute{Computed: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}},
		"allowed":                      schema.BoolAttribute{Required: true},
	}
}

func qualityItemAttributes() map[string]schema.Attribute {
	attributes := qualityLeafAttributes()
	attributes["id"] = schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(1)}, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}, MarkdownDescription: "Required for a group; omit for a direct quality leaf."}
	attributes["name"] = schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, MarkdownDescription: "Required for a group; omit for a direct quality leaf."}
	attributes["quality_id"] = schema.Int64Attribute{Optional: true, Computed: true, Validators: []validator.Int64{int64validator.AtLeast(0)}, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}, MarkdownDescription: "Quality identifier for a direct leaf. `0` is Unknown Text. Omit for a group."}
	attributes["items"] = schema.ListNestedAttribute{Optional: true, Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: qualityLeafAttributes()}}
	return attributes
}

func formatItemAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"format_id":    schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
		"built_in_key": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"score":        schema.Int64Attribute{Required: true},
	}
}

func (r *qualityProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage an audiobook or ebook quality profile. `items` and `format_items` are ordered lists. Copy each group's ID and name from the schema data source; direct-quality IDs/names and format names/built-in keys are server-owned, while allowed flags and scores are declarative. Unknown Text uses `quality_id = 0`. Empty `format_items` is valid for ebook profiles and is sent as `[]`. Update GETs the current profile and merges those server-owned names before PUT.",
		Attributes: map[string]schema.Attribute{
			"id":                                 schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":                               schema.StringAttribute{Required: true},
			"profile_type":                       schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}},
			"upgrade_allowed":                    schema.BoolAttribute{Required: true},
			"prefer_custom_formats_over_quality": schema.BoolAttribute{Required: true},
			"convert_to_quality_id":              schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}, MarkdownDescription: "Optional conversion target quality. Omit to disable conversion."},
			"cutoff":                             schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(1)}},
			"items":                              schema.ListNestedAttribute{Required: true, NestedObject: schema.NestedAttributeObject{Attributes: qualityItemAttributes()}},
			"minimum_format_score":               schema.Int64Attribute{Required: true},
			"cutoff_format_score":                schema.Int64Attribute{Required: true},
			"format_items":                       schema.ListNestedAttribute{Required: true, NestedObject: schema.NestedAttributeObject{Attributes: formatItemAttributes()}},
		},
	}
}

func (r *qualityProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *qualityProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan qualityProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	payload := qualityProfilePayload(ctx, plan, 0, &resp.Diagnostics)
	if !validateQualityProfile(payload, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/qualityprofile", payload, "quality profile", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *qualityProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state qualityProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}

func (r *qualityProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan qualityProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Invalid quality-profile state", "The profile has no valid numeric identifier.")
		}
		return
	}
	payload := qualityProfilePayload(ctx, plan, id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !validateQualityProfile(payload, &resp.Diagnostics) {
		return
	}
	current, ok := r.currentQualityProfile(ctx, id, &resp.State, &resp.Diagnostics)
	if !ok {
		return
	}
	mergeQualityProfileServerOwned(current, &payload)
	updateProfile(ctx, r.client, "/api/v1/qualityprofile/"+strconv.FormatInt(id, 10), payload, "quality profile", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}

func (r *qualityProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state qualityProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/qualityprofile/", state.ID, "quality profile", &resp.Diagnostics)
	}
}

func (r *qualityProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "quality profile", &resp.State, &resp.Diagnostics)
}

func qualityProfilePayload(ctx context.Context, model qualityProfileModel, id int64, diagnostics *diag.Diagnostics) qualityProfileAPI {
	var items []qualityItemModel
	diagnostics.Append(model.Items.ElementsAs(ctx, &items, false)...)
	var formats []formatItemModel
	diagnostics.Append(model.FormatItems.ElementsAs(ctx, &formats, false)...)
	payload := qualityProfileAPI{ID: id, Name: model.Name.ValueString(), ProfileType: model.ProfileType.ValueString(), UpgradeAllowed: valueBool(model.UpgradeAllowed), PreferCustomFormatsOverQuality: valueBool(model.PreferCustomFormatsOverQuality), ConvertToQualityID: intPointer(model.ConvertToQualityID), Cutoff: valueInt64(model.Cutoff), Items: make([]qualityProfileItemAPI, 0, len(items)), MinimumFormatScore: valueInt64(model.MinimumFormatScore), CutoffFormatScore: valueInt64(model.CutoffFormatScore), FormatItems: make([]formatItemAPI, 0, len(formats))}
	payload.ConvertMP3ToM4B = payload.ConvertToQualityID != nil && *payload.ConvertToQualityID == 12
	for _, item := range items {
		payload.Items = append(payload.Items, qualityItemPayload(ctx, item, diagnostics))
	}
	for _, item := range formats {
		payload.FormatItems = append(payload.FormatItems, formatItemAPI{Format: item.FormatID.ValueInt64(), BuiltInKey: item.BuiltInKey.ValueString(), Name: item.Name.ValueString(), Score: item.Score.ValueInt64()})
	}
	return payload
}

func qualityItemPayload(ctx context.Context, model qualityItemModel, diagnostics *diag.Diagnostics) qualityProfileItemAPI {
	result := qualityProfileItemAPI{ID: valueInt64(model.ID), Name: model.Name.ValueString(), Allowed: valueBool(model.Allowed), Items: []qualityProfileItemAPI{}}
	if configured(model.QualityID) {
		result.Quality = &qualityReference{ID: model.QualityID.ValueInt64(), Name: model.QualityName.ValueString(), IsConversionTarget: valueBool(model.QualityIsConversionTarget)}
	}
	if !model.Items.IsNull() && !model.Items.IsUnknown() {
		var children []qualityLeafModel
		diagnostics.Append(model.Items.ElementsAs(ctx, &children, false)...)
		for _, child := range children {
			quality := &qualityReference{ID: child.QualityID.ValueInt64(), Name: child.QualityName.ValueString(), IsConversionTarget: valueBool(child.QualityIsConversionTarget)}
			result.Items = append(result.Items, qualityProfileItemAPI{ID: valueInt64(child.ID), Name: child.Name.ValueString(), Quality: quality, Items: []qualityProfileItemAPI{}, Allowed: valueBool(child.Allowed)})
		}
	}
	return result
}

func validateQualityProfile(payload qualityProfileAPI, diagnostics *diag.Diagnostics) bool {
	if payload.ProfileType == "ebook" && payload.PreferCustomFormatsOverQuality {
		diagnostics.AddError("Invalid ebook quality profile", "prefer_custom_formats_over_quality is supported only for audiobook profiles.")
	}
	if len(payload.Items) == 0 {
		diagnostics.AddError("Quality items required", "Configure the ordered items returned by the matching quality-profile schema data source.")
	}
	groupIDs := map[int64]struct{}{}
	qualityIDs := map[int64]struct{}{}
	allowed := false
	for _, item := range payload.Items {
		allowed = allowed || item.Allowed
		if item.Quality == nil {
			if item.ID < 1 || strings.TrimSpace(item.Name) == "" || len(item.Items) < 2 {
				diagnostics.AddError("Invalid quality group", "Every quality group must have a positive ID, a non-empty name, and at least two quality children.")
				continue
			}
			if _, exists := groupIDs[item.ID]; exists {
				diagnostics.AddError("Duplicate quality group", fmt.Sprintf("Quality group ID %d is configured more than once.", item.ID))
			}
			groupIDs[item.ID] = struct{}{}
			for _, child := range item.Items {
				validateUniqueQuality(child.Quality, qualityIDs, diagnostics)
			}
			continue
		}
		if strings.TrimSpace(item.Name) != "" || len(item.Items) != 0 {
			diagnostics.AddError("Invalid quality leaf", "A direct quality leaf cannot have a group name or child items.")
		}
		validateUniqueQuality(item.Quality, qualityIDs, diagnostics)
	}
	if !allowed {
		diagnostics.AddError("Allowed quality required", "At least one top-level quality or group must be allowed.")
	}
	return !diagnostics.HasError()
}

func validateUniqueQuality(quality *qualityReference, seen map[int64]struct{}, diagnostics *diag.Diagnostics) {
	if quality == nil || quality.ID < 0 {
		diagnostics.AddError("Invalid quality item", "Every quality leaf must have a non-negative quality ID.")
		return
	}
	if _, exists := seen[quality.ID]; exists {
		diagnostics.AddError("Duplicate quality item", fmt.Sprintf("Quality ID %d is configured more than once.", quality.ID))
	}
	seen[quality.ID] = struct{}{}
}

func (r *qualityProfileResource) currentQualityProfile(ctx context.Context, id int64, target *tfsdk.State, diagnostics *diag.Diagnostics) (qualityProfileAPI, bool) {
	body, found := readProfile(ctx, r.client, "/api/v1/qualityprofile/"+strconv.FormatInt(id, 10), "quality profile", target, diagnostics)
	if !found || diagnostics.HasError() {
		return qualityProfileAPI{}, false
	}
	var current qualityProfileAPI
	if json.Unmarshal(body, &current) != nil || (current.ProfileType != "audiobook" && current.ProfileType != "ebook") {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid quality-profile document or profile type.")
		return qualityProfileAPI{}, false
	}
	return current, true
}

func mergeQualityProfileServerOwned(current qualityProfileAPI, payload *qualityProfileAPI) {
	byQuality := map[int64]qualityProfileItemAPI{}
	byGroup := map[int64]qualityProfileItemAPI{}
	indexQualityItems(current.Items, byQuality, byGroup)
	for i := range payload.Items {
		mergeQualityItemServerOwned(&payload.Items[i], byQuality, byGroup)
	}
	byFormat := map[int64]formatItemAPI{}
	for _, item := range current.FormatItems {
		byFormat[item.Format] = item
	}
	for i := range payload.FormatItems {
		existing, ok := byFormat[payload.FormatItems[i].Format]
		if !ok {
			continue
		}
		if strings.TrimSpace(payload.FormatItems[i].Name) == "" {
			payload.FormatItems[i].Name = existing.Name
		}
		if strings.TrimSpace(payload.FormatItems[i].BuiltInKey) == "" {
			payload.FormatItems[i].BuiltInKey = existing.BuiltInKey
		}
	}
}

func indexQualityItems(items []qualityProfileItemAPI, byQuality, byGroup map[int64]qualityProfileItemAPI) {
	for _, item := range items {
		if item.Quality != nil {
			byQuality[item.Quality.ID] = item
			continue
		}
		byGroup[item.ID] = item
		indexQualityItems(item.Items, byQuality, byGroup)
	}
}

func mergeQualityItemServerOwned(item *qualityProfileItemAPI, byQuality, byGroup map[int64]qualityProfileItemAPI) {
	if item.Items == nil {
		item.Items = []qualityProfileItemAPI{}
	}
	if item.Quality != nil {
		existing, ok := byQuality[item.Quality.ID]
		if !ok || existing.Quality == nil {
			return
		}
		if item.ID == 0 {
			item.ID = existing.ID
		}
		if strings.TrimSpace(item.Name) == "" {
			item.Name = existing.Name
		}
		if strings.TrimSpace(item.Quality.Name) == "" {
			item.Quality.Name = existing.Quality.Name
		}
		item.Quality.IsConversionTarget = existing.Quality.IsConversionTarget
		return
	}
	if existing, ok := byGroup[item.ID]; ok && strings.TrimSpace(item.Name) == "" {
		item.Name = existing.Name
	}
	for i := range item.Items {
		mergeQualityItemServerOwned(&item.Items[i], byQuality, byGroup)
	}
}

func (r *qualityProfileResource) refresh(ctx context.Context, state *qualityProfileModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid quality-profile state", "The profile has no valid numeric identifier.")
		return
	}
	current, ok := r.currentQualityProfile(ctx, id, target, diagnostics)
	if !ok {
		return
	}
	currentState, ok := qualityProfileState(ctx, current, diagnostics)
	if !ok {
		return
	}
	*state = currentState
	diagnostics.Append(target.Set(ctx, state)...)
}

func qualityProfileState(ctx context.Context, current qualityProfileAPI, diagnostics *diag.Diagnostics) (qualityProfileModel, bool) {
	qualityItems, ok := qualityItemState(ctx, current.Items, diagnostics)
	if !ok {
		return qualityProfileModel{}, false
	}
	formats := make([]formatItemModel, 0, len(current.FormatItems))
	for _, item := range current.FormatItems {
		formats = append(formats, formatItemModel{FormatID: types.Int64Value(item.Format), BuiltInKey: types.StringValue(item.BuiltInKey), Name: types.StringValue(item.Name), Score: types.Int64Value(item.Score)})
	}
	return qualityProfileModel{
		ID: types.StringValue(strconv.FormatInt(current.ID, 10)), Name: types.StringValue(current.Name), ProfileType: types.StringValue(current.ProfileType),
		UpgradeAllowed: types.BoolValue(current.UpgradeAllowed), PreferCustomFormatsOverQuality: types.BoolValue(current.PreferCustomFormatsOverQuality), ConvertToQualityID: nullableInt(current.ConvertToQualityID),
		Cutoff: types.Int64Value(current.Cutoff), Items: qualityItems, MinimumFormatScore: types.Int64Value(current.MinimumFormatScore), CutoffFormatScore: types.Int64Value(current.CutoffFormatScore), FormatItems: listObjectState(ctx, formatItemType(), formats, diagnostics),
	}, !diagnostics.HasError()
}

func qualityItemState(ctx context.Context, values []qualityProfileItemAPI, diagnostics *diag.Diagnostics) (types.List, bool) {
	models := make([]qualityItemModel, 0, len(values))
	for _, item := range values {
		children := make([]qualityLeafModel, 0, len(item.Items))
		for _, child := range item.Items {
			if len(child.Items) != 0 || child.Quality == nil {
				diagnostics.AddError("Unsupported Chaptarr quality tree", "The quality profile contains nesting deeper than the provider's typed two-level schema.")
				return types.ListNull(qualityItemType()), false
			}
			children = append(children, qualityLeafFromAPI(child))
		}
		model := qualityItemModel{ID: types.Int64Value(item.ID), Name: types.StringValue(item.Name), Allowed: types.BoolValue(item.Allowed), Items: listObjectState(ctx, qualityLeafType(), children, diagnostics)}
		if item.Quality == nil {
			model.QualityID, model.QualityName, model.QualityIsConversionTarget = types.Int64Null(), types.StringNull(), types.BoolNull()
		} else {
			model.QualityID, model.QualityName, model.QualityIsConversionTarget = types.Int64Value(item.Quality.ID), types.StringValue(item.Quality.Name), types.BoolValue(item.Quality.IsConversionTarget)
		}
		models = append(models, model)
	}
	return listObjectState(ctx, qualityItemType(), models, diagnostics), !diagnostics.HasError()
}

func qualityLeafFromAPI(item qualityProfileItemAPI) qualityLeafModel {
	return qualityLeafModel{ID: types.Int64Value(item.ID), Name: types.StringValue(item.Name), QualityID: types.Int64Value(item.Quality.ID), QualityName: types.StringValue(item.Quality.Name), QualityIsConversionTarget: types.BoolValue(item.Quality.IsConversionTarget), Allowed: types.BoolValue(item.Allowed)}
}

func qualityLeafType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "name": types.StringType, "quality_id": types.Int64Type, "quality_name": types.StringType, "quality_is_conversion_target": types.BoolType, "allowed": types.BoolType}}
}

func qualityItemType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "name": types.StringType, "quality_id": types.Int64Type, "quality_name": types.StringType, "quality_is_conversion_target": types.BoolType, "allowed": types.BoolType, "items": types.ListType{ElemType: qualityLeafType()}}}
}

func formatItemType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"format_id": types.Int64Type, "built_in_key": types.StringType, "name": types.StringType, "score": types.Int64Type}}
}

func listObjectState(ctx context.Context, objectType types.ObjectType, values any, diagnostics *diag.Diagnostics) types.List {
	result, converted := types.ListValueFrom(ctx, objectType, values)
	diagnostics.Append(converted...)
	return result
}
