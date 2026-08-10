terraform {
  required_version = ">= 1.11.2"

  required_providers {
    chaptarr = {
      source = "josh-archer/chaptarr"
    }
  }
}

provider "chaptarr" {
  url = "https://chaptarr.example.test"
}

resource "chaptarr_ui_config" "this" {
  theme                      = "auto"
  add_new_default_media_type = "audiobook"
}

resource "chaptarr_media_management_config" "this" {
  default_audiobook_root_folder_path = "/library/audiobooks"
  default_ebook_root_folder_path     = "/library/ebooks"
  copy_using_hardlinks               = true
}

resource "chaptarr_naming_config" "this" {
  rename_books               = true
  standard_book_format       = "{Book Title}"
  ebook_rename_books         = true
  ebook_standard_book_format = "{Book Title}"
}

resource "chaptarr_conversion_config" "this" {
  audiobook_concurrent_conversions = 2
  audiobook_max_cpu_threads        = 2
  audiobook_no_upscale             = true
  ebook_enabled                    = true
  ebook_target_format              = "epub"
}

data "chaptarr_naming_pattern" "validate" {
  operation = "validate"
  pattern   = "{Author Name}/{Book Title}"
}
