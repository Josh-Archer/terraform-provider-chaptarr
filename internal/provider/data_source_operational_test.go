package provider

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const operationalSyntheticAPIKey = "test-api-key-not-real"

func TestOperationalDataSourceSchemasValidateImplementation(t *testing.T) {
	t.Parallel()

	factories := []func() datasource.DataSource{
		newBackupsDataSource,
		newSettingsBackupsDataSource,
		newHistoryDataSource,
		newIgnoredDataSource,
		newManualImportCandidatesDataSource,
		newPendingAuthorImportDataSource,
		newCutoffWantedDataSource,
		newMissingDataSource,
	}
	for _, factory := range factories {
		instance := factory()
		meta := &datasource.MetadataResponse{}
		instance.Metadata(t.Context(), datasource.MetadataRequest{ProviderTypeName: "chaptarr"}, meta)
		schemaResp := &datasource.SchemaResponse{}
		instance.Schema(t.Context(), datasource.SchemaRequest{}, schemaResp)
		if diagnostics := schemaResp.Schema.ValidateImplementation(t.Context()); diagnostics.HasError() {
			t.Fatalf("%s schema invalid: %v", meta.TypeName, diagnostics)
		}

		// Verify unconfigured read fails cleanly without panicking.
		req, resp := operationalReadRequest(t, instance, nil)
		instance.Read(t.Context(), req, resp)
		if !resp.Diagnostics.HasError() {
			t.Fatalf("%s must report an error when provider client is not configured", meta.TypeName)
		}

		// Verify unexpected provider data type produces a clean diagnostic.
		cfgResp := &datasource.ConfigureResponse{}
		instance.(datasource.DataSourceWithConfigure).Configure(t.Context(), datasource.ConfigureRequest{ProviderData: "wrong-type"}, cfgResp)
		if !cfgResp.Diagnostics.HasError() {
			t.Fatalf("%s must report an error for unexpected provider data", meta.TypeName)
		}
	}
}

func TestSafeEntryNameAndReleaseTitleRedactionHelpers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"/config/Backups/scheduled/chaptarr_v0.9.zip", "chaptarr_v0.9.zip"},
		{`C:\ProgramData\Chaptarr\Backups\manual.zip`, "manual.zip"},
		{"/mnt/media/audiobooks/", "audiobooks"},
		{"/", ""},
		{`C:\`, ""},
		{"..", ""},
		{".", ""},
		{"https://indexer.example.test/download?apikey=secret", ""},
		{"magnet:?xt=urn:btih:12345", ""},
		{"release_passkey=leaked.zip", "[REDACTED]"},
	}
	for _, tc := range cases {
		if got := safeEntryName(tc.in); got != tc.want {
			t.Fatalf("safeEntryName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	titleCases := []struct {
		in   string
		want string
	}{
		{"Brandon Sanderson - Mistborn [M4B]", "Brandon Sanderson - Mistborn [M4B]"},
		{"/downloads/complete/SecretDir/Book.m4b", "Book.m4b"},
		{"https://indexer.example.test/get?passkey=abc", ""},
		{"magnet:?xt=urn:btih:abcdef", ""},
		{"Release token=supersecret", "[REDACTED]"},
	}
	for _, tc := range titleCases {
		if got := sanitizeReleaseTitle(tc.in); got != tc.want {
			t.Fatalf("sanitizeReleaseTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBackupsDataSourceRedactsServerPaths(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/backup" || r.Header.Get("X-Api-Key") != operationalSyntheticAPIKey {
			t.Fatalf("unexpected backup request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":9,"name":"/private/var/chaptarr/Backups/manual/second.zip","path":"/private/var/chaptarr/Backups/manual/second.zip","type":"manual","size":2048,"time":"2026-10-02T00:00:00Z"},
			{"id":2,"name":"first.zip","path":"/private/var/chaptarr/Backups/scheduled/first.zip","type":"scheduled","size":1024,"time":"2026-10-01T00:00:00Z"}
		]`))
	}))
	defer server.Close()

	instance := configuredOperationalDataSource(t, newBackupsDataSource(), server.URL)
	req, resp := operationalReadRequest(t, instance, nil)
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state backupsState
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	if resp.Diagnostics.HasError() || state.TotalCount.ValueInt64() != 2 {
		t.Fatalf("unexpected backups state: %+v diags=%v", state, resp.Diagnostics)
	}
	var items []backupItemModel
	resp.Diagnostics.Append(state.Backups.ElementsAs(t.Context(), &items, false)...)
	if len(items) != 2 || items[0].ID.ValueInt64() != 2 || items[1].Name.ValueString() != "second.zip" {
		t.Fatalf("unexpected backup items: %+v", items)
	}
	assertNoForbiddenSubstrings(t, resp.State.Raw.String(),
		operationalSyntheticAPIKey,
		"/private/var/chaptarr",
		"Backups/manual",
		"Backups/scheduled",
	)
}

func TestSettingsBackupsDataSourceModesAndRedaction(t *testing.T) {
	t.Parallel()

	var seenPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/settingsbackup/files":
			_, _ = w.Write([]byte(`[{"path":"/secret/internal/settings/backup-2026.json","name":"/secret/internal/settings/backup-2026.json","size":4096,"lastWriteTimeUtc":"2026-10-10T12:00:00Z"}]`))
		case "/api/v1/system/settingsbackup/locations":
			_, _ = w.Write([]byte(`[{"path":"/secret/internal/settings","exists":true,"writable":false,"warning":"Permission denied on /secret/internal/settings for uid 1000"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	instance := configuredOperationalDataSource(t, newSettingsBackupsDataSource(), server.URL)

	// Default "all" mode fetches both endpoints and omits paths + warning strings.
	req, resp := operationalReadRequest(t, instance, nil)
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected settings_backups diagnostics: %v", resp.Diagnostics)
	}
	var state settingsBackupsState
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	if state.Operation.ValueString() != "all" || len(state.Files.Elements()) != 1 || len(state.Locations.Elements()) != 1 {
		t.Fatalf("unexpected settings_backups state: %+v", state)
	}
	var files []settingsBackupFileModel
	resp.Diagnostics.Append(state.Files.ElementsAs(t.Context(), &files, false)...)
	if files[0].Name.ValueString() != "backup-2026.json" || files[0].PathSHA256.ValueString() == "" {
		t.Fatalf("unexpected file model: %+v", files[0])
	}
	var locations []settingsBackupLocationModel
	resp.Diagnostics.Append(state.Locations.ElementsAs(t.Context(), &locations, false)...)
	if !locations[0].HasWarning.ValueBool() || locations[0].PathSHA256.ValueString() == "" {
		t.Fatalf("unexpected location model: %+v", locations[0])
	}
	assertNoForbiddenSubstrings(t, resp.State.Raw.String(),
		operationalSyntheticAPIKey,
		"/secret/internal/settings",
		"Permission denied",
		"uid 1000",
	)

	// "files" mode with root_folder sends rootFolder query param and hashes ID.
	req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
		"operation":   tftypes.NewValue(tftypes.String, "files"),
		"root_folder": tftypes.NewValue(tftypes.String, "/books"),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected files mode diagnostics: %v", resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	if strings.Contains(state.ID.ValueString(), "books") || len(state.Locations.Elements()) != 0 {
		t.Fatalf("unexpected files mode state: id=%q locations=%d", state.ID.ValueString(), len(state.Locations.Elements()))
	}

	// Reject root_folder when operation = "locations".
	req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
		"operation":   tftypes.NewValue(tftypes.String, "locations"),
		"root_folder": tftypes.NewValue(tftypes.String, "/books"),
	})
	instance.Read(t.Context(), req, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error when root_folder is set with operation = locations")
	}

	if len(seenPaths) != 3 || seenPaths[2] != "/api/v1/system/settingsbackup/files?rootFolder=%2Fbooks" {
		t.Fatalf("unexpected request sequence: %v", seenPaths)
	}
}

func TestHistoryDataSourceModesValidationAndRedaction(t *testing.T) {
	t.Parallel()

	var seenURIs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenURIs = append(seenURIs, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		recordJSON := `{
			"id":11,
			"bookId":22,
			"authorId":33,
			"sourceTitle":"/private/downloads/Brandon.Sanderson.m4b",
			"quality":{"quality":{"id":4,"name":"M4B"}},
			"customFormatScore":15,
			"qualityCutoffNotMet":false,
			"date":"2026-10-05T08:00:00Z",
			"downloadId":"DL-99",
			"eventType":"bookFileImported",
			"data":{"droppedPath":"/private/downloads/raw.m4b","importedPath":"/private/library/final.m4b","downloadUrl":"https://indexer.example/get?apikey=secret-passkey"},
			"author":{"id":33,"authorName":"Brandon Sanderson","foreignAuthorId":"hc:1","path":"/private/library/Brandon Sanderson","rootFolderPath":"/private/library"},
			"book":{"id":22,"title":"Mistborn","foreignBookId":"hc:book:1","remoteCover":"https://covers.example/secret-cover.jpg"}
		}`
		switch r.URL.Path {
		case "/api/v1/history":
			_, _ = w.Write([]byte(`{"page":1,"pageSize":10,"totalRecords":1,"records":[` + recordJSON + `]}`))
		case "/api/v1/history/author", "/api/v1/history/since":
			_, _ = w.Write([]byte(`[ ` + recordJSON + ` ]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	instance := configuredOperationalDataSource(t, newHistoryDataSource(), server.URL)

	// 1. Paged mode
	req, resp := operationalReadRequest(t, instance, map[string]tftypes.Value{
		"page":           tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
		"page_size":      tftypes.NewValue(tftypes.Number, big.NewFloat(10)),
		"sort_key":       tftypes.NewValue(tftypes.String, "date"),
		"sort_direction": tftypes.NewValue(tftypes.String, "descending"),
		"event_types":    tftypes.NewValue(tftypes.Set{ElementType: tftypes.Number}, []tftypes.Value{tftypes.NewValue(tftypes.Number, big.NewFloat(3)), tftypes.NewValue(tftypes.Number, big.NewFloat(1))}),
		"book_id":        tftypes.NewValue(tftypes.Number, big.NewFloat(22)),
		"download_id":    tftypes.NewValue(tftypes.String, "DL-99"),
		"include_author": tftypes.NewValue(tftypes.Bool, true),
		"include_book":   tftypes.NewValue(tftypes.Bool, true),
		"media_type":     tftypes.NewValue(tftypes.String, "audiobook"),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected paged history diagnostics: %v", resp.Diagnostics)
	}
	var state historyState
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	var records []historyRecordModel
	resp.Diagnostics.Append(state.Records.ElementsAs(t.Context(), &records, false)...)
	if len(records) != 1 || records[0].QualityName.ValueString() != "M4B" || records[0].AuthorName.ValueString() != "Brandon Sanderson" || records[0].BookTitle.ValueString() != "Mistborn" {
		t.Fatalf("unexpected history records: %+v", records)
	}
	assertNoForbiddenSubstrings(t, resp.State.Raw.String(),
		operationalSyntheticAPIKey,
		"/private/downloads",
		"/private/library",
		"secret-passkey",
		"secret-cover.jpg",
	)

	// 2. Author mode
	req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
		"mode":       tftypes.NewValue(tftypes.String, "author"),
		"author_id":  tftypes.NewValue(tftypes.Number, big.NewFloat(33)),
		"book_id":    tftypes.NewValue(tftypes.Number, big.NewFloat(22)),
		"event_type": tftypes.NewValue(tftypes.String, "bookFileImported"),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected author history diagnostics: %v", resp.Diagnostics)
	}

	// 3. Since mode
	req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
		"mode":       tftypes.NewValue(tftypes.String, "since"),
		"date":       tftypes.NewValue(tftypes.String, "2026-10-01T00:00:00Z"),
		"event_type": tftypes.NewValue(tftypes.String, "bookFileImported"),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected since history diagnostics: %v", resp.Diagnostics)
	}

	// 4. Validation errors for invalid mode combinations
	for _, bad := range []map[string]tftypes.Value{
		{"mode": tftypes.NewValue(tftypes.String, "author")}, // missing author_id
		{"mode": tftypes.NewValue(tftypes.String, "since")},  // missing date
		{"mode": tftypes.NewValue(tftypes.String, "paged"), "author_id": tftypes.NewValue(tftypes.Number, big.NewFloat(1))},
		{"mode": tftypes.NewValue(tftypes.String, "author"), "page": tftypes.NewValue(tftypes.Number, big.NewFloat(1)), "author_id": tftypes.NewValue(tftypes.Number, big.NewFloat(1))},
		{"mode": tftypes.NewValue(tftypes.String, "since"), "book_id": tftypes.NewValue(tftypes.Number, big.NewFloat(1)), "date": tftypes.NewValue(tftypes.String, "2026-10-01T00:00:00Z")},
	} {
		badReq, badResp := operationalReadRequest(t, instance, bad)
		instance.Read(t.Context(), badReq, badResp)
		if !badResp.Diagnostics.HasError() {
			t.Fatalf("expected validation error for history config %v", bad)
		}
	}

	if len(seenURIs) != 3 {
		t.Fatalf("expected 3 HTTP calls, got %v", seenURIs)
	}
}

func TestIgnoredDataSourceSanitizesSourceTitles(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ignored" || r.URL.Query().Get("page") != "1" {
			t.Fatalf("unexpected ignored request: %s", r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"page":1,"pageSize":20,"totalRecords":3,
			"records":[
				{"id":1,"authorId":2,"bookId":3,"downloadId":"D1","sourceTitle":"/var/downloads/secret/Release.One.m4b","date":"2026-10-01T00:00:00Z","protocol":"torrent","downloadClientId":4,"downloadClient":"qBittorrent","downloadClientName":"TorrentPrimary","isInClient":true},
				{"id":2,"authorId":2,"bookId":4,"downloadId":"D2","sourceTitle":"https://indexer.example/dl?passkey=topsecret","date":"2026-10-02T00:00:00Z","protocol":"usenet","downloadClientId":5,"downloadClient":"SABnzbd","downloadClientName":"UsenetPrimary","isInClient":false},
				{"id":3,"authorId":2,"bookId":5,"downloadId":"D3","sourceTitle":"Normal Release Title","date":"2026-10-03T00:00:00Z","protocol":"torrent","downloadClientId":4,"downloadClient":"qBittorrent","downloadClientName":"TorrentPrimary","isInClient":false}
			]
		}`))
	}))
	defer server.Close()

	instance := configuredOperationalDataSource(t, newIgnoredDataSource(), server.URL)
	req, resp := operationalReadRequest(t, instance, map[string]tftypes.Value{
		"page":           tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
		"page_size":      tftypes.NewValue(tftypes.Number, big.NewFloat(20)),
		"sort_key":       tftypes.NewValue(tftypes.String, "date"),
		"sort_direction": tftypes.NewValue(tftypes.String, "descending"),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected ignored diagnostics: %v", resp.Diagnostics)
	}

	var state ignoredState
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	var records []ignoredRecordModel
	resp.Diagnostics.Append(state.Records.ElementsAs(t.Context(), &records, false)...)
	if len(records) != 3 || records[0].SourceTitle.ValueString() != "Release.One.m4b" || records[1].SourceTitle.ValueString() != "" || records[2].SourceTitle.ValueString() != "Normal Release Title" {
		t.Fatalf("unexpected ignored records: %+v", records)
	}
	assertNoForbiddenSubstrings(t, resp.State.Raw.String(),
		operationalSyntheticAPIKey,
		"/var/downloads/secret",
		"topsecret",
		"https://indexer.example",
	)
}

func TestManualImportCandidatesDataSourceRedactsPathsAndRejections(t *testing.T) {
	t.Parallel()

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"id":5,
			"path":"/private/incoming/Brandon Sanderson/The Way of Kings.m4b",
			"name":"/private/incoming/Brandon Sanderson/The Way of Kings.m4b",
			"size":987654,
			"author":{"id":10,"authorName":"Brandon Sanderson","foreignAuthorId":"hc:10","path":"/private/library/Sanderson"},
			"book":{"id":20,"title":"The Way of Kings","foreignBookId":"hc:book:20","remoteCover":"https://covers.example/private.jpg"},
			"editionId":30,
			"foreignEditionId":"hc:ed:30",
			"suggestedForeignAuthorId":"hc:10",
			"suggestedAuthorName":"Brandon Sanderson",
			"suggestedForeignBookId":"hc:book:20",
			"suggestedBookTitle":"The Way of Kings",
			"suggestedForeignEditionId":"hc:ed:30",
			"suggestedEditionTitle":"Unabridged",
			"quality":{"quality":{"id":2,"name":"M4B"}},
			"releaseGroup":"AUDIOGRP",
			"qualityWeight":10,
			"downloadId":"DL-55",
			"indexerFlags":0,
			"rejections":[{"reason":"Path /private/incoming/Brandon Sanderson is locked by token=secret-log-token","type":"permanent"}],
			"warnings":["Warning referencing /private/incoming/Brandon Sanderson"],
			"tags":{"comment":["ripped from /private/source"]},
			"additionalFile":false,
			"replaceExistingFiles":false,
			"disableReleaseSwitching":true
		}]`))
	}))
	defer server.Close()

	instance := configuredOperationalDataSource(t, newManualImportCandidatesDataSource(), server.URL)
	req, resp := operationalReadRequest(t, instance, map[string]tftypes.Value{
		"download_id":            tftypes.NewValue(tftypes.String, "DL-55"),
		"author_id":              tftypes.NewValue(tftypes.Number, big.NewFloat(10)),
		"filter_existing_files":  tftypes.NewValue(tftypes.Bool, true),
		"replace_existing_files": tftypes.NewValue(tftypes.Bool, false),
		"media_type":             tftypes.NewValue(tftypes.String, "audiobook"),
		"book_file_ids":          tftypes.NewValue(tftypes.Set{ElementType: tftypes.Number}, []tftypes.Value{tftypes.NewValue(tftypes.Number, big.NewFloat(8)), tftypes.NewValue(tftypes.Number, big.NewFloat(3))}),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected manual_import_candidates diagnostics: %v", resp.Diagnostics)
	}
	if !strings.Contains(gotQuery, "downloadId=DL-55") || !strings.Contains(gotQuery, "bookFileIds=3&bookFileIds=8") {
		t.Fatalf("unexpected query string: %q", gotQuery)
	}

	var state manualImportCandidatesState
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	var candidates []manualImportCandidateModel
	resp.Diagnostics.Append(state.Candidates.ElementsAs(t.Context(), &candidates, false)...)
	if len(candidates) != 1 ||
		candidates[0].Name.ValueString() != "The Way of Kings.m4b" ||
		candidates[0].PathSHA256.ValueString() == "" ||
		!candidates[0].HasRejections.ValueBool() ||
		candidates[0].RejectionCount.ValueInt64() != 1 ||
		candidates[0].WarningCount.ValueInt64() != 1 {
		t.Fatalf("unexpected candidate model: %+v", candidates)
	}
	assertNoForbiddenSubstrings(t, resp.State.Raw.String(),
		operationalSyntheticAPIKey,
		"/private/incoming",
		"/private/library",
		"/private/source",
		"secret-log-token",
		"https://covers.example",
	)
}

func TestPendingAuthorImportDataSourceOperationsAndRedaction(t *testing.T) {
	t.Parallel()

	itemPayload := `{
		"id":7,
		"providerId":"hc:777",
		"providerPrefix":"hc",
		"authorName":"Ursula K. Le Guin",
		"audiobookStatus":"pending",
		"ebookStatus":"completed",
		"overallStatus":"pending",
		"audiobookMonitored":true,
		"audiobookMonitorNewItems":"all",
		"audiobookMonitorExistingMode":"all",
		"audiobookQualityProfileId":1,
		"audiobookMetadataProfileId":2,
		"audiobookRootFolderPath":"/private/srv/audiobooks",
		"audiobookBooksToMonitor":["b1"],
		"audiobookBooksToSearch":[],
		"audiobookTags":[3,1],
		"ebookMonitored":false,
		"ebookMonitorNewItems":"none",
		"ebookMonitorExistingMode":"none",
		"ebookQualityProfileId":null,
		"ebookMetadataProfileId":null,
		"ebookRootFolderPath":"/private/srv/ebooks",
		"ebookBooksToMonitor":null,
		"ebookBooksToSearch":null,
		"ebookTags":null,
		"tags":[5],
		"searchForMissingBooks":false,
		"createdAt":"2026-10-01T00:00:00Z",
		"updatedAt":null,
		"lastAttemptAt":"2026-10-01T01:00:00Z",
		"nextAttemptAt":"2026-10-01T02:00:00Z",
		"attemptCount":2,
		"maxAttempts":5,
		"lastError":"Upstream timeout calling https://api.hardcover.app with Bearer secret-upstream-token",
		"requestedBy":"admin@internal.lan",
		"sourceApplication":"seerr",
		"correlationId":"corr-secret-999"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/pendingauthorimport":
			_, _ = w.Write([]byte(`[ ` + itemPayload + ` ]`))
		case "/api/v1/pendingauthorimport/7":
			_, _ = w.Write([]byte(itemPayload))
		case "/api/v1/pendingauthorimport/author/exists/hc:777":
			_, _ = w.Write([]byte(`{"exists":false,"authorId":null,"authorName":"Ursula K. Le Guin","pending":true,"pendingId":7,"status":"pending","nextAttempt":"2026-10-01T02:00:00Z","attemptCount":2}`))
		case "/api/v1/pendingauthorimport/profiles/options":
			_, _ = w.Write([]byte(`{
				"audiobook":{"qualityProfiles":[{"id":1,"name":"Spoken"}],"metadataProfiles":[{"id":2,"name":"Standard"}],"rootFolders":[{"path":"/private/srv/audiobooks","name":"/private/srv/audiobooks"}]},
				"ebook":{"qualityProfiles":[{"id":3,"name":"EPUB"}],"metadataProfiles":[{"id":2,"name":"Standard"}],"rootFolders":[{"path":"/private/srv/ebooks","name":"Ebooks"}]},
				"all":{"qualityProfiles":[{"id":1,"name":"Spoken"}],"metadataProfiles":[{"id":2,"name":"Standard"}],"rootFolders":[{"path":"/private/srv/audiobooks","name":"/private/srv/audiobooks"},{"path":"/private/srv/ebooks","name":"Ebooks"}]}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	instance := configuredOperationalDataSource(t, newPendingAuthorImportDataSource(), server.URL)

	// 1. list
	req, resp := operationalReadRequest(t, instance, nil)
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected list diagnostics: %v", resp.Diagnostics)
	}
	var state pendingAuthorImportState
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	if len(state.Items.Elements()) != 1 || !state.Existence.IsNull() || !state.ProfileOptions.IsNull() {
		t.Fatalf("unexpected list state: %+v", state)
	}
	assertNoForbiddenSubstrings(t, resp.State.Raw.String(),
		operationalSyntheticAPIKey,
		"/private/srv/audiobooks",
		"/private/srv/ebooks",
		"secret-upstream-token",
		"admin@internal.lan",
		"corr-secret-999",
	)

	// 2. get
	req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
		"operation":  tftypes.NewValue(tftypes.String, "get"),
		"pending_id": tftypes.NewValue(tftypes.Number, big.NewFloat(7)),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected get diagnostics: %v", resp.Diagnostics)
	}

	// 3. exists
	req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
		"operation":   tftypes.NewValue(tftypes.String, "exists"),
		"provider_id": tftypes.NewValue(tftypes.String, "hc:777"),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected exists diagnostics: %v", resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	if state.Existence.IsNull() || len(state.Items.Elements()) != 0 || strings.Contains(state.ID.ValueString(), "777") {
		t.Fatalf("unexpected exists state: %+v", state)
	}

	// 4. profile_options
	req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
		"operation": tftypes.NewValue(tftypes.String, "profile_options"),
	})
	instance.Read(t.Context(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected profile_options diagnostics: %v", resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
	if state.ProfileOptions.IsNull() {
		t.Fatal("expected non-null profile_options")
	}
	assertNoForbiddenSubstrings(t, resp.State.Raw.String(),
		"/private/srv/audiobooks",
		"/private/srv/ebooks",
	)

	// 5. Validation errors
	for _, bad := range []map[string]tftypes.Value{
		{"operation": tftypes.NewValue(tftypes.String, "get")},                                                                            // missing pending_id
		{"operation": tftypes.NewValue(tftypes.String, "exists")},                                                                         // missing provider_id
		{"operation": tftypes.NewValue(tftypes.String, "exists"), "provider_id": tftypes.NewValue(tftypes.String, "../traversal")},        // unsafe provider_id
		{"operation": tftypes.NewValue(tftypes.String, "list"), "pending_id": tftypes.NewValue(tftypes.Number, big.NewFloat(1))},          // incompatible pending_id
		{"operation": tftypes.NewValue(tftypes.String, "profile_options"), "provider_id": tftypes.NewValue(tftypes.String, "hc:invalid")}, // incompatible provider_id
	} {
		badReq, badResp := operationalReadRequest(t, instance, bad)
		instance.Read(t.Context(), badReq, badResp)
		if !badResp.Diagnostics.HasError() {
			t.Fatalf("expected validation error for pending_author_import config %v", bad)
		}
	}
}

func TestWantedCutoffAndMissingDataSourcesListSingleAndRedaction(t *testing.T) {
	t.Parallel()

	bookPayload := `{
		"id":42,
		"foreignBookId":"hc:book:42",
		"authorId":12,
		"title":"Left Hand of Darkness",
		"seriesTitle":"Hainish Cycle #4",
		"mediaType":"audiobook",
		"monitored":true,
		"anyEditionOk":true,
		"releaseDate":"1969-03-01T00:00:00Z",
		"narrator":"George Guidall",
		"hasFiles":false,
		"grabbed":false,
		"remoteCover":"https://covers.example/secret-wanted.jpg",
		"author":{"id":12,"authorName":"Ursula K. Le Guin","foreignAuthorId":"hc:author:12","path":"/private/library/LeGuin"},
		"bookFiles":[{"id":99,"path":"/private/library/LeGuin/file.m4b"}]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/wanted/cutoff", "/api/v1/wanted/missing":
			_, _ = w.Write([]byte(`{"page":1,"pageSize":10,"totalRecords":1,"records":[` + bookPayload + `]}`))
		case "/api/v1/wanted/cutoff/42", "/api/v1/wanted/missing/42":
			_, _ = w.Write([]byte(bookPayload))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	for _, factory := range []func() datasource.DataSource{newCutoffWantedDataSource, newMissingDataSource} {
		instance := configuredOperationalDataSource(t, factory(), server.URL)

		// Paged query
		req, resp := operationalReadRequest(t, instance, map[string]tftypes.Value{
			"page":           tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
			"page_size":      tftypes.NewValue(tftypes.Number, big.NewFloat(10)),
			"include_author": tftypes.NewValue(tftypes.Bool, true),
			"monitored":      tftypes.NewValue(tftypes.Bool, true),
			"media_type":     tftypes.NewValue(tftypes.String, "audiobook"),
		})
		instance.Read(t.Context(), req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected paged wanted diagnostics: %v", resp.Diagnostics)
		}
		var state wantedBooksState
		resp.Diagnostics.Append(resp.State.Get(t.Context(), &state)...)
		if state.TotalRecords.ValueInt64() != 1 || len(state.Records.Elements()) != 1 {
			t.Fatalf("unexpected wanted paged state: %+v", state)
		}
		assertNoForbiddenSubstrings(t, resp.State.Raw.String(),
			operationalSyntheticAPIKey,
			"/private/library/LeGuin",
			"secret-wanted.jpg",
		)

		// Single book_id lookup
		req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
			"book_id": tftypes.NewValue(tftypes.Number, big.NewFloat(42)),
		})
		instance.Read(t.Context(), req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected single wanted diagnostics: %v", resp.Diagnostics)
		}

		// Reject mixing book_id with paging arguments
		req, resp = operationalReadRequest(t, instance, map[string]tftypes.Value{
			"book_id": tftypes.NewValue(tftypes.Number, big.NewFloat(42)),
			"page":    tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
		})
		instance.Read(t.Context(), req, resp)
		if !resp.Diagnostics.HasError() {
			t.Fatal("expected error when combining book_id and page")
		}
	}
}

func configuredOperationalDataSource(t *testing.T, instance datasource.DataSource, baseURL string) datasource.DataSource {
	t.Helper()
	apiClient, err := client.New(client.Config{
		BaseURL:   baseURL,
		APIKey:    operationalSyntheticAPIKey,
		UserAgent: "test/1.0",
	})
	if err != nil {
		t.Fatalf("create test client: %v", err)
	}
	withConfigure, ok := instance.(datasource.DataSourceWithConfigure)
	if !ok {
		t.Fatalf("%T does not implement DataSourceWithConfigure", instance)
	}
	cfgResp := &datasource.ConfigureResponse{}
	withConfigure.Configure(t.Context(), datasource.ConfigureRequest{ProviderData: apiClient}, cfgResp)
	if cfgResp.Diagnostics.HasError() {
		t.Fatalf("configure %T: %v", instance, cfgResp.Diagnostics)
	}
	return instance
}

func operationalReadRequest(t *testing.T, instance datasource.DataSource, overrides map[string]tftypes.Value) (datasource.ReadRequest, *datasource.ReadResponse) {
	t.Helper()
	schemaResp := &datasource.SchemaResponse{}
	instance.Schema(t.Context(), datasource.SchemaRequest{}, schemaResp)
	typeValue := schemaResp.Schema.Type().TerraformType(t.Context())
	objectType := typeValue.(tftypes.Object)

	configValues := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	stateValues := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		configValues[name] = tftypes.NewValue(attributeType, nil)
		stateValues[name] = tftypes.NewValue(attributeType, tftypes.UnknownValue)
	}
	for name, value := range overrides {
		configValues[name] = value
	}
	configRaw := tftypes.NewValue(typeValue, configValues)
	stateRaw := tftypes.NewValue(typeValue, stateValues)
	return datasource.ReadRequest{Config: tfsdk.Config{Raw: configRaw, Schema: schemaResp.Schema}},
		&datasource.ReadResponse{State: tfsdk.State{Raw: stateRaw, Schema: schemaResp.Schema}}
}

func assertNoForbiddenSubstrings(t *testing.T, rawState string, forbidden ...string) {
	t.Helper()
	for _, needle := range forbidden {
		if strings.Contains(rawState, needle) {
			t.Fatalf("state leaked forbidden substring %q in %s", needle, rawState)
		}
	}
}
