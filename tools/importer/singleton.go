package main

import (
	"fmt"
)

type fieldKind uint8

const (
	fieldString fieldKind = iota
	fieldBool
	fieldInt
	fieldSecret
)

// configField is one singleton attribute the importer knows how to emit.
type configField struct {
	hcl       string
	api       string
	kind      fieldKind
	sensitive bool
}

type singletonSpec struct {
	typeName string
	apiPath  string
	drop     []string
	fields   []configField
}

func stringFieldSpec(hcl, api string) configField {
	return configField{hcl: hcl, api: api, kind: fieldString}
}

func boolFieldSpec(hcl, api string) configField {
	return configField{hcl: hcl, api: api, kind: fieldBool}
}

func intFieldSpec(hcl, api string) configField {
	return configField{hcl: hcl, api: api, kind: fieldInt}
}

func secretFieldSpec(hcl, api string) configField {
	return configField{hcl: hcl, api: api, kind: fieldSecret, sensitive: true}
}

func mapSingleton(spec singletonSpec, object map[string]any) (DiscoveredResource, error) {
	rawID, exists := object["id"]
	number, ok := intFromAny(rawID)
	if !exists || rawID == nil || !ok || number < 0 {
		return DiscoveredResource{}, fmt.Errorf("decode error from %s: missing id", spec.apiPath)
	}

	attrs := map[string]any{}
	for _, field := range spec.fields {
		raw, present := object[field.api]
		if !present || raw == nil {
			continue
		}
		switch field.kind {
		case fieldString:
			text, isString := raw.(string)
			if !isString {
				return DiscoveredResource{}, fmt.Errorf("decode error from %s: field %q was not a string", spec.apiPath, field.api)
			}
			attrs[field.hcl] = text
		case fieldBool:
			value, isBool := raw.(bool)
			if !isBool {
				return DiscoveredResource{}, fmt.Errorf("decode error from %s: field %q was not a boolean", spec.apiPath, field.api)
			}
			attrs[field.hcl] = value
		case fieldInt:
			value, isInt := intFromAny(raw)
			if !isInt {
				return DiscoveredResource{}, fmt.Errorf("decode error from %s: field %q was not an integer", spec.apiPath, field.api)
			}
			attrs[field.hcl] = value
		case fieldSecret:
			// Never copy the live value, including redacted placeholders.
			if !secretPresent(raw) {
				continue
			}
			attrs[field.hcl] = sensitiveValue{Attr: field.hcl}
		default:
			return DiscoveredResource{}, fmt.Errorf("decode error from %s: unsupported field %q", spec.apiPath, field.api)
		}
	}

	return DiscoveredResource{
		ResourceType: "chaptarr_" + spec.typeName,
		ResourceName: "current",
		ImportID:     spec.typeName,
		Attributes:   attrs,
	}, nil
}

func secretPresent(value any) bool {
	if value == nil {
		return false
	}
	text, isString := value.(string)
	if isString {
		return text != ""
	}
	return true
}

var singletonSpecs = []singletonSpec{
	{
		typeName: "host_config",
		apiPath:  "/api/v1/config/host",
		drop:     []string{"apiKey"},
		fields: []configField{
			stringFieldSpec("bind_address", "bindAddress"),
			intFieldSpec("port", "port"),
			intFieldSpec("ssl_port", "sslPort"),
			boolFieldSpec("enable_ssl", "enableSsl"),
			boolFieldSpec("launch_browser", "launchBrowser"),
			stringFieldSpec("authentication_method", "authenticationMethod"),
			stringFieldSpec("authentication_required", "authenticationRequired"),
			stringFieldSpec("username", "username"),
			secretFieldSpec("password", "password"),
			secretFieldSpec("password_confirmation", "passwordConfirmation"),
			stringFieldSpec("log_level", "logLevel"),
			stringFieldSpec("console_log_level", "consoleLogLevel"),
			stringFieldSpec("branch", "branch"),
			stringFieldSpec("ssl_cert_path", "sslCertPath"),
			secretFieldSpec("ssl_cert_password", "sslCertPassword"),
			stringFieldSpec("url_base", "urlBase"),
			stringFieldSpec("instance_name", "instanceName"),
			stringFieldSpec("application_url", "applicationUrl"),
			boolFieldSpec("update_automatically", "updateAutomatically"),
			stringFieldSpec("update_mechanism", "updateMechanism"),
			stringFieldSpec("update_script_path", "updateScriptPath"),
			stringFieldSpec("proxy_mode", "proxyMode"),
			intFieldSpec("global_proxy_id", "globalProxyId"),
			stringFieldSpec("proxy_type", "proxyType"),
			stringFieldSpec("proxy_hostname", "proxyHostname"),
			intFieldSpec("proxy_port", "proxyPort"),
			stringFieldSpec("proxy_username", "proxyUsername"),
			secretFieldSpec("proxy_password", "proxyPassword"),
			stringFieldSpec("proxy_bypass_filter", "proxyBypassFilter"),
			boolFieldSpec("proxy_bypass_local_addresses", "proxyBypassLocalAddresses"),
			stringFieldSpec("proxy_name", "proxyName"),
			stringFieldSpec("certificate_validation", "certificateValidation"),
			stringFieldSpec("backup_folder", "backupFolder"),
			intFieldSpec("backup_interval", "backupInterval"),
			intFieldSpec("backup_retention", "backupRetention"),
			boolFieldSpec("trust_cgnat_ip_addresses", "trustCgnatIpAddresses"),
			stringFieldSpec("oidc_authority", "oidcAuthority"),
			stringFieldSpec("oidc_client_id", "oidcClientId"),
			secretFieldSpec("oidc_client_secret", "oidcClientSecret"),
			stringFieldSpec("oidc_scopes", "oidcScopes"),
			stringFieldSpec("oidc_allowed_emails", "oidcAllowedEmails"),
			stringFieldSpec("oidc_allowed_email_domains", "oidcAllowedEmailDomains"),
			boolFieldSpec("oidc_require_email_verified", "oidcRequireEmailVerified"),
			boolFieldSpec("oidc_allow_any_verified_user", "oidcAllowAnyVerifiedUser"),
		},
	},
	{
		typeName: "ui_config",
		apiPath:  "/api/v1/config/ui",
		fields: []configField{
			intFieldSpec("first_day_of_week", "firstDayOfWeek"),
			stringFieldSpec("calendar_week_column_header", "calendarWeekColumnHeader"),
			stringFieldSpec("short_date_format", "shortDateFormat"),
			stringFieldSpec("long_date_format", "longDateFormat"),
			stringFieldSpec("time_format", "timeFormat"),
			boolFieldSpec("show_relative_dates", "showRelativeDates"),
			boolFieldSpec("enable_color_impaired_mode", "enableColorImpairedMode"),
			intFieldSpec("ui_language", "uiLanguage"),
			stringFieldSpec("theme", "theme"),
			stringFieldSpec("add_new_default_media_type", "addNewDefaultMediaType"),
		},
	},
	{
		typeName: "media_management_config",
		apiPath:  "/api/v1/config/mediamanagement",
		fields: []configField{
			boolFieldSpec("auto_unmonitor_previously_downloaded_books", "autoUnmonitorPreviouslyDownloadedBooks"),
			stringFieldSpec("recycle_bin", "recycleBin"),
			intFieldSpec("recycle_bin_cleanup_days", "recycleBinCleanupDays"),
			stringFieldSpec("download_propers_and_repacks", "downloadPropersAndRepacks"),
			boolFieldSpec("create_empty_author_folders", "createEmptyAuthorFolders"),
			boolFieldSpec("create_empty_ebook_author_folders", "createEmptyEbookAuthorFolders"),
			boolFieldSpec("delete_empty_folders", "deleteEmptyFolders"),
			stringFieldSpec("file_date", "fileDate"),
			boolFieldSpec("watch_library_for_changes", "watchLibraryForChanges"),
			boolFieldSpec("granular_file_system_scanning", "granularFileSystemScanning"),
			stringFieldSpec("rescan_after_refresh", "rescanAfterRefresh"),
			stringFieldSpec("allow_fingerprinting", "allowFingerprinting"),
			stringFieldSpec("book_matching_strictness", "bookMatchingStrictness"),
			boolFieldSpec("use_path_as_tags_fallback", "usePathAsTagsFallback"),
			boolFieldSpec("auto_add_missing_authors_from_completed_downloads", "autoAddMissingAuthorsFromCompletedDownloads"),
			stringFieldSpec("default_audiobook_root_folder_path", "defaultAudiobookRootFolderPath"),
			stringFieldSpec("default_ebook_root_folder_path", "defaultEbookRootFolderPath"),
			boolFieldSpec("set_permissions_linux", "setPermissionsLinux"),
			stringFieldSpec("chmod_folder", "chmodFolder"),
			stringFieldSpec("chown_group", "chownGroup"),
			boolFieldSpec("skip_free_space_check_when_importing", "skipFreeSpaceCheckWhenImporting"),
			intFieldSpec("minimum_free_space_when_importing", "minimumFreeSpaceWhenImporting"),
			boolFieldSpec("copy_using_hardlinks", "copyUsingHardlinks"),
			boolFieldSpec("import_extra_files", "importExtraFiles"),
			stringFieldSpec("extra_file_extensions", "extraFileExtensions"),
		},
	},
	{
		typeName: "naming_config",
		apiPath:  "/api/v1/config/naming",
		fields: []configField{
			boolFieldSpec("rename_books", "renameBooks"),
			boolFieldSpec("replace_illegal_characters", "replaceIllegalCharacters"),
			intFieldSpec("colon_replacement_format", "colonReplacementFormat"),
			stringFieldSpec("standard_book_format", "standardBookFormat"),
			stringFieldSpec("author_folder_format", "authorFolderFormat"),
			boolFieldSpec("ebook_rename_books", "ebookRenameBooks"),
			boolFieldSpec("ebook_replace_illegal_characters", "ebookReplaceIllegalCharacters"),
			intFieldSpec("ebook_colon_replacement_format", "ebookColonReplacementFormat"),
			stringFieldSpec("ebook_standard_book_format", "ebookStandardBookFormat"),
			stringFieldSpec("ebook_author_folder_format", "ebookAuthorFolderFormat"),
			boolFieldSpec("include_author_name", "includeAuthorName"),
			boolFieldSpec("include_book_title", "includeBookTitle"),
			boolFieldSpec("include_quality", "includeQuality"),
			boolFieldSpec("replace_spaces", "replaceSpaces"),
			stringFieldSpec("separator", "separator"),
			stringFieldSpec("number_style", "numberStyle"),
		},
	},
	{
		typeName: "conversion_config",
		apiPath:  "/api/v1/config/conversion",
		fields: []configField{
			intFieldSpec("audiobook_concurrent_conversions", "audiobookConversionConcurrentConversions"),
			intFieldSpec("audiobook_max_bitrate", "audiobookConversionMaxBitrate"),
			intFieldSpec("audiobook_max_cpu_threads", "audiobookConversionMaxCpuThreads"),
			boolFieldSpec("audiobook_no_upscale", "audiobookConversionNoUpscale"),
			stringFieldSpec("audiobook_audio_channels", "audiobookConversionAudioChannels"),
			stringFieldSpec("audiobook_tag_mode", "audiobookConversionTagMode"),
			boolFieldSpec("ebook_enabled", "ebookConversionEnabled"),
			stringFieldSpec("ebook_target_format", "ebookConversionTargetFormat"),
		},
	},
	{
		typeName: "metadata_provider_config",
		apiPath:  "/api/v1/config/metadataprovider",
		fields: []configField{
			stringFieldSpec("write_audio_tags", "writeAudioTags"),
			boolFieldSpec("scrub_audio_tags", "scrubAudioTags"),
			stringFieldSpec("write_book_tags", "writeBookTags"),
			boolFieldSpec("update_covers", "updateCovers"),
			boolFieldSpec("embed_metadata", "embedMetadata"),
		},
	},
	{
		typeName: "download_client_config",
		apiPath:  "/api/v1/config/downloadclient",
		fields: []configField{
			stringFieldSpec("working_folders", "downloadClientWorkingFolders"),
			boolFieldSpec("enable_completed_download_handling", "enableCompletedDownloadHandling"),
			boolFieldSpec("auto_redownload_failed", "autoRedownloadFailed"),
			boolFieldSpec("auto_redownload_failed_from_interactive_search", "autoRedownloadFailedFromInteractiveSearch"),
		},
	},
	{
		typeName: "indexer_config",
		apiPath:  "/api/v1/config/indexer",
		fields: []configField{
			intFieldSpec("minimum_age", "minimumAge"),
			intFieldSpec("maximum_size", "maximumSize"),
			intFieldSpec("retention", "retention"),
			intFieldSpec("rss_sync_interval", "rssSyncInterval"),
		},
	},
	{
		typeName: "development_config",
		apiPath:  "/api/v1/config/development",
		fields: []configField{
			stringFieldSpec("metadata_server_url", "metadataServerUrl"),
			stringFieldSpec("console_log_level", "consoleLogLevel"),
			boolFieldSpec("log_sql", "logSql"),
			intFieldSpec("log_rotate", "logRotate"),
			boolFieldSpec("filter_sentry_events", "filterSentryEvents"),
		},
	},
}
