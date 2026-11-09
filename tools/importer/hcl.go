package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// sensitiveValue marks an attribute whose live value must not be written.
// bindSensitiveVars replaces it with a varRef after resource names are final.
type sensitiveValue struct {
	Attr string
}

// varRef is a Terraform variable reference. It never carries the secret value.
type varRef struct {
	Name string
}

// nestedObject is a Terraform object, rendered as `name = { ... }`.
type nestedObject struct {
	Attrs map[string]any
}

// objectList is an ordered list of nested objects.
type objectList []nestedObject

// stringSet is rendered as toset([...]) so it matches set attributes.
type stringSet []string

// int64Set is rendered as toset([...]) so it matches set attributes.
type int64Set []int64

var hclIdentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// sanitizeName transforms arbitrary names into valid HCL identifiers.
func sanitizeName(name string) string {
	if name == "" {
		return "main"
	}
	var sb strings.Builder
	lastWasUnderscore := false
	for _, ch := range strings.ToLower(name) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			sb.WriteRune(ch)
			lastWasUnderscore = false
			continue
		}
		if ch == '_' || ch == ' ' || ch == '-' || ch == '.' || ch == '(' || ch == ')' {
			if !lastWasUnderscore && sb.Len() > 0 {
				sb.WriteRune('_')
				lastWasUnderscore = true
			}
		}
	}
	res := strings.Trim(sb.String(), "_")
	if res == "" {
		return "main"
	}
	if res[0] >= '0' && res[0] <= '9' {
		res = "item_" + res
	}
	return res
}

func assignUniqueResourceNames(resources []DiscoveredResource) {
	order := make([]int, len(resources))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		left, right := resources[order[a]], resources[order[b]]
		if left.ResourceType != right.ResourceType {
			return left.ResourceType < right.ResourceType
		}
		if c := compareImportID(left.ImportID, right.ImportID); c != 0 {
			return c < 0
		}
		if left.ResourceName != right.ResourceName {
			return left.ResourceName < right.ResourceName
		}
		return order[a] < order[b]
	})

	used := map[string]struct{}{}
	for _, idx := range order {
		base := resources[idx].ResourceName
		if base == "" {
			base = "main"
		}
		candidate := base
		for n := 2; ; n++ {
			key := resources[idx].ResourceType + "\x00" + candidate
			if _, exists := used[key]; !exists {
				used[key] = struct{}{}
				resources[idx].ResourceName = candidate
				break
			}
			candidate = fmt.Sprintf("%s_%d", base, n)
		}
	}
}

func compareImportID(left, right string) int {
	leftNum, leftErr := strconv.ParseInt(left, 10, 64)
	rightNum, rightErr := strconv.ParseInt(right, 10, 64)
	if leftErr == nil && rightErr == nil {
		switch {
		case leftNum < rightNum:
			return -1
		case leftNum > rightNum:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(left, right)
}

func bindSensitiveVars(resources []DiscoveredResource) {
	seen := map[string]struct{}{}
	for i := range resources {
		resources[i].Attributes = bindAttrMap(resources[i].ResourceType, resources[i].ResourceName, resources[i].Attributes, seen)
	}
}

func bindAttrMap(resourceType, resourceName string, attrs map[string]any, seen map[string]struct{}) map[string]any {
	for key, value := range attrs {
		switch typed := value.(type) {
		case sensitiveValue:
			attr := typed.Attr
			if attr == "" {
				attr = key
			}
			attrs[key] = varRef{Name: uniqueVarName(sensitiveVarName(resourceType, resourceName, attr), seen)}
		case nestedObject:
			typed.Attrs = bindAttrMap(resourceType, resourceName, typed.Attrs, seen)
			attrs[key] = typed
		case objectList:
			for i := range typed {
				typed[i].Attrs = bindAttrMap(resourceType, resourceName, typed[i].Attrs, seen)
			}
			attrs[key] = typed
		}
	}
	return attrs
}

func sensitiveVarName(resourceType, resourceName, attr string) string {
	return sanitizeName(resourceType + "_" + resourceName + "_" + attr)
}

func uniqueVarName(name string, seen map[string]struct{}) string {
	candidate := name
	for n := 2; ; n++ {
		if _, exists := seen[candidate]; !exists {
			seen[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", name, n)
	}
}

type sensitiveDecl struct {
	Name        string
	Description string
}

func collectSensitiveVars(resources []DiscoveredResource) []sensitiveDecl {
	var decls []sensitiveDecl
	seen := map[string]struct{}{}
	var walk func(res DiscoveredResource, prefix string, attrs map[string]any)
	walk = func(res DiscoveredResource, prefix string, attrs map[string]any) {
		keys := make([]string, 0, len(attrs))
		for key := range attrs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			switch typed := attrs[key].(type) {
			case varRef:
				if _, ok := seen[typed.Name]; ok {
					continue
				}
				seen[typed.Name] = struct{}{}
				decls = append(decls, sensitiveDecl{
					Name: typed.Name,
					Description: fmt.Sprintf(
						"Sensitive value for %s.%s %s. Set TF_VAR_%s or an uncommitted tfvars file. Write-only attributes are not stored in state; other state is plaintext, so use an encrypted backend or OpenTofu state encryption.",
						res.ResourceType, res.ResourceName, path, typed.Name,
					),
				})
			case nestedObject:
				walk(res, path, typed.Attrs)
			case objectList:
				for i := range typed {
					walk(res, fmt.Sprintf("%s[%d]", path, i), typed[i].Attrs)
				}
			}
		}
	}
	for _, res := range resources {
		walk(res, "", res.Attributes)
	}
	sort.Slice(decls, func(i, j int) bool { return decls[i].Name < decls[j].Name })
	return decls
}

// GenerateVariables writes sensitive variable declarations with no defaults and no secret values.
func GenerateVariables(resources []DiscoveredResource) string {
	decls := collectSensitiveVars(resources)
	if len(decls) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`# Sensitive credentials discovered on the live instance.
# Values are intentionally not written here. Supply them at plan time.
# Write-only attributes are sent to Chaptarr and then stored as null.
# The rest of the state is still plaintext. Use an encrypted backend,
# or OpenTofu state encryption.

`)
	for _, decl := range decls {
		sb.WriteString("variable ")
		sb.WriteString(strconv.Quote(decl.Name))
		sb.WriteString(" {\n")
		sb.WriteString("  type        = string\n")
		sb.WriteString("  sensitive   = true\n")
		sb.WriteString("  description = ")
		sb.WriteString(strconv.Quote(decl.Description))
		sb.WriteString("\n}\n\n")
	}
	return sb.String()
}

// GenerateHCL produces OpenTofu HCL for discovered resources.
func GenerateHCL(resources []DiscoveredResource, manual []string, providerHeader bool) string {
	var sb strings.Builder
	if providerHeader {
		sb.WriteString(`# --------------------------------------------------------------------------
# Terraform & OpenTofu configuration for Chaptarr
# Generated by chaptarr-import CLI
# --------------------------------------------------------------------------

terraform {
  required_version = ">= 1.11.2"
  required_providers {
    chaptarr = {
      source  = "josh-archer/chaptarr"
      version = "~> 0.11"
    }
  }
}

provider "chaptarr" {
  # Set CHAPTARR_URL and CHAPTARR_API_KEY in the environment.
}

`)
	}
	if len(manual) > 0 {
		sb.WriteString("# Settings left for manual adoption:\n")
		for _, note := range manual {
			sb.WriteString("# ")
			sb.WriteString(strings.ReplaceAll(note, "\n", " "))
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	for _, res := range resources {
		for _, comment := range res.Comments {
			sb.WriteString("# ")
			sb.WriteString(strings.ReplaceAll(comment, "\n", " "))
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "resource \"%s\" \"%s\" {\n", res.ResourceType, res.ResourceName)
		writeAttrs(&sb, "  ", res.Attributes)
		sb.WriteString("}\n\n")
	}
	return sb.String()
}

func hclIdent(key string) string {
	if hclIdentPattern.MatchString(key) {
		return key
	}
	return strconv.Quote(key)
}

func writeAttrs(sb *strings.Builder, indent string, attrs map[string]any) {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeAttribute(sb, indent, key, attrs[key])
	}
}

func writeAttribute(sb *strings.Builder, indent, key string, value any) {
	rendered := hclIdent(key)
	switch typed := value.(type) {
	case nestedObject:
		if len(typed.Attrs) == 0 {
			fmt.Fprintf(sb, "%s%s = {}\n", indent, rendered)
			return
		}
		fmt.Fprintf(sb, "%s%s = {\n", indent, rendered)
		writeAttrs(sb, indent+"  ", typed.Attrs)
		fmt.Fprintf(sb, "%s}\n", indent)
	case objectList:
		fmt.Fprintf(sb, "%s%-25s = ", indent, rendered)
		writeObjectList(sb, indent, typed)
		sb.WriteByte('\n')
	case varRef:
		fmt.Fprintf(sb, "%s%-25s = var.%s\n", indent, rendered, typed.Name)
	case string:
		fmt.Fprintf(sb, "%s%-25s = %s\n", indent, rendered, strconv.Quote(typed))
	case bool:
		fmt.Fprintf(sb, "%s%-25s = %t\n", indent, rendered, typed)
	case int64:
		fmt.Fprintf(sb, "%s%-25s = %d\n", indent, rendered, typed)
	case int:
		fmt.Fprintf(sb, "%s%-25s = %d\n", indent, rendered, typed)
	case float64:
		fmt.Fprintf(sb, "%s%-25s = %s\n", indent, rendered, strconv.FormatFloat(typed, 'f', -1, 64))
	case []string:
		fmt.Fprintf(sb, "%s%-25s = [%s]\n", indent, rendered, quoteStrings(typed))
	case stringSet:
		fmt.Fprintf(sb, "%s%-25s = toset([%s])\n", indent, rendered, quoteStrings([]string(typed)))
	case []int64:
		fmt.Fprintf(sb, "%s%-25s = [%s]\n", indent, rendered, formatInts(typed))
	case int64Set:
		fmt.Fprintf(sb, "%s%-25s = toset([%s])\n", indent, rendered, formatInts([]int64(typed)))
	case sensitiveValue:
		// An unbound secret must not be rendered as a value.
	}
}

func writeObjectList(sb *strings.Builder, indent string, items objectList) {
	if len(items) == 0 {
		sb.WriteString("[]")
		return
	}
	sb.WriteString("[\n")
	for _, item := range items {
		sb.WriteString(indent)
		sb.WriteString("  {\n")
		writeAttrs(sb, indent+"    ", item.Attrs)
		sb.WriteString(indent)
		sb.WriteString("  },\n")
	}
	sb.WriteString(indent)
	sb.WriteString("]")
}

func quoteStrings(values []string) string {
	quoted := make([]string, len(values))
	for i, item := range values {
		quoted[i] = strconv.Quote(item)
	}
	return strings.Join(quoted, ", ")
}

func formatInts(values []int64) string {
	parts := make([]string, len(values))
	for i, item := range values {
		parts[i] = strconv.FormatInt(item, 10)
	}
	return strings.Join(parts, ", ")
}

// GenerateImportBlocks produces OpenTofu import blocks for discovered resources.
func GenerateImportBlocks(resources []DiscoveredResource) string {
	var sb strings.Builder
	sb.WriteString(`# --------------------------------------------------------------------------
# OpenTofu / Terraform 1.5+ import blocks
# Run tofu apply, then tofu plan and confirm there are no changes.
# Delete this file after the import succeeds.
# --------------------------------------------------------------------------

`)
	for _, res := range resources {
		sb.WriteString("import {\n")
		fmt.Fprintf(&sb, "  to = %s.%s\n", res.ResourceType, res.ResourceName)
		fmt.Fprintf(&sb, "  id = %s\n", strconv.Quote(res.ImportID))
		sb.WriteString("}\n\n")
	}
	return sb.String()
}

// GenerateImportScript produces legacy shell import commands.
func GenerateImportScript(resources []DiscoveredResource) string {
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\n")
	sb.WriteString("# Legacy Terraform import script\n")
	sb.WriteString("# Prefer CHAPTARR_URL and CHAPTARR_API_KEY over putting the API key on the command line.\n")
	sb.WriteString("set -euo pipefail\n\n")
	for _, res := range resources {
		fmt.Fprintf(&sb, "terraform import %s.%s %s\n", res.ResourceType, res.ResourceName, strconv.Quote(res.ImportID))
	}
	return sb.String()
}

func canonicalFormat(format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "all", "hcl", "imports", "script":
		return strings.ToLower(strings.TrimSpace(format)), nil
	default:
		return "", fmt.Errorf("invalid output format %q: expected all, hcl, imports, or script", format)
	}
}
