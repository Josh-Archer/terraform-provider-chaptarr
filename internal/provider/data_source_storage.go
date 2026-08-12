package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

func storageReadOnlyDefinitions() []readOnlyDefinition {
	return []readOnlyDefinition{
		jsonDefinition("remote_path_mappings", "Read the configured remote-path mappings without probing download clients or changing Chaptarr.", "/api/v1/remotepathmapping"),
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
