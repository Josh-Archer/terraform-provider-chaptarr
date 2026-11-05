data "chaptarr_api_info" "current" {}

data "chaptarr_ping" "liveness" {
  method = "GET"
}

data "chaptarr_health" "current" {}

data "chaptarr_system_status" "current" {}

data "chaptarr_system_statistics" "audiobooks" {
  media_type = "audiobook"
}

data "chaptarr_languages" "all" {}

data "chaptarr_library_search" "terraform" {
  term  = "Terraform"
  limit = 10
}

data "chaptarr_calendar" "next_month" {
  start          = "2026-08-01"
  end            = "2026-09-01"
  unmonitored    = false
  include_author = true
}

data "chaptarr_queue" "current" {
  page      = 1
  page_size = 20
}

data "chaptarr_queue_details" "current" {
  include_author = true
  include_book   = true
}

data "chaptarr_queue_status" "current" {}

data "chaptarr_backups" "current" {}

data "chaptarr_settings_backups" "current" {
  operation = "all"
}

data "chaptarr_history" "recent" {
  mode           = "paged"
  page           = 1
  page_size      = 20
  include_author = true
  include_book   = true
}

data "chaptarr_ignored" "current" {
  page      = 1
  page_size = 20
}

data "chaptarr_manual_import_candidates" "audiobooks" {
  filter_existing_files = true
  media_type            = "audiobook"
}

data "chaptarr_pending_author_import" "queue" {
  operation = "list"
}

data "chaptarr_cutoff_wanted" "audiobooks" {
  page           = 1
  page_size      = 20
  include_author = true
  monitored      = true
  media_type     = "audiobook"
}

data "chaptarr_missing" "audiobooks" {
  page           = 1
  page_size      = 20
  include_author = true
  monitored      = true
  media_type     = "audiobook"
}

check "chaptarr_ready" {
  assert {
    condition     = data.chaptarr_ping.liveness.http_status == 200
    error_message = "Chaptarr /ping did not report HTTP 200."
  }

  assert {
    condition     = data.chaptarr_api_info.current.current == "v1"
    error_message = "The module requires Chaptarr API v1."
  }

  assert {
    condition     = !data.chaptarr_health.current.has_errors
    error_message = "Chaptarr reports one or more health errors."
  }
}
