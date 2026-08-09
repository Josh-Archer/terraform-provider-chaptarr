package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestSingletonConfigConversionWritesAudiobookAndEbookValues(t *testing.T) {
	t.Parallel()

	current := map[string]any{
		"id": 1,
		"audiobookConversionConcurrentConversions": 1,
		"audiobookConversionMaxBitrate":            64,
		"audiobookConversionMaxCpuThreads":         2,
		"audiobookConversionNoUpscale":             false,
		"audiobookConversionAudioChannels":         "stereo",
		"audiobookConversionTagMode":               "preserve",
		"ebookConversionEnabled":                   false,
		"ebookConversionTargetFormat":              "epub",
	}
	var written map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodGet && (req.URL.Path == "/api/v1/config/conversion" || req.URL.Path == "/api/v1/config/conversion/1"):
			_ = json.NewEncoder(w).Encode(current)
		case req.Method == http.MethodPut && req.URL.Path == "/api/v1/config/conversion/1":
			if err := json.NewDecoder(req.Body).Decode(&written); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			current = written
			_ = json.NewEncoder(w).Encode(current)
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	definition := singletonDefinition(t, "conversion_config")
	resourceUnderTest := &singletonConfigResource{
		client:     testAPIClient(t, server.URL),
		definition: definition,
	}
	plan, resourceSchema := singletonPlan(t, resourceUnderTest, map[string]any{
		"audiobook_concurrent_conversions": int64(3),
		"audiobook_max_bitrate":            int64(128),
		"audiobook_no_upscale":             true,
		"audiobook_audio_channels":         "mono",
		"ebook_enabled":                    true,
		"ebook_target_format":              "azw3",
	})
	response := &frameworkresource.CreateResponse{State: emptyState(resourceSchema)}
	resourceUnderTest.Create(context.Background(), frameworkresource.CreateRequest{Plan: plan}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", response.Diagnostics)
	}

	assertJSONValue(t, written, "audiobookConversionConcurrentConversions", float64(3))
	assertJSONValue(t, written, "audiobookConversionMaxBitrate", float64(128))
	assertJSONValue(t, written, "audiobookConversionNoUpscale", true)
	assertJSONValue(t, written, "audiobookConversionAudioChannels", "mono")
	assertJSONValue(t, written, "ebookConversionEnabled", true)
	assertJSONValue(t, written, "ebookConversionTargetFormat", "azw3")
	// Unconfigured values must survive the read/merge/write cycle.
	assertJSONValue(t, written, "audiobookConversionMaxCpuThreads", float64(2))
}

func TestSingletonConfigMediaManagementWritesAudiobookAndEbookValues(t *testing.T) {
	t.Parallel()

	current := map[string]any{
		"id":                             4,
		"createEmptyAuthorFolders":       false,
		"createEmptyEbookAuthorFolders":  false,
		"defaultAudiobookRootFolderPath": "/old/audio",
		"defaultEbookRootFolderPath":     "/old/ebooks",
		"copyUsingHardlinks":             false,
	}
	var written map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodGet && (req.URL.Path == "/api/v1/config/mediamanagement" || req.URL.Path == "/api/v1/config/mediamanagement/4"):
			_ = json.NewEncoder(w).Encode(current)
		case req.Method == http.MethodPut && req.URL.Path == "/api/v1/config/mediamanagement/4":
			if err := json.NewDecoder(req.Body).Decode(&written); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			current = written
			_ = json.NewEncoder(w).Encode(current)
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	definition := singletonDefinition(t, "media_management_config")
	resourceUnderTest := &singletonConfigResource{client: testAPIClient(t, server.URL), definition: definition}
	plan, resourceSchema := singletonPlan(t, resourceUnderTest, map[string]any{
		"create_empty_author_folders":        true,
		"create_empty_ebook_author_folders":  true,
		"default_audiobook_root_folder_path": "/library/audiobooks",
		"default_ebook_root_folder_path":     "/library/ebooks",
		"copy_using_hardlinks":               true,
	})
	response := &frameworkresource.UpdateResponse{State: emptyState(resourceSchema)}
	resourceUnderTest.Update(context.Background(), frameworkresource.UpdateRequest{Plan: plan}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("update diagnostics: %v", response.Diagnostics)
	}

	assertJSONValue(t, written, "createEmptyAuthorFolders", true)
	assertJSONValue(t, written, "createEmptyEbookAuthorFolders", true)
	assertJSONValue(t, written, "defaultAudiobookRootFolderPath", "/library/audiobooks")
	assertJSONValue(t, written, "defaultEbookRootFolderPath", "/library/ebooks")
	assertJSONValue(t, written, "copyUsingHardlinks", true)
}

func TestSingletonConfigReadUsesStableIDEndpoint(t *testing.T) {
	t.Parallel()

	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestedPath = req.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":7,"theme":"dark"}`))
	}))
	defer server.Close()

	definition := singletonDefinition(t, "ui_config")
	resourceUnderTest := &singletonConfigResource{client: testAPIClient(t, server.URL), definition: definition}
	plan, resourceSchema := singletonPlan(t, resourceUnderTest, map[string]any{"id": "7"})
	response := &frameworkresource.ReadResponse{State: emptyState(resourceSchema)}
	resourceUnderTest.Read(context.Background(), frameworkresource.ReadRequest{
		State: tfsdk.State{Raw: plan.Raw, Schema: resourceSchema},
	}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("read diagnostics: %v", response.Diagnostics)
	}
	if requestedPath != "/api/v1/config/ui/7" {
		t.Fatalf("read path = %q, want stable ID endpoint", requestedPath)
	}
}

func TestSingletonConfigNeverEchoesServerCredential(t *testing.T) {
	t.Parallel()

	const serverCredential = "server-returned-credential-sentinel"
	current := map[string]any{
		"id":            1,
		"port":          8787,
		"instanceName":  "safe-name",
		"apiKey":        serverCredential,
		"proxyPassword": serverCredential,
	}
	var writtenBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(current)
		case http.MethodPut:
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			encoded, _ := json.Marshal(payload)
			writtenBody = string(encoded)
			current = payload
			_ = json.NewEncoder(w).Encode(payload)
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	definition := singletonDefinition(t, "host_config")
	resourceUnderTest := &singletonConfigResource{client: testAPIClient(t, server.URL), definition: definition}
	plan, resourceSchema := singletonPlan(t, resourceUnderTest, map[string]any{"instance_name": "updated-name"})
	response := &frameworkresource.UpdateResponse{State: emptyState(resourceSchema)}
	resourceUnderTest.Update(context.Background(), frameworkresource.UpdateRequest{Plan: plan}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("update diagnostics: %v", response.Diagnostics)
	}
	if strings.Contains(writtenBody, serverCredential) {
		t.Fatalf("write echoed a server-returned credential: %s", writtenBody)
	}
	if _, ok := current["apiKey"]; ok {
		t.Fatal("API key should be omitted unless the write-only plan attribute is configured")
	}
	if _, ok := current["proxyPassword"]; ok {
		t.Fatal("proxy password should be omitted unless explicitly configured")
	}
}

func TestSingletonConfigDestroyDoesNotCallAPI(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	resourceUnderTest := &singletonConfigResource{
		client:     testAPIClient(t, server.URL),
		definition: singletonDefinition(t, "media_management_config"),
	}
	resourceUnderTest.Delete(context.Background(), frameworkresource.DeleteRequest{}, &frameworkresource.DeleteResponse{})
	if got := requests.Load(); got != 0 {
		t.Fatalf("destroy made %d API requests", got)
	}
}

func TestHostCredentialFieldsAreSensitiveWriteOnly(t *testing.T) {
	t.Parallel()

	resourceUnderTest := &singletonConfigResource{definition: singletonDefinition(t, "host_config")}
	response := &frameworkresource.SchemaResponse{}
	resourceUnderTest.Schema(context.Background(), frameworkresource.SchemaRequest{}, response)
	for _, name := range []string{"password", "password_confirmation", "ssl_cert_password", "proxy_password", "oidc_client_secret"} {
		attribute, ok := response.Schema.Attributes[name].(interface {
			IsSensitive() bool
			IsWriteOnly() bool
			IsComputed() bool
		})
		if !ok {
			t.Fatalf("attribute %s does not expose security flags", name)
		}
		if !attribute.IsSensitive() || !attribute.IsWriteOnly() || attribute.IsComputed() {
			t.Fatalf("attribute %s must be sensitive, write-only, and non-computed", name)
		}
	}
}

func TestSingletonDefinitionsCoverPinnedOpenAPIProperties(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to locate test source")
	}
	specPath := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "third_party", "chaptarr", "openapi.json"))
	contents, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(contents, &spec); err != nil {
		t.Fatal(err)
	}

	schemaByResource := map[string]string{
		"host_config":              "HostConfigResource",
		"ui_config":                "UiConfigResource",
		"media_management_config":  "MediaManagementConfigResource",
		"naming_config":            "NamingConfigResource",
		"conversion_config":        "ConversionConfigResource",
		"metadata_provider_config": "MetadataProviderConfigResource",
		"download_client_config":   "DownloadClientConfigResource",
		"indexer_config":           "IndexerConfigResource",
		"development_config":       "DevelopmentConfigResource",
	}
	for resourceName, schemaName := range schemaByResource {
		definition := singletonDefinition(t, resourceName)
		apiFields := make(map[string]bool, len(definition.fields))
		for _, field := range definition.fields {
			apiFields[field.apiName] = true
		}
		for property := range spec.Components.Schemas[schemaName].Properties {
			if property == "id" {
				continue
			}
			if !apiFields[property] && !containsString(definition.dropAPIFields, property) {
				t.Errorf("%s does not model OpenAPI property %s.%s", resourceName, schemaName, property)
			}
		}
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func singletonDefinition(t *testing.T, name string) singletonConfigDefinition {
	t.Helper()
	for _, definition := range singletonConfigDefinitions {
		if definition.typeName == name {
			return definition
		}
	}
	t.Fatalf("definition %s not found", name)
	return singletonConfigDefinition{}
}

func testAPIClient(t *testing.T, baseURL string) *client.Client {
	t.Helper()
	apiClient, err := client.New(client.Config{
		BaseURL:   baseURL,
		APIKey:    "singleton-test-api-key",
		UserAgent: "singleton-test",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return apiClient
}

func singletonPlan(t *testing.T, resourceUnderTest *singletonConfigResource, configured map[string]any) (tfsdk.Plan, resourceschema.Schema) {
	t.Helper()
	response := &frameworkresource.SchemaResponse{}
	resourceUnderTest.Schema(context.Background(), frameworkresource.SchemaRequest{}, response)
	typeValue := response.Schema.Type().TerraformType(context.Background())
	objectType, ok := typeValue.(tftypes.Object)
	if !ok {
		t.Fatalf("resource schema type = %T", typeValue)
	}
	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		value, exists := configured[name]
		if exists {
			values[name] = tftypes.NewValue(attributeType, value)
			continue
		}
		if name == "id" {
			values[name] = tftypes.NewValue(attributeType, tftypes.UnknownValue)
			continue
		}
		field := configFieldByTerraformName(resourceUnderTest.definition, name)
		if field.writeOnly {
			values[name] = tftypes.NewValue(attributeType, nil)
		} else {
			values[name] = tftypes.NewValue(attributeType, tftypes.UnknownValue)
		}
	}
	return tfsdk.Plan{Raw: tftypes.NewValue(typeValue, values), Schema: response.Schema}, response.Schema
}

func configFieldByTerraformName(definition singletonConfigDefinition, name string) configField {
	for _, field := range definition.fields {
		if field.terraformName == name {
			return field
		}
	}
	return configField{}
}

func emptyState(resourceSchema resourceschema.Schema) tfsdk.State {
	typeValue := resourceSchema.Type().TerraformType(context.Background())
	return tfsdk.State{Schema: resourceSchema, Raw: tftypes.NewValue(typeValue, nil)}
}

func assertJSONValue(t *testing.T, values map[string]any, key string, expected any) {
	t.Helper()
	if actual := values[key]; actual != expected {
		t.Fatalf("%s = %#v, want %#v", key, actual, expected)
	}
}
