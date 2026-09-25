package provider

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/lib/pq"
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

	if got := grantRoleMembershipSQL("chaptarr", "admin"); got != `GRANT "chaptarr" TO "admin";` {
		t.Errorf("expected %s, got %s", `GRANT "chaptarr" TO "admin";`, got)
	}

	if got := grantSchemaPermissionsSQL("chaptarr"); got != `GRANT ALL ON SCHEMA public TO "chaptarr";` {
		t.Errorf("expected %s, got %s", `GRANT ALL ON SCHEMA public TO "chaptarr";`, got)
	}

	if got := escapeDSNLiteral("token with spaces"); got != `'token with spaces'` {
		t.Errorf("expected 'token with spaces', got %s", got)
	}

	if got := escapeDSNLiteral(`token'with\'quotes`); got != `'token\'with\\\'quotes'` {
		t.Errorf(`expected 'token\'with\\\'quotes', got %s`, got)
	}
}

func TestPostgresDatabaseGrantSchemaPermissionsSQL(t *testing.T) {
	t.Parallel()

	got := grantSchemaPermissionsSQL("chaptarr")
	expected := `GRANT ALL ON SCHEMA public TO "chaptarr";`
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}

	got = grantSchemaPermissionsSQL(`app"role`)
	expected = `GRANT ALL ON SCHEMA public TO "app""role";`
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestPostgresDatabaseGetSSLMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input types.String
		want  string
	}{
		{types.StringNull(), "require"},
		{types.StringUnknown(), "require"},
		{types.StringValue(""), "require"},
		{types.StringValue("require"), "require"},
		{types.StringValue("prefer"), "prefer"},
		{types.StringValue("disable"), "disable"},
		{types.StringValue("verify-ca"), "verify-ca"},
		{types.StringValue("verify-full"), "verify-full"},
		{types.StringValue("allow"), "allow"},
		{types.StringValue(" REQUIRE "), "require"},
		{types.StringValue(" Prefer "), "prefer"},
		{types.StringValue("require extra_keyword=value"), "require"},
		{types.StringValue("disable sslmode=require"), "require"},
		{types.StringValue("require host=evil.com"), "require"},
		{types.StringValue("invalid_mode"), "require"},
	}

	for _, tt := range tests {
		got := getSSLMode(tt.input)
		if got != tt.want {
			t.Errorf("getSSLMode(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPostgresDatabaseBuildDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		targetHost    string
		targetPort    int
		targetUser    string
		targetDB      string
		secretToken   string
		targetSSLMode string
		wantHost      string
		wantPort      int
		wantUser      string
		wantSecret    string
		wantDB        string
		wantSSLMode   pq.SSLMode
	}{
		{
			name:          "standard values",
			targetHost:    "postgres.example.test",
			targetPort:    5432,
			targetUser:    "homelabdbadmin",
			targetDB:      "postgres",
			secretToken:   "REDACTED_TEST_VALUE",
			targetSSLMode: "require",
			wantHost:      "postgres.example.test",
			wantPort:      5432,
			wantUser:      "homelabdbadmin",
			wantSecret:    "REDACTED_TEST_VALUE",
			wantDB:        "postgres",
			wantSSLMode:   pq.SSLModeRequire,
		},
		{
			name:          "secret with spaces",
			targetHost:    "postgres.example.test",
			targetPort:    5432,
			targetUser:    "homelabdbadmin",
			targetDB:      "chaptarr-main",
			secretToken:   "REDACTED_TEST_VALUE with multiple spaces",
			targetSSLMode: "require",
			wantHost:      "postgres.example.test",
			wantPort:      5432,
			wantUser:      "homelabdbadmin",
			wantSecret:    "REDACTED_TEST_VALUE with multiple spaces",
			wantDB:        "chaptarr-main",
			wantSSLMode:   pq.SSLModeRequire,
		},
		{
			name:          "secret with quotes and backslashes",
			targetHost:    "postgres.example.test",
			targetPort:    5432,
			targetUser:    "homelabdbadmin",
			targetDB:      "chaptarr-log",
			secretToken:   `REDACTED_TEST_VALUE'with\'quotes\\and\backslashes`,
			targetSSLMode: "prefer",
			wantHost:      "postgres.example.test",
			wantPort:      5432,
			wantUser:      "homelabdbadmin",
			wantSecret:    `REDACTED_TEST_VALUE'with\'quotes\\and\backslashes`,
			wantDB:        "chaptarr-log",
			wantSSLMode:   pq.SSLModePrefer,
		},
		{
			name:          "secret with equals and special characters",
			targetHost:    "postgres.example.test",
			targetPort:    5432,
			targetUser:    "admin@server",
			targetDB:      "chaptarr-cache",
			secretToken:   "param=value;REDACTED_TEST_VALUE",
			targetSSLMode: "disable",
			wantHost:      "postgres.example.test",
			wantPort:      5432,
			wantUser:      "admin@server",
			wantSecret:    "param=value;REDACTED_TEST_VALUE",
			wantDB:        "chaptarr-cache",
			wantSSLMode:   pq.SSLModeDisable,
		},
		{
			name:          "empty secret",
			targetHost:    "localhost",
			targetPort:    5433,
			targetUser:    "app user",
			targetDB:      "my database",
			secretToken:   "",
			targetSSLMode: "require",
			wantHost:      "localhost",
			wantPort:      5433,
			wantUser:      "app user",
			wantSecret:    "",
			wantDB:        "my database",
			wantSSLMode:   pq.SSLModeRequire,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := buildPostgresDSN(tt.targetHost, tt.targetPort, tt.targetUser, tt.targetDB, tt.secretToken, tt.targetSSLMode)
			cfg, err := pq.NewConfig(dsn)
			if err != nil {
				t.Fatalf("pq.NewConfig(%q) failed: %v", dsn, err)
			}
			if cfg.Host != tt.wantHost {
				t.Errorf("host mismatch: got %q, want %q", cfg.Host, tt.wantHost)
			}
			if cfg.Port != uint16(tt.wantPort) {
				t.Errorf("port mismatch: got %d, want %d", cfg.Port, tt.wantPort)
			}
			if cfg.User != tt.wantUser {
				t.Errorf("user mismatch: got %q, want %q", cfg.User, tt.wantUser)
			}
			if cfg.Password != tt.wantSecret {
				t.Errorf("password mismatch: got %q, want %q", cfg.Password, tt.wantSecret)
			}
			if cfg.Database != tt.wantDB {
				t.Errorf("database mismatch: got %q, want %q", cfg.Database, tt.wantDB)
			}
			if cfg.SSLMode != tt.wantSSLMode {
				t.Errorf("sslmode mismatch: got %q, want %q", cfg.SSLMode, tt.wantSSLMode)
			}
		})
	}
}

func TestPostgresDatabaseGrantRoleMembershipSQL(t *testing.T) {
	t.Parallel()

	// Verify that the application role is granted to the admin user
	// (so the admin user becomes a member of the application role to assign ownership),
	// rather than granting the admin role to the application role.
	got := grantRoleMembershipSQL("chaptarr", "homelabdbadmin")
	expected := `GRANT "chaptarr" TO "homelabdbadmin";`
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}

	got = grantRoleMembershipSQL(`app"role`, `admin"user`)
	expected = `GRANT "app""role" TO "admin""user";`
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
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

var mockDriverCounter atomic.Int64

func registerMockDriver(t *testing.T, d driver.Driver) string {
	t.Helper()
	name := fmt.Sprintf("mock_pg_%d", mockDriverCounter.Add(1))
	sql.Register(name, d)
	return name
}

type mockPostgresDriver struct {
	openFunc func(name string) (driver.Conn, error)
}

func (d *mockPostgresDriver) Open(name string) (driver.Conn, error) {
	if d.openFunc != nil {
		return d.openFunc(name)
	}
	return nil, errors.New("openFunc not set")
}

type mockPostgresConn struct {
	execFunc  func(ctx context.Context, query string) error
	queryFunc func(ctx context.Context, query string) (driver.Rows, error)
}

func (c *mockPostgresConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("prepare not implemented")
}

func (c *mockPostgresConn) Close() error {
	return nil
}

func (c *mockPostgresConn) Begin() (driver.Tx, error) {
	return nil, errors.New("begin not implemented")
}

func (c *mockPostgresConn) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if c.execFunc != nil {
		if err := c.execFunc(ctx, query); err != nil {
			return nil, err
		}
	}
	return driver.RowsAffected(1), nil
}

func (c *mockPostgresConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.queryFunc != nil {
		return c.queryFunc(ctx, query)
	}
	return nil, errors.New("queryFunc not set")
}

type singleBoolRows struct {
	val  bool
	read bool
}

func (r *singleBoolRows) Columns() []string {
	return []string{"exists"}
}

func (r *singleBoolRows) Close() error {
	return nil
}

func (r *singleBoolRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	dest[0] = r.val
	r.read = true
	return nil
}

func postgresCreateRequest(t *testing.T, model postgresDatabaseModel) resource.CreateRequest {
	t.Helper()
	state := tfsdk.State{Schema: postgresDatabaseSchema(1)}
	if diags := state.Set(t.Context(), &model); diags.HasError() {
		t.Fatalf("state.Set: %v", diags)
	}
	return resource.CreateRequest{Plan: tfsdk.Plan(state), Config: tfsdk.Config(state)}
}

func assertDiagnosticMatch(t *testing.T, diagnostics diag.Diagnostics, expectedSummary, expectedDetailSubstring string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Summary() == expectedSummary && strings.Contains(diagnostic.Detail(), expectedDetailSubstring) {
			return
		}
	}
	t.Fatalf("expected diagnostic with summary %q and detail containing %q, got: %v", expectedSummary, expectedDetailSubstring, diagnostics)
}

func TestPostgresDatabaseCreateFailsWhenAlterRoleErrors(t *testing.T) {
	t.Parallel()

	driverName := registerMockDriver(t, &mockPostgresDriver{
		openFunc: func(name string) (driver.Conn, error) {
			return &mockPostgresConn{
				queryFunc: func(ctx context.Context, query string) (driver.Rows, error) {
					if strings.Contains(query, "pg_roles") {
						return &singleBoolRows{val: true}, nil
					}
					return nil, fmt.Errorf("unexpected query: %s", query)
				},
				execFunc: func(ctx context.Context, query string) error {
					if strings.HasPrefix(query, "ALTER ROLE") {
						return errors.New("simulated alter role error")
					}
					return nil
				},
			}, nil
		},
	})

	instance := &postgresDatabaseResource{driverName: driverName}
	model := postgresTestModel("")
	req := postgresCreateRequest(t, model)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}

	instance.Create(t.Context(), req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error diagnostic when ALTER ROLE fails, got none")
	}
	assertDiagnosticMatch(t, resp.Diagnostics, "Role Update Failed", "Failed to update role chaptarr.")

	var state postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &state)
	if state.IsHealthy.ValueBool() {
		t.Fatal("expected is_healthy not to be set to true in state on error")
	}
}

func TestPostgresDatabaseCreateFailsWhenGrantRoleMembershipErrors(t *testing.T) {
	t.Parallel()

	driverName := registerMockDriver(t, &mockPostgresDriver{
		openFunc: func(name string) (driver.Conn, error) {
			return &mockPostgresConn{
				queryFunc: func(ctx context.Context, query string) (driver.Rows, error) {
					if strings.Contains(query, "pg_roles") {
						return &singleBoolRows{val: false}, nil
					}
					return nil, fmt.Errorf("unexpected query: %s", query)
				},
				execFunc: func(ctx context.Context, query string) error {
					if strings.HasPrefix(query, "GRANT") {
						return errors.New("simulated grant admin error")
					}
					return nil
				},
			}, nil
		},
	})

	instance := &postgresDatabaseResource{driverName: driverName}
	model := postgresTestModel("")
	req := postgresCreateRequest(t, model)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}

	instance.Create(t.Context(), req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error diagnostic when role grant fails, got none")
	}
	assertDiagnosticMatch(t, resp.Diagnostics, "Role Grant Failed", "Failed to grant role chaptarr to admin.")

	var state postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &state)
	if state.IsHealthy.ValueBool() {
		t.Fatal("expected is_healthy not to be set to true in state on error")
	}
}

func TestPostgresDatabaseCreateFailsWhenTargetDatabaseOpenErrors(t *testing.T) {
	t.Parallel()

	driverName := registerMockDriver(t, &mockPostgresDriver{
		openFunc: func(name string) (driver.Conn, error) {
			return &mockPostgresConn{
				queryFunc: func(ctx context.Context, query string) (driver.Rows, error) {
					if strings.Contains(query, "pg_roles") {
						return &singleBoolRows{val: false}, nil
					}
					if strings.Contains(query, "pg_database") {
						return &singleBoolRows{val: true}, nil
					}
					return nil, fmt.Errorf("unexpected query: %s", query)
				},
				execFunc: func(ctx context.Context, query string) error {
					return nil
				},
			}, nil
		},
	})

	instance := &postgresDatabaseResource{
		driverName: driverName,
		sqlOpener: func(drv, dsn string) (*sql.DB, error) {
			if strings.Contains(dsn, "dbname='chaptarr-main'") || strings.Contains(dsn, "dbname=chaptarr-main") {
				return nil, errors.New("simulated connection failure")
			}
			return sql.Open(drv, dsn)
		},
	}
	model := postgresTestModel("")
	req := postgresCreateRequest(t, model)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}

	instance.Create(t.Context(), req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error diagnostic when target db connection fails, got none")
	}
	assertDiagnosticMatch(t, resp.Diagnostics, "PostgreSQL Connection Error", "Failed to connect to PostgreSQL database chaptarr-main.")

	var state postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &state)
	if state.IsHealthy.ValueBool() {
		t.Fatal("expected is_healthy not to be set to true in state on error")
	}
}

func TestPostgresDatabaseCreateFailsWhenGrantPublicSchemaErrors(t *testing.T) {
	t.Parallel()

	driverName := registerMockDriver(t, &mockPostgresDriver{
		openFunc: func(name string) (driver.Conn, error) {
			return &mockPostgresConn{
				queryFunc: func(ctx context.Context, query string) (driver.Rows, error) {
					if strings.Contains(query, "pg_roles") {
						return &singleBoolRows{val: false}, nil
					}
					if strings.Contains(query, "pg_database") {
						return &singleBoolRows{val: true}, nil
					}
					return nil, fmt.Errorf("unexpected query: %s", query)
				},
				execFunc: func(ctx context.Context, query string) error {
					if strings.HasPrefix(query, "GRANT ALL ON SCHEMA public") {
						return errors.New("simulated schema grant failure")
					}
					return nil
				},
			}, nil
		},
	})

	instance := &postgresDatabaseResource{driverName: driverName}
	model := postgresTestModel("")
	req := postgresCreateRequest(t, model)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}

	instance.Create(t.Context(), req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error diagnostic when schema grant fails, got none")
	}
	assertDiagnosticMatch(t, resp.Diagnostics, "Schema Grant Failed", "Failed to grant schema permissions on database chaptarr-main to role chaptarr.")

	var state postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &state)
	if state.IsHealthy.ValueBool() {
		t.Fatal("expected is_healthy not to be set to true in state on error")
	}
}

func TestPostgresDatabaseCreateHappyPathSetsHealthy(t *testing.T) {
	t.Parallel()

	driverName := registerMockDriver(t, &mockPostgresDriver{
		openFunc: func(name string) (driver.Conn, error) {
			return &mockPostgresConn{
				queryFunc: func(ctx context.Context, query string) (driver.Rows, error) {
					if strings.Contains(query, "pg_roles") {
						return &singleBoolRows{val: true}, nil
					}
					if strings.Contains(query, "pg_database") {
						return &singleBoolRows{val: false}, nil
					}
					return nil, fmt.Errorf("unexpected query: %s", query)
				},
				execFunc: func(ctx context.Context, query string) error {
					return nil
				},
			}, nil
		},
	})

	instance := &postgresDatabaseResource{driverName: driverName}
	model := postgresTestModel("")
	req := postgresCreateRequest(t, model)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}

	instance.Create(t.Context(), req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected create failure: %v", resp.Diagnostics)
	}

	var state postgresDatabaseModel
	if diags := resp.State.Get(t.Context(), &state); diags.HasError() {
		t.Fatalf("get state failed: %v", diags)
	}
	if !state.IsHealthy.ValueBool() {
		t.Fatal("expected is_healthy to be true on successful create")
	}
	if state.ID.ValueString() != "postgres.example.test:5432:chaptarr" {
		t.Fatalf("unexpected id: %s", state.ID.ValueString())
	}
	assertPostgresCredentialsCleared(t, state)
}

func TestPostgresDatabaseUpdateFailsWhenGrantRoleMembershipErrors(t *testing.T) {
	t.Parallel()

	driverName := registerMockDriver(t, &mockPostgresDriver{
		openFunc: func(name string) (driver.Conn, error) {
			return &mockPostgresConn{
				queryFunc: func(ctx context.Context, query string) (driver.Rows, error) {
					if strings.Contains(query, "pg_roles") {
						return &singleBoolRows{val: true}, nil
					}
					return nil, fmt.Errorf("unexpected query: %s", query)
				},
				execFunc: func(ctx context.Context, query string) error {
					if strings.HasPrefix(query, "GRANT") {
						return errors.New("simulated grant admin error")
					}
					return nil
				},
			}, nil
		},
	})

	instance := &postgresDatabaseResource{driverName: driverName}
	model := postgresTestModel("")
	state := tfsdk.State{Schema: postgresDatabaseSchema(1)}
	if diags := state.Set(t.Context(), &model); diags.HasError() {
		t.Fatalf("state.Set: %v", diags)
	}
	req := resource.UpdateRequest{Plan: tfsdk.Plan(state), Config: tfsdk.Config(state)}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}

	instance.Update(t.Context(), req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error diagnostic when role grant fails during update, got none")
	}
	assertDiagnosticMatch(t, resp.Diagnostics, "Role Grant Failed", "Failed to grant role chaptarr to admin.")

	var result postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &result)
	if result.IsHealthy.ValueBool() {
		t.Fatal("expected is_healthy not to be set to true in state on update error")
	}
}
