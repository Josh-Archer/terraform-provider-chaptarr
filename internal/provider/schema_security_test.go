package provider

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode"

	frameworkdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestCredentialLikeSchemaAttributesAreSensitiveOrWriteOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := &ChaptarrProvider{version: "schema-security-test"}

	providerResponse := &frameworkprovider.SchemaResponse{}
	p.Schema(ctx, frameworkprovider.SchemaRequest{}, providerResponse)
	auditSchemaContainer(t, "provider", reflect.ValueOf(providerResponse.Schema))

	for index, factory := range p.Resources(ctx) {
		response := &frameworkresource.SchemaResponse{}
		factory().Schema(ctx, frameworkresource.SchemaRequest{}, response)
		auditSchemaContainer(t, "resource["+strconv.Itoa(index)+"]", reflect.ValueOf(response.Schema))
	}
	for index, factory := range p.DataSources(ctx) {
		response := &frameworkdatasource.SchemaResponse{}
		factory().Schema(ctx, frameworkdatasource.SchemaRequest{}, response)
		auditSchemaContainer(t, "data-source["+strconv.Itoa(index)+"]", reflect.ValueOf(response.Schema))
	}
}

func auditSchemaContainer(t *testing.T, prefix string, value reflect.Value) {
	t.Helper()
	value = indirect(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return
	}
	for _, fieldName := range []string{"Attributes", "Blocks"} {
		field := value.FieldByName(fieldName)
		if field.IsValid() {
			auditSchemaMap(t, prefix, field)
		}
	}
}

func auditSchemaMap(t *testing.T, prefix string, value reflect.Value) {
	t.Helper()
	value = indirect(value)
	if !value.IsValid() || value.Kind() != reflect.Map {
		return
	}
	for _, key := range value.MapKeys() {
		name := key.String()
		attribute := value.MapIndex(key)
		path := prefix + "." + name
		if isCredentialAttributeName(name) && !schemaValueIsProtected(attribute) {
			t.Errorf("credential-like schema attribute %s must be Sensitive or WriteOnly", path)
		}
		auditNestedSchemaValue(t, path, attribute)
	}
}

func auditNestedSchemaValue(t *testing.T, prefix string, value reflect.Value) {
	t.Helper()
	value = indirect(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return
	}
	if attributes := value.FieldByName("Attributes"); attributes.IsValid() {
		auditSchemaMap(t, prefix, attributes)
	}
	if nestedObject := value.FieldByName("NestedObject"); nestedObject.IsValid() {
		auditSchemaContainer(t, prefix, nestedObject)
	}
}

func schemaValueIsProtected(value reflect.Value) bool {
	value = indirect(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return false
	}
	for _, fieldName := range []string{"Sensitive", "WriteOnly"} {
		field := value.FieldByName(fieldName)
		if field.IsValid() && field.Kind() == reflect.Bool && field.Bool() {
			return true
		}
	}
	return false
}

func isCredentialAttributeName(name string) bool {
	canonical := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
	for _, marker := range []string{"apikey", "authorization", "cookie", "credential", "password", "passphrase", "privatekey", "secret", "token"} {
		if strings.Contains(canonical, marker) {
			return true
		}
	}
	return false
}

func indirect(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}
