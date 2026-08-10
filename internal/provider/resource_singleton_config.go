package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &singletonConfigResource{}
	_ resource.ResourceWithImportState = &singletonConfigResource{}
)

type configFieldKind uint8

const (
	configString configFieldKind = iota
	configBool
	configInt64
)

type configField struct {
	terraformName string
	apiName       string
	kind          configFieldKind
	description   string
	sensitive     bool
	writeOnly     bool
}

type singletonConfigDefinition struct {
	typeName      string
	apiPath       string
	description   string
	fields        []configField
	dropAPIFields []string
}

type singletonConfigResource struct {
	client     *client.Client
	definition singletonConfigDefinition
}

func newSingletonConfigResource(definition singletonConfigDefinition) func() resource.Resource {
	return func() resource.Resource {
		return &singletonConfigResource{definition: definition}
	}
}

func stringConfigField(terraformName, apiName, description string) configField {
	return configField{terraformName: terraformName, apiName: apiName, kind: configString, description: description}
}

func boolConfigField(terraformName, apiName, description string) configField {
	return configField{terraformName: terraformName, apiName: apiName, kind: configBool, description: description}
}

func intConfigField(terraformName, apiName, description string) configField {
	return configField{terraformName: terraformName, apiName: apiName, kind: configInt64, description: description}
}

func writeOnlyStringConfigField(terraformName, apiName, description string) configField {
	return configField{
		terraformName: terraformName,
		apiName:       apiName,
		kind:          configString,
		description:   description,
		sensitive:     true,
		writeOnly:     true,
	}
}

func (r *singletonConfigResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.definition.typeName
}

func (r *singletonConfigResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: "Chaptarr's stable numeric identifier for this singleton configuration object.",
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
	}

	for _, field := range r.definition.fields {
		switch field.kind {
		case configString:
			attributes[field.terraformName] = schema.StringAttribute{
				MarkdownDescription: field.description,
				Optional:            true,
				Computed:            !field.writeOnly,
				Sensitive:           field.sensitive,
				WriteOnly:           field.writeOnly,
			}
		case configBool:
			attributes[field.terraformName] = schema.BoolAttribute{
				MarkdownDescription: field.description,
				Optional:            true,
				Computed:            !field.writeOnly,
				Sensitive:           field.sensitive,
				WriteOnly:           field.writeOnly,
			}
		case configInt64:
			attributes[field.terraformName] = schema.Int64Attribute{
				MarkdownDescription: field.description,
				Optional:            true,
				Computed:            !field.writeOnly,
				Sensitive:           field.sensitive,
				WriteOnly:           field.writeOnly,
			}
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: r.definition.description + " Destroy removes only Terraform ownership; it never resets the Chaptarr singleton.",
		Attributes:          attributes,
	}
}

func (r *singletonConfigResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	r.client = apiClient
}

func (r *singletonConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics, "create")
}

func (r *singletonConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var stateID types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &stateID)...)
	if resp.Diagnostics.HasError() {
		return
	}
	identifier := ""
	if !stateID.IsNull() && !stateID.IsUnknown() {
		if parsed, err := strconv.ParseInt(stateID.ValueString(), 10, 64); err == nil && parsed >= 0 {
			identifier = strconv.FormatInt(parsed, 10)
		}
	}

	current, err := r.readCurrent(ctx, identifier)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read Chaptarr configuration", err.Error())
		return
	}

	r.setState(ctx, &resp.State, current, &resp.Diagnostics)
}

func (r *singletonConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics, "update")
}

func (r *singletonConfigResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
	// Singleton configuration has no safe reset/delete operation. Terraform
	// destroy deliberately relinquishes ownership without mutating Chaptarr.
}

func (r *singletonConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import identifier", "Use the singleton name or its numeric Chaptarr identifier.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *singletonConfigResource) write(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics, operation string) {
	current, err := r.readCurrent(ctx, "")
	if err != nil {
		diagnostics.AddError("Unable to prepare Chaptarr configuration "+operation, err.Error())
		return
	}

	identifier, ok := numericIdentifier(current["id"])
	if !ok {
		diagnostics.AddError("Invalid Chaptarr configuration response", "The singleton response omitted its numeric identifier; no update was attempted.")
		return
	}

	payload := make(map[string]any, len(current))
	for key, value := range current {
		payload[key] = value
	}
	// Never echo credentials returned by the API. A write-only field is sent
	// only when the current plan explicitly supplies it.
	for _, field := range r.definition.fields {
		if field.writeOnly {
			delete(payload, field.apiName)
		}
	}
	for _, apiField := range r.definition.dropAPIFields {
		delete(payload, apiField)
	}

	for _, field := range r.definition.fields {
		value, configured := plannedConfigValue(ctx, plan, field, diagnostics)
		if diagnostics.HasError() {
			return
		}
		if configured {
			payload[field.apiName] = value
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		diagnostics.AddError("Unable to encode Chaptarr configuration", "The configuration payload could not be encoded.")
		return
	}
	if _, err := r.client.Do(ctx, http.MethodPut, r.definition.apiPath+"/"+identifier, body); err != nil {
		diagnostics.AddError("Unable to "+operation+" Chaptarr configuration", err.Error())
		return
	}

	refreshed, err := r.readCurrent(ctx, identifier)
	if err != nil {
		diagnostics.AddError("Unable to refresh Chaptarr configuration", err.Error())
		return
	}
	r.setState(ctx, state, refreshed, diagnostics)
}

func plannedConfigValue(ctx context.Context, plan tfsdk.Plan, field configField, diagnostics *diag.Diagnostics) (any, bool) {
	switch field.kind {
	case configString:
		var value types.String
		diagnostics.Append(plan.GetAttribute(ctx, path.Root(field.terraformName), &value)...)
		if value.IsNull() || value.IsUnknown() {
			return nil, false
		}
		return value.ValueString(), true
	case configBool:
		var value types.Bool
		diagnostics.Append(plan.GetAttribute(ctx, path.Root(field.terraformName), &value)...)
		if value.IsNull() || value.IsUnknown() {
			return nil, false
		}
		return value.ValueBool(), true
	case configInt64:
		var value types.Int64
		diagnostics.Append(plan.GetAttribute(ctx, path.Root(field.terraformName), &value)...)
		if value.IsNull() || value.IsUnknown() {
			return nil, false
		}
		return value.ValueInt64(), true
	default:
		diagnostics.AddError("Unsupported configuration field", "The provider encountered an unsupported internal field type.")
		return nil, false
	}
}

func (r *singletonConfigResource) readCurrent(ctx context.Context, identifier string) (map[string]any, error) {
	requestPath := r.definition.apiPath
	if identifier != "" {
		requestPath += "/" + identifier
	}
	response, err := r.client.Do(ctx, http.MethodGet, requestPath, nil)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(response.Body))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, errors.New("chaptarr returned an invalid configuration document")
	}

	switch value := decoded.(type) {
	case map[string]any:
		return value, nil
	case []any:
		if len(value) != 1 {
			return nil, fmt.Errorf("chaptarr returned %d singleton configuration objects", len(value))
		}
		object, ok := value[0].(map[string]any)
		if !ok {
			return nil, errors.New("chaptarr returned an invalid singleton configuration object")
		}
		return object, nil
	default:
		return nil, errors.New("chaptarr returned an invalid singleton configuration document")
	}
}

func (r *singletonConfigResource) setState(ctx context.Context, state *tfsdk.State, current map[string]any, diagnostics *diag.Diagnostics) {
	identifier, ok := numericIdentifier(current["id"])
	if !ok {
		diagnostics.AddError("Invalid Chaptarr configuration response", "The singleton response omitted its numeric identifier.")
		return
	}
	diagnostics.Append(state.SetAttribute(ctx, path.Root("id"), identifier)...)

	for _, field := range r.definition.fields {
		if field.writeOnly {
			continue
		}

		raw, exists := current[field.apiName]
		if !exists || raw == nil {
			switch field.kind {
			case configString:
				diagnostics.Append(state.SetAttribute(ctx, path.Root(field.terraformName), types.StringNull())...)
			case configBool:
				diagnostics.Append(state.SetAttribute(ctx, path.Root(field.terraformName), types.BoolNull())...)
			case configInt64:
				diagnostics.Append(state.SetAttribute(ctx, path.Root(field.terraformName), types.Int64Null())...)
			}
			continue
		}

		switch field.kind {
		case configString:
			value, ok := raw.(string)
			if !ok {
				diagnostics.AddError("Invalid Chaptarr configuration response", fmt.Sprintf("Field %q was not a string.", field.apiName))
				continue
			}
			diagnostics.Append(state.SetAttribute(ctx, path.Root(field.terraformName), value)...)
		case configBool:
			value, ok := raw.(bool)
			if !ok {
				diagnostics.AddError("Invalid Chaptarr configuration response", fmt.Sprintf("Field %q was not a boolean.", field.apiName))
				continue
			}
			diagnostics.Append(state.SetAttribute(ctx, path.Root(field.terraformName), value)...)
		case configInt64:
			value, ok := integerValue(raw)
			if !ok {
				diagnostics.AddError("Invalid Chaptarr configuration response", fmt.Sprintf("Field %q was not an integer.", field.apiName))
				continue
			}
			diagnostics.Append(state.SetAttribute(ctx, path.Root(field.terraformName), value)...)
		}
	}
}

func numericIdentifier(value any) (string, bool) {
	integer, ok := integerValue(value)
	if !ok || integer < 0 {
		return "", false
	}
	return strconv.FormatInt(integer, 10), true
}

func integerValue(value any) (int64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil
	case float64:
		parsed := int64(number)
		return parsed, float64(parsed) == number
	case int:
		return int64(number), true
	case int64:
		return number, true
	default:
		return 0, false
	}
}

var singletonConfigDefinitions = []singletonConfigDefinition{
	{
		typeName:      "host_config",
		apiPath:       "/api/v1/config/host",
		description:   "Manage Chaptarr host, authentication, proxy, update, backup, and OIDC configuration.",
		dropAPIFields: []string{"apiKey"},
		fields: []configField{
			stringConfigField("bind_address", "bindAddress", "Address on which Chaptarr listens."),
			intConfigField("port", "port", "HTTP listening port."),
			intConfigField("ssl_port", "sslPort", "HTTPS listening port."),
			boolConfigField("enable_ssl", "enableSsl", "Whether HTTPS is enabled."),
			boolConfigField("launch_browser", "launchBrowser", "Whether Chaptarr launches a browser on startup."),
			stringConfigField("authentication_method", "authenticationMethod", "Authentication method: none, basic, forms, external, oidc, or plex."),
			stringConfigField("authentication_required", "authenticationRequired", "Whether authentication is required for all or only non-local addresses."),
			stringConfigField("username", "username", "Local authentication username."),
			writeOnlyStringConfigField("password", "password", "Local authentication password. This value is write-only and never stored in state."),
			writeOnlyStringConfigField("password_confirmation", "passwordConfirmation", "Local authentication password confirmation. This value is write-only and never stored in state."),
			stringConfigField("log_level", "logLevel", "File log level."),
			stringConfigField("console_log_level", "consoleLogLevel", "Console log level."),
			stringConfigField("branch", "branch", "Chaptarr update branch."),
			stringConfigField("ssl_cert_path", "sslCertPath", "TLS certificate path."),
			writeOnlyStringConfigField("ssl_cert_password", "sslCertPassword", "TLS certificate password. This value is write-only and never stored in state."),
			stringConfigField("url_base", "urlBase", "Reverse-proxy URL base."),
			stringConfigField("instance_name", "instanceName", "Displayed Chaptarr instance name."),
			stringConfigField("application_url", "applicationUrl", "Externally reachable application URL."),
			boolConfigField("update_automatically", "updateAutomatically", "Whether Chaptarr installs updates automatically."),
			stringConfigField("update_mechanism", "updateMechanism", "Update mechanism."),
			stringConfigField("update_script_path", "updateScriptPath", "External update script path."),
			stringConfigField("proxy_mode", "proxyMode", "Proxy routing mode."),
			intConfigField("global_proxy_id", "globalProxyId", "Optional global proxy identifier."),
			stringConfigField("proxy_type", "proxyType", "Proxy protocol."),
			stringConfigField("proxy_hostname", "proxyHostname", "Proxy host name."),
			intConfigField("proxy_port", "proxyPort", "Proxy port."),
			stringConfigField("proxy_username", "proxyUsername", "Proxy username."),
			writeOnlyStringConfigField("proxy_password", "proxyPassword", "Proxy password. This value is write-only and never stored in state."),
			stringConfigField("proxy_bypass_filter", "proxyBypassFilter", "Comma-separated proxy bypass filter."),
			boolConfigField("proxy_bypass_local_addresses", "proxyBypassLocalAddresses", "Whether local addresses bypass the proxy."),
			stringConfigField("proxy_name", "proxyName", "Displayed proxy name."),
			stringConfigField("certificate_validation", "certificateValidation", "Remote certificate validation mode."),
			stringConfigField("backup_folder", "backupFolder", "Backup folder path."),
			intConfigField("backup_interval", "backupInterval", "Backup interval."),
			intConfigField("backup_retention", "backupRetention", "Backup retention period."),
			boolConfigField("trust_cgnat_ip_addresses", "trustCgnatIpAddresses", "Whether CGNAT addresses are treated as trusted local addresses."),
			stringConfigField("oidc_authority", "oidcAuthority", "OIDC authority URL."),
			stringConfigField("oidc_client_id", "oidcClientId", "OIDC client identifier."),
			writeOnlyStringConfigField("oidc_client_secret", "oidcClientSecret", "OIDC client secret. This value is write-only and never stored in state."),
			stringConfigField("oidc_scopes", "oidcScopes", "OIDC scopes."),
			stringConfigField("oidc_allowed_emails", "oidcAllowedEmails", "OIDC allow-listed email addresses."),
			stringConfigField("oidc_allowed_email_domains", "oidcAllowedEmailDomains", "OIDC allow-listed email domains."),
			boolConfigField("oidc_require_email_verified", "oidcRequireEmailVerified", "Whether OIDC requires a verified email."),
			boolConfigField("oidc_allow_any_verified_user", "oidcAllowAnyVerifiedUser", "Whether any verified OIDC user is allowed."),
		},
	},
	{
		typeName:    "ui_config",
		apiPath:     "/api/v1/config/ui",
		description: "Manage Chaptarr user-interface defaults.",
		fields: []configField{
			intConfigField("first_day_of_week", "firstDayOfWeek", "First day of the calendar week."),
			stringConfigField("calendar_week_column_header", "calendarWeekColumnHeader", "Calendar week-column header format."),
			stringConfigField("short_date_format", "shortDateFormat", "Short date format."),
			stringConfigField("long_date_format", "longDateFormat", "Long date format."),
			stringConfigField("time_format", "timeFormat", "Time format."),
			boolConfigField("show_relative_dates", "showRelativeDates", "Whether relative dates are displayed."),
			boolConfigField("enable_color_impaired_mode", "enableColorImpairedMode", "Whether color-impaired mode is enabled."),
			intConfigField("ui_language", "uiLanguage", "UI language identifier."),
			stringConfigField("theme", "theme", "UI theme."),
			stringConfigField("add_new_default_media_type", "addNewDefaultMediaType", "Default media type when adding library items."),
		},
	},
	{
		typeName:    "media_management_config",
		apiPath:     "/api/v1/config/mediamanagement",
		description: "Manage Chaptarr audiobook and ebook media-management behavior.",
		fields: []configField{
			boolConfigField("auto_unmonitor_previously_downloaded_books", "autoUnmonitorPreviouslyDownloadedBooks", "Automatically unmonitor previously downloaded books."),
			stringConfigField("recycle_bin", "recycleBin", "Recycle-bin path."),
			intConfigField("recycle_bin_cleanup_days", "recycleBinCleanupDays", "Recycle-bin cleanup age in days."),
			stringConfigField("download_propers_and_repacks", "downloadPropersAndRepacks", "Proper and repack preference."),
			boolConfigField("create_empty_author_folders", "createEmptyAuthorFolders", "Create empty audiobook author folders."),
			boolConfigField("create_empty_ebook_author_folders", "createEmptyEbookAuthorFolders", "Create empty ebook author folders."),
			boolConfigField("delete_empty_folders", "deleteEmptyFolders", "Delete empty folders."),
			stringConfigField("file_date", "fileDate", "File date behavior."),
			boolConfigField("watch_library_for_changes", "watchLibraryForChanges", "Watch the library filesystem for changes."),
			boolConfigField("granular_file_system_scanning", "granularFileSystemScanning", "Use granular filesystem scanning."),
			stringConfigField("rescan_after_refresh", "rescanAfterRefresh", "Rescan behavior after refresh."),
			stringConfigField("allow_fingerprinting", "allowFingerprinting", "Fingerprinting policy."),
			stringConfigField("book_matching_strictness", "bookMatchingStrictness", "Book matching strictness."),
			boolConfigField("use_path_as_tags_fallback", "usePathAsTagsFallback", "Use path components as fallback tags."),
			boolConfigField("auto_add_missing_authors_from_completed_downloads", "autoAddMissingAuthorsFromCompletedDownloads", "Automatically add missing authors from completed downloads."),
			stringConfigField("default_audiobook_root_folder_path", "defaultAudiobookRootFolderPath", "Default audiobook root folder."),
			stringConfigField("default_ebook_root_folder_path", "defaultEbookRootFolderPath", "Default ebook root folder."),
			boolConfigField("set_permissions_linux", "setPermissionsLinux", "Set Linux permissions after import."),
			stringConfigField("chmod_folder", "chmodFolder", "Linux folder mode."),
			stringConfigField("chown_group", "chownGroup", "Linux group ownership."),
			boolConfigField("skip_free_space_check_when_importing", "skipFreeSpaceCheckWhenImporting", "Skip free-space checks during import."),
			intConfigField("minimum_free_space_when_importing", "minimumFreeSpaceWhenImporting", "Minimum free space required for import."),
			boolConfigField("copy_using_hardlinks", "copyUsingHardlinks", "Use hardlinks when copying."),
			boolConfigField("import_extra_files", "importExtraFiles", "Import additional file extensions."),
			stringConfigField("extra_file_extensions", "extraFileExtensions", "Additional imported file extensions."),
		},
	},
	{
		typeName:    "naming_config",
		apiPath:     "/api/v1/config/naming",
		description: "Manage deterministic audiobook and ebook naming patterns.",
		fields: []configField{
			boolConfigField("rename_books", "renameBooks", "Rename audiobook files."),
			boolConfigField("replace_illegal_characters", "replaceIllegalCharacters", "Replace illegal audiobook filename characters."),
			intConfigField("colon_replacement_format", "colonReplacementFormat", "Audiobook colon replacement format."),
			stringConfigField("standard_book_format", "standardBookFormat", "Audiobook filename pattern."),
			stringConfigField("author_folder_format", "authorFolderFormat", "Audiobook author-folder pattern."),
			boolConfigField("ebook_rename_books", "ebookRenameBooks", "Rename ebook files."),
			boolConfigField("ebook_replace_illegal_characters", "ebookReplaceIllegalCharacters", "Replace illegal ebook filename characters."),
			intConfigField("ebook_colon_replacement_format", "ebookColonReplacementFormat", "Ebook colon replacement format."),
			stringConfigField("ebook_standard_book_format", "ebookStandardBookFormat", "Ebook filename pattern."),
			stringConfigField("ebook_author_folder_format", "ebookAuthorFolderFormat", "Ebook author-folder pattern."),
			boolConfigField("include_author_name", "includeAuthorName", "Include author name in legacy naming."),
			boolConfigField("include_book_title", "includeBookTitle", "Include book title in legacy naming."),
			boolConfigField("include_quality", "includeQuality", "Include quality in legacy naming."),
			boolConfigField("replace_spaces", "replaceSpaces", "Replace spaces in legacy naming."),
			stringConfigField("separator", "separator", "Legacy naming separator."),
			stringConfigField("number_style", "numberStyle", "Legacy number style."),
		},
	},
	{
		typeName:    "conversion_config",
		apiPath:     "/api/v1/config/conversion",
		description: "Manage Chaptarr audiobook and ebook conversion settings.",
		fields: []configField{
			intConfigField("audiobook_concurrent_conversions", "audiobookConversionConcurrentConversions", "Maximum concurrent audiobook conversions."),
			intConfigField("audiobook_max_bitrate", "audiobookConversionMaxBitrate", "Maximum audiobook bitrate."),
			intConfigField("audiobook_max_cpu_threads", "audiobookConversionMaxCpuThreads", "Maximum CPU threads per audiobook conversion."),
			boolConfigField("audiobook_no_upscale", "audiobookConversionNoUpscale", "Prevent audiobook bitrate upscaling."),
			stringConfigField("audiobook_audio_channels", "audiobookConversionAudioChannels", "Audiobook output channel configuration."),
			stringConfigField("audiobook_tag_mode", "audiobookConversionTagMode", "Audiobook conversion tag mode."),
			boolConfigField("ebook_enabled", "ebookConversionEnabled", "Enable ebook conversion."),
			stringConfigField("ebook_target_format", "ebookConversionTargetFormat", "Ebook conversion target format."),
		},
	},
	{
		typeName:    "metadata_provider_config",
		apiPath:     "/api/v1/config/metadataprovider",
		description: "Manage Chaptarr metadata-writing behavior.",
		fields: []configField{
			stringConfigField("write_audio_tags", "writeAudioTags", "Audio tag writing mode."),
			boolConfigField("scrub_audio_tags", "scrubAudioTags", "Scrub existing audio tags before writing."),
			stringConfigField("write_book_tags", "writeBookTags", "Book tag writing mode."),
			boolConfigField("update_covers", "updateCovers", "Update embedded covers."),
			boolConfigField("embed_metadata", "embedMetadata", "Embed metadata in supported files."),
		},
	},
	{
		typeName:    "download_client_config",
		apiPath:     "/api/v1/config/downloadclient",
		description: "Manage global completed-download handling settings.",
		fields: []configField{
			stringConfigField("working_folders", "downloadClientWorkingFolders", "Download-client working folders."),
			boolConfigField("enable_completed_download_handling", "enableCompletedDownloadHandling", "Enable completed-download handling."),
			boolConfigField("auto_redownload_failed", "autoRedownloadFailed", "Automatically redownload failed releases."),
			boolConfigField("auto_redownload_failed_from_interactive_search", "autoRedownloadFailedFromInteractiveSearch", "Automatically redownload interactive-search failures."),
		},
	},
	{
		typeName:    "indexer_config",
		apiPath:     "/api/v1/config/indexer",
		description: "Manage global indexer limits and RSS synchronization.",
		fields: []configField{
			intConfigField("minimum_age", "minimumAge", "Minimum release age."),
			intConfigField("maximum_size", "maximumSize", "Maximum release size."),
			intConfigField("retention", "retention", "Maximum release retention."),
			intConfigField("rss_sync_interval", "rssSyncInterval", "RSS synchronization interval."),
		},
	},
	{
		typeName:    "development_config",
		apiPath:     "/api/v1/config/development",
		description: "Manage advanced Chaptarr development diagnostics without invoking its connectivity-test action.",
		fields: []configField{
			stringConfigField("metadata_server_url", "metadataServerUrl", "Metadata server URL."),
			stringConfigField("console_log_level", "consoleLogLevel", "Console log level."),
			boolConfigField("log_sql", "logSql", "Enable SQL logging."),
			intConfigField("log_rotate", "logRotate", "Log rotation count."),
			boolConfigField("filter_sentry_events", "filterSentryEvents", "Filter Sentry events."),
		},
	},
}
