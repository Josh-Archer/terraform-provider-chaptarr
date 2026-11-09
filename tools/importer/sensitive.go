package main

import "sort"

// sensitiveConfigurableAttributes lists Sensitive, non-computed attributes the
// importer must treat as secrets. It is derived from the same tables the
// mappers use. Computed sensitive values such as hardcover has_token are
// omitted because refresh owns them.
func sensitiveConfigurableAttributes() map[string][]string {
	out := map[string][]string{}
	for _, spec := range singletonSpecs {
		var paths []string
		for _, field := range spec.fields {
			if field.sensitive {
				paths = append(paths, field.hcl)
			}
		}
		sort.Strings(paths)
		out[chaptarrType(spec.typeName)] = paths
	}
	for _, name := range []string{"indexer", "download_client", "notification", "import_list", "metadata"} {
		out[chaptarrType(name)] = []string{"secret_fields"}
	}
	out[chaptarrType("hardcover_config")] = []string{"token"}
	out[chaptarrType("root_folder")] = []string{"password"}
	out[chaptarrType("proxy")] = []string{"password"}
	out[chaptarrType("postgres_database")] = []string{
		"admin_password",
		"role_password",
		"vaultwarden_bridge_token",
		"vaultwarden_secret_key",
	}
	return out
}
