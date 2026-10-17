data "chaptarr_api_info" "current" {}

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

check "chaptarr_ready" {
  assert {
    condition     = data.chaptarr_api_info.current.current == "v1"
    error_message = "The module requires Chaptarr API v1."
  }

  assert {
    condition     = !data.chaptarr_health.current.has_errors
    error_message = "Chaptarr reports one or more health errors."
  }
}
