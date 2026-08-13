package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPostgresDatabaseHelperGetters(t *testing.T) {
	if got := getPort(types.Int64Null()); got != 5432 {
		t.Errorf("expected 5432, got %d", got)
	}

	if got := getPort(types.Int64Value(5433)); got != 5433 {
		t.Errorf("expected 5433, got %d", got)
	}

	if got := getSSLMode(types.StringNull()); got != "require" {
		t.Errorf("expected require, got %s", got)
	}

	if got := getRoleName(types.StringNull()); got != "chaptarr" {
		t.Errorf("expected chaptarr, got %s", got)
	}

	dbs := getDatabases(context.Background(), types.ListNull(types.StringType))
	if len(dbs) != 3 || dbs[0] != "chaptarr-main" {
		t.Errorf("expected 3 default dbs starting with chaptarr-main, got %v", dbs)
	}

	if got := sanitizeIdent("chaptarr-main"); got != `"chaptarr-main"` {
		t.Errorf("expected \"chaptarr-main\", got %s", got)
	}

	if got := escapeLiteral("pwd'with'quotes"); got != "pwd''with''quotes" {
		t.Errorf("expected pwd''with''quotes, got %s", got)
	}
}

func TestPostgresDatabaseCredentialsAreSensitiveWriteOnly(t *testing.T) {
	t.Parallel()

	postgresResource := &postgresDatabaseResource{}
	response := &resource.SchemaResponse{}
	postgresResource.Schema(t.Context(), resource.SchemaRequest{}, response)
	if response.Schema.Version != 1 {
		t.Fatalf("expected schema version 1, got %d", response.Schema.Version)
	}
	for _, name := range []string{"admin_password", "vaultwarden_bridge_token", "vaultwarden_secret_key", "role_password"} {
		attribute, ok := response.Schema.Attributes[name].(schema.StringAttribute)
		if !ok || !attribute.Sensitive || !attribute.WriteOnly || attribute.Computed {
			t.Fatalf("%s must be Sensitive+WriteOnly and not Computed: %#v", name, attribute)
		}
	}
}

func TestPostgresDatabaseReadScrubsLegacyCredentialsWithoutBridgeRequest(t *testing.T) {
	t.Parallel()

	requests := 0
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bridge.Close()

	legacy := postgresTestModel(bridge.URL)
	state := tfsdk.State{Schema: postgresDatabaseSchema(1)}
	if diagnostics := state.Set(t.Context(), &legacy); diagnostics.HasError() {
		t.Fatalf("set legacy state: %v", diagnostics)
	}
	response := &resource.ReadResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}
	(&postgresDatabaseResource{}).Read(t.Context(), resource.ReadRequest{State: state}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("read diagnostics: %v", response.Diagnostics)
	}
	if requests != 0 {
		t.Fatalf("refresh contacted Vaultwarden bridge %d times", requests)
	}
	var got postgresDatabaseModel
	if diagnostics := response.State.Get(t.Context(), &got); diagnostics.HasError() {
		t.Fatalf("get refreshed state: %v", diagnostics)
	}
	assertPostgresCredentialsCleared(t, got)
	if !got.IsHealthy.ValueBool() {
		t.Fatal("refresh did not preserve last mutation health result")
	}
}

func TestPostgresDatabaseStateUpgradeScrubsLegacyCredentials(t *testing.T) {
	t.Parallel()

	legacy := postgresTestModel("https://bridge.example.test")
	prior := postgresDatabaseSchema(0)
	state := tfsdk.State{Schema: prior}
	if diagnostics := state.Set(t.Context(), &legacy); diagnostics.HasError() {
		t.Fatalf("set prior state: %v", diagnostics)
	}
	request := resource.UpgradeStateRequest{State: &state}
	response := &resource.UpgradeStateResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}
	upgrader := (&postgresDatabaseResource{}).UpgradeState(t.Context())[0]
	upgrader.StateUpgrader(t.Context(), request, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("upgrade diagnostics: %v", response.Diagnostics)
	}
	var got postgresDatabaseModel
	if diagnostics := response.State.Get(t.Context(), &got); diagnostics.HasError() {
		t.Fatalf("get upgraded state: %v", diagnostics)
	}
	assertPostgresCredentialsCleared(t, got)
}

func TestPostgresDatabaseImportLeavesCredentialsNull(t *testing.T) {
	t.Parallel()

	initial := postgresTestModel("https://bridge.example.test")
	clearPostgresDatabaseCredentials(&initial)
	state := tfsdk.State{Schema: postgresDatabaseSchema(1)}
	if diagnostics := state.Set(t.Context(), &initial); diagnostics.HasError() {
		t.Fatalf("set empty import state: %v", diagnostics)
	}
	response := &resource.ImportStateResponse{State: state}
	(&postgresDatabaseResource{}).ImportState(t.Context(), resource.ImportStateRequest{ID: "postgres.example.test:5432:chaptarr"}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("import diagnostics: %v", response.Diagnostics)
	}
	var got postgresDatabaseModel
	if diagnostics := response.State.Get(t.Context(), &got); diagnostics.HasError() {
		t.Fatalf("get imported state: %v", diagnostics)
	}
	assertPostgresCredentialsCleared(t, got)
}

func TestPostgresDatabaseBridgeResolutionIsMutationLocalAndRedacted(t *testing.T) {
	t.Parallel()

	requests := 0
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		if !strings.HasPrefix(req.URL.Path, "/v1/secret/") {
			t.Fatalf("unexpected bridge path %q", req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"CHAPTARR_POSTGRES_TEST_SENTINEL_DO_NOT_USE_7fa2"}`))
	}))
	defer bridge.Close()

	response := &resource.CreateResponse{}
	password := (&postgresDatabaseResource{}).resolveRolePassword(t.Context(), postgresDatabaseCredentials{
		bridgeURL:   bridge.URL,
		bridgeToken: "CHAPTARR_POSTGRES_BRIDGE_TOKEN_SENTINEL_DO_NOT_USE_5bd1",
	}, response)
	if password == "" || response.Diagnostics.HasError() || requests != 1 {
		t.Fatalf("bridge resolution failed: requests=%d diagnostics=%v", requests, response.Diagnostics)
	}
	for _, diagnostic := range response.Diagnostics {
		if strings.Contains(diagnostic.Detail(), "SENTINEL") {
			t.Fatal("bridge resolution diagnostic leaked credential material")
		}
	}
}

func postgresTestModel(bridgeURL string) postgresDatabaseModel {
	return postgresDatabaseModel{
		ID: types.StringValue("postgres.example.test:5432:chaptarr"), ServerHost: types.StringValue("postgres.example.test"), ServerPort: types.Int64Value(5432),
		AdminUsername: types.StringValue("admin"), AdminPassword: types.StringValue("CHAPTARR_POSTGRES_ADMIN_SENTINEL_DO_NOT_USE_02c1"),
		VaultwardenBridgeURL: types.StringValue(bridgeURL), VaultwardenBridgeToken: types.StringValue("CHAPTARR_POSTGRES_BRIDGE_TOKEN_SENTINEL_DO_NOT_USE_5bd1"),
		VaultwardenSecretKey: types.StringValue("CHAPTARR_POSTGRES_KEY_SENTINEL_DO_NOT_USE_813e"), RoleName: types.StringValue("chaptarr"),
		RolePassword: types.StringValue("CHAPTARR_POSTGRES_ROLE_SENTINEL_DO_NOT_USE_7fa2"), Databases: types.ListNull(types.StringType), SSLMode: types.StringValue("require"), IsHealthy: types.BoolValue(true),
	}
}

func assertPostgresCredentialsCleared(t *testing.T, model postgresDatabaseModel) {
	t.Helper()
	for name, value := range map[string]types.String{
		"admin_password": model.AdminPassword, "vaultwarden_bridge_token": model.VaultwardenBridgeToken,
		"vaultwarden_secret_key": model.VaultwardenSecretKey, "role_password": model.RolePassword,
	} {
		if !value.IsNull() {
			t.Fatalf("%s remained in state", name)
		}
	}
}
