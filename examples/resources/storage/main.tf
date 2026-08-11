resource "chaptarr_root_folder" "audiobooks" {
  name        = "Audiobooks"
  path        = "/library/audiobooks"
  folder_type = "audiobook"

  audiobook_quality_profile_id  = 1
  audiobook_metadata_profile_id = 1
  audiobook_monitor_existing    = 1
  audiobook_monitor_future      = true

  # Set true and apply before intentionally removing this registration.
  allow_destroy = false
}

resource "chaptarr_remote_path_mapping" "downloads" {
  download_client_id = 1
  remote_path        = "/downloads/books"
  local_path         = "/mnt/downloads/books"

  # Opt-in network/filesystem probe, executed only during create/update apply.
  test_before_apply = false
}

data "chaptarr_root_folders" "audiobook_compatible" {
  media_type = "audiobook"
}

data "chaptarr_remote_path_mappings" "all" {}
