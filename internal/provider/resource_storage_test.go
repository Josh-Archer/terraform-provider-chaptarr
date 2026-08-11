package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const storageSyntheticKey = "storage-api-key-sentinel-891c"

func TestRootFolderPasswordIsWriteOnlyAndSensitive(t *testing.T) {
	t.Parallel()

	response := &resource.SchemaResponse{}
	(&rootFolderResource{}).Schema(t.Context(), resource.SchemaRequest{}, response)
	attribute, ok := response.Schema.Attributes["password"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("password schema type = %T", response.Schema.Attributes["password"])
	}
	if !attribute.Sensitive || !attribute.WriteOnly || attribute.Computed {
		t.Fatalf("password must be Sensitive+WriteOnly and not Computed: %#v", attribute)
	}
}

func TestRootFolderValidationRejectsCrossMediaAndUnsafeCalibreCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		model    rootFolderModel
		creating bool
	}{
		{
			name:  "ebook settings on audiobook root",
			model: rootFolderModel{FolderType: types.StringValue("audiobook"), EbookMonitorFuture: types.BoolValue(true)},
		},
		{
			name:     "password without username",
			model:    rootFolderModel{FolderType: types.StringValue("mixed"), IsCalibreLibrary: types.BoolValue(true), Host: types.StringValue("calibre"), Port: types.Int64Value(8080), Library: types.StringValue("library"), OutputProfile: types.StringValue("default"), Password: types.StringValue("synthetic-password")},
			creating: true,
		},
		{
			name:     "create username without password",
			model:    rootFolderModel{FolderType: types.StringValue("mixed"), IsCalibreLibrary: types.BoolValue(true), Host: types.StringValue("calibre"), Port: types.Int64Value(8080), Library: types.StringValue("library"), OutputProfile: types.StringValue("default"), Username: types.StringValue("user")},
			creating: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var diagnostics diag.Diagnostics
			if validateRootFolderModel(test.model, test.creating, &diagnostics) || !diagnostics.HasError() {
				t.Fatal("expected validation error")
			}
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Detail(), "synthetic-password") {
					t.Fatal("diagnostic leaked a password")
				}
			}
		})
	}
}

func TestRootFolderPayloadIncludesPasswordOnlyWhenExplicit(t *testing.T) {
	t.Parallel()

	model := rootFolderModel{
		Name: types.StringValue("Library"), Path: types.StringValue("/library"), FolderType: types.StringValue("mixed"),
		DefaultTags: types.SetValueMust(types.Int64Type, []attr.Value{types.Int64Value(2), types.Int64Value(5)}),
	}
	payload, diagnostics := rootFolderPayload(context.Background(), model, 7)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if payload.ID != 7 || payload.FolderType != 0 || payload.Password != nil || len(payload.DefaultTags) != 2 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	model.Password = types.StringValue("synthetic-explicit-password")
	payload, diagnostics = rootFolderPayload(context.Background(), model, 7)
	if diagnostics.HasError() || payload.Password == nil || *payload.Password != "synthetic-explicit-password" {
		t.Fatal("explicit password was not included in the request-only payload")
	}
}

func TestRootFolderWriteOnlyPasswordLoadsFromApplyConfigOnly(t *testing.T) {
	t.Parallel()

	schemaResponse := &resource.SchemaResponse{}
	(&rootFolderResource{}).Schema(t.Context(), resource.SchemaRequest{}, schemaResponse)
	terraformType := schemaResponse.Schema.Type().TerraformType(t.Context())
	objectType, ok := terraformType.(tftypes.Object)
	if !ok {
		t.Fatalf("root-folder schema type = %T", terraformType)
	}
	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	const ephemeralPassword = "ephemeral-calibre-password-4812"
	values["password"] = tftypes.NewValue(tftypes.String, ephemeralPassword)
	config := tfsdk.Config{Raw: tftypes.NewValue(terraformType, values), Schema: schemaResponse.Schema}
	plan := rootFolderModel{
		Name:        types.StringValue("Library"),
		Path:        types.StringValue("/library"),
		FolderType:  types.StringValue("mixed"),
		DefaultTags: types.SetNull(types.Int64Type),
		Password:    types.StringNull(),
	}
	var diagnostics diag.Diagnostics
	loadRootFolderWriteOnlyConfig(t.Context(), config, &plan, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	payload, payloadDiagnostics := rootFolderPayload(t.Context(), plan, 0)
	if payloadDiagnostics.HasError() || payload.Password == nil || *payload.Password != ephemeralPassword {
		t.Fatal("configured write-only password did not reach the apply request payload")
	}
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Detail(), ephemeralPassword) || strings.Contains(diagnostic.Summary(), ephemeralPassword) {
			t.Fatal("diagnostics leaked the configured write-only password")
		}
	}
	plan.Password = types.StringNull()
	if strings.Contains(plan.Password.String(), ephemeralPassword) {
		t.Fatal("write-only password remained in the state model")
	}
}

func TestRemotePathMappingValidationRequiresHostWithoutClient(t *testing.T) {
	t.Parallel()

	var diagnostics diag.Diagnostics
	resource := &remotePathMappingResource{}
	model := remotePathMappingModel{DownloadClientID: types.Int64Value(0), Host: types.StringValue(" ")}
	if resource.validateModel(model, &diagnostics) || !diagnostics.HasError() {
		t.Fatal("expected missing-host diagnostic")
	}
}

func TestRemotePathProbeIsOptInAndStoresNoObservedPaths(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/remotepathmapping/test" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("X-Api-Key") != storageSyntheticKey {
			t.Fatal("missing header-only API key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"isMapped":true,"localPathExists":true,"localPathWritable":true,"downloadClientPathChecked":true,"downloadClientMatchedPath":"/private/remote","downloadClientItemMappedPath":"/private/local"}`))
	}))
	defer server.Close()

	apiClient, err := client.New(client.Config{BaseURL: server.URL, APIKey: storageSyntheticKey, UserAgent: "test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	resource := &remotePathMappingResource{client: apiClient}
	model := remotePathMappingModel{DownloadClientID: types.Int64Value(1), RemotePath: types.StringValue("/remote"), LocalPath: types.StringValue("/local"), TestBeforeApply: types.BoolValue(false)}
	var diagnostics diag.Diagnostics
	if !resource.test(t.Context(), &model, &diagnostics) || diagnostics.HasError() || requests != 0 {
		t.Fatalf("disabled probe performed work: requests=%d diagnostics=%v", requests, diagnostics)
	}
	model.TestBeforeApply = types.BoolValue(true)
	if !resource.test(t.Context(), &model, &diagnostics) || diagnostics.HasError() || requests != 1 {
		t.Fatalf("enabled probe failed: requests=%d diagnostics=%v", requests, diagnostics)
	}
	serialized := model.LastTestError.String() + model.LastTestIsMapped.String()
	if strings.Contains(serialized, "/private/") {
		t.Fatal("probe state included observed remote or local paths")
	}
}

func TestCreatedIdentifierAcceptsPositiveNumericRepresentations(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{"id":42}`, `42`, `"42"`} {
		if id, err := createdIdentifier([]byte(body)); err != nil || id != 42 {
			t.Fatalf("createdIdentifier(%s) = %d, %v", body, id, err)
		}
	}
	for _, body := range []string{`{"id":0}`, `0`, `not-json`} {
		if _, err := createdIdentifier([]byte(body)); err == nil {
			t.Fatalf("createdIdentifier(%s) unexpectedly succeeded", body)
		}
	}
}
