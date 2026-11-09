# Sanitized replay of home terraform/arr-config Chaptarr resources.
# See README.md for the differences required by a disposable instance.

provider "chaptarr" {
  url                     = var.chaptarr_url
  api_key                 = var.chaptarr_api_key
  request_timeout_seconds = 120
}

data "chaptarr_api_info" "current" {}

data "chaptarr_health" "current" {}

data "chaptarr_updates" "available" {}

data "chaptarr_database_status" "db" {}

data "chaptarr_system_status" "status" {}

data "chaptarr_root_folders" "all" {
  depends_on = [chaptarr_root_folder.library]
}

data "chaptarr_remote_path_mappings" "all" {}

data "chaptarr_quality_profile_schema" "audiobook" {
  media_type = "audiobook"
}

data "chaptarr_quality_profile_schema" "ebook" {
  media_type = "ebook"
}

data "chaptarr_metadata_profile_schema" "current" {}

resource "chaptarr_host_config" "main" {
  instance_name = "Home Chaptarr"
  branch        = "develop"
}

resource "chaptarr_naming_config" "main" {
  rename_books                     = true
  replace_illegal_characters       = true
  colon_replacement_format         = 4
  author_folder_format             = "{Author Name}"
  standard_book_format             = "{Book Title}{ - Narrator}/{Book Title}{ (PartNumber:smart)}"
  ebook_rename_books               = true
  ebook_replace_illegal_characters = true
  ebook_colon_replacement_format   = 4
  ebook_author_folder_format       = "{Author Name}"
  ebook_standard_book_format       = "{Book Title}/{Author Name} - {Book Title}{ (PartNumber)}"
}

resource "chaptarr_media_management_config" "main" {
  auto_unmonitor_previously_downloaded_books        = false
  recycle_bin                                       = ""
  recycle_bin_cleanup_days                          = 7
  download_propers_and_repacks                      = "preferAndUpgrade"
  create_empty_author_folders                       = false
  create_empty_ebook_author_folders                 = false
  delete_empty_folders                              = false
  file_date                                         = "none"
  watch_library_for_changes                         = true
  granular_file_system_scanning                     = true
  rescan_after_refresh                              = "always"
  allow_fingerprinting                              = "newFiles"
  book_matching_strictness                          = "balanced"
  use_path_as_tags_fallback                         = true
  auto_add_missing_authors_from_completed_downloads = false
  default_audiobook_root_folder_path                = ""
  default_ebook_root_folder_path                    = ""
  set_permissions_linux                             = false
  chmod_folder                                      = "755"
  chown_group                                       = ""
  skip_free_space_check_when_importing              = false
  minimum_free_space_when_importing                 = 100
  copy_using_hardlinks                              = true
  import_extra_files                                = false
  extra_file_extensions                             = "jpg,png,cue,m3u,opf"
}

resource "chaptarr_quality_profile" "ebook" {
  name                               = "Home E-Book"
  profile_type                       = "ebook"
  upgrade_allowed                    = true
  prefer_custom_formats_over_quality = false
  convert_to_quality_id              = data.chaptarr_quality_profile_schema.ebook.convert_to_quality_id
  cutoff                             = data.chaptarr_quality_profile_schema.ebook.cutoff
  minimum_format_score               = data.chaptarr_quality_profile_schema.ebook.minimum_format_score
  cutoff_format_score                = data.chaptarr_quality_profile_schema.ebook.cutoff_format_score

  items = [for item in data.chaptarr_quality_profile_schema.ebook.items : {
    id         = item.quality_id == null ? item.id : null
    name       = item.quality_id == null ? item.name : null
    quality_id = item.quality_id
    allowed    = item.allowed
    items = [for child in item.items : {
      quality_id = child.quality_id
      allowed    = child.allowed
    }]
  }]

  format_items = [for item in data.chaptarr_quality_profile_schema.ebook.format_items : {
    format_id = item.format_id
    score     = item.score
  }]
}

resource "chaptarr_quality_profile" "audiobook" {
  name                               = "Home Audiobook"
  profile_type                       = "audiobook"
  upgrade_allowed                    = true
  prefer_custom_formats_over_quality = false
  convert_to_quality_id              = data.chaptarr_quality_profile_schema.audiobook.convert_to_quality_id
  cutoff                             = data.chaptarr_quality_profile_schema.audiobook.cutoff
  minimum_format_score               = data.chaptarr_quality_profile_schema.audiobook.minimum_format_score
  cutoff_format_score                = data.chaptarr_quality_profile_schema.audiobook.cutoff_format_score

  items = [for item in data.chaptarr_quality_profile_schema.audiobook.items : {
    id         = item.quality_id == null ? item.id : null
    name       = item.quality_id == null ? item.name : null
    quality_id = item.quality_id
    allowed    = item.allowed
    items = [for child in item.items : {
      quality_id = child.quality_id
      allowed    = child.allowed
    }]
  }]

  format_items = [for item in data.chaptarr_quality_profile_schema.audiobook.format_items : {
    format_id = item.format_id
    score     = item.score
  }]
}

resource "chaptarr_metadata_profile" "audiobook_default" {
  name                = "Home Audiobook Default"
  profile_type        = "audiobook"
  minimum_popularity  = 0
  minimum_pages       = 0
  skip_missing_date   = false
  skip_missing_isbn   = false
  skip_parts_and_sets = false
}

resource "chaptarr_metadata_profile" "ebook_default" {
  name                = "Home Ebook Default"
  profile_type        = "ebook"
  minimum_popularity  = 350
  minimum_pages       = 0
  skip_missing_date   = true
  skip_missing_isbn   = true
  skip_parts_and_sets = true
  allowed_languages   = ["eng", "null"]

  lifecycle {
    precondition {
      condition = alltrue([
        for code in ["eng", "null"] : contains(
          concat(data.chaptarr_metadata_profile_schema.current.languages[*].code, data.chaptarr_metadata_profile_schema.current.special_languages[*].code),
          code,
        )
      ])
      error_message = "Every language must be advertised by the current Chaptarr metadata schema."
    }
  }
}

resource "chaptarr_metadata_profile" "none" {
  name               = "Home None"
  profile_type       = "general"
  minimum_popularity = 1000000
  minimum_pages      = 0
}

resource "chaptarr_root_folder" "library" {
  name                          = "Home Chaptarr Library"
  path                          = var.library_path
  folder_type                   = "mixed"
  audiobook_quality_profile_id  = tonumber(chaptarr_quality_profile.audiobook.id)
  audiobook_metadata_profile_id = tonumber(chaptarr_metadata_profile.audiobook_default.id)
  ebook_quality_profile_id      = tonumber(chaptarr_quality_profile.ebook.id)
  ebook_metadata_profile_id     = tonumber(chaptarr_metadata_profile.ebook_default.id)
  place_ebooks_with_audiobooks  = false
  allow_destroy                 = true
}

resource "chaptarr_download_client" "transmission" {
  name                       = "Transmission"
  enable                     = false
  test_on_apply              = false
  protocol                   = "torrent"
  priority                   = 1
  implementation             = "Transmission"
  config_contract            = "TransmissionSettings"
  remove_completed_downloads = true
  remove_failed_downloads    = true
  copy_unmanaged_downloads   = false
  audiobook_tags             = []
  ebook_tags                 = []

  # Full object Chaptarr 0.9.929 echoes, including null defaults. The released
  # provider stores that object, and apply requires it to match the plan.
  # jsonencode drops nulls, so this is a literal canonical string.
  field_values_json = "{\"addPaused\":false,\"audiobookCategory\":\"audiobooks\",\"ebookCategory\":\"ebooks\",\"host\":\"transmission.example.invalid\",\"musicCategory\":null,\"olderTvPriority\":0,\"port\":9091,\"recentTvPriority\":0,\"tvDirectory\":null,\"urlBase\":\"/transmission/\",\"useSsl\":false,\"username\":null}"

  lifecycle {
    ignore_changes = [field_values_json]
  }
}

resource "chaptarr_notification" "media_updates" {
  name            = "ntfy-media-updates"
  implementation  = "Ntfy"
  config_contract = "NtfySettings"
  enable          = false
  test_on_apply   = false
  # Every trigger stays false. Chaptarr treats any trigger as enable and
  # tests on create; enable=false and forceSave do not skip that test.
  on_grab                         = false
  on_release_import               = false
  on_upgrade                      = false
  on_rename                       = false
  on_author_added                 = false
  on_book_added                   = false
  on_author_delete                = false
  on_book_delete                  = false
  on_book_file_delete             = false
  on_book_file_delete_for_upgrade = false
  on_health_issue                 = false
  include_health_warnings         = true
  on_download_failure             = false
  on_import_failure               = false
  on_book_retag                   = false
  on_application_update           = false
  tags                            = []

  # Same canonical object 0.9.929 stores. Null defaults must remain in the
  # string; jsonencode would omit them and the released provider would disagree.
  field_values_json = "{\"clickUrl\":null,\"priority\":3,\"serverUrl\":\"${var.ntfy_server_url}\",\"tags\":[],\"topics\":[\"${var.ntfy_topic_media}\"],\"userName\":null}"

  lifecycle {
    ignore_changes = [field_values_json]
  }
}

output "chaptarr_database_status" {
  value = {
    type        = data.chaptarr_database_status.db.database_type
    version     = data.chaptarr_database_status.db.database_version
    is_postgres = data.chaptarr_database_status.db.is_postgres
    is_healthy  = data.chaptarr_database_status.db.is_healthy
  }
}

output "chaptarr_system_status" {
  value = {
    version  = data.chaptarr_system_status.status.version
    app_name = data.chaptarr_system_status.status.app_name
    branch   = data.chaptarr_system_status.status.branch
  }
}

output "chaptarr_api_compatibility" {
  value = {
    current    = data.chaptarr_api_info.current.current
    deprecated = data.chaptarr_api_info.current.deprecated
  }
}

output "chaptarr_health_summary" {
  value = {
    has_errors    = data.chaptarr_health.current.has_errors
    has_warnings  = data.chaptarr_health.current.has_warnings
    error_count   = data.chaptarr_health.current.error_count
    warning_count = data.chaptarr_health.current.warning_count
  }
}

output "chaptarr_update_metadata" {
  value = {
    result_fingerprint = sha256(data.chaptarr_updates.available.result_json)
    update_count       = try(length(jsondecode(data.chaptarr_updates.available.result_json)), 0)
  }
}
