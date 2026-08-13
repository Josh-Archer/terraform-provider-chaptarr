data "chaptarr_author_lookup" "author" {
  term = "Example Author"
}

resource "chaptarr_author" "author" {
  foreign_author_id          = "hc:191785"
  monitored                  = true
  audiobook_monitor_existing = 1
  audiobook_monitor_future   = true
  ebook_monitor_existing     = 0
  ebook_monitor_future       = false

  audiobook_root_folder_path    = "/audiobooks"
  audiobook_quality_profile_id  = 2
  audiobook_metadata_profile_id = 3
  audiobook_tags                = [4]

  search_for_missing_books = false
  move_files_on_update     = false
  delete_files_on_destroy  = false
}

data "chaptarr_series_lookup" "series" {
  foreign_series_id = "hc:series-200"
  metadata_provider = "hardcover"
}

resource "chaptarr_series" "series" {
  foreign_series_id   = "hc:series-200"
  media_type          = "ebook"
  monitor_existing    = "select"
  monitor_future      = false
  root_folder_path    = "/ebooks"
  quality_profile_id  = 5
  metadata_profile_id = 6
  tags                = [4]

  selected_books = [{
    foreign_book_id   = "hc:book-201"
    foreign_author_id = chaptarr_author.author.foreign_author_id
  }]
}
