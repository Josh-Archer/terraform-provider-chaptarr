# Read-only discovery and observability

These data sources call only Chaptarr `GET` endpoints. Terraform refreshes them
during normal reads, so values such as health, free space, scheduled-task
status, search results, and update availability can change without a
configuration change. They are observations rather than managed resources;
destroying their Terraform state never changes Chaptarr.

## Capability and check inputs

- `chaptarr_api_info` exposes `current` and `deprecated` API versions for
  module preconditions.
- `chaptarr_health` exposes only aggregate warning/error flags and counts. It
  intentionally omits health messages, sources, logs, and internal details.
- `chaptarr_system_status` exposes a fixed capability whitelist: application,
  version, branch, database type, authentication mode, process mode, OS name,
  and runtime version. Installation identifiers, application paths, and URL
  base values are not stored.
- `chaptarr_system_statistics` exposes aggregate counts and accepts an optional
  `media_type` of `all`, `audiobook`, or `ebook`.

Example check:

```hcl
data "chaptarr_api_info" "current" {}
data "chaptarr_health" "current" {}

check "supported_chaptarr" {
  assert {
    condition     = data.chaptarr_api_info.current.current == "v1"
    error_message = "This module requires Chaptarr API v1."
  }

  assert {
    condition     = !data.chaptarr_health.current.has_errors
    error_message = "Chaptarr reports one or more health errors."
  }
}
```

## Discovery data sources

- `chaptarr_languages` reads the language catalog or one `language_id`.
- `chaptarr_localization` reads the active localization dictionary.
- `chaptarr_search`, `chaptarr_library_search`, and `chaptarr_parse` perform
  escaped, bounded catalog queries and return canonical `result_json`.
- `chaptarr_calendar` reads a date range or one `calendar_id`.
- `chaptarr_system_routes` fingerprints all or duplicate development-mode
  routes without storing route graph/details in state.
- `chaptarr_tasks` reads all tasks or one `task_id` without running it.
- `chaptarr_updates` observes update metadata without installing anything.
- `chaptarr_disk_space` returns current disk-space observations.
- `chaptarr_file_system` supports `contents`, `type`, and `media_files` lookups.
  Its path is query-escaped and no file operation is performed.

`result_json` is validated and canonicalized but can contain library names or
filesystem paths returned by the selected discovery endpoint. Treat the state
file with the same access controls as the Chaptarr instance. Search terms and
filesystem paths are also sent to Chaptarr; never place credentials or other
secrets in those query inputs.

## Binary and feed responses

`chaptarr_media_cover` validates a single filename and returns only
`content_type`, `content_length`, and `sha256`; image bytes are not stored.
`chaptarr_calendar_feed` likewise stores only metadata and a fingerprint, not
the feed or its book titles. Use `tags` for tag filtering;
`legacy_tag_list` maps to the compatibility-only upstream `tagList` parameter.
Derived identifiers are hashed and do not contain filenames, tags, search
terms, or filesystem paths.

## Bookshelf is not read-only

Although the upstream OpenAPI groups Bookshelf with catalog APIs, its only
operation is `POST /api/v1/bookshelf`. The controller changes author and book
monitoring state. It is therefore not registered as a data source and remains
an explicitly safeguarded action-only decision tracked by issue #11.
