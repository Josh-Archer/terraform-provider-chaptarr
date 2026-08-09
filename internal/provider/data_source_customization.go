package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type customizationSchemaDataSource struct {
	client *client.Client
	kind   string
}
type customizationSchemaState struct {
	ID              types.String `tfsdk:"id"`
	TemplatesJSON   types.String `tfsdk:"templates_json"`
	TemplatesSHA256 types.String `tfsdk:"templates_sha256"`
}

func newMetadataSchemaDataSource() datasource.DataSource {
	return &customizationSchemaDataSource{kind: "metadata"}
}
func newCustomFormatSchemaDataSource() datasource.DataSource {
	return &customizationSchemaDataSource{kind: "custom_format"}
}
func newIndexerSchemaDataSource() datasource.DataSource {
	return &customizationSchemaDataSource{kind: "indexer"}
}
func newDownloadClientSchemaDataSource() datasource.DataSource {
	return &customizationSchemaDataSource{kind: "download_client"}
}
func newNotificationSchemaDataSource() datasource.DataSource {
	return &customizationSchemaDataSource{kind: "notification"}
}
func (d *customizationSchemaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind + "_schema"
}
func (d *customizationSchemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Fetch the current Chaptarr customization template contract as deterministic canonical JSON. Metadata password/API-key values are removed while field privacy/type metadata remains available.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{Computed: true}, "templates_json": schema.StringAttribute{Computed: true}, "templates_sha256": schema.StringAttribute{Computed: true}}}
}
func (d *customizationSchemaDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	d.client = apiClient
}
func (d *customizationSchemaDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	endpoints := map[string]string{"custom_format": "/api/v1/customformat/schema", "metadata": "/api/v1/metadata/schema", "indexer": "/api/v1/indexer/schema", "download_client": "/api/v1/downloadclient/schema", "notification": "/api/v1/notification/schema"}
	endpoint, ok := endpoints[d.kind]
	if !ok {
		resp.Diagnostics.AddError("Invalid customization schema", "The provider requested an unknown customization schema kind.")
		return
	}
	response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read customization schema", err.Error())
		return
	}
	var decoded any
	if json.Unmarshal(response.Body, &decoded) != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid customization schema.")
		return
	}
	if d.kind != "custom_format" {
		decoded = sanitizeMetadataTemplates(decoded)
	}
	canonical, hash := canonicalValue(decoded)
	state := customizationSchemaState{ID: types.StringValue(publicFingerprint(endpoint)), TemplatesJSON: types.StringValue(canonical), TemplatesSHA256: types.StringValue(hash)}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func sanitizeMetadataTemplates(value any) any {
	switch current := value.(type) {
	case []any:
		for index := range current {
			current[index] = sanitizeMetadataTemplates(current[index])
		}
	case map[string]any:
		privacy, _ := current["privacy"].(string)
		fieldType, _ := current["type"].(string)
		if _, isField := current["name"]; isField && (privacy == "password" || privacy == "apiKey" || fieldType == "password") {
			current["value"] = nil
		}
		for name, child := range current {
			current[name] = sanitizeMetadataTemplates(child)
		}
	}
	return value
}

type tagDetailsDataSource struct{ client *client.Client }
type tagDetailsState struct {
	ID    types.String `tfsdk:"id"`
	TagID types.Int64  `tfsdk:"tag_id"`
	Tags  types.List   `tfsdk:"tags"`
}
type tagDetailModel struct {
	ID                types.Int64  `tfsdk:"id"`
	Label             types.String `tfsdk:"label"`
	DelayProfileIDs   types.Set    `tfsdk:"delay_profile_ids"`
	ImportListIDs     types.Set    `tfsdk:"import_list_ids"`
	NotificationIDs   types.Set    `tfsdk:"notification_ids"`
	RestrictionIDs    types.Set    `tfsdk:"restriction_ids"`
	IndexerIDs        types.Set    `tfsdk:"indexer_ids"`
	DownloadClientIDs types.Set    `tfsdk:"download_client_ids"`
	AuthorIDs         types.Set    `tfsdk:"author_ids"`
}
type tagDetailAPI struct {
	ID                int64   `json:"id"`
	Label             string  `json:"label"`
	DelayProfileIDs   []int64 `json:"delayProfileIds"`
	ImportListIDs     []int64 `json:"importListIds"`
	NotificationIDs   []int64 `json:"notificationIds"`
	RestrictionIDs    []int64 `json:"restrictionIds"`
	IndexerIDs        []int64 `json:"indexerIds"`
	DownloadClientIDs []int64 `json:"downloadClientIds"`
	AuthorIDs         []int64 `json:"authorIds"`
}

func newTagDetailsDataSource() datasource.DataSource { return &tagDetailsDataSource{} }
func (d *tagDetailsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag_details"
}
func (d *tagDetailsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	set := func() schema.SetAttribute { return schema.SetAttribute{Computed: true, ElementType: types.Int64Type} }
	resp.Schema = schema.Schema{MarkdownDescription: "Observe tag associations without mutating them. Omit tag_id for every tag or set it for one tag.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{Computed: true}, "tag_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)}}, "tags": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"id": schema.Int64Attribute{Computed: true}, "label": schema.StringAttribute{Computed: true}, "delay_profile_ids": set(), "import_list_ids": set(), "notification_ids": set(), "restriction_ids": set(), "indexer_ids": set(), "download_client_ids": set(), "author_ids": set()}}}}}
}
func (d *tagDetailsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	d.client = apiClient
}
func (d *tagDetailsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var configuredID types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathFor("tag_id"), &configuredID)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := "/api/v1/tag/detail"
	if !configuredID.IsNull() && !configuredID.IsUnknown() {
		endpoint += "/" + url.PathEscape(strconv.FormatInt(configuredID.ValueInt64(), 10))
	}
	response, err := d.client.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read tag details", err.Error())
		return
	}
	var values []tagDetailAPI
	if !configuredID.IsNull() && !configuredID.IsUnknown() {
		var one tagDetailAPI
		if json.Unmarshal(response.Body, &one) != nil {
			resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid tag details.")
			return
		}
		values = []tagDetailAPI{one}
	} else if json.Unmarshal(response.Body, &values) != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid tag details.")
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	models := make([]tagDetailModel, 0, len(values))
	for _, value := range values {
		models = append(models, tagDetailModel{ID: types.Int64Value(value.ID), Label: types.StringValue(value.Label), DelayProfileIDs: setInt64State(ctx, value.DelayProfileIDs, &resp.Diagnostics), ImportListIDs: setInt64State(ctx, value.ImportListIDs, &resp.Diagnostics), NotificationIDs: setInt64State(ctx, value.NotificationIDs, &resp.Diagnostics), RestrictionIDs: setInt64State(ctx, value.RestrictionIDs, &resp.Diagnostics), IndexerIDs: setInt64State(ctx, value.IndexerIDs, &resp.Diagnostics), DownloadClientIDs: setInt64State(ctx, value.DownloadClientIDs, &resp.Diagnostics), AuthorIDs: setInt64State(ctx, value.AuthorIDs, &resp.Diagnostics)})
	}
	state := tagDetailsState{ID: types.StringValue(publicFingerprint(endpoint)), TagID: configuredID, Tags: listObjectState(ctx, tagDetailObjectType(), models, &resp.Diagnostics)}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func tagDetailObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "label": types.StringType, "delay_profile_ids": types.SetType{ElemType: types.Int64Type}, "import_list_ids": types.SetType{ElemType: types.Int64Type}, "notification_ids": types.SetType{ElemType: types.Int64Type}, "restriction_ids": types.SetType{ElemType: types.Int64Type}, "indexer_ids": types.SetType{ElemType: types.Int64Type}, "download_client_ids": types.SetType{ElemType: types.Int64Type}, "author_ids": types.SetType{ElemType: types.Int64Type}}}
}
