package provider

import (
	"context"
	"testing"

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
