package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type qualityProfileSchemaDataSource struct{ client *client.Client }

type qualityProfileSchemaModel struct {
	ID                             types.String `tfsdk:"id"`
	MediaType                      types.String `tfsdk:"media_type"`
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

func newQualityProfileSchemaDataSource() datasource.DataSource {
	return &qualityProfileSchemaDataSource{}
}

func (d *qualityProfileSchemaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_quality_profile_schema"
}

func qualityLeafDataSourceAttributes() map[string]dsschema.Attribute {
	return map[string]dsschema.Attribute{
		"id": dsschema.Int64Attribute{Computed: true}, "name": dsschema.StringAttribute{Computed: true},
		"quality_id": dsschema.Int64Attribute{Computed: true}, "quality_name": dsschema.StringAttribute{Computed: true},
		"quality_is_conversion_target": dsschema.BoolAttribute{Computed: true}, "allowed": dsschema.BoolAttribute{Computed: true},
	}
}

func qualityItemDataSourceAttributes() map[string]dsschema.Attribute {
	attributes := qualityLeafDataSourceAttributes()
	attributes["items"] = dsschema.ListNestedAttribute{Computed: true, NestedObject: dsschema.NestedAttributeObject{Attributes: qualityLeafDataSourceAttributes()}}
	return attributes
}

func (d *qualityProfileSchemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{MarkdownDescription: "Fetch the typed Chaptarr quality-profile template for one media type before configuring a profile.", Attributes: map[string]dsschema.Attribute{
		"id":         dsschema.StringAttribute{Computed: true},
		"media_type": dsschema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}},
		"name":       dsschema.StringAttribute{Computed: true}, "profile_type": dsschema.StringAttribute{Computed: true},
		"upgrade_allowed": dsschema.BoolAttribute{Computed: true}, "prefer_custom_formats_over_quality": dsschema.BoolAttribute{Computed: true},
		"convert_to_quality_id": dsschema.Int64Attribute{Computed: true}, "cutoff": dsschema.Int64Attribute{Computed: true},
		"items":                dsschema.ListNestedAttribute{Computed: true, NestedObject: dsschema.NestedAttributeObject{Attributes: qualityItemDataSourceAttributes()}},
		"minimum_format_score": dsschema.Int64Attribute{Computed: true}, "cutoff_format_score": dsschema.Int64Attribute{Computed: true},
		"format_items": dsschema.ListNestedAttribute{Computed: true, NestedObject: dsschema.NestedAttributeObject{Attributes: map[string]dsschema.Attribute{
			"format_id": dsschema.Int64Attribute{Computed: true}, "built_in_key": dsschema.StringAttribute{Computed: true}, "name": dsschema.StringAttribute{Computed: true}, "score": dsschema.Int64Attribute{Computed: true},
		}}},
	}}
}

func (d *qualityProfileSchemaDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *qualityProfileSchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var mediaType types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathFor("media_type"), &mediaType)...)
	if resp.Diagnostics.HasError() || mediaType.IsNull() || mediaType.IsUnknown() {
		return
	}
	requestPath := "/api/v1/qualityprofile/schema?" + url.Values{"mediaType": []string{mediaType.ValueString()}}.Encode()
	response, err := d.client.Do(ctx, http.MethodGet, requestPath, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read quality-profile schema", err.Error())
		return
	}
	var current qualityProfileAPI
	if json.Unmarshal(response.Body, &current) != nil || current.ProfileType != mediaType.ValueString() {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid or mismatched quality-profile schema.")
		return
	}
	profile, ok := qualityProfileState(ctx, current, &resp.Diagnostics)
	if !ok {
		return
	}
	state := qualityProfileSchemaModel{ID: types.StringValue(publicFingerprint(requestPath)), MediaType: mediaType, Name: profile.Name, ProfileType: profile.ProfileType, UpgradeAllowed: profile.UpgradeAllowed, PreferCustomFormatsOverQuality: profile.PreferCustomFormatsOverQuality, ConvertToQualityID: profile.ConvertToQualityID, Cutoff: profile.Cutoff, Items: profile.Items, MinimumFormatScore: profile.MinimumFormatScore, CutoffFormatScore: profile.CutoffFormatScore, FormatItems: profile.FormatItems}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

type metadataProfileSchemaDataSource struct{ client *client.Client }

type metadataLanguageModel struct {
	Name types.String `tfsdk:"name"`
	Code types.String `tfsdk:"code"`
}

type metadataProfileSchemaState struct {
	ID                           types.String  `tfsdk:"id"`
	MinimumPopularity            types.Float64 `tfsdk:"minimum_popularity"`
	MinimumPages                 types.Int64   `tfsdk:"minimum_pages"`
	SkipMissingDate              types.Bool    `tfsdk:"skip_missing_date"`
	SkipMissingISBN              types.Bool    `tfsdk:"skip_missing_isbn"`
	SkipPartsAndSets             types.Bool    `tfsdk:"skip_parts_and_sets"`
	SkipSecondarySeries          types.Bool    `tfsdk:"skip_secondary_series"`
	SkipOmnibusWithoutIdentifier types.Bool    `tfsdk:"skip_omnibus_without_identifier"`
	SkipOmnibus                  types.Bool    `tfsdk:"skip_omnibus"`
	SkipMissingASIN              types.Bool    `tfsdk:"skip_missing_asin"`
	Languages                    types.List    `tfsdk:"languages"`
	SpecialLanguages             types.List    `tfsdk:"special_languages"`
}

type metadataLanguagesAPI struct {
	Languages     []metadataLanguageAPI `json:"languages"`
	SpecialValues []metadataLanguageAPI `json:"specialValues"`
}
type metadataLanguageAPI struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

func newMetadataProfileSchemaDataSource() datasource.DataSource {
	return &metadataProfileSchemaDataSource{}
}
func (d *metadataProfileSchemaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metadata_profile_schema"
}
func (d *metadataProfileSchemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	language := dsschema.NestedAttributeObject{Attributes: map[string]dsschema.Attribute{"name": dsschema.StringAttribute{Computed: true}, "code": dsschema.StringAttribute{Computed: true}}}
	resp.Schema = dsschema.Schema{MarkdownDescription: "Fetch Chaptarr metadata-profile defaults and the typed language vocabulary used to validate profile configuration.", Attributes: map[string]dsschema.Attribute{
		"id": dsschema.StringAttribute{Computed: true}, "minimum_popularity": dsschema.Float64Attribute{Computed: true}, "minimum_pages": dsschema.Int64Attribute{Computed: true},
		"skip_missing_date": dsschema.BoolAttribute{Computed: true}, "skip_missing_isbn": dsschema.BoolAttribute{Computed: true}, "skip_parts_and_sets": dsschema.BoolAttribute{Computed: true},
		"skip_secondary_series": dsschema.BoolAttribute{Computed: true}, "skip_omnibus_without_identifier": dsschema.BoolAttribute{Computed: true}, "skip_omnibus": dsschema.BoolAttribute{Computed: true}, "skip_missing_asin": dsschema.BoolAttribute{Computed: true},
		"languages": dsschema.ListNestedAttribute{Computed: true, NestedObject: language}, "special_languages": dsschema.ListNestedAttribute{Computed: true, NestedObject: language},
	}}
}
func (d *metadataProfileSchemaDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *metadataProfileSchemaDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	schemaResponse, err := d.client.Do(ctx, http.MethodGet, "/api/v1/metadataprofile/schema", nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read metadata-profile schema", err.Error())
		return
	}
	languageResponse, err := d.client.Do(ctx, http.MethodGet, "/api/v1/metadataprofile/languages", nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read metadata-profile languages", err.Error())
		return
	}
	var defaults metadataProfileAPI
	var vocabulary metadataLanguagesAPI
	if json.Unmarshal(schemaResponse.Body, &defaults) != nil || json.Unmarshal(languageResponse.Body, &vocabulary) != nil {
		resp.Diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid metadata-profile schema or language vocabulary.")
		return
	}
	state := metadataProfileSchemaState{ID: types.StringValue(publicFingerprint("/api/v1/metadataprofile/schema+/languages")), MinimumPopularity: types.Float64Value(defaults.MinPopularity), MinimumPages: types.Int64Value(defaults.MinPages), SkipMissingDate: types.BoolValue(defaults.SkipMissingDate), SkipMissingISBN: types.BoolValue(defaults.SkipMissingISBN), SkipPartsAndSets: types.BoolValue(defaults.SkipPartsAndSets), SkipSecondarySeries: types.BoolValue(defaults.SkipSeriesSecondary), SkipOmnibusWithoutIdentifier: types.BoolValue(defaults.SkipMissingIdentifierOmnibus), SkipOmnibus: types.BoolValue(defaults.SkipOmnibus), SkipMissingASIN: types.BoolValue(defaults.SkipMissingASIN)}
	state.Languages = languageState(ctx, vocabulary.Languages, &resp.Diagnostics)
	state.SpecialLanguages = languageState(ctx, vocabulary.SpecialValues, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func languageState(ctx context.Context, values []metadataLanguageAPI, diagnostics *diag.Diagnostics) types.List {
	models := make([]metadataLanguageModel, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.Code) == "" {
			diagnostics.AddError("Invalid Chaptarr response", "A metadata language is missing its code.")
			continue
		}
		models = append(models, metadataLanguageModel{Name: types.StringValue(value.Name), Code: types.StringValue(value.Code)})
	}
	return listObjectState(ctx, types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType, "code": types.StringType}}, models, diagnostics)
}

func publicFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
