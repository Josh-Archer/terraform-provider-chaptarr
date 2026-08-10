package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &namingPatternDataSource{}

type namingPatternDataSource struct {
	client *client.Client
}

type namingPatternModel struct {
	ID         types.String `tfsdk:"id"`
	Operation  types.String `tfsdk:"operation"`
	Pattern    types.String `tfsdk:"pattern"`
	ASTJSON    types.String `tfsdk:"ast_json"`
	SampleJSON types.String `tfsdk:"sample_json"`
	ResultJSON types.String `tfsdk:"result_json"`
}

func newNamingPatternDataSource() datasource.DataSource {
	return &namingPatternDataSource{}
}

func (d *namingPatternDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_naming_pattern"
}

func (d *namingPatternDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Compile, decompile, validate, or preview a Chaptarr naming pattern without changing configuration.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true},
			"operation": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Read-only naming operation: compile, decompile, validate, or preview.",
				Validators: []validator.String{
					stringvalidator.OneOf("compile", "decompile", "validate", "preview"),
				},
			},
			"pattern": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Naming pattern used by decompile, validate, or preview.",
			},
			"ast_json": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "JSON-encoded pattern AST used by compile, validate, or preview.",
			},
			"sample_json": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "JSON-encoded sample context used by preview.",
			},
			"result_json": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Bounded JSON response returned by Chaptarr.",
			},
		},
	}
}

func (d *namingPatternDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *namingPatternDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data namingPatternModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	operation := data.Operation.ValueString()
	payload := make(map[string]any)
	if !data.Pattern.IsNull() && !data.Pattern.IsUnknown() {
		payload["pattern"] = data.Pattern.ValueString()
	}
	if !data.ASTJSON.IsNull() && !data.ASTJSON.IsUnknown() {
		var ast any
		if err := json.Unmarshal([]byte(data.ASTJSON.ValueString()), &ast); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("ast_json"), "Invalid naming AST", "`ast_json` must be valid JSON.")
			return
		}
		payload["ast"] = ast
	}
	if !data.SampleJSON.IsNull() && !data.SampleJSON.IsUnknown() {
		var sample any
		if err := json.Unmarshal([]byte(data.SampleJSON.ValueString()), &sample); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("sample_json"), "Invalid naming sample", "`sample_json` must be valid JSON.")
			return
		}
		payload["sample"] = sample
	}

	switch operation {
	case "compile":
		if _, ok := payload["ast"]; !ok {
			resp.Diagnostics.AddAttributeError(path.Root("ast_json"), "Naming AST required", "The compile operation requires `ast_json`.")
			return
		}
	case "decompile":
		if _, ok := payload["pattern"]; !ok {
			resp.Diagnostics.AddAttributeError(path.Root("pattern"), "Naming pattern required", "The decompile operation requires `pattern`.")
			return
		}
	case "validate", "preview":
		_, hasPattern := payload["pattern"]
		_, hasAST := payload["ast"]
		if !hasPattern && !hasAST {
			resp.Diagnostics.AddError("Naming input required", "The operation requires `pattern` or `ast_json`.")
			return
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode naming request", "The naming request could not be encoded.")
		return
	}
	response, err := d.client.Do(ctx, http.MethodPost, "/api/v1/config/naming-pattern/"+operation, body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to evaluate naming pattern", err.Error())
		return
	}

	digest := sha256.Sum256(append([]byte(operation+":"), body...))
	data.ID = types.StringValue(operation + ":" + hex.EncodeToString(digest[:8]))
	data.ResultJSON = types.StringValue(string(response.Body))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

var _ datasource.DataSource = &namingExamplesDataSource{}

type namingExamplesDataSource struct {
	client *client.Client
}

type namingExamplesModel struct {
	ID         types.String `tfsdk:"id"`
	ResultJSON types.String `tfsdk:"result_json"`
}

func newNamingExamplesDataSource() datasource.DataSource {
	return &namingExamplesDataSource{}
}

func (d *namingExamplesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_naming_examples"
}

func (d *namingExamplesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read Chaptarr's current naming examples without changing configuration.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true},
			"result_json": schema.StringAttribute{Computed: true, MarkdownDescription: "Bounded JSON response returned by Chaptarr."},
		},
	}
}

func (d *namingExamplesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *namingExamplesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	response, err := d.client.Do(ctx, http.MethodGet, "/api/v1/config/naming/examples", nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read naming examples", err.Error())
		return
	}
	data := namingExamplesModel{
		ID:         types.StringValue("naming-examples"),
		ResultJSON: types.StringValue(string(response.Body)),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
