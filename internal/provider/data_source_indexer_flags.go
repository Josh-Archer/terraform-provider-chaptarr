package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type indexerFlagsDataSource struct{ client *client.Client }
type indexerFlagsState struct {
	ID    types.String `tfsdk:"id"`
	Flags types.List   `tfsdk:"flags"`
}
type indexerFlagModel struct {
	ID   types.Int64  `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}
type indexerFlagAPI struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func newIndexerFlagsDataSource() datasource.DataSource { return &indexerFlagsDataSource{} }
func (d *indexerFlagsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_indexer_flags"
}
func (d *indexerFlagsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Read the server-owned indexer flag catalog.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{Computed: true}, "flags": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"id": schema.Int64Attribute{Computed: true}, "name": schema.StringAttribute{Computed: true}}}}}}
}
func (d *indexerFlagsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *indexerFlagsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	response, err := d.client.Do(ctx, http.MethodGet, "/api/v1/indexerflag", nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read indexer flags", err.Error())
		return
	}
	var values []indexerFlagAPI
	if json.Unmarshal(response.Body, &values) != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned invalid indexer flags.")
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	models := make([]indexerFlagModel, 0, len(values))
	for _, v := range values {
		models = append(models, indexerFlagModel{ID: types.Int64Value(v.ID), Name: types.StringValue(v.Name)})
	}
	state := indexerFlagsState{ID: types.StringValue(publicFingerprint("/api/v1/indexerflag")), Flags: listObjectState(ctx, indexerFlagObjectType(), models, &resp.Diagnostics)}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func indexerFlagObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "name": types.StringType}}
}
