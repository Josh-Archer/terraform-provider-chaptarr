package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &qualityDefinitionResource{}
	_ resource.ResourceWithImportState = &qualityDefinitionResource{}
)

type qualityDefinitionResource struct{ client *client.Client }

type qualityDefinitionModel struct {
	ID          types.String  `tfsdk:"id"`
	QualityID   types.Int64   `tfsdk:"quality_id"`
	QualityName types.String  `tfsdk:"quality_name"`
	Title       types.String  `tfsdk:"title"`
	GroupName   types.String  `tfsdk:"group_name"`
	GroupWeight types.Int64   `tfsdk:"group_weight"`
	Weight      types.Int64   `tfsdk:"weight"`
	MinimumSize types.Float64 `tfsdk:"minimum_size"`
	MaximumSize types.Float64 `tfsdk:"maximum_size"`
}

type qualityDefinitionAPI struct {
	ID          int64            `json:"id"`
	Quality     qualityReference `json:"quality"`
	Title       string           `json:"title"`
	GroupName   string           `json:"groupName"`
	GroupWeight int64            `json:"groupWeight"`
	Weight      int64            `json:"weight"`
	MinimumSize *float64         `json:"minSize"`
	MaximumSize *float64         `json:"maxSize"`
}

type qualityReference struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	IsConversionTarget bool   `json:"isConversionTarget"`
}

func newQualityDefinitionResource() resource.Resource { return &qualityDefinitionResource{} }

func (r *qualityDefinitionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_quality_definition"
}

func (r *qualityDefinitionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage the size thresholds of a built-in Chaptarr quality definition. Chaptarr owns definition creation and deletion; creating this Terraform resource adopts the matching quality, and destroy only stops managing it.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"quality_id":   schema.Int64Attribute{Required: true, Validators: []validator.Int64{int64validator.AtLeast(1)}, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"quality_name": schema.StringAttribute{Computed: true},
			"title":        schema.StringAttribute{Computed: true},
			"group_name":   schema.StringAttribute{Computed: true},
			"group_weight": schema.Int64Attribute{Computed: true},
			"weight":       schema.Int64Attribute{Computed: true},
			"minimum_size": schema.Float64Attribute{Optional: true, Computed: true},
			"maximum_size": schema.Float64Attribute{Optional: true, Computed: true},
		},
	}
}

func (r *qualityDefinitionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *qualityDefinitionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan qualityDefinitionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.QualityID.IsUnknown() || plan.QualityID.IsNull() {
		return
	}
	response, err := r.client.Do(ctx, http.MethodGet, "/api/v1/qualitydefinition", nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to adopt quality definition", err.Error())
		return
	}
	var definitions []qualityDefinitionAPI
	if json.Unmarshal(response.Body, &definitions) != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid quality definitions.")
		return
	}
	var current *qualityDefinitionAPI
	for index := range definitions {
		if definitions[index].Quality.ID == plan.QualityID.ValueInt64() {
			current = &definitions[index]
			break
		}
	}
	if current == nil {
		resp.Diagnostics.AddAttributeError(pathFor("quality_id"), "Quality definition not found", fmt.Sprintf("Chaptarr has no built-in definition for quality ID %d.", plan.QualityID.ValueInt64()))
		return
	}
	mergeQualityDefinitionPlan(plan, current)
	updateProfile(ctx, r.client, "/api/v1/qualitydefinition/"+strconv.FormatInt(current.ID, 10), current, "quality definition", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(current.ID, 10))
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *qualityDefinitionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state qualityDefinitionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &state, &resp.State, &resp.Diagnostics)
	}
}

func (r *qualityDefinitionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan qualityDefinitionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	id, ok := positiveModelID(plan.ID)
	if resp.Diagnostics.HasError() || !ok {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Invalid quality-definition state", "The definition has no valid numeric identifier.")
		}
		return
	}
	current := qualityDefinitionFromModel(plan, id)
	updateProfile(ctx, r.client, "/api/v1/qualitydefinition/"+strconv.FormatInt(id, 10), current, "quality definition", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
	}
}

func (r *qualityDefinitionResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
	// Definitions are built into Chaptarr. Destroy intentionally removes only Terraform state.
}

func (r *qualityDefinitionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "quality definition", &resp.State, &resp.Diagnostics)
}

func (r *qualityDefinitionResource) refresh(ctx context.Context, state *qualityDefinitionModel, target *tfsdk.State, diagnostics *diag.Diagnostics) {
	id, ok := positiveModelID(state.ID)
	if !ok {
		diagnostics.AddError("Invalid quality-definition state", "The definition has no valid numeric identifier.")
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/qualitydefinition/"+strconv.FormatInt(id, 10), "quality definition", target, diagnostics)
	if !found || diagnostics.HasError() {
		return
	}
	var current qualityDefinitionAPI
	if json.Unmarshal(body, &current) != nil || current.Quality.ID < 1 {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid quality-definition document.")
		return
	}
	state.ID, state.QualityID, state.QualityName = types.StringValue(strconv.FormatInt(current.ID, 10)), types.Int64Value(current.Quality.ID), types.StringValue(current.Quality.Name)
	state.Title, state.GroupName = types.StringValue(current.Title), types.StringValue(current.GroupName)
	state.GroupWeight, state.Weight = types.Int64Value(current.GroupWeight), types.Int64Value(current.Weight)
	state.MinimumSize, state.MaximumSize = nullableFloat(current.MinimumSize), nullableFloat(current.MaximumSize)
	diagnostics.Append(target.Set(ctx, state)...)
}

func mergeQualityDefinitionPlan(plan qualityDefinitionModel, current *qualityDefinitionAPI) {
	if !plan.MinimumSize.IsNull() && !plan.MinimumSize.IsUnknown() {
		value := plan.MinimumSize.ValueFloat64()
		current.MinimumSize = &value
	}
	if !plan.MaximumSize.IsNull() && !plan.MaximumSize.IsUnknown() {
		value := plan.MaximumSize.ValueFloat64()
		current.MaximumSize = &value
	}
}

func qualityDefinitionFromModel(model qualityDefinitionModel, id int64) qualityDefinitionAPI {
	return qualityDefinitionAPI{ID: id, Quality: qualityReference{ID: model.QualityID.ValueInt64(), Name: model.QualityName.ValueString()}, Title: model.Title.ValueString(), GroupName: model.GroupName.ValueString(), GroupWeight: model.GroupWeight.ValueInt64(), Weight: model.Weight.ValueInt64(), MinimumSize: floatPointer(model.MinimumSize), MaximumSize: floatPointer(model.MaximumSize)}
}

func floatPointer(value types.Float64) *float64 {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	result := value.ValueFloat64()
	return &result
}

func nullableFloat(value *float64) types.Float64 {
	if value == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*value)
}

func pathFor(name string) path.Path { return path.Root(name) }
