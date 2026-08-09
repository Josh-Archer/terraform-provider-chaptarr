package provider

import (
	"context"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type baseURLValidator struct{}

func (baseURLValidator) Description(context.Context) string {
	return "must be an HTTP or HTTPS URL with a host and no user information, query, fragment, or dot segments"
}

func (v baseURLValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (baseURLValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := client.ParseBaseURL(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.Append(diag.NewAttributeErrorDiagnostic(
			req.Path,
			"Invalid Chaptarr URL",
			"The provider URL must use HTTP or HTTPS, include a host, and omit user information, query parameters, fragments, and dot segments.",
		))
	}
}
