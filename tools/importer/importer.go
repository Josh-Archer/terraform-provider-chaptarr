// Command chaptarr-import reads a live Chaptarr instance and writes OpenTofu
// configuration, import blocks, and sensitive variable declarations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxPages  = 1000
	maxResponseBytes = 16 << 20
)

const (
	postgresManualNote  = "chaptarr_postgres_database is not a Chaptarr API object. Adopt it by hand with the server host, port, role, and write-only passwords."
	hardcoverManualNote = "chaptarr_hardcover_config is populated. Leave it out of generated configuration so import does not contact Hardcover or rotate its token. The import id is the literal hardcover."
)

// ImporterConfig configures discovery against one Chaptarr instance.
type ImporterConfig struct {
	BaseURL      string
	APIKey       string
	Timeout      time.Duration
	HTTPClient   *http.Client
	AllowPartial bool
	// MaxPages caps record pagination. Zero uses defaultMaxPages.
	MaxPages int
}

// DiscoveredResource is one live object ready for HCL and an import block.
type DiscoveredResource struct {
	ResourceType string
	ResourceName string
	ImportID     string
	Attributes   map[string]any
	Comments     []string
}

// Importer fetches configuration from a live Chaptarr instance.
type Importer struct {
	cfg         ImporterConfig
	httpClient  *http.Client
	failures    []error
	unsupported []string
	manual      []string
}

// NewImporter creates an importer. A nil HTTP client does not follow redirects,
// so the API key cannot be forwarded to another host.
func NewImporter(cfg ImporterConfig) *Importer {
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client = &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Importer{cfg: cfg, httpClient: client}
}

// Failures returns authentication, transport, server, and pagination errors from the last discovery.
func (imp *Importer) Failures() []error {
	return append([]error(nil), imp.failures...)
}

// UnsupportedEndpoints returns endpoints that responded HTTP 404.
func (imp *Importer) UnsupportedEndpoints() []string {
	return append([]string(nil), imp.unsupported...)
}

// ManualAdoption returns resources that were seen but intentionally not emitted.
func (imp *Importer) ManualAdoption() []string {
	return append([]string(nil), imp.manual...)
}

type unsupportedEndpointError struct {
	Path string
}

func (e *unsupportedEndpointError) Error() string {
	return "unsupported endpoint " + e.Path
}

type discoveryIncompleteError struct {
	Failures []error
}

func (e *discoveryIncompleteError) Error() string {
	var sb strings.Builder
	sb.WriteString("incomplete discovery:")
	for _, err := range e.Failures {
		sb.WriteString("\n- ")
		sb.WriteString(err.Error())
	}
	sb.WriteString("\nre-run with -allow-partial to write the resources discovered so far")
	return sb.String()
}

func (imp *Importer) maxPages() int {
	if imp.cfg.MaxPages > 0 {
		return imp.cfg.MaxPages
	}
	return defaultMaxPages
}

func (imp *Importer) fetch(ctx context.Context, apiPath string) (any, error) {
	baseURL := strings.TrimRight(imp.cfg.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("transport error from %s: %w", apiPath, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "chaptarr-import")
	if imp.cfg.APIKey != "" {
		req.Header.Set("X-Api-Key", imp.cfg.APIKey)
	}

	resp, err := imp.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("transport error from %s: %w", apiPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, &unsupportedEndpointError{Path: apiPath}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("transport error from %s: %w", apiPath, err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("transport error from %s: response exceeded size limit", apiPath)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := "request"
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			kind = "authentication"
		} else if resp.StatusCode >= 500 {
			kind = "server"
		}
		// Response bodies are omitted: error payloads can echo credentials.
		return nil, fmt.Errorf("%s error from %s (HTTP %d)", kind, apiPath, resp.StatusCode)
	}

	if len(strings.TrimSpace(string(body))) == 0 {
		return map[string]any{}, nil
	}

	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode error from %s: invalid JSON", apiPath)
	}
	return result, nil
}

func (imp *Importer) noteFetchErr(err error) {
	var unsupported *unsupportedEndpointError
	if errors.As(err, &unsupported) {
		imp.unsupported = append(imp.unsupported, unsupported.Path)
		return
	}
	imp.failures = append(imp.failures, err)
}

func (imp *Importer) fetchObject(ctx context.Context, apiPath string) (map[string]any, bool) {
	payload, err := imp.fetch(ctx, apiPath)
	if err != nil {
		imp.noteFetchErr(err)
		return nil, false
	}
	if object, ok := payload.(map[string]any); ok {
		return object, true
	}
	list, ok := payload.([]any)
	if ok && len(list) == 1 {
		if object, ok := list[0].(map[string]any); ok {
			return object, true
		}
	}
	imp.failures = append(imp.failures, fmt.Errorf("decode error from %s: expected a JSON object", apiPath))
	return nil, false
}

func (imp *Importer) fetchList(ctx context.Context, apiPath string) ([]map[string]any, bool) {
	payload, err := imp.fetch(ctx, apiPath)
	if err != nil {
		imp.noteFetchErr(err)
		return nil, false
	}
	objects, err := imp.listFromPayload(ctx, apiPath, payload)
	if err != nil {
		imp.failures = append(imp.failures, err)
		return nil, false
	}
	return objects, true
}

func (imp *Importer) listFromPayload(ctx context.Context, apiPath string, payload any) ([]map[string]any, error) {
	if list, ok := payload.([]any); ok {
		objects, err := objectsFromList(list)
		if err != nil {
			return nil, fmt.Errorf("decode error from %s: expected JSON objects", apiPath)
		}
		return dedupeObjects(objects), nil
	}
	page, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("decode error from %s: expected a JSON array", apiPath)
	}
	if _, has := page["records"]; !has {
		return nil, fmt.Errorf("decode error from %s: expected a JSON array", apiPath)
	}
	return imp.fetchPaged(ctx, apiPath, page)
}

func (imp *Importer) fetchPaged(ctx context.Context, apiPath string, first map[string]any) ([]map[string]any, error) {
	total, hasTotal := intIfPresent(first, "totalRecords")
	pageSize, hasSize := intIfPresent(first, "pageSize")
	if !hasSize || pageSize < 1 {
		raw, _ := first["records"].([]any)
		if hasTotal && total > int64(len(raw)) {
			return nil, fmt.Errorf("pagination error from %s: incomplete page", apiPath)
		}
		objects, err := objectsFromList(raw)
		if err != nil {
			return nil, fmt.Errorf("decode error from %s: expected JSON objects", apiPath)
		}
		return dedupeObjects(objects), nil
	}

	seen := map[string]struct{}{}
	var all []map[string]any
	add := func(page map[string]any) (bool, error) {
		raw, ok := page["records"].([]any)
		if !ok {
			return false, fmt.Errorf("decode error from %s: records was not an array", apiPath)
		}
		objects, err := objectsFromList(raw)
		if err != nil {
			return false, fmt.Errorf("decode error from %s: expected JSON objects", apiPath)
		}
		for _, object := range objects {
			if id, ok := objectID(object); ok {
				if _, exists := seen[id]; exists {
					return false, fmt.Errorf("pagination error from %s: repeated id", apiPath)
				}
				seen[id] = struct{}{}
			}
			all = append(all, object)
		}
		return int64(len(objects)) >= pageSize, nil
	}

	cont, err := add(first)
	if err != nil {
		return nil, err
	}
	for page := 2; cont && page <= imp.maxPages(); page++ {
		if hasTotal && int64(len(all)) >= total {
			break
		}
		nextPath := withPage(apiPath, page, int(pageSize))
		payload, err := imp.fetch(ctx, nextPath)
		if err != nil {
			return nil, err
		}
		obj, ok := payload.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("decode error from %s: expected a page object", nextPath)
		}
		cont, err = add(obj)
		if err != nil {
			return nil, err
		}
	}
	if cont && (!hasTotal || int64(len(all)) < total) {
		return nil, fmt.Errorf("pagination error from %s: exceeded %d pages", apiPath, imp.maxPages())
	}
	if hasTotal && int64(len(all)) != total {
		return nil, fmt.Errorf("pagination error from %s: received %d records, totalRecords is %d", apiPath, len(all), total)
	}
	return all, nil
}

func withPage(apiPath string, page, pageSize int) string {
	parsed, err := url.Parse(apiPath)
	if err != nil {
		return apiPath
	}
	query := parsed.Query()
	query.Set("page", strconv.Itoa(page))
	query.Set("pageSize", strconv.Itoa(pageSize))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func objectsFromList(list []any) ([]map[string]any, error) {
	if list == nil {
		return []map[string]any{}, nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("expected a JSON object in the list")
		}
		out = append(out, object)
	}
	return out, nil
}

func dedupeObjects(objects []map[string]any) []map[string]any {
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(objects))
	for _, object := range objects {
		if id, ok := objectID(object); ok {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
		}
		out = append(out, object)
	}
	return out
}

func payloadPopulated(v any) bool {
	switch typed := v.(type) {
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return false
	}
}

func (imp *Importer) mapEach(objects []map[string]any, mapFn func(map[string]any) (DiscoveredResource, error)) []DiscoveredResource {
	out := make([]DiscoveredResource, 0, len(objects))
	for _, object := range objects {
		resource, err := mapFn(object)
		if err != nil {
			imp.manual = append(imp.manual, err.Error())
			continue
		}
		out = append(out, resource)
	}
	return out
}

// DiscoverAll retrieves manageable resources from the live instance.
// Unsupported HTTP 404s are skipped. Authentication, transport, decode, and
// server errors fail discovery unless AllowPartial is set.
func (imp *Importer) DiscoverAll(ctx context.Context) ([]DiscoveredResource, error) {
	imp.failures = nil
	imp.unsupported = nil
	imp.manual = nil

	var discovered []DiscoveredResource
	for _, spec := range singletonSpecs {
		discovered = append(discovered, imp.discoverSingleton(ctx, spec)...)
	}
	imp.discoverHardcover(ctx)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/tag", mapTag)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/importlistexclusion", mapImportListExclusion)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/customfilter", mapCustomFilter)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/customformat", mapCustomFormat)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/qualityprofile", mapQualityProfile)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/metadataprofile", mapMetadataProfile)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/releaseprofile", mapReleaseProfile)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/delayprofile", mapDelayProfile)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/qualitydefinition", mapQualityDefinition)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/settings/proxy", mapProxy)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/rootfolder", mapRootFolder)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/remotepathmapping", mapRemotePathMapping)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/metadata", mapMetadata)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/indexer", mapIndexer)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/downloadclient", mapDownloadClient)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/notification", mapNotification)...)
	discovered = append(discovered, imp.discoverCollection(ctx, "/api/v1/importlist", mapImportList)...)

	authors, authorsOK := imp.fetchList(ctx, "/api/v1/author")
	if authorsOK {
		discovered = append(discovered, imp.mapEach(authors, mapAuthor)...)
	}
	discovered = append(discovered, imp.discoverSeries(ctx, authors)...)
	discovered = append(discovered, imp.discoverBooks(ctx)...)

	imp.manual = append([]string{postgresManualNote}, imp.manual...)
	if len(imp.failures) > 0 && !imp.cfg.AllowPartial {
		return nil, &discoveryIncompleteError{Failures: append([]error(nil), imp.failures...)}
	}

	assignUniqueResourceNames(discovered)
	bindSensitiveVars(discovered)
	sort.SliceStable(discovered, func(i, j int) bool {
		if discovered[i].ResourceType != discovered[j].ResourceType {
			return discovered[i].ResourceType < discovered[j].ResourceType
		}
		if c := compareImportID(discovered[i].ImportID, discovered[j].ImportID); c != 0 {
			return c < 0
		}
		return discovered[i].ResourceName < discovered[j].ResourceName
	})
	return discovered, nil
}

func (imp *Importer) discoverCollection(ctx context.Context, apiPath string, mapFn func(map[string]any) (DiscoveredResource, error)) []DiscoveredResource {
	objects, ok := imp.fetchList(ctx, apiPath)
	if !ok {
		return nil
	}
	return imp.mapEach(objects, mapFn)
}

func (imp *Importer) discoverSingleton(ctx context.Context, spec singletonSpec) []DiscoveredResource {
	object, ok := imp.fetchObject(ctx, spec.apiPath)
	if !ok {
		return nil
	}
	resource, err := mapSingleton(spec, object)
	if err != nil {
		imp.failures = append(imp.failures, err)
		return nil
	}
	return []DiscoveredResource{resource}
}

func (imp *Importer) discoverHardcover(ctx context.Context) {
	payload, err := imp.fetch(ctx, "/api/v1/config/hardcover")
	if err != nil {
		imp.noteFetchErr(err)
		return
	}
	if payloadPopulated(payload) {
		imp.manual = append(imp.manual, hardcoverManualNote)
	}
}

func (imp *Importer) discoverSeries(ctx context.Context, authors []map[string]any) []DiscoveredResource {
	payload, err := imp.fetch(ctx, "/api/v1/series")
	if err == nil {
		objects, convErr := imp.listFromPayload(ctx, "/api/v1/series", payload)
		if convErr != nil {
			imp.failures = append(imp.failures, convErr)
			return nil
		}
		return imp.mapEach(objects, mapSeries)
	}
	var unsupported *unsupportedEndpointError
	if !errors.As(err, &unsupported) {
		imp.failures = append(imp.failures, err)
		return nil
	}

	if authors == nil {
		listed, ok := imp.fetchList(ctx, "/api/v1/author")
		if !ok {
			imp.unsupported = append(imp.unsupported, "/api/v1/series")
			return nil
		}
		authors = listed
	}
	seen := map[string]struct{}{}
	var merged []map[string]any
	anyOK := false
	for _, author := range authors {
		id, _, ok := positiveID(author)
		if !ok {
			continue
		}
		path := "/api/v1/series?authorId=" + id
		listed, ok := imp.fetchList(ctx, path)
		if !ok {
			continue
		}
		anyOK = true
		for _, series := range listed {
			sid, _, ok := positiveID(series)
			if ok {
				if _, exists := seen[sid]; exists {
					continue
				}
				seen[sid] = struct{}{}
			}
			merged = append(merged, series)
		}
	}
	if !anyOK {
		imp.unsupported = append(imp.unsupported, "/api/v1/series")
	}
	return imp.mapEach(merged, mapSeries)
}

func (imp *Importer) discoverBooks(ctx context.Context) []DiscoveredResource {
	books, ok := imp.fetchList(ctx, "/api/v1/book")
	if !ok {
		return nil
	}
	var discovered []DiscoveredResource
	for _, book := range books {
		if _, has := book["editions"]; !has {
			if id, _, ok := positiveID(book); ok {
				if full, ok := imp.fetchObject(ctx, "/api/v1/book/"+id); ok {
					book = full
				}
			}
		}
		resource, editions, err := mapBook(book)
		if err != nil {
			imp.manual = append(imp.manual, err.Error())
		} else {
			discovered = append(discovered, resource)
		}
		discovered = append(discovered, editions...)
	}
	return discovered
}

func intFromAny(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false
		}
		parsed := int64(typed)
		return parsed, float64(parsed) == typed
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case json.Number:
		number, err := typed.Int64()
		return number, err == nil
	default:
		return 0, false
	}
}

func intIfPresent(m map[string]any, key string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return 0, false
	}
	return intFromAny(raw)
}

func objectID(m map[string]any) (string, bool) {
	raw, ok := m["id"]
	if !ok || raw == nil {
		return "", false
	}
	if number, ok := intFromAny(raw); ok {
		return strconv.FormatInt(number, 10), true
	}
	text, ok := raw.(string)
	if !ok || text == "" {
		return "", false
	}
	return text, true
}

func positiveID(object map[string]any) (string, int64, bool) {
	raw, ok := object["id"]
	if !ok || raw == nil {
		return "", 0, false
	}
	if text, ok := raw.(string); ok {
		number, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil || number < 1 {
			return "", 0, false
		}
		return strconv.FormatInt(number, 10), number, true
	}
	number, ok := intFromAny(raw)
	if !ok || number < 1 {
		return "", 0, false
	}
	return strconv.FormatInt(number, 10), number, true
}

func unmanaged(typeName, id, reason string) error {
	if id == "" {
		return fmt.Errorf("%s was left unmanaged: %s", typeName, reason)
	}
	return fmt.Errorf("%s %s was left unmanaged: %s", typeName, id, reason)
}
