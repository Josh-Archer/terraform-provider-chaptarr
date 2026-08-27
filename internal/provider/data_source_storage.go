package provider

import (
	"context"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

func storageReadOnlyDefinitions() []readOnlyDefinition {
	return []readOnlyDefinition{
		jsonDefinition("remote_path_mappings", "Read the configured remote-path mappings without probing download clients or changing Chaptarr.", "/api/v1/remotepathmapping"),
		{
			name:        "remote_path_mapping_suggestions",
			description: "Read path mapping suggestions from Chaptarr and configured download clients without creating path mappings.",
			attributes: map[string]schema.Attribute{
				"download_client_id": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(0)}},
				"host":               schema.StringAttribute{Optional: true},
				"result_json":        resultJSONAttribute(),
			},
			request: remotePathMappingSuggestionsRequest,
			decode:  jsonDecode,
		},
		{
			name:        "root_folders",
			description: "Read root folders, optionally filtered by audiobook or ebook compatibility. Mixed roots appear in both filtered results.",
			attributes: map[string]schema.Attribute{
				"media_type":  schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("audiobook", "ebook")}},
				"result_json": resultJSONAttribute(),
			},
			request: func(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
				values := url.Values{}
				if value := stringInput(ctx, req, resp, "media_type", false); value != "" {
					values.Set("mediaType", value)
				}
				return queryPath("/api/v1/rootfolder", values)
			},
			decode: jsonDecode,
		},
	}
}

func remotePathMappingSuggestionsRequest(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) (string, string) {
	values := url.Values{}
	if clientID, ok := intInput(ctx, req, resp, "download_client_id"); ok {
		values.Set("downloadClientId", strconv.FormatInt(clientID, 10))
	}
	if host := stringInput(ctx, req, resp, "host", false); host != "" {
		values.Set("host", host)
	}
	return queryPath("/api/v1/remotepathmapping/suggestions", values)
}
