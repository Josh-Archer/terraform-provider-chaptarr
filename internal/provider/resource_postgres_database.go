package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	_ "github.com/lib/pq"
)

var (
	_ resource.Resource = &postgresDatabaseResource{}
)

type postgresDatabaseResource struct {
	client *client.Client
}

type postgresDatabaseModel struct {
	ID                     types.String `tfsdk:"id"`
	ServerHost             types.String `tfsdk:"server_host"`
	ServerPort             types.Int64  `tfsdk:"server_port"`
	AdminUsername          types.String `tfsdk:"admin_username"`
	AdminPassword          types.String `tfsdk:"admin_password"`
	VaultwardenBridgeURL   types.String `tfsdk:"vaultwarden_bridge_url"`
	VaultwardenBridgeToken types.String `tfsdk:"vaultwarden_bridge_token"`
	VaultwardenSecretKey   types.String `tfsdk:"vaultwarden_secret_key"`
	RoleName               types.String `tfsdk:"role_name"`
	RolePassword           types.String `tfsdk:"role_password"`
	Databases              types.List   `tfsdk:"databases"`
	SSLMode                types.String `tfsdk:"ssl_mode"`
	IsHealthy              types.Bool   `tfsdk:"is_healthy"`
}

type bridgeSecretResponse struct {
	Value string `json:"value"`
}

func newPostgresDatabaseResource() resource.Resource {
	return &postgresDatabaseResource{}
}

func (r *postgresDatabaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_postgres_database"
}

func (r *postgresDatabaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage Azure PostgreSQL roles, databases, and Vaultwarden secret resolution directly within the provider.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique resource identifier (server:port:role).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"server_host": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "PostgreSQL server host (e.g. homelabdb.postgres.database.azure.com).",
			},
			"server_port": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "PostgreSQL server port (default 5432).",
			},
			"admin_username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "PostgreSQL administrator login username.",
			},
			"admin_password": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				MarkdownDescription: "PostgreSQL administrator password.",
			},
			"vaultwarden_bridge_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional Vaultwarden ESO bridge HTTP REST API URL.",
			},
			"vaultwarden_bridge_token": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Optional Bearer token for authenticating with the Vaultwarden ESO bridge.",
			},
			"vaultwarden_secret_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Vaultwarden secret item key (default 'media/chaptarr-postgres-credentials').",
			},
			"role_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "PostgreSQL database role name (default 'chaptarr').",
			},
			"role_password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Explicit PostgreSQL role password (resolved automatically from Vaultwarden if bridge URL/token are provided).",
			},
			"databases": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "List of databases owned by role_name (default ['chaptarr-main', 'chaptarr-log', 'chaptarr-cache']).",
			},
			"ssl_mode": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "PostgreSQL SSL mode ('require', 'prefer', 'disable'). Default 'require'.",
			},
			"is_healthy": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "True if all databases exist and accept connections from the role.",
			},
		},
	}
}

func (r *postgresDatabaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *postgresDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan postgresDatabaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.applyDatabaseSetup(ctx, &plan, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	id := fmt.Sprintf("%s:%d:%s", plan.ServerHost.ValueString(), plan.ServerPort.ValueInt64(), plan.RoleName.ValueString())
	plan.ID = types.StringValue(id)
	plan.IsHealthy = types.BoolValue(true)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *postgresDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state postgresDatabaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rolePwd := r.resolveRolePassword(ctx, &state, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	host := state.ServerHost.ValueString()
	port := getPort(state.ServerPort)
	adminUser := state.AdminUsername.ValueString()
	adminPwd := state.AdminPassword.ValueString()
	sslMode := getSSLMode(state.SSLMode)
	roleName := getRoleName(state.RoleName)
	dbs := getDatabases(ctx, state.Databases)

	adminConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=%s", host, port, adminUser, adminPwd, sslMode)
	db, err := sql.Open("postgres", adminConnStr)
	if err != nil {
		state.IsHealthy = types.BoolValue(false)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	defer db.Close()

	// Check if role exists
	var exists bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1);", roleName).Scan(&exists)
	if err != nil || !exists {
		state.IsHealthy = types.BoolValue(false)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	// Check if all databases exist
	for _, dbName := range dbs {
		var dbExists bool
		err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1);", dbName).Scan(&dbExists)
		if err != nil || !dbExists {
			state.IsHealthy = types.BoolValue(false)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	// Verify role authentication if role password is provided
	if rolePwd != "" {
		for _, dbName := range dbs {
			roleConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", host, port, roleName, rolePwd, dbName, sslMode)
			rDb, err := sql.Open("postgres", roleConnStr)
			if err != nil {
				state.IsHealthy = types.BoolValue(false)
				resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
				return
			}
			err = rDb.PingContext(ctx)
			rDb.Close()
			if err != nil {
				state.IsHealthy = types.BoolValue(false)
				resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
				return
			}
		}
	}

	state.IsHealthy = types.BoolValue(true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *postgresDatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan postgresDatabaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.applyDatabaseSetup(ctx, &plan, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.IsHealthy = types.BoolValue(true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *postgresDatabaseResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Deleting the resource only relinquishes Terraform ownership to prevent accidental DROPs of production databases.
}

func (r *postgresDatabaseResource) applyDatabaseSetup(ctx context.Context, model *postgresDatabaseModel, resp interface{}) {
	rolePwd := r.resolveRolePassword(ctx, model, resp)
	if rolePwd == "" {
		return
	}

	host := model.ServerHost.ValueString()
	port := getPort(model.ServerPort)
	adminUser := model.AdminUsername.ValueString()
	adminPwd := model.AdminPassword.ValueString()
	sslMode := getSSLMode(model.SSLMode)
	roleName := getRoleName(model.RoleName)
	dbs := getDatabases(ctx, model.Databases)

	adminConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=%s", host, port, adminUser, adminPwd, sslMode)
	db, err := sql.Open("postgres", adminConnStr)
	if err != nil {
		r.addError(resp, "PostgreSQL Connection Error", fmt.Sprintf("Failed to connect to PostgreSQL at %s:%d as %s: %v", host, port, adminUser, err))
		return
	}
	defer db.Close()

	// 1. Create or update role
	var roleExists bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1);", roleName).Scan(&roleExists)
	if err != nil {
		r.addError(resp, "Role Check Failed", fmt.Sprintf("Failed to query pg_roles: %v", err))
		return
	}

	if !roleExists {
		createRoleSQL := fmt.Sprintf("CREATE ROLE %s WITH LOGIN PASSWORD '%s';", sanitizeIdent(roleName), escapeLiteral(rolePwd))
		if _, err := db.ExecContext(ctx, createRoleSQL); err != nil {
			r.addError(resp, "Role Creation Failed", fmt.Sprintf("Failed to create role %s: %v", roleName, err))
			return
		}
	} else {
		alterRoleSQL := fmt.Sprintf("ALTER ROLE %s WITH LOGIN PASSWORD '%s';", sanitizeIdent(roleName), escapeLiteral(rolePwd))
		_, _ = db.ExecContext(ctx, alterRoleSQL)
	}

	// Grant admin user membership so admin can assign database ownership
	grantAdminSQL := fmt.Sprintf("GRANT %s TO %s;", sanitizeIdent(adminUser), sanitizeIdent(roleName))
	_, _ = db.ExecContext(ctx, grantAdminSQL)

	// 2. Create databases
	for _, dbName := range dbs {
		var dbExists bool
		err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1);", dbName).Scan(&dbExists)
		if err != nil {
			r.addError(resp, "Database Check Failed", fmt.Sprintf("Failed to query pg_database for %s: %v", dbName, err))
			return
		}

		if !dbExists {
			createDBSQL := fmt.Sprintf("CREATE DATABASE %s OWNER %s;", sanitizeIdent(dbName), sanitizeIdent(roleName))
			if _, err := db.ExecContext(ctx, createDBSQL); err != nil {
				r.addError(resp, "Database Creation Failed", fmt.Sprintf("Failed to create database %s: %v", dbName, err))
				return
			}
		}

		// Grant schema permissions on each database
		targetConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", host, port, adminUser, adminPwd, dbName, sslMode)
		targetDb, err := sql.Open("postgres", targetConnStr)
		if err == nil {
			grantSchemaSQL := fmt.Sprintf("GRANT ALL ON SCHEMA public TO %s;", sanitizeIdent(roleName))
			_, _ = targetDb.ExecContext(ctx, grantSchemaSQL)
			targetDb.Close()
		}
	}
}

func (r *postgresDatabaseResource) resolveRolePassword(ctx context.Context, model *postgresDatabaseModel, resp interface{}) string {
	if !model.RolePassword.IsNull() && model.RolePassword.ValueString() != "" {
		return model.RolePassword.ValueString()
	}

	bridgeURL := model.VaultwardenBridgeURL.ValueString()
	bridgeToken := model.VaultwardenBridgeToken.ValueString()
	secretKey := "media/chaptarr-postgres-credentials"
	if !model.VaultwardenSecretKey.IsNull() && model.VaultwardenSecretKey.ValueString() != "" {
		secretKey = model.VaultwardenSecretKey.ValueString()
	}

	if bridgeURL != "" && bridgeToken != "" {
		reqURL := fmt.Sprintf("%s/v1/secret/%s/CHAPTARR_PASSWORD", strings.TrimSuffix(bridgeURL, "/"), secretKey)
		httpReq, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err == nil {
			httpReq.Header.Set("Authorization", "Bearer "+bridgeToken)
			httpClient := &http.Client{Timeout: 10 * time.Second}
			httpResp, err := httpClient.Do(httpReq)
			if err == nil && httpResp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(httpResp.Body)
				httpResp.Body.Close()
				var bResp bridgeSecretResponse
				if err := json.Unmarshal(body, &bResp); err == nil && bResp.Value != "" {
					model.RolePassword = types.StringValue(bResp.Value)
					return bResp.Value
				}
			}
		}
	}

	r.addError(resp, "Missing Role Password", "Specify `role_password` or configure valid `vaultwarden_bridge_url` and `vaultwarden_bridge_token`.")
	return ""
}

func (r *postgresDatabaseResource) addError(resp interface{}, title, detail string) {
	switch v := resp.(type) {
	case *resource.CreateResponse:
		v.Diagnostics.AddError(title, detail)
	case *resource.ReadResponse:
		v.Diagnostics.AddError(title, detail)
	case *resource.UpdateResponse:
		v.Diagnostics.AddError(title, detail)
	}
}

func getPort(v types.Int64) int {
	if !v.IsNull() && v.ValueInt64() > 0 {
		return int(v.ValueInt64())
	}
	return 5432
}

func getSSLMode(v types.String) string {
	if !v.IsNull() && v.ValueString() != "" {
		return v.ValueString()
	}
	return "require"
}

func getRoleName(v types.String) string {
	if !v.IsNull() && v.ValueString() != "" {
		return v.ValueString()
	}
	return "chaptarr"
}

func getDatabases(ctx context.Context, list types.List) []string {
	if !list.IsNull() && !list.IsUnknown() {
		var res []string
		_ = list.ElementsAs(ctx, &res, false)
		if len(res) > 0 {
			return res
		}
	}
	return []string{"chaptarr-main", "chaptarr-log", "chaptarr-cache"}
}

func sanitizeIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func escapeLiteral(lit string) string {
	return strings.ReplaceAll(lit, `'`, `''`)
}
