package provider

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithImportState = &importListExclusionResource{}

type importListExclusionResource struct{ client *client.Client }
type importListExclusionModel struct {
	ID         types.String `tfsdk:"id"`
	ForeignID  types.String `tfsdk:"foreign_id"`
	AuthorName types.String `tfsdk:"author_name"`
	MediaType  types.String `tfsdk:"media_type"`
}
type importListExclusionAPI struct {
	ID         int64  `json:"id,omitempty"`
	ForeignID  string `json:"foreignId"`
	AuthorName string `json:"authorName"`
	MediaType  string `json:"mediaType,omitempty"`
}

func newImportListExclusionResource() resource.Resource { return &importListExclusionResource{} }
func (r *importListExclusionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_import_list_exclusion"
}
func (r *importListExclusionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manage exactly one Chaptarr import-list exclusion. This resource never uses bulk deletion and therefore cannot remove user-owned exclusions outside its own numeric ID.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "foreign_id": schema.StringAttribute{Required: true}, "author_name": schema.StringAttribute{Required: true}, "media_type": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("all", "audiobook", "ebook")}}}}
}
func (r *importListExclusionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *importListExclusionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var p importListExclusionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	payload := importListExclusionPayload(p, 0, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/importlistexclusion", payload, "import list exclusion", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	p.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &p, &resp.State, &resp.Diagnostics)
}
func (r *importListExclusionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var s importListExclusionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &s, &resp.State, &resp.Diagnostics)
	}
}
func (r *importListExclusionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var p importListExclusionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	id, ok := positiveModelID(p.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	payload := importListExclusionPayload(p, id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/importlistexclusion/"+strconv.FormatInt(id, 10), payload, "import list exclusion", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &p, &resp.State, &resp.Diagnostics)
	}
}
func (r *importListExclusionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var s importListExclusionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/importlistexclusion/", s.ID, "import list exclusion", &resp.Diagnostics)
	}
}
func (r *importListExclusionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "import list exclusion", &resp.State, &resp.Diagnostics)
}
func importListExclusionPayload(p importListExclusionModel, id int64, d *diag.Diagnostics) importListExclusionAPI {
	foreign, name := strings.TrimSpace(p.ForeignID.ValueString()), strings.TrimSpace(p.AuthorName.ValueString())
	if foreign == "" || name == "" {
		d.AddError("Invalid import exclusion", "foreign_id and author_name must not be empty.")
	}
	media := p.MediaType.ValueString()
	if media == "all" {
		media = ""
	}
	return importListExclusionAPI{ID: id, ForeignID: foreign, AuthorName: name, MediaType: media}
}
func (r *importListExclusionResource) refresh(ctx context.Context, s *importListExclusionModel, target *tfsdk.State, d *diag.Diagnostics) {
	id, ok := positiveModelID(s.ID)
	if !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/importlistexclusion/"+strconv.FormatInt(id, 10), "import list exclusion", target, d)
	if !found || d.HasError() {
		return
	}
	var c importListExclusionAPI
	if json.Unmarshal(body, &c) != nil || c.ID < 1 || strings.TrimSpace(c.ForeignID) == "" {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid import-list exclusion.")
		return
	}
	media := c.MediaType
	if strings.TrimSpace(media) == "" {
		media = "all"
	}
	s.ID = types.StringValue(strconv.FormatInt(c.ID, 10))
	s.ForeignID = types.StringValue(c.ForeignID)
	s.AuthorName = types.StringValue(c.AuthorName)
	s.MediaType = types.StringValue(media)
	d.Append(target.Set(ctx, s)...)
}
