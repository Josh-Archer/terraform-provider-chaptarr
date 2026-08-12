data "chaptarr_quality_profile_schema" "audiobook" {
  media_type = "audiobook"
}

data "chaptarr_quality_profile_schema" "ebook" {
  media_type = "ebook"
}

data "chaptarr_metadata_profile_schema" "current" {}

resource "chaptarr_quality_profile" "audiobook" {
  name                               = "Managed Audiobooks"
  profile_type                       = "audiobook"
  upgrade_allowed                    = true
  prefer_custom_formats_over_quality = true
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

resource "chaptarr_quality_profile" "ebook" {
  name                               = "Managed Ebooks"
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

resource "chaptarr_metadata_profile" "english_ebooks" {
  name              = "English ebooks"
  profile_type      = "ebook"
  allowed_languages = ["eng", "null"]
  ignored_terms     = ["abridged"]

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

resource "chaptarr_release_profile" "preferred" {
  enabled        = true
  required_terms = ["unabridged"]
  ignored_terms  = ["sample", "preview"]
  indexer_id     = 0
  tags           = []
}

# Replace tag IDs with tags already configured in Chaptarr.
resource "chaptarr_delay_profile" "tagged" {
  enable_usenet                       = true
  enable_torrent                      = true
  preferred_protocol                  = "usenet"
  usenet_delay_minutes                = 0
  torrent_delay_minutes               = 30
  bypass_if_highest_quality           = true
  bypass_if_above_custom_format_score = false
  minimum_custom_format_score         = 0
  tags                                = [1]
}

# quality_id adopts an existing built-in definition rather than creating one.
resource "chaptarr_quality_definition" "m4b" {
  quality_id   = 12
  minimum_size = 0
  maximum_size = 500
}
