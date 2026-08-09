// Package version centralizes provider version-derived identifiers.
package version

import "strings"

const userAgentPrefix = "terraform-provider-chaptarr/"

// UserAgent returns the fixed provider user agent for a build version.
func UserAgent(buildVersion string) string {
	buildVersion = strings.TrimSpace(buildVersion)
	if buildVersion == "" {
		buildVersion = "dev"
	}
	return userAgentPrefix + buildVersion
}
