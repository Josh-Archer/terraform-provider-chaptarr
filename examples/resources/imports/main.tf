variable "list_api_key" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "hardcover_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

data "chaptarr_import_list_schema" "current" {}

resource "chaptarr_import_list" "library" {
  name                     = "Managed reading list"
  implementation           = "ExampleImportList"
  config_contract          = "ExampleImportListSettings"
  enable                   = false
  test_on_apply            = false
  enable_automatic_add     = true
  should_monitor           = "entireAuthor"
  should_monitor_existing  = true
  should_search            = false
  root_folder_path         = "/library/books"
  monitor_new_items        = "new"
  quality_profile_id       = 1
  metadata_profile_id      = 1
  list_type                = "other"
  minimum_refresh_interval = "01:00:00"
  field_values_json        = jsonencode({ listId = "reading" })
  secret_fields            = { apiKey = var.list_api_key }
}

resource "chaptarr_import_list_exclusion" "example" {
  foreign_id  = "hardcover-author-id"
  author_name = "Example Author"
  media_type  = "all"
}

resource "chaptarr_hardcover_config" "current" {
  token                     = var.hardcover_token
  allow_external_validation = true
  observe_server            = false
}
