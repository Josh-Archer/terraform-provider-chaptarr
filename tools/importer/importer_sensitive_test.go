package main

import (
	"context"
	"slices"
	"sort"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

func TestSensitiveAttributeMappingMatchesProviderSchema(t *testing.T) {
	classified := sensitiveConfigurableAttributes()
	registered := map[string]schema.Schema{}
	for _, factory := range provider.New("test")().Resources(context.Background()) {
		res := factory()
		typeName := resourceTypeName(t, res)
		registered[typeName] = providerResourceSchema(t, res)
	}
	for typeName := range classified {
		if _, ok := registered[typeName]; !ok {
			t.Errorf("importer classifies %s but the provider does not register it", typeName)
		}
	}
	for typeName, sch := range registered {
		got := append([]string(nil), classified[typeName]...)
		sort.Strings(got)
		want := schemaSensitiveConfigurablePaths(t, sch)
		if !slices.Equal(got, want) {
			t.Errorf("%s importer sensitive paths = %#v, schema Sensitive non-computed paths = %#v", typeName, got, want)
		}
	}
}

func TestSingletonSpecsMatchProviderSchema(t *testing.T) {
	schemas := map[string]schema.Schema{}
	for _, factory := range provider.New("test")().Resources(context.Background()) {
		res := factory()
		schemas[resourceTypeName(t, res)] = providerResourceSchema(t, res)
	}
	for _, spec := range singletonSpecs {
		typeName := chaptarrType(spec.typeName)
		sch, ok := schemas[typeName]
		if !ok {
			t.Errorf("singleton %s is not registered", typeName)
			continue
		}
		specified := map[string]configField{}
		for _, field := range spec.fields {
			specified[field.hcl] = field
			attr, ok := sch.Attributes[field.hcl]
			if !ok {
				t.Errorf("%s schema has no %s", typeName, field.hcl)
				continue
			}
			if attr.IsSensitive() != field.sensitive || attr.IsWriteOnly() != field.sensitive {
				t.Errorf("%s.%s importer sensitive=%t, schema sensitive=%t write-only=%t", typeName, field.hcl, field.sensitive, attr.IsSensitive(), attr.IsWriteOnly())
			}
		}
		for name := range sch.Attributes {
			if name == "id" {
				continue
			}
			if _, ok := specified[name]; !ok {
				t.Errorf("%s schema attribute %s is missing from the importer spec", typeName, name)
			}
		}
	}
}

func resourceTypeName(t *testing.T, res resource.Resource) string {
	t.Helper()
	var meta resource.MetadataResponse
	res.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "chaptarr"}, &meta)
	if meta.TypeName == "" {
		t.Fatalf("%T has an empty type name", res)
	}
	return meta.TypeName
}

func providerResourceSchema(t *testing.T, res resource.Resource) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	res.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%T schema: %v", res, resp.Diagnostics)
	}
	return resp.Schema
}

func schemaSensitiveConfigurablePaths(t *testing.T, sch schema.Schema) []string {
	t.Helper()
	var paths []string
	for name, attr := range sch.Attributes {
		collectSensitiveAttribute(t, "", name, attr, &paths)
	}
	for name, block := range sch.Blocks {
		collectSensitiveBlock(t, "", name, block, &paths)
	}
	sort.Strings(paths)
	return paths
}

func collectSensitiveAttribute(t *testing.T, prefix, name string, attr schema.Attribute, out *[]string) {
	t.Helper()
	path := name
	if prefix != "" {
		path = prefix + "." + name
	}
	if attr.IsSensitive() && !attr.IsComputed() {
		*out = append(*out, path)
	}
	switch typed := attr.(type) {
	case schema.BoolAttribute, schema.DynamicAttribute, schema.Float32Attribute, schema.Float64Attribute,
		schema.Int32Attribute, schema.Int64Attribute, schema.ListAttribute, schema.MapAttribute,
		schema.NumberAttribute, schema.ObjectAttribute, schema.SetAttribute, schema.StringAttribute:
	case schema.SingleNestedAttribute:
		for childName, child := range typed.Attributes {
			collectSensitiveAttribute(t, path, childName, child, out)
		}
	case schema.ListNestedAttribute:
		for childName, child := range typed.NestedObject.Attributes {
			collectSensitiveAttribute(t, path, childName, child, out)
		}
	case schema.SetNestedAttribute:
		for childName, child := range typed.NestedObject.Attributes {
			collectSensitiveAttribute(t, path, childName, child, out)
		}
	case schema.MapNestedAttribute:
		for childName, child := range typed.NestedObject.Attributes {
			collectSensitiveAttribute(t, path, childName, child, out)
		}
	default:
		t.Fatalf("unhandled schema attribute %T at %s", attr, path)
	}
}

func collectSensitiveBlock(t *testing.T, prefix, name string, block schema.Block, out *[]string) {
	t.Helper()
	path := name
	if prefix != "" {
		path = prefix + "." + name
	}
	switch typed := block.(type) {
	case schema.SingleNestedBlock:
		for childName, child := range typed.Attributes {
			collectSensitiveAttribute(t, path, childName, child, out)
		}
		for childName, child := range typed.Blocks {
			collectSensitiveBlock(t, path, childName, child, out)
		}
	case schema.ListNestedBlock:
		for childName, child := range typed.NestedObject.Attributes {
			collectSensitiveAttribute(t, path, childName, child, out)
		}
		for childName, child := range typed.NestedObject.Blocks {
			collectSensitiveBlock(t, path, childName, child, out)
		}
	case schema.SetNestedBlock:
		for childName, child := range typed.NestedObject.Attributes {
			collectSensitiveAttribute(t, path, childName, child, out)
		}
		for childName, child := range typed.NestedObject.Blocks {
			collectSensitiveBlock(t, path, childName, child, out)
		}
	default:
		t.Fatalf("unhandled schema block %T at %s", block, path)
	}
}
