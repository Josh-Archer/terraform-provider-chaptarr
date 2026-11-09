package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	authorComment  = "search_for_missing_books, move_files_on_update, and delete flags stay unset so import does not search, move, or delete."
	bookComment    = "search_for_new_book and delete flags stay unset. lookup_json is not recoverable from the API."
	seriesComment  = "selected_books stays empty. preferred_narrator is observed and is not managed, so later updates do not call the preferred-narrator endpoint."
	editionComment = "monitored must stay true. Destroy forgets Terraform ownership and does not change the edition."
	rootComment    = "allow_destroy is false so Terraform will not remove the root-folder registration."
)

func chaptarrType(name string) string { return "chaptarr_" + name }

func validProviderID(value string) bool {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func stringValue(object map[string]any, key string) string {
	text, _ := object[key].(string)
	return strings.TrimSpace(text)
}

func boolValue(object map[string]any, key string) (bool, error) {
	raw, exists := object[key]
	if !exists || raw == nil {
		return false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%s was not a boolean", key)
	}
	return value, nil
}

func intValue(object map[string]any, key string) (int64, error) {
	raw, exists := object[key]
	if !exists || raw == nil {
		return 0, nil
	}
	value, ok := intFromAny(raw)
	if !ok {
		return 0, fmt.Errorf("%s was not an integer", key)
	}
	return value, nil
}

func optionalString(object map[string]any, key string) (string, bool, error) {
	raw, exists := object[key]
	if !exists || raw == nil {
		return "", false, nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", false, fmt.Errorf("%s was not a string", key)
	}
	return text, true, nil
}

func optionalInt(object map[string]any, key string) (int64, bool, error) {
	raw, exists := object[key]
	if !exists || raw == nil {
		return 0, false, nil
	}
	value, ok := intFromAny(raw)
	if !ok {
		return 0, false, fmt.Errorf("%s was not an integer", key)
	}
	return value, true, nil
}

func optionalBool(object map[string]any, key string) (bool, bool, error) {
	raw, exists := object[key]
	if !exists || raw == nil {
		return false, false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, false, fmt.Errorf("%s was not a boolean", key)
	}
	return value, true, nil
}

func optionalPositiveID(object map[string]any, key string) (int64, bool, error) {
	value, present, err := optionalInt(object, key)
	if err != nil || !present {
		return 0, false, err
	}
	if value < 1 {
		return 0, false, fmt.Errorf("%s %d is below 1", key, value)
	}
	return value, true, nil
}

func putOptionalString(attrs map[string]any, object map[string]any, apiKey, hclKey string) error {
	value, present, err := optionalString(object, apiKey)
	if err != nil || !present {
		return err
	}
	attrs[hclKey] = value
	return nil
}

func putOptionalBool(attrs map[string]any, object map[string]any, apiKey, hclKey string) error {
	value, present, err := optionalBool(object, apiKey)
	if err != nil || !present {
		return err
	}
	attrs[hclKey] = value
	return nil
}

func putPositiveID(attrs map[string]any, object map[string]any, apiKey, hclKey string) error {
	value, present, err := optionalPositiveID(object, apiKey)
	if err != nil || !present {
		return err
	}
	attrs[hclKey] = value
	return nil
}

func intSet(object map[string]any, key string) (int64Set, error) {
	raw, exists := object[key]
	if !exists || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s was not an array", key)
	}
	seen := map[int64]struct{}{}
	values := make([]int64, 0, len(list))
	for _, item := range list {
		number, ok := intFromAny(item)
		if !ok {
			return nil, fmt.Errorf("%s contained a non-integer", key)
		}
		if _, exists := seen[number]; exists {
			continue
		}
		seen[number] = struct{}{}
		values = append(values, number)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return int64Set(values), nil
}

func putIntSet(attrs map[string]any, object map[string]any, apiKey, hclKey string) error {
	values, err := intSet(object, apiKey)
	if err != nil || len(values) == 0 {
		return err
	}
	attrs[hclKey] = values
	return nil
}

func stringList(value any) ([]string, bool) {
	if value == nil {
		return []string{}, true
	}
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

func normalizeStringValues(values []string) []string {
	byKey := make(map[string]string, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := strings.ToLower(trimmed)
		if trimmed == "" {
			continue
		}
		current, exists := byKey[key]
		if !exists || preferredString(trimmed, current, key) {
			byKey[key] = trimmed
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}

func preferredString(candidate, current, folded string) bool {
	candidateFolded := candidate == folded
	currentFolded := current == folded
	if candidateFolded != currentFolded {
		return candidateFolded
	}
	return candidate < current
}

func putStringSet(attrs map[string]any, object map[string]any, apiKey, hclKey string, splitCSV bool) error {
	raw, exists := object[apiKey]
	if !exists || raw == nil {
		return nil
	}
	var values []string
	if splitCSV {
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("%s was not a string", apiKey)
		}
		values = strings.Split(text, ",")
	} else {
		list, ok := stringList(raw)
		if !ok {
			return fmt.Errorf("%s was not an array of strings", apiKey)
		}
		values = list
	}
	normalized := normalizeStringValues(values)
	if len(normalized) == 0 {
		return nil
	}
	attrs[hclKey] = stringSet(normalized)
	return nil
}

func canonicalJSON(value any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func sensitiveField(field map[string]any) bool {
	privacy, _ := field["privacy"].(string)
	fieldType, _ := field["type"].(string)
	return privacy == "password" || privacy == "apiKey" || fieldType == "password"
}

func splitProviderFields(raw any) (string, nestedObject, error) {
	if raw == nil {
		raw = []any{}
	}
	list, ok := raw.([]any)
	if !ok {
		return "", nestedObject{}, fmt.Errorf("fields was not an array")
	}
	values := map[string]any{}
	secrets := map[string]any{}
	for _, item := range list {
		field, ok := item.(map[string]any)
		if !ok {
			return "", nestedObject{}, fmt.Errorf("a provider field was not an object")
		}
		name := strings.TrimSpace(stringValue(field, "name"))
		if name == "" {
			return "", nestedObject{}, fmt.Errorf("a provider field name was empty")
		}
		if sensitiveField(field) {
			if secretPresent(field["value"]) {
				secrets[name] = sensitiveValue{Attr: "secret_fields_" + name}
			}
			continue
		}
		if _, exists := field["value"]; exists {
			values[name] = field["value"]
		}
	}
	encoded, err := canonicalJSON(values)
	if err != nil {
		return "", nestedObject{}, err
	}
	return encoded, nestedObject{Attrs: secrets}, nil
}

type integrationOptions struct {
	resourceType    string
	nameKey         string
	emitTags        bool
	emitTestOnApply bool
}

func mapIntegration(object map[string]any, opts integrationOptions) (DiscoveredResource, map[string]any, error) {
	id, _, ok := positiveID(object)
	name := stringValue(object, "name")
	if name == "" {
		name = stringValue(object, opts.nameKey)
	}
	if !ok || name == "" {
		return DiscoveredResource{}, nil, unmanaged(opts.resourceType, id, "it has no positive id or name")
	}
	implementation := stringValue(object, "implementation")
	contract := stringValue(object, "configContract")
	if implementation == "" || contract == "" {
		return DiscoveredResource{}, nil, unmanaged(opts.resourceType, id, "implementation or config_contract is empty")
	}
	enabled, err := boolValue(object, "enable")
	if err != nil {
		return DiscoveredResource{}, nil, unmanaged(opts.resourceType, id, err.Error())
	}
	encoded, secrets, err := splitProviderFields(object["fields"])
	if err != nil {
		return DiscoveredResource{}, nil, unmanaged(opts.resourceType, id, err.Error())
	}
	attrs := map[string]any{
		"name":              name,
		"implementation":    implementation,
		"config_contract":   contract,
		"enable":            enabled,
		"field_values_json": encoded,
	}
	if len(secrets.Attrs) > 0 {
		attrs["secret_fields"] = secrets
	}
	if opts.emitTestOnApply && enabled {
		attrs["test_on_apply"] = true
	}
	if opts.emitTags {
		if err := putIntSet(attrs, object, "tags", "tags"); err != nil {
			return DiscoveredResource{}, nil, unmanaged(opts.resourceType, id, err.Error())
		}
	}
	return DiscoveredResource{
		ResourceType: opts.resourceType,
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
	}, attrs, nil
}

func mapTag(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	label := stringValue(object, "label")
	if !ok || label == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("tag"), id, "it has no positive id or label")
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("tag"),
		ResourceName: sanitizeName(label),
		ImportID:     id,
		Attributes:   map[string]any{"label": label},
	}, nil
}

func mapImportListExclusion(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	foreignID := stringValue(object, "foreignId")
	authorName := stringValue(object, "authorName")
	if !ok || foreignID == "" || authorName == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("import_list_exclusion"), id, "foreign_id or author_name is empty")
	}
	media := stringValue(object, "mediaType")
	if media == "" {
		media = "all"
	}
	if media != "all" && media != "audiobook" && media != "ebook" {
		return DiscoveredResource{}, unmanaged(chaptarrType("import_list_exclusion"), id, "media_type is not all, audiobook, or ebook")
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("import_list_exclusion"),
		ResourceName: sanitizeName(authorName),
		ImportID:     id,
		Attributes: map[string]any{
			"foreign_id":  foreignID,
			"author_name": authorName,
			"media_type":  media,
		},
	}, nil
}

func mapCustomFilter(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	filterType := stringValue(object, "type")
	label := stringValue(object, "label")
	if !ok || filterType == "" || label == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_filter"), id, "type or label is empty")
	}
	raw, exists := object["filters"]
	if !exists || raw == nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_filter"), id, "filters was not an array")
	}
	if _, isArray := raw.([]any); !isArray {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_filter"), id, "filters was not an array")
	}
	encoded, err := canonicalJSON(raw)
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_filter"), id, "filters could not be encoded")
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("custom_filter"),
		ResourceName: sanitizeName(label),
		ImportID:     id,
		Attributes: map[string]any{
			"type":         filterType,
			"label":        label,
			"filters_json": encoded,
		},
	}, nil
}

func mapCustomFormat(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	name := stringValue(object, "name")
	applies := stringValue(object, "appliesTo")
	if !ok || name == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_format"), id, "it has no positive id or name")
	}
	if applies != "both" && applies != "audiobook" && applies != "ebook" {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_format"), id, "applies_to is not both, audiobook, or ebook")
	}
	raw, exists := object["specifications"]
	list, isArray := raw.([]any)
	if !exists || raw == nil || !isArray || len(list) == 0 {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_format"), id, "specifications must be a non-empty array")
	}
	for _, item := range list {
		spec, ok := item.(map[string]any)
		if !ok || stringValue(spec, "name") == "" || stringValue(spec, "implementation") == "" {
			return DiscoveredResource{}, unmanaged(chaptarrType("custom_format"), id, "a specification is missing name or implementation")
		}
	}
	encoded, err := canonicalJSON(raw)
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_format"), id, "specifications could not be encoded")
	}
	attrs := map[string]any{
		"name":                name,
		"applies_to":          applies,
		"specifications_json": encoded,
	}
	if value, present, err := optionalBool(object, "includeCustomFormatWhenRenaming"); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("custom_format"), id, err.Error())
	} else if present {
		attrs["include_when_renaming"] = value
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("custom_format"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
	}, nil
}

func mapQualityProfile(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	name := stringValue(object, "name")
	profileType := stringValue(object, "profileType")
	if !ok || name == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, "it has no positive id or name")
	}
	if profileType != "audiobook" && profileType != "ebook" {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, "profile_type is not audiobook or ebook")
	}
	upgrade, err := boolValue(object, "upgradeAllowed")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, err.Error())
	}
	prefer, err := boolValue(object, "preferCustomFormatsOverQuality")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, err.Error())
	}
	if profileType == "ebook" && prefer {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, "ebook profiles cannot prefer custom formats over quality")
	}
	cutoff, err := intValue(object, "cutoff")
	if err != nil || cutoff < 1 {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, "cutoff must be at least 1")
	}
	minScore, err := intValue(object, "minFormatScore")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, err.Error())
	}
	cutoffScore, err := intValue(object, "cutoffFormatScore")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, err.Error())
	}
	items, err := mapQualityItems(object["items"])
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, err.Error())
	}
	formats, err := mapFormatItems(object["formatItems"])
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, err.Error())
	}
	attrs := map[string]any{
		"name":                               name,
		"profile_type":                       profileType,
		"upgrade_allowed":                    upgrade,
		"prefer_custom_formats_over_quality": prefer,
		"cutoff":                             cutoff,
		"items":                              items,
		"minimum_format_score":               minScore,
		"cutoff_format_score":                cutoffScore,
		"format_items":                       formats,
	}
	if _, present := object["convertToQualityId"]; present && object["convertToQualityId"] != nil {
		convert, ok := intFromAny(object["convertToQualityId"])
		if !ok || convert < 1 {
			return DiscoveredResource{}, unmanaged(chaptarrType("quality_profile"), id, "convert_to_quality_id is present but below 1")
		}
		attrs["convert_to_quality_id"] = convert
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("quality_profile"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
	}, nil
}

func mapQualityItems(raw any) (objectList, error) {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("items must be a non-empty array")
	}
	items := make(objectList, 0, len(list))
	allowed := false
	for _, item := range list {
		mapped, itemAllowed, err := mapQualityItem(item)
		if err != nil {
			return nil, err
		}
		allowed = allowed || itemAllowed
		items = append(items, mapped)
	}
	if !allowed {
		return nil, fmt.Errorf("at least one quality item must be allowed")
	}
	return items, nil
}

func mapQualityItem(raw any) (nestedObject, bool, error) {
	item, ok := raw.(map[string]any)
	if !ok {
		return nestedObject{}, false, fmt.Errorf("a quality item was not an object")
	}
	itemID, present, err := optionalInt(item, "id")
	if err != nil {
		return nestedObject{}, false, err
	}
	if !present || itemID < 1 {
		return nestedObject{}, false, fmt.Errorf("a quality item id must be at least 1")
	}
	allowed, err := boolValue(item, "allowed")
	if err != nil {
		return nestedObject{}, false, err
	}
	quality, hasQuality := item["quality"].(map[string]any)
	children, _ := item["items"].([]any)
	if hasQuality && len(children) > 0 {
		return nestedObject{}, false, fmt.Errorf("quality item %d has both a quality and child items", itemID)
	}
	// Direct leaves omit id and name. Those attributes are server-owned; groups still require both.
	attrs := map[string]any{"allowed": allowed}
	if hasQuality {
		qualityID, ok := intFromAny(quality["id"])
		if !ok || qualityID < 0 {
			return nestedObject{}, false, fmt.Errorf("quality item %d has no quality id", itemID)
		}
		attrs["quality_id"] = qualityID
		return nestedObject{Attrs: attrs}, allowed, nil
	}
	if stringValue(item, "name") == "" || len(children) < 2 {
		return nestedObject{}, false, fmt.Errorf("quality group %d needs a name and at least two children", itemID)
	}
	attrs["id"] = itemID
	attrs["name"] = stringValue(item, "name")
	nested := make(objectList, 0, len(children))
	for _, child := range children {
		mapped, err := mapQualityLeaf(child)
		if err != nil {
			return nestedObject{}, false, err
		}
		nested = append(nested, mapped)
	}
	attrs["items"] = nested
	return nestedObject{Attrs: attrs}, allowed, nil
}

func mapQualityLeaf(raw any) (nestedObject, error) {
	item, ok := raw.(map[string]any)
	if !ok {
		return nestedObject{}, fmt.Errorf("a quality child was not an object")
	}
	quality, hasQuality := item["quality"].(map[string]any)
	children, _ := item["items"].([]any)
	if !hasQuality || len(children) > 0 {
		return nestedObject{}, fmt.Errorf("a quality child must be a leaf")
	}
	qualityID, ok := intFromAny(quality["id"])
	if !ok || qualityID < 0 {
		return nestedObject{}, fmt.Errorf("a quality child has no quality id")
	}
	allowed, err := boolValue(item, "allowed")
	if err != nil {
		return nestedObject{}, err
	}
	return nestedObject{Attrs: map[string]any{"quality_id": qualityID, "allowed": allowed}}, nil
}

func mapFormatItems(raw any) (objectList, error) {
	if raw == nil {
		return objectList{}, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("format_items was not an array")
	}
	items := make(objectList, 0, len(list))
	for _, item := range list {
		field, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("a format item was not an object")
		}
		formatID, present := intIfPresent(field, "format")
		if !present || formatID < 1 {
			return nil, fmt.Errorf("a format item id must be at least 1")
		}
		score, err := intValue(field, "score")
		if err != nil {
			return nil, err
		}
		items = append(items, nestedObject{Attrs: map[string]any{"format_id": formatID, "score": score}})
	}
	return items, nil
}

func mapMetadataProfile(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	name := stringValue(object, "name")
	if !ok || name == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("metadata_profile"), id, "it has no positive id or name")
	}
	profileType, err := intValue(object, "profileType")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("metadata_profile"), id, err.Error())
	}
	profileName, valid := map[int64]string{0: "general", 1: "audiobook", 2: "ebook"}[profileType]
	if !valid {
		return DiscoveredResource{}, unmanaged(chaptarrType("metadata_profile"), id, "profile_type is not general, audiobook, or ebook")
	}
	popularity, err := floatValue(object, "minPopularity")
	if err != nil || popularity < 0 {
		return DiscoveredResource{}, unmanaged(chaptarrType("metadata_profile"), id, "minimum_popularity is invalid")
	}
	pages, err := intValue(object, "minPages")
	if err != nil || pages < 0 {
		return DiscoveredResource{}, unmanaged(chaptarrType("metadata_profile"), id, "minimum_pages is invalid")
	}
	attrs := map[string]any{
		"name":               name,
		"profile_type":       profileName,
		"minimum_popularity": popularity,
		"minimum_pages":      pages,
	}
	for _, pair := range [][2]string{
		{"skipMissingDate", "skip_missing_date"},
		{"skipMissingIsbn", "skip_missing_isbn"},
		{"skipPartsAndSets", "skip_parts_and_sets"},
		{"skipSeriesSecondary", "skip_secondary_series"},
		{"skipMissingIdentifierOmnibus", "skip_omnibus_without_identifier"},
		{"skipOmnibus", "skip_omnibus"},
		{"skipMissingAsin", "skip_missing_asin"},
	} {
		value, err := boolValue(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("metadata_profile"), id, err.Error())
		}
		attrs[pair[1]] = value
	}
	if err := putStringSet(attrs, object, "allowedLanguages", "allowed_languages", true); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("metadata_profile"), id, err.Error())
	}
	if err := putStringSet(attrs, object, "ignored", "ignored_terms", false); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("metadata_profile"), id, err.Error())
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("metadata_profile"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
	}, nil
}

func floatValue(object map[string]any, key string) (float64, error) {
	raw, exists := object[key]
	if !exists || raw == nil {
		return 0, nil
	}
	switch typed := raw.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, fmt.Errorf("%s was not a number", key)
		}
		return typed, nil
	case int64:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	default:
		if number, ok := intFromAny(raw); ok {
			return float64(number), nil
		}
		return 0, fmt.Errorf("%s was not a number", key)
	}
}

func mapReleaseProfile(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	if !ok {
		return DiscoveredResource{}, unmanaged(chaptarrType("release_profile"), "", "it has no positive id")
	}
	enabled, err := boolValue(object, "enabled")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("release_profile"), id, err.Error())
	}
	indexerID, err := intValue(object, "indexerId")
	if err != nil || indexerID < 0 {
		return DiscoveredResource{}, unmanaged(chaptarrType("release_profile"), id, "indexer_id is invalid")
	}
	required, ok := stringList(object["required"])
	if !ok {
		return DiscoveredResource{}, unmanaged(chaptarrType("release_profile"), id, "required terms were not an array of strings")
	}
	ignored, ok := stringList(object["ignored"])
	if !ok {
		return DiscoveredResource{}, unmanaged(chaptarrType("release_profile"), id, "ignored terms were not an array of strings")
	}
	attrs := map[string]any{"enabled": enabled, "indexer_id": indexerID}
	if len(required) > 0 {
		attrs["required_terms"] = required
	}
	if len(ignored) > 0 {
		attrs["ignored_terms"] = ignored
	}
	if err := putIntSet(attrs, object, "tags", "tags"); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("release_profile"), id, err.Error())
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("release_profile"),
		ResourceName: "current",
		ImportID:     id,
		Attributes:   attrs,
	}, nil
}

func mapDelayProfile(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	if !ok {
		return DiscoveredResource{}, unmanaged(chaptarrType("delay_profile"), "", "it has no positive id")
	}
	protocol := stringValue(object, "preferredProtocol")
	if protocol != "usenet" && protocol != "torrent" && protocol != "unknown" {
		return DiscoveredResource{}, unmanaged(chaptarrType("delay_profile"), id, "preferred_protocol is not usenet, torrent, or unknown")
	}
	usenet, err := intValue(object, "usenetDelay")
	if err != nil || usenet < 0 {
		return DiscoveredResource{}, unmanaged(chaptarrType("delay_profile"), id, "usenet_delay_minutes is invalid")
	}
	torrent, err := intValue(object, "torrentDelay")
	if err != nil || torrent < 0 {
		return DiscoveredResource{}, unmanaged(chaptarrType("delay_profile"), id, "torrent_delay_minutes is invalid")
	}
	attrs := map[string]any{
		"preferred_protocol":    protocol,
		"usenet_delay_minutes":  usenet,
		"torrent_delay_minutes": torrent,
	}
	for _, pair := range [][2]string{
		{"enableUsenet", "enable_usenet"},
		{"enableTorrent", "enable_torrent"},
		{"bypassIfHighestQuality", "bypass_if_highest_quality"},
		{"bypassIfAboveCustomFormatScore", "bypass_if_above_custom_format_score"},
	} {
		value, err := boolValue(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("delay_profile"), id, err.Error())
		}
		attrs[pair[1]] = value
	}
	score, err := intValue(object, "minimumCustomFormatScore")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("delay_profile"), id, err.Error())
	}
	attrs["minimum_custom_format_score"] = score
	if err := putIntSet(attrs, object, "tags", "tags"); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("delay_profile"), id, err.Error())
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("delay_profile"),
		ResourceName: sanitizeName(protocol),
		ImportID:     id,
		Attributes:   attrs,
	}, nil
}

func mapQualityDefinition(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	quality, _ := object["quality"].(map[string]any)
	qualityID, present := intIfPresent(quality, "id")
	if !ok || !present || qualityID < 1 {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_definition"), id, "quality_id must be at least 1")
	}
	name := stringValue(quality, "name")
	if name == "" {
		name = stringValue(object, "title")
	}
	if name == "" {
		name = "quality_" + id
	}
	attrs := map[string]any{"quality_id": qualityID}
	if value, present, err := optionalFloat(object, "minSize"); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_definition"), id, err.Error())
	} else if present {
		attrs["minimum_size"] = value
	}
	if value, present, err := optionalFloat(object, "maxSize"); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("quality_definition"), id, err.Error())
	} else if present {
		attrs["maximum_size"] = value
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("quality_definition"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
	}, nil
}

func optionalFloat(object map[string]any, key string) (float64, bool, error) {
	raw, exists := object[key]
	if !exists || raw == nil {
		return 0, false, nil
	}
	switch typed := raw.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false, fmt.Errorf("%s was not a number", key)
		}
		return typed, true, nil
	default:
		if number, ok := intFromAny(raw); ok {
			return float64(number), true, nil
		}
		return 0, false, fmt.Errorf("%s was not a number", key)
	}
}

func mapProxy(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	name := stringValue(object, "name")
	proxyType := stringValue(object, "type")
	hostname := stringValue(object, "hostname")
	if !ok || name == "" || hostname == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("proxy"), id, "name or hostname is empty")
	}
	if proxyType != "http" && proxyType != "socks4" && proxyType != "socks5" {
		return DiscoveredResource{}, unmanaged(chaptarrType("proxy"), id, "type is not http, socks4, or socks5")
	}
	port, err := intValue(object, "port")
	if err != nil || port < 1 || port > 65535 {
		return DiscoveredResource{}, unmanaged(chaptarrType("proxy"), id, "port is outside 1-65535")
	}
	attrs := map[string]any{
		"name":     name,
		"type":     proxyType,
		"hostname": hostname,
		"port":     port,
	}
	username, present, err := optionalString(object, "username")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("proxy"), id, err.Error())
	}
	if present && username != "" {
		attrs["username"] = username
	}
	if secretPresent(object["password"]) {
		if strings.TrimSpace(username) == "" {
			return DiscoveredResource{}, unmanaged(chaptarrType("proxy"), id, "a password is set without a username")
		}
		attrs["password"] = sensitiveValue{Attr: "password"}
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("proxy"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
	}, nil
}

func mapRootFolder(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	name := stringValue(object, "name")
	path := stringValue(object, "path")
	if !ok || name == "" || path == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, "name or path is empty")
	}
	folderType, err := intValue(object, "folderType")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
	}
	folderName, valid := map[int64]string{0: "mixed", 1: "audiobook", 2: "ebook"}[folderType]
	if !valid {
		return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, "folder_type is not mixed, audiobook, or ebook")
	}
	calibre, err := boolValue(object, "isCalibreLibrary")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
	}
	useSSL, err := boolValue(object, "useSsl")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
	}
	place, err := boolValue(object, "placeEbooksWithAudiobooks")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
	}
	port, err := intValue(object, "port")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
	}
	attrs := map[string]any{
		"name":                         name,
		"path":                         path,
		"folder_type":                  folderName,
		"is_calibre_library":           calibre,
		"port":                         port,
		"use_ssl":                      useSSL,
		"place_ebooks_with_audiobooks": place,
		"allow_destroy":                false,
	}
	for _, pair := range [][2]string{
		{"host", "host"},
		{"urlBase", "url_base"},
		{"username", "username"},
		{"library", "library"},
		{"outputFormat", "output_format"},
		{"outputProfile", "output_profile"},
	} {
		if err := putOptionalString(attrs, object, pair[0], pair[1]); err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
		}
	}
	if secretPresent(object["password"]) {
		attrs["password"] = sensitiveValue{Attr: "password"}
	}
	for _, pair := range [][2]string{
		{"defaultSyncMonitoredAcrossFormats", "default_sync_monitored_across_formats"},
		{"audiobookMonitorFuture", "audiobook_monitor_future"},
		{"audiobookWriteAudioBookShelfMetadataJson", "audiobook_write_metadata_json"},
		{"audiobookWriteAudioBookShelfCover", "audiobook_write_cover"},
		{"ebookMonitorFuture", "ebook_monitor_future"},
		{"ebookWriteAudioBookShelfMetadataJson", "ebook_write_metadata_json"},
		{"ebookWriteAudioBookShelfCover", "ebook_write_cover"},
	} {
		if err := putOptionalBool(attrs, object, pair[0], pair[1]); err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
		}
	}
	for _, pair := range [][2]string{
		{"audiobookQualityProfileId", "audiobook_quality_profile_id"},
		{"audiobookMetadataProfileId", "audiobook_metadata_profile_id"},
		{"ebookQualityProfileId", "ebook_quality_profile_id"},
		{"ebookMetadataProfileId", "ebook_metadata_profile_id"},
	} {
		if err := putPositiveID(attrs, object, pair[0], pair[1]); err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
		}
	}
	for _, pair := range [][2]string{
		{"audiobookMonitorExisting", "audiobook_monitor_existing"},
		{"ebookMonitorExisting", "ebook_monitor_existing"},
	} {
		value, present, err := optionalInt(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
		}
		if !present {
			continue
		}
		if value < 0 || value > 2 {
			return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, pair[1]+" is outside 0-2")
		}
		attrs[pair[1]] = value
	}
	for _, pair := range [][2]string{
		{"defaultTags", "default_tags"},
		{"audiobookTags", "audiobook_tags"},
		{"ebookTags", "ebook_tags"},
	} {
		if err := putIntSet(attrs, object, pair[0], pair[1]); err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("root_folder"), id, err.Error())
		}
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("root_folder"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
		Comments:     []string{rootComment},
	}, nil
}

func mapRemotePathMapping(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	remotePath := stringValue(object, "remotePath")
	localPath := stringValue(object, "localPath")
	if !ok || remotePath == "" || localPath == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("remote_path_mapping"), id, "remote_path or local_path is empty")
	}
	clientID, err := intValue(object, "downloadClientId")
	if err != nil || clientID < 0 {
		return DiscoveredResource{}, unmanaged(chaptarrType("remote_path_mapping"), id, "download_client_id is invalid")
	}
	host := stringValue(object, "host")
	if clientID == 0 && host == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("remote_path_mapping"), id, "host is required when download_client_id is 0")
	}
	name := host
	if name == "" {
		name = localPath
	}
	attrs := map[string]any{
		"download_client_id": clientID,
		"remote_path":        remotePath,
		"local_path":         localPath,
		"test_before_apply":  false,
	}
	if host != "" {
		attrs["host"] = host
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("remote_path_mapping"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
	}, nil
}

func mapMetadata(object map[string]any) (DiscoveredResource, error) {
	resource, _, err := mapIntegration(object, integrationOptions{
		resourceType:    chaptarrType("metadata"),
		emitTags:        true,
		emitTestOnApply: false,
	})
	return resource, err
}

func mapIndexer(object map[string]any) (DiscoveredResource, error) {
	resource, attrs, err := mapIntegration(object, integrationOptions{
		resourceType:    chaptarrType("indexer"),
		emitTags:        true,
		emitTestOnApply: true,
	})
	if err != nil {
		return DiscoveredResource{}, err
	}
	for _, pair := range [][2]string{
		{"enableRss", "enable_rss"},
		{"enableAutomaticSearch", "enable_automatic_search"},
		{"enableInteractiveSearch", "enable_interactive_search"},
	} {
		value, err := boolValue(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("indexer"), resource.ImportID, err.Error())
		}
		attrs[pair[1]] = value
	}
	priority, err := intValue(object, "priority")
	if err != nil || priority < 1 || priority > 50 {
		return DiscoveredResource{}, unmanaged(chaptarrType("indexer"), resource.ImportID, "priority must be from 1 through 50")
	}
	clientID, err := intValue(object, "downloadClientId")
	if err != nil || clientID < 0 {
		return DiscoveredResource{}, unmanaged(chaptarrType("indexer"), resource.ImportID, "download_client_id is invalid")
	}
	attrs["priority"] = priority
	attrs["download_client_id"] = clientID
	if _, present := object["proxyId"]; present && object["proxyId"] != nil {
		proxyID, ok := intFromAny(object["proxyId"])
		if !ok || proxyID < 1 {
			return DiscoveredResource{}, unmanaged(chaptarrType("indexer"), resource.ImportID, "proxy_id is present but below 1")
		}
		attrs["proxy_id"] = proxyID
	}
	return resource, nil
}

func mapDownloadClient(object map[string]any) (DiscoveredResource, error) {
	resource, attrs, err := mapIntegration(object, integrationOptions{
		resourceType:    chaptarrType("download_client"),
		emitTags:        false,
		emitTestOnApply: true,
	})
	if err != nil {
		return DiscoveredResource{}, err
	}
	protocol := stringValue(object, "protocol")
	if protocol != "usenet" && protocol != "torrent" {
		return DiscoveredResource{}, unmanaged(chaptarrType("download_client"), resource.ImportID, "protocol is not usenet or torrent")
	}
	priority, err := intValue(object, "priority")
	if err != nil || priority < 1 || priority > 50 {
		return DiscoveredResource{}, unmanaged(chaptarrType("download_client"), resource.ImportID, "priority must be from 1 through 50")
	}
	attrs["protocol"] = protocol
	attrs["priority"] = priority
	for _, pair := range [][2]string{
		{"removeCompletedDownloads", "remove_completed_downloads"},
		{"removeFailedDownloads", "remove_failed_downloads"},
		{"copyUnmanagedDownloads", "copy_unmanaged_downloads"},
	} {
		value, err := boolValue(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("download_client"), resource.ImportID, err.Error())
		}
		attrs[pair[1]] = value
	}
	if err := putIntSet(attrs, object, "audiobookTags", "audiobook_tags"); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("download_client"), resource.ImportID, err.Error())
	}
	if err := putIntSet(attrs, object, "ebookTags", "ebook_tags"); err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("download_client"), resource.ImportID, err.Error())
	}
	return resource, nil
}

func mapNotification(object map[string]any) (DiscoveredResource, error) {
	resource, attrs, err := mapIntegration(object, integrationOptions{
		resourceType:    chaptarrType("notification"),
		emitTags:        true,
		emitTestOnApply: true,
	})
	if err != nil {
		return DiscoveredResource{}, err
	}
	for _, pair := range [][2]string{
		{"onGrab", "on_grab"},
		{"onReleaseImport", "on_release_import"},
		{"onUpgrade", "on_upgrade"},
		{"onRename", "on_rename"},
		{"onAuthorAdded", "on_author_added"},
		{"onBookAdded", "on_book_added"},
		{"onAuthorDelete", "on_author_delete"},
		{"onBookDelete", "on_book_delete"},
		{"onBookFileDelete", "on_book_file_delete"},
		{"onBookFileDeleteForUpgrade", "on_book_file_delete_for_upgrade"},
		{"onHealthIssue", "on_health_issue"},
		{"includeHealthWarnings", "include_health_warnings"},
		{"onDownloadFailure", "on_download_failure"},
		{"onImportFailure", "on_import_failure"},
		{"onBookRetag", "on_book_retag"},
		{"onApplicationUpdate", "on_application_update"},
	} {
		value, err := boolValue(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("notification"), resource.ImportID, err.Error())
		}
		attrs[pair[1]] = value
	}
	return resource, nil
}

func mapImportList(object map[string]any) (DiscoveredResource, error) {
	resource, attrs, err := mapIntegration(object, integrationOptions{
		resourceType:    chaptarrType("import_list"),
		emitTags:        true,
		emitTestOnApply: true,
	})
	if err != nil {
		return DiscoveredResource{}, err
	}
	root := stringValue(object, "rootFolderPath")
	if root == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("import_list"), resource.ImportID, "root_folder_path is empty")
	}
	monitor := stringValue(object, "shouldMonitor")
	if monitor != "none" && monitor != "specificBook" && monitor != "entireAuthor" {
		return DiscoveredResource{}, unmanaged(chaptarrType("import_list"), resource.ImportID, "should_monitor is invalid")
	}
	newItems := stringValue(object, "monitorNewItems")
	if newItems != "all" && newItems != "none" && newItems != "new" {
		return DiscoveredResource{}, unmanaged(chaptarrType("import_list"), resource.ImportID, "monitor_new_items is invalid")
	}
	qualityID, err := intValue(object, "qualityProfileId")
	if err != nil || qualityID < 1 {
		return DiscoveredResource{}, unmanaged(chaptarrType("import_list"), resource.ImportID, "quality_profile_id must be at least 1")
	}
	metadataID, err := intValue(object, "metadataProfileId")
	if err != nil || metadataID < 1 {
		return DiscoveredResource{}, unmanaged(chaptarrType("import_list"), resource.ImportID, "metadata_profile_id must be at least 1")
	}
	for _, pair := range [][2]string{
		{"enableAutomaticAdd", "enable_automatic_add"},
		{"shouldMonitorExisting", "should_monitor_existing"},
		{"shouldSearch", "should_search"},
	} {
		value, err := boolValue(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("import_list"), resource.ImportID, err.Error())
		}
		attrs[pair[1]] = value
	}
	attrs["root_folder_path"] = root
	attrs["should_monitor"] = monitor
	attrs["monitor_new_items"] = newItems
	attrs["quality_profile_id"] = qualityID
	attrs["metadata_profile_id"] = metadataID
	if listType := stringValue(object, "listType"); listType != "" {
		if listType != "program" && listType != "goodreads" && listType != "other" {
			return DiscoveredResource{}, unmanaged(chaptarrType("import_list"), resource.ImportID, "list_type is invalid")
		}
		attrs["list_type"] = listType
	}
	if interval := stringValue(object, "minRefreshInterval"); interval != "" {
		attrs["minimum_refresh_interval"] = interval
	}
	return resource, nil
}

func mapAuthor(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	foreignID := stringValue(object, "foreignAuthorId")
	if !ok || !validProviderID(foreignID) {
		return DiscoveredResource{}, unmanaged(chaptarrType("author"), id, "foreign_author_id is not a provider-prefixed identifier")
	}
	monitored, err := boolValue(object, "monitored")
	if err != nil {
		return DiscoveredResource{}, unmanaged(chaptarrType("author"), id, err.Error())
	}
	attrs := map[string]any{
		"foreign_author_id": foreignID,
		"monitored":         monitored,
	}
	for _, pair := range [][2]string{
		{"audiobookMonitorExisting", "audiobook_monitor_existing"},
		{"ebookMonitorExisting", "ebook_monitor_existing"},
	} {
		value, present, err := optionalInt(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("author"), id, err.Error())
		}
		if !present {
			value = 0
		}
		if value < 0 || value > 2 {
			return DiscoveredResource{}, unmanaged(chaptarrType("author"), id, pair[1]+" is outside 0-2")
		}
		attrs[pair[1]] = value
	}
	for _, pair := range [][2]string{
		{"audiobookMonitorFuture", "audiobook_monitor_future"},
		{"ebookMonitorFuture", "ebook_monitor_future"},
	} {
		value, present, err := optionalBool(object, pair[0])
		if err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("author"), id, err.Error())
		}
		if !present {
			value = false
		}
		attrs[pair[1]] = value
	}
	for _, pair := range [][2]string{
		{"audiobookRootFolderPath", "audiobook_root_folder_path"},
		{"ebookRootFolderPath", "ebook_root_folder_path"},
	} {
		if err := putOptionalString(attrs, object, pair[0], pair[1]); err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("author"), id, err.Error())
		}
	}
	for _, pair := range [][2]string{
		{"audiobookQualityProfileId", "audiobook_quality_profile_id"},
		{"ebookQualityProfileId", "ebook_quality_profile_id"},
		{"audiobookMetadataProfileId", "audiobook_metadata_profile_id"},
		{"ebookMetadataProfileId", "ebook_metadata_profile_id"},
	} {
		if err := putPositiveID(attrs, object, pair[0], pair[1]); err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("author"), id, err.Error())
		}
	}
	for _, pair := range [][2]string{
		{"audiobookTags", "audiobook_tags"},
		{"ebookTags", "ebook_tags"},
	} {
		if err := putIntSet(attrs, object, pair[0], pair[1]); err != nil {
			return DiscoveredResource{}, unmanaged(chaptarrType("author"), id, err.Error())
		}
	}
	name := stringValue(object, "authorName")
	if name == "" {
		name = foreignID
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("author"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes:   attrs,
		Comments:     []string{authorComment},
	}, nil
}

func mapSeries(object map[string]any) (DiscoveredResource, error) {
	id, _, ok := positiveID(object)
	foreignID := stringValue(object, "foreignSeriesId")
	media := stringValue(object, "mediaType")
	if !ok || foreignID == "" {
		return DiscoveredResource{}, unmanaged(chaptarrType("series"), id, "foreign_series_id is empty")
	}
	if media != "audiobook" && media != "ebook" {
		return DiscoveredResource{}, unmanaged(chaptarrType("series"), id, "media_type is not audiobook or ebook")
	}
	name := stringValue(object, "title")
	if name == "" {
		name = foreignID
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("series"),
		ResourceName: sanitizeName(name),
		ImportID:     id,
		Attributes: map[string]any{
			"foreign_series_id": foreignID,
			"media_type":        media,
		},
		Comments: []string{seriesComment},
	}, nil
}

func mapBook(object map[string]any) (DiscoveredResource, []DiscoveredResource, error) {
	idText, bookID, ok := positiveID(object)
	foreignID := stringValue(object, "foreignBookId")
	authorID, authorPresent, authorErr := optionalInt(object, "authorId")
	media := stringValue(object, "mediaType")
	editions, monitored, editionErr := mapEditions(object, bookID)
	if editionErr != nil {
		return DiscoveredResource{}, editions, unmanaged(chaptarrType("book"), idText, editionErr.Error())
	}
	if !ok || !validProviderID(foreignID) || authorErr != nil || !authorPresent || authorID < 1 || (media != "audiobook" && media != "ebook") {
		reason := "required book identity fields cannot be reconstructed"
		if editionErr != nil {
			reason = editionErr.Error()
		}
		return DiscoveredResource{}, editions, unmanaged(chaptarrType("book"), idText, reason)
	}
	monitoredFlag, err := boolValue(object, "monitored")
	if err != nil {
		return DiscoveredResource{}, editions, unmanaged(chaptarrType("book"), idText, err.Error())
	}
	anyEdition, err := boolValue(object, "anyEditionOk")
	if err != nil {
		return DiscoveredResource{}, editions, unmanaged(chaptarrType("book"), idText, err.Error())
	}
	if len(monitored) > 1 {
		return DiscoveredResource{}, editions, unmanaged(chaptarrType("book"), idText, "more than one edition is monitored")
	}
	attrs := map[string]any{
		"foreign_book_id": foreignID,
		"author_id":       authorID,
		"media_type":      media,
		"monitored":       monitoredFlag,
		"any_edition_ok":  anyEdition,
	}
	if len(monitored) == 1 {
		foreignEdition := monitored[0]
		if validProviderID(foreignEdition) {
			attrs["monitored_edition_id"] = foreignEdition
		} else if !anyEdition {
			return DiscoveredResource{}, editions, unmanaged(chaptarrType("book"), idText, "any_edition_ok is false and the monitored edition has no provider-prefixed id")
		}
	} else if !anyEdition {
		return DiscoveredResource{}, editions, unmanaged(chaptarrType("book"), idText, "any_edition_ok is false and there is no monitored edition")
	}
	name := stringValue(object, "title")
	if name == "" {
		name = foreignID
	}
	return DiscoveredResource{
		ResourceType: chaptarrType("book"),
		ResourceName: sanitizeName(name),
		ImportID:     idText,
		Attributes:   attrs,
		Comments:     []string{bookComment},
	}, editions, nil
}

func mapEditions(object map[string]any, bookID int64) ([]DiscoveredResource, []string, error) {
	raw, exists := object["editions"]
	if !exists || raw == nil {
		return nil, nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("editions was not an array")
	}
	var resources []DiscoveredResource
	var monitored []string
	for _, item := range list {
		edition, ok := item.(map[string]any)
		if !ok {
			return resources, monitored, fmt.Errorf("an edition was not an object")
		}
		isMonitored, err := boolValue(edition, "monitored")
		if err != nil {
			return resources, monitored, err
		}
		if !isMonitored {
			continue
		}
		editionID, present, err := optionalInt(edition, "id")
		if err != nil || !present || editionID < 1 || bookID < 1 {
			return resources, monitored, fmt.Errorf("a monitored edition has no positive id")
		}
		foreign := stringValue(edition, "foreignEditionId")
		monitored = append(monitored, foreign)
		name := stringValue(edition, "title")
		if name == "" {
			name = fmt.Sprintf("edition_%d", editionID)
		}
		resources = append(resources, DiscoveredResource{
			ResourceType: chaptarrType("edition"),
			ResourceName: sanitizeName(name),
			ImportID:     fmt.Sprintf("%d/%d", bookID, editionID),
			Attributes: map[string]any{
				"book_id":    bookID,
				"edition_id": editionID,
				"monitored":  true,
			},
			Comments: []string{editionComment},
		})
	}
	return resources, monitored, nil
}
