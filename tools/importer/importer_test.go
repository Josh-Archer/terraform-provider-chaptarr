package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Main Server", "main_server"},
		{"Sonarr - 4K (Anime)", "sonarr_4k_anime"},
		{"123_test", "item_123_test"},
		{"Radarr.HD", "radarr_hd"},
		{"", "main"},
		{"---", "main"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := sanitizeName(tt.input); got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestImporterDiscoveryAndGeneration(t *testing.T) {
	live := newPopulatedChaptarr(t)
	defer live.Close()

	imp := NewImporter(ImporterConfig{BaseURL: live.URL, APIKey: fakeCanary("importer-key"), HTTPClient: live.server.Client()})
	resources, err := imp.DiscoverAll(context.Background())
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	byType := map[string][]DiscoveredResource{}
	for _, resource := range resources {
		byType[resource.ResourceType] = append(byType[resource.ResourceType], resource)
	}
	for _, resourceType := range []string{
		"chaptarr_host_config", "chaptarr_ui_config", "chaptarr_media_management_config", "chaptarr_naming_config",
		"chaptarr_conversion_config", "chaptarr_metadata_provider_config", "chaptarr_download_client_config",
		"chaptarr_indexer_config", "chaptarr_development_config", "chaptarr_tag", "chaptarr_import_list_exclusion",
		"chaptarr_custom_filter", "chaptarr_custom_format", "chaptarr_quality_profile", "chaptarr_metadata_profile",
		"chaptarr_release_profile", "chaptarr_delay_profile", "chaptarr_quality_definition", "chaptarr_proxy",
		"chaptarr_root_folder", "chaptarr_remote_path_mapping", "chaptarr_metadata", "chaptarr_indexer",
		"chaptarr_download_client", "chaptarr_notification", "chaptarr_import_list", "chaptarr_author",
		"chaptarr_series", "chaptarr_book", "chaptarr_edition",
	} {
		if len(byType[resourceType]) != 1 {
			t.Errorf("%s count = %d, want 1", resourceType, len(byType[resourceType]))
		}
	}
	if _, ok := byType["chaptarr_postgres_database"]; ok {
		t.Error("postgres database was emitted")
	}
	if _, ok := byType["chaptarr_hardcover_config"]; ok {
		t.Error("hardcover config was emitted")
	}
	notes := strings.Join(imp.ManualAdoption(), "\n")
	if !strings.Contains(notes, "chaptarr_postgres_database") || !strings.Contains(notes, "chaptarr_hardcover_config") {
		t.Errorf("manual notes = %s", notes)
	}
	for _, note := range imp.ManualAdoption() {
		if strings.Contains(note, "was left unmanaged") {
			t.Errorf("unexpected unmanaged resource: %s", note)
		}
	}
	if byType["chaptarr_host_config"][0].ImportID != "host_config" {
		t.Errorf("host import id = %q", byType["chaptarr_host_config"][0].ImportID)
	}
	if byType["chaptarr_edition"][0].ImportID != "20/21" {
		t.Errorf("edition import id = %q", byType["chaptarr_edition"][0].ImportID)
	}

	hcl := GenerateHCL(resources, imp.ManualAdoption(), true)
	imports := GenerateImportBlocks(resources)
	variables := GenerateVariables(resources)
	script := GenerateImportScript(resources)
	combined := hcl + imports + variables + script
	for _, secret := range append(live.Canaries.values(), "Example Narrator") {
		if strings.Contains(combined, secret) {
			t.Errorf("generated output contains %q", secret)
		}
	}
	if strings.Contains(hcl, "\n  tags ") && strings.Contains(resourceSection(hcl, "chaptarr_download_client", "qbittorrent"), "99") {
		t.Errorf("download client tags were emitted:\n%s", resourceSection(hcl, "chaptarr_download_client", "qbittorrent"))
	}
	for _, want := range []string{
		`version = "~> 0.11"`,
		`resource "chaptarr_host_config" "current"`,
		"var.chaptarr_host_config_current_password",
		`resource "chaptarr_tag" "fiction"`,
		fmt.Sprintf("%-25s = %s", "media_type", `"all"`),
		fmt.Sprintf("%-25s = %s", "quality_id", "2"),
		fmt.Sprintf("%-25s = %s", "format_items", "["),
		fmt.Sprintf("%-25s = %s", "minimum_size", "20.5"),
		fmt.Sprintf("%-25s = %s", "allow_destroy", "false"),
		fmt.Sprintf("%-25s = %s", "test_before_apply", "false"),
		fmt.Sprintf("%-25s = %s", "foreign_author_id", `"hc:191785"`),
		fmt.Sprintf("%-25s = %s", "foreign_series_id", `"hc:555"`),
		fmt.Sprintf("%-25s = %s", "monitored_edition_id", `"hc:ed1"`),
		"selected_books stays empty",
		"chaptarr_postgres_database is not a Chaptarr API object",
		"apiKey",
	} {
		if !strings.Contains(hcl, want) {
			t.Errorf("HCL missing %q", want)
		}
	}
	host := resourceSection(hcl, "chaptarr_host_config", "current")
	if strings.Contains(host, "apiKey") || strings.Contains(host, "apikey") {
		t.Errorf("host config emitted the API key field:\n%s", host)
	}
	profile := resourceSection(hcl, "chaptarr_quality_profile", "spoken")
	for _, forbidden := range []string{"101", "102", `"EPUB"`, `"FLAC"`, `"MP3"`} {
		if strings.Contains(profile, forbidden) {
			t.Errorf("quality profile emitted server-owned value %s:\n%s", forbidden, profile)
		}
	}
	if strings.Contains(hcl, "\n  preferred_narrator") || strings.Contains(hcl, "\n  lookup_json") {
		t.Errorf("HCL contains an attribute that must not be emitted:\n%s", hcl)
	}
	if !strings.Contains(imports, `to = chaptarr_edition.first_edition`) || !strings.Contains(imports, `id = "20/21"`) {
		t.Errorf("imports:\n%s", imports)
	}
	if !strings.Contains(imports, `id = "host_config"`) {
		t.Errorf("singleton import id missing:\n%s", imports)
	}
	if !strings.Contains(variables, `sensitive   = true`) || strings.Contains(variables, "default") {
		t.Errorf("variables:\n%s", variables)
	}
	if !strings.Contains(script, `terraform import chaptarr_host_config.current "host_config"`) {
		t.Errorf("script:\n%s", script)
	}
	for _, call := range live.Calls() {
		if strings.Contains(call.Path, "preserved") {
			t.Fatalf("discovery requested preserved settings: %+v", call)
		}
	}
}

func resourceSection(hcl, resourceType, name string) string {
	marker := `resource "` + resourceType + `" "` + name + `"`
	start := strings.Index(hcl, marker)
	if start < 0 {
		return ""
	}
	rest := hcl[start+len(marker):]
	next := strings.Index(rest, "\nresource ")
	if next < 0 {
		return hcl[start:]
	}
	return hcl[start : start+len(marker)+next]
}

func TestDuplicateNamesUseNumericOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tag" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": 10, "label": "Fiction"},
			map[string]any{"id": 2, "label": "Fiction"},
		})
	}))
	defer server.Close()

	imp := NewImporter(ImporterConfig{BaseURL: server.URL, HTTPClient: server.Client()})
	resources, err := imp.DiscoverAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, resource := range resources {
		if resource.ResourceType == "chaptarr_tag" {
			names = append(names, resource.ImportID+":"+resource.ResourceName)
		}
	}
	got := strings.Join(names, ",")
	if got != "2:fiction,10:fiction_2" {
		t.Fatalf("tag names = %s", got)
	}
}

func TestPagedCollectionAndRepeatedID(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/tag" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("page") == "2" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"records": []any{map[string]any{"id": 3, "label": "c"}}, "page": 2, "pageSize": 2, "totalRecords": 3,
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"records":      []any{map[string]any{"id": 1, "label": "a"}, map[string]any{"id": 2, "label": "b"}},
				"page":         1,
				"pageSize":     2,
				"totalRecords": 3,
			})
		}))
		defer server.Close()
		imp := NewImporter(ImporterConfig{BaseURL: server.URL, HTTPClient: server.Client()})
		resources, err := imp.DiscoverAll(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var labels []string
		for _, resource := range resources {
			if resource.ResourceType == "chaptarr_tag" {
				labels = append(labels, resource.Attributes["label"].(string))
			}
		}
		if strings.Join(labels, ",") != "a,b,c" {
			t.Fatalf("labels = %v", labels)
		}
	})

	t.Run("repeated id", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/tag" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			page := r.URL.Query().Get("page")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"records": []any{map[string]any{"id": 1, "label": page}}, "pageSize": 1, "totalRecords": 2,
			})
		}))
		defer server.Close()
		imp := NewImporter(ImporterConfig{BaseURL: server.URL, HTTPClient: server.Client()})
		if _, err := imp.DiscoverAll(context.Background()); err == nil || !strings.Contains(err.Error(), "repeated id") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestMapBookLeavesMultipleMonitoredEditionsUnmanaged(t *testing.T) {
	book, editions, err := mapBook(map[string]any{
		"id": 20, "foreignBookId": "hc:777", "authorId": 41, "mediaType": "audiobook",
		"monitored": true, "anyEditionOk": false, "title": "Example",
		"editions": []any{
			map[string]any{"id": 21, "foreignEditionId": "hc:ed1", "title": "One", "monitored": true},
			map[string]any{"id": 22, "foreignEditionId": "hc:ed2", "title": "Two", "monitored": true},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "chaptarr_book") {
		t.Fatalf("error = %v", err)
	}
	if book.ResourceType != "" {
		t.Fatalf("book emitted: %+v", book)
	}
	if len(editions) != 2 {
		t.Fatalf("editions = %d", len(editions))
	}
}

func TestSeriesWithoutMediaTypeIsManual(t *testing.T) {
	_, err := mapSeries(map[string]any{"id": 12, "foreignSeriesId": "hc:555", "title": "Example", "narrator": "Kept"})
	if err == nil || !strings.Contains(err.Error(), "chaptarr_series") || strings.Contains(err.Error(), "Kept") {
		t.Fatalf("error = %v", err)
	}
}
