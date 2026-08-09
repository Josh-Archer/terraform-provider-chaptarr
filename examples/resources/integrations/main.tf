variable "mam_id" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "transmission_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "ntfy_access_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

data "chaptarr_indexer_schema" "current" {}
data "chaptarr_download_client_schema" "current" {}
data "chaptarr_notification_schema" "current" {}
data "chaptarr_indexer_flags" "current" {}

resource "chaptarr_indexer" "mam" {
  name                      = "My AnonaMouse"
  implementation            = "MyAnonaMouse"
  config_contract           = "MyAnonaMouseSettings"
  enable                    = true
  test_on_apply             = true
  enable_rss                = true
  enable_automatic_search   = true
  enable_interactive_search = true
  priority                  = 25

  field_values_json = jsonencode({
    baseUrl                       = "https://www.myanonamouse.net"
    mamSsl                        = true
    useFreeleechWedge             = 0
    useFreeleechOnlyForAudiobooks = true
    minimumSeeders                = 1
    enableDeepSearch              = true
    integrateWithAbs              = false
    protectUnsatisfiedSlots       = true
    unsatisfiedSlotReserve        = 5
    manualGrabBuffer              = 0
    neverMoveOnImport             = false
  })
  secret_fields = { mamId = var.mam_id }
}

resource "chaptarr_download_client" "transmission" {
  name                       = "Transmission"
  implementation             = "Transmission"
  config_contract            = "TransmissionSettings"
  enable                     = true
  test_on_apply              = true
  protocol                   = "torrent"
  priority                   = 1
  audiobook_tags             = []
  ebook_tags                 = []
  remove_completed_downloads = true
  remove_failed_downloads    = true
  copy_unmanaged_downloads   = false

  field_values_json = jsonencode({
    host              = "transmission.example.test"
    port              = 9091
    useSsl            = true
    urlBase           = "/transmission/"
    username          = "chaptarr"
    audiobookCategory = "chaptarr-audio"
    ebookCategory     = "chaptarr-ebook"
    addPaused         = false
  })
  secret_fields = { password = var.transmission_password }
}

resource "chaptarr_notification" "ntfy" {
  name                            = "ntfy"
  implementation                  = "Ntfy"
  config_contract                 = "NtfySettings"
  enable                          = true
  test_on_apply                   = true
  on_grab                         = true
  on_release_import               = true
  on_upgrade                      = true
  on_rename                       = false
  on_author_added                 = false
  on_book_added                   = false
  on_author_delete                = false
  on_book_delete                  = false
  on_book_file_delete             = false
  on_book_file_delete_for_upgrade = false
  on_health_issue                 = true
  include_health_warnings         = true
  on_download_failure             = true
  on_import_failure               = true
  on_book_retag                   = false
  on_application_update           = false

  field_values_json = jsonencode({
    serverUrl = "https://ntfy.example.test"
    priority  = 3
    topics    = ["chaptarr"]
    tags      = ["books"]
  })
  secret_fields = { accessToken = var.ntfy_access_token }
}
