// Package provider implements the Terraform Plugin Framework provider.
package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	providerversion "github.com/Josh-Archer/terraform-provider-chaptarr/internal/version"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	defaultRequestTimeoutSeconds = int64(client.DefaultRequestTimeout / time.Second)
	minimumRequestTimeoutSeconds = int64(client.MinRequestTimeout / time.Second)
	maximumRequestTimeoutSeconds = int64(client.MaxRequestTimeout / time.Second)
)

var _ provider.Provider = &ChaptarrProvider{}

// ChaptarrProvider is the root provider implementation.
type ChaptarrProvider struct {
	version string
}

type providerModel struct {
	URL                types.String `tfsdk:"url"`
	APIKey             types.String `tfsdk:"api_key"`
	InsecureSkipVerify types.Bool   `tfsdk:"insecure_skip_verify"`
	RequestTimeout     types.Int64  `tfsdk:"request_timeout_seconds"`
}

type providerConfig struct {
	baseURL            string
	apiKey             string
	insecureSkipVerify bool
	requestTimeout     time.Duration
}

// New returns a provider factory for the requested build version.
func New(buildVersion string) func() provider.Provider {
	return func() provider.Provider {
		return &ChaptarrProvider{version: buildVersion}
	}
}

func (p *ChaptarrProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "chaptarr"
	resp.Version = p.version
}

func (p *ChaptarrProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage Chaptarr through its API.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				MarkdownDescription: "Chaptarr base URL. May also be set with `CHAPTARR_URL`. Reverse-proxy subpaths are supported.",
				Optional:            true,
				Validators: []validator.String{
					baseURLValidator{},
				},
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Chaptarr API key sent only in the `X-Api-Key` header. May also be set with `CHAPTARR_API_KEY`.",
				Optional:            true,
				Sensitive:           true,
			},
			"insecure_skip_verify": schema.BoolAttribute{
				MarkdownDescription: "Skip TLS certificate verification. Defaults to `false`. Use only for explicitly trusted private endpoints.",
				Optional:            true,
			},
			"request_timeout_seconds": schema.Int64Attribute{
				MarkdownDescription: fmt.Sprintf("Maximum duration for an API operation, including retries. Defaults to %d seconds.", defaultRequestTimeoutSeconds),
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.Between(minimumRequestTimeoutSeconds, maximumRequestTimeoutSeconds),
				},
			},
		},
	}
}

func (p *ChaptarrProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, diagnostics := resolveProviderConfig(data, os.Getenv)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient, err := client.New(client.Config{
		BaseURL:            config.baseURL,
		APIKey:             config.apiKey,
		UserAgent:          providerversion.UserAgent(p.version),
		InsecureSkipVerify: config.insecureSkipVerify,
		Timeout:            config.requestTimeout,
	})
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", "The Chaptarr client could not be configured. Check the provider URL, credentials, and timeout settings.")
		return
	}

	// Client construction is intentionally offline; API access begins only when
	// a future resource or data source performs an operation.
	resp.DataSourceData = apiClient
	resp.ResourceData = apiClient
}

func (p *ChaptarrProvider) Resources(context.Context) []func() resource.Resource {
	resources := make([]func() resource.Resource, 0, len(singletonConfigDefinitions)+13)
	for _, definition := range singletonConfigDefinitions {
		resources = append(resources, newSingletonConfigResource(definition))
	}
	resources = append(resources,
		newHardcoverConfigResource,
		newRootFolderResource,
		newRemotePathMappingResource,
		newQualityProfileResource,
		newMetadataProfileResource,
		newReleaseProfileResource,
		newDelayProfileResource,
		newQualityDefinitionResource,
		newPostgresDatabaseResource,
		newMetadataResource,
		newCustomFormatResource,
		newCustomFilterResource,
		newTagResource,
		newProxyResource,
	)
	return resources
}

func (p *ChaptarrProvider) DataSources(context.Context) []func() datasource.DataSource {
	dataSources := []func() datasource.DataSource{
		newNamingPatternDataSource,
		newNamingExamplesDataSource,
	}
	for _, definition := range readOnlyDefinitions() {
		dataSources = append(dataSources, newReadOnlyDataSource(definition))
	}
	dataSources = append(dataSources,
		newQualityProfileSchemaDataSource,
		newMetadataProfileSchemaDataSource,
		newMetadataSchemaDataSource,
		newCustomFormatSchemaDataSource,
		newTagDetailsDataSource,
	)
	return dataSources
}

func resolveProviderConfig(data providerModel, getenv func(string) string) (providerConfig, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	if data.URL.IsUnknown() {
		diagnostics.AddError("Unknown Chaptarr URL", "The provider URL must be known while configuring the provider.")
	}
	if data.APIKey.IsUnknown() {
		diagnostics.AddError("Unknown Chaptarr API key", "The provider API key must be known while configuring the provider.")
	}
	if data.InsecureSkipVerify.IsUnknown() {
		diagnostics.AddError("Unknown TLS verification setting", "`insecure_skip_verify` must be known while configuring the provider.")
	}
	if data.RequestTimeout.IsUnknown() {
		diagnostics.AddError("Unknown request timeout", "`request_timeout_seconds` must be known while configuring the provider.")
	}
	if diagnostics.HasError() {
		return providerConfig{}, diagnostics
	}

	baseURL := configuredString(data.URL, "CHAPTARR_URL", getenv)
	apiKey := configuredString(data.APIKey, "CHAPTARR_API_KEY", getenv)
	if baseURL == "" {
		diagnostics.AddError("Missing Chaptarr URL", "Set the provider `url` attribute or the `CHAPTARR_URL` environment variable.")
	}
	if apiKey == "" {
		diagnostics.AddError("Missing Chaptarr API key", "Set the sensitive provider `api_key` attribute or the `CHAPTARR_API_KEY` environment variable.")
	}
	if diagnostics.HasError() {
		return providerConfig{}, diagnostics
	}
	if _, err := client.ParseBaseURL(baseURL); err != nil {
		diagnostics.AddError("Invalid Chaptarr URL", "The provider URL must use HTTP or HTTPS, include a host, and omit user information, query parameters, fragments, and dot segments.")
		return providerConfig{}, diagnostics
	}

	insecure := false
	if !data.InsecureSkipVerify.IsNull() && !data.InsecureSkipVerify.IsUnknown() {
		insecure = data.InsecureSkipVerify.ValueBool()
	}
	timeout := client.DefaultRequestTimeout
	if !data.RequestTimeout.IsNull() && !data.RequestTimeout.IsUnknown() {
		seconds := data.RequestTimeout.ValueInt64()
		if seconds < minimumRequestTimeoutSeconds || seconds > maximumRequestTimeoutSeconds {
			diagnostics.AddError("Invalid request timeout", fmt.Sprintf("`request_timeout_seconds` must be between %d and %d.", minimumRequestTimeoutSeconds, maximumRequestTimeoutSeconds))
			return providerConfig{}, diagnostics
		}
		timeout = time.Duration(seconds) * time.Second
	}

	return providerConfig{
		baseURL:            baseURL,
		apiKey:             apiKey,
		insecureSkipVerify: insecure,
		requestTimeout:     timeout,
	}, diagnostics
}

func configuredString(value types.String, environmentName string, getenv func(string) string) string {
	if !value.IsNull() {
		return strings.TrimSpace(value.ValueString())
	}
	return strings.TrimSpace(getenv(environmentName))
}
