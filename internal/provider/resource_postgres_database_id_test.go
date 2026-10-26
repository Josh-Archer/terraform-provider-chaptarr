package provider

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func okPostgresDriver(t *testing.T) string {
	t.Helper()
	return registerMockDriver(t, &mockPostgresDriver{
		openFunc: func(string) (driver.Conn, error) {
			return &mockPostgresConn{
				queryFunc: func(_ context.Context, query string) (driver.Rows, error) {
					return &singleBoolRows{val: strings.Contains(query, "pg_roles")}, nil
				},
				execFunc: func(context.Context, string) error { return nil },
			}, nil
		},
	})
}

func defaultsModel() postgresDatabaseModel {
	m := postgresTestModel("")
	m.ID = types.StringUnknown()
	m.ServerPort = types.Int64Null()
	m.RoleName = types.StringNull()
	return m
}

func TestPostgresDatabaseIDHelperUsesDefaults(t *testing.T) {
	t.Parallel()
	if got := postgresDatabaseID(defaultsModel()); got != "postgres.example.test:5432:chaptarr" {
		t.Fatalf("id = %q", got)
	}
	m := defaultsModel()
	m.ServerPort, m.RoleName = types.Int64Value(6432), types.StringValue("svc")
	if got := postgresDatabaseID(m); got != "postgres.example.test:6432:svc" {
		t.Fatalf("id = %q", got)
	}
}

func TestPostgresDatabaseCreateUsesResolvedDefaultsForID(t *testing.T) {
	t.Parallel()
	r := &postgresDatabaseResource{driverName: okPostgresDriver(t)}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: postgresDatabaseSchema(1)}}
	r.Create(t.Context(), postgresCreateRequest(t, defaultsModel()), resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}
	var got postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &got)
	if got.ID.ValueString() != "postgres.example.test:5432:chaptarr" {
		t.Fatalf("id = %q, want postgres.example.test:5432:chaptarr", got.ID.ValueString())
	}
}

func TestPostgresDatabaseUpdateRecomputesID(t *testing.T) {
	t.Parallel()
	r := &postgresDatabaseResource{driverName: okPostgresDriver(t)}
	prior := postgresTestModel("")
	priorState := tfsdk.State{Schema: postgresDatabaseSchema(1)}
	_ = priorState.Set(t.Context(), &prior)
	planned := postgresTestModel("")
	planned.ServerPort = types.Int64Value(6432)
	planned.ID = types.StringUnknown()
	ps := tfsdk.State{Schema: postgresDatabaseSchema(1)}
	if d := ps.Set(t.Context(), &planned); d.HasError() {
		t.Fatal(d)
	}
	resp := &resource.UpdateResponse{State: priorState}
	r.Update(t.Context(), resource.UpdateRequest{Plan: tfsdk.Plan(ps), Config: tfsdk.Config(ps), State: priorState}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	var got postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &got)
	if got.ID.ValueString() != "postgres.example.test:6432:chaptarr" {
		t.Fatalf("id = %q", got.ID.ValueString())
	}
}

func TestPostgresDatabaseReadRepairsLegacyID(t *testing.T) {
	t.Parallel()
	legacy := postgresTestModel("")
	legacy.ID = types.StringValue("postgres.example.test:0:")
	legacy.ServerPort, legacy.RoleName = types.Int64Null(), types.StringNull()
	st := tfsdk.State{Schema: postgresDatabaseSchema(1)}
	_ = st.Set(t.Context(), &legacy)
	resp := &resource.ReadResponse{State: st}
	(&postgresDatabaseResource{}).Read(t.Context(), resource.ReadRequest{State: st}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &got)
	if got.ID.ValueString() != "postgres.example.test:5432:chaptarr" {
		t.Fatalf("id = %q", got.ID.ValueString())
	}
}

func TestPostgresDatabaseImportParsesID(t *testing.T) {
	t.Parallel()
	empty := postgresTestModel("")
	clearPostgresDatabaseCredentials(&empty)
	empty.ID, empty.ServerHost, empty.ServerPort, empty.RoleName = types.StringNull(), types.StringNull(), types.Int64Null(), types.StringNull()
	st := tfsdk.State{Schema: postgresDatabaseSchema(1)}
	_ = st.Set(t.Context(), &empty)
	resp := &resource.ImportStateResponse{State: st}
	(&postgresDatabaseResource{}).ImportState(t.Context(), resource.ImportStateRequest{ID: "db.example.test:6432:svc"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got postgresDatabaseModel
	_ = resp.State.Get(t.Context(), &got)
	if got.ServerHost.ValueString() != "db.example.test" || got.ServerPort.ValueInt64() != 6432 || got.RoleName.ValueString() != "svc" || got.ID.ValueString() != "db.example.test:6432:svc" {
		t.Fatalf("unexpected imported state: %+v", got)
	}
}

func TestPostgresDatabaseIDPlanModifier(t *testing.T) {
	t.Parallel()
	s := postgresDatabaseSchema(1)
	m := defaultsModel()
	m.ServerPort = types.Int64Value(6432)
	ps := tfsdk.State{Schema: s}
	_ = ps.Set(t.Context(), &m)
	req := planmodifier.StringRequest{Path: path.Root("id"), Plan: tfsdk.Plan(ps), PlanValue: types.StringValue("postgres.example.test:5432:chaptarr")}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	postgresDatabaseIDPlanModifier{}.PlanModifyString(t.Context(), req, resp)
	if resp.PlanValue.ValueString() != "postgres.example.test:6432:chaptarr" {
		t.Fatalf("planned id = %s", resp.PlanValue)
	}
}

func TestPostgresDatabaseHostAndRoleRequireReplace(t *testing.T) {
	t.Parallel()
	s := postgresDatabaseSchema(1)
	for _, name := range []string{"server_host", "role_name"} {
		if len(s.Attributes[name].(schema.StringAttribute).PlanModifiers) == 0 {
			t.Errorf("%s has no RequiresReplace plan modifier", name)
		}
	}
	cases := []struct {
		state, plan types.String
		want        bool
	}{
		{types.StringNull(), types.StringValue("chaptarr"), false},
		{types.StringValue("chaptarr"), types.StringNull(), false},
		{types.StringValue("chaptarr"), types.StringValue("svc"), true},
		{types.StringNull(), types.StringValue("svc"), true},
	}
	for _, c := range cases {
		resp := &planmodifier.StringResponse{PlanValue: c.plan}
		st := tfsdk.State{Schema: s}
		_ = st.Set(t.Context(), ptr(postgresTestModel("")))
		mod := s.Attributes["role_name"].(schema.StringAttribute).PlanModifiers[0]
		mod.PlanModifyString(t.Context(), planmodifier.StringRequest{Path: path.Root("role_name"), State: st, Plan: tfsdk.Plan(st), StateValue: c.state, PlanValue: c.plan, ConfigValue: c.plan}, resp)
		if resp.RequiresReplace != c.want {
			t.Errorf("role %v -> %v: RequiresReplace = %v, want %v", c.state, c.plan, resp.RequiresReplace, c.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
