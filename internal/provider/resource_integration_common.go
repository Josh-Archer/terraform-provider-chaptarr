package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type integrationBaseAPI struct {
	ID                 int64              `json:"id,omitempty"`
	Name               string             `json:"name"`
	Fields             []providerFieldAPI `json:"fields"`
	ImplementationName string             `json:"implementationName,omitempty"`
	Implementation     string             `json:"implementation"`
	ConfigContract     string             `json:"configContract"`
	Tags               []int64            `json:"tags"`
	Enable             bool               `json:"enable"`
}

func integrationBaseAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":                    schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":                  schema.StringAttribute{Required: true},
		"implementation_name":   schema.StringAttribute{Computed: true},
		"implementation":        schema.StringAttribute{Required: true},
		"config_contract":       schema.StringAttribute{Required: true},
		"enable":                schema.BoolAttribute{Required: true},
		"test_on_apply":         schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Explicitly permit Chaptarr's built-in provider connection/test call when enable is true during create or update."},
		"tags":                  schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type, MarkdownDescription: "Omit to preserve associations managed outside Terraform; configure explicitly to replace them."},
		"field_values_json":     schema.StringAttribute{Required: true, MarkdownDescription: "JSON object mapping non-secret provider setting names to values."},
		"field_values_sha256":   schema.StringAttribute{Computed: true},
		"secret_fields":         schema.MapAttribute{Optional: true, Sensitive: true, WriteOnly: true, ElementType: types.StringType, MarkdownDescription: "Apply-only map of password, token, or API-key field names to values."},
		"protected_field_names": schema.SetAttribute{Computed: true, ElementType: types.StringType},
	}
}

func validateIntegrationActivation(enable, testOnApply types.Bool, diagnostics *diag.Diagnostics) bool {
	if valueBool(enable) && !valueBool(testOnApply) {
		diagnostics.AddAttributeError(path.Root("test_on_apply"), "Explicit provider test opt-in required", "Chaptarr automatically tests enabled providers during create and update. Set test_on_apply=true to permit that external action, or keep enable=false.")
	}
	return !diagnostics.HasError()
}

func normalizeIntegrationTestAuthorization(value *types.Bool) {
	if value.IsNull() || value.IsUnknown() {
		*value = types.BoolValue(false)
	}
}

func loadIntegrationSecrets(ctx context.Context, config tfsdk.Config, target *types.Map, diagnostics *diag.Diagnostics) {
	diagnostics.Append(config.GetAttribute(ctx, path.Root("secret_fields"), target)...)
}

func integrationBasePayload(ctx context.Context, id int64, name, implementation, contract types.String, enable types.Bool, tags types.Set, valuesJSON types.String, secrets types.Map, diagnostics *diag.Diagnostics) integrationBaseAPI {
	if strings.TrimSpace(name.ValueString()) == "" || strings.TrimSpace(implementation.ValueString()) == "" || strings.TrimSpace(contract.ValueString()) == "" {
		diagnostics.AddError("Invalid integration identity", "Name, implementation, and config_contract must all be non-empty.")
	}
	values := map[string]any{}
	if json.Unmarshal([]byte(valuesJSON.ValueString()), &values) != nil {
		diagnostics.AddError("Invalid field_values_json", "Provide a valid JSON object of non-secret provider setting values.")
		return integrationBaseAPI{}
	}
	fields := make([]providerFieldAPI, 0, len(values))
	for fieldName, value := range values {
		if strings.TrimSpace(fieldName) == "" {
			diagnostics.AddError("Invalid provider field", "Provider field names must not be empty.")
			continue
		}
		fields = append(fields, providerFieldAPI{Name: fieldName, Value: value})
	}
	if !secrets.IsNull() && !secrets.IsUnknown() {
		secretValues := map[string]string{}
		diagnostics.Append(secrets.ElementsAs(ctx, &secretValues, false)...)
		for fieldName, value := range secretValues {
			if strings.TrimSpace(fieldName) == "" {
				diagnostics.AddError("Invalid protected field", "Protected provider field names must not be empty.")
				continue
			}
			fields = append(fields, providerFieldAPI{Name: fieldName, Value: value, Privacy: "password", Type: "password"})
		}
	}
	sortProviderFields(fields)
	return integrationBaseAPI{ID: id, Name: strings.TrimSpace(name.ValueString()), Fields: fields, Implementation: implementation.ValueString(), ConfigContract: contract.ValueString(), Tags: setInt64Values(ctx, tags, diagnostics), Enable: valueBool(enable)}
}

func validateIntegrationFields(ctx context.Context, apiClient *client.Client, endpoint, implementation, contract string, fields []providerFieldAPI, diagnostics *diag.Diagnostics) bool {
	response, err := apiClient.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		diagnostics.AddError("Unable to validate provider settings", err.Error())
		return false
	}
	var templates []metadataAPI
	if json.Unmarshal(response.Body, &templates) != nil {
		diagnostics.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid provider schema.")
		return false
	}
	privacy := map[string]bool{}
	for _, template := range templates {
		if template.Implementation == implementation && template.ConfigContract == contract {
			for _, field := range template.Fields {
				privacy[field.Name] = sensitiveProviderField(field)
			}
		}
	}
	if len(privacy) == 0 {
		diagnostics.AddError("Provider schema not found", "The configured implementation and config contract are not advertised by Chaptarr.")
		return false
	}
	for _, field := range fields {
		expectedSensitive, exists := privacy[field.Name]
		if !exists {
			diagnostics.AddError("Unknown provider field", fmt.Sprintf("Field %q is not advertised by the selected provider schema.", field.Name))
			continue
		}
		providedSensitive := sensitiveProviderField(field)
		if expectedSensitive && !providedSensitive {
			diagnostics.AddError("Sensitive provider field misplaced", fmt.Sprintf("Move field %q from field_values_json to the write-only secret_fields map.", field.Name))
		}
		if !expectedSensitive && providedSensitive {
			diagnostics.AddError("Non-secret provider field misplaced", fmt.Sprintf("Move field %q from secret_fields to field_values_json.", field.Name))
		}
	}
	return !diagnostics.HasError()
}

func integrationFieldState(fields []providerFieldAPI) (string, string, []string) {
	values := map[string]any{}
	protected := []string{}
	for _, field := range fields {
		if sensitiveProviderField(field) {
			protected = append(protected, field.Name)
			continue
		}
		values[field.Name] = field.Value
	}
	sort.Strings(protected)
	canonical, hash := canonicalValue(values)
	return canonical, hash, protected
}

func setIntegrationBaseState(ctx context.Context, current integrationBaseAPI, id, name, implementationName, implementation, contract *types.String, enable *types.Bool, tags *types.Set, valuesJSON, valuesHash *types.String, secrets *types.Map, protected *types.Set, diagnostics *diag.Diagnostics) {
	*id = types.StringValue(fmt.Sprintf("%d", current.ID))
	*name = types.StringValue(current.Name)
	*implementationName = types.StringValue(current.ImplementationName)
	*implementation = types.StringValue(current.Implementation)
	*contract = types.StringValue(current.ConfigContract)
	*enable = types.BoolValue(current.Enable)
	*tags = setInt64State(ctx, current.Tags, diagnostics)
	canonical, hash, protectedNames := integrationFieldState(current.Fields)
	*valuesJSON, *valuesHash = types.StringValue(canonical), types.StringValue(hash)
	*secrets = types.MapNull(types.StringType)
	*protected = setStringState(ctx, protectedNames, diagnostics)
}
