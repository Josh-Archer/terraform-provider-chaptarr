data "chaptarr_book_lookup" "book" {
  term       = "Example Book"
  media_type = "audiobook"
}

# Select one candidate object from the lookup result. jsondecode is shown to
# make the choice explicit rather than passing an ambiguous result array.
locals {
  book_candidate = jsondecode(data.chaptarr_book_lookup.book.result_json)[0]
}

resource "chaptarr_book" "book" {
  lookup_json          = jsonencode(local.book_candidate)
  foreign_book_id      = local.book_candidate.foreignBookId
  author_id            = 7
  media_type           = "audiobook"
  monitored            = true
  any_edition_ok       = false
  monitored_edition_id = local.book_candidate.editions[0].foreignEditionId

  search_for_new_book     = false
  delete_files_on_destroy = false
}

data "chaptarr_editions" "book" {
  book_id = tonumber(chaptarr_book.book.id)
}

data "chaptarr_book_file" "book" {
  book_id    = tonumber(chaptarr_book.book.id)
  media_type = "audiobook"
}

data "chaptarr_rename_book_preview" "book" {
  book_id = tonumber(chaptarr_book.book.id)
}

data "chaptarr_retag_book_preview" "book" {
  book_id = tonumber(chaptarr_book.book.id)
}
