# Read-only discovery and observability

These data sources call only non-mutating Chaptarr `GET` endpoints (plus `HEAD`
for `chaptarr_ping`). Terraform refreshes them during normal reads, so values
such as health, free space, scheduled-task status, search results, operational
queues/history, and update availability can change without a configuration
change. They are observations rather than managed resources; destroying their
Terraform state never changes Chaptarr.

## Capability and check inputs

- `chaptarr_api_info` exposes `current` and `deprecated` API versions for
  module preconditions.
- `chaptarr_ping` probes `/ping` using `method` (`GET` by default, or `HEAD`)
  and exposes `status` (`OK` on `GET`, empty string on `HEAD`) and
  `http_status`.
- `chaptarr_health` exposes only aggregate warning/error flags and counts. It
  intentionally omits health messages, sources, logs, and internal details.
- `chaptarr_system_status` exposes a fixed capability whitelist: application,
  version, branch, database type, authentication mode, process mode, OS name,
  and runtime version. Installation identifiers, application paths, and URL
  base values are not stored.
- `chaptarr_database_status` exposes active database engine (`database_type`),
  engine version (`database_version`), `is_postgres` boolean flag, schema
  migration status (`migration_version`), and connection health (`is_healthy`).
- `chaptarr_system_statistics` exposes aggregate counts and accepts an optional
  `media_type` of `all`, `audiobook`, or `ebook`.
- `chaptarr_author_statistics` reads disk usage (`size_on_disk`) for a single
  `author_id` and `media_type` (`all`, `audiobook`, or `ebook`).

Example check:

```hcl
data "chaptarr_api_info" "current" {}
data "chaptarr_ping" "liveness" {}
data "chaptarr_health" "current" {}

check "supported_chaptarr" {
  assert {
    condition     = data.chaptarr_ping.liveness.http_status == 200
    error_message = "Chaptarr /ping did not report HTTP 200."
  }

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

## Redacted operational observations

These typed data sources expose read-only operational state while omitting
server-returned filesystem paths, operational/passkey URLs, raw error strings,
and log-like payloads from Terraform state:

- `chaptarr_backups` reads `GET /api/v1/system/backup` and returns `total_count`
  and `backups` (`id`, basename-sanitized `name`, `type`, `size`, `time`).
  Server backup `path` values are omitted.
- `chaptarr_settings_backups` reads settings-backup files and destination
  locations (`operation` of `all`, `files`, or `locations`, plus optional
  `root_folder` filter for `all`/`files`). `files` exposes basename-sanitized
  `name`, `size`, `last_write_time_utc`, and `path_sha256`; `locations` exposes
  `path_sha256`, `exists`, `writable`, and boolean `has_warning`. Raw server
  paths and warning messages are never stored.
- `chaptarr_history` supports `mode` of `paged` (`GET /api/v1/history`),
  `author` (`GET /api/v1/history/author`, requiring `author_id`), and `since`
  (`GET /api/v1/history/since`, requiring `date`), along with mode-compatible
  filters (`page`, `page_size`, `sort_key`, `sort_direction`, `event_types`,
  `event_type`, `book_id`, `download_id`, `include_author`, `include_book`,
  `media_type`). Records expose `id`, `book_id`, `author_id`, `event_type`,
  `date`, `quality_name`, `custom_format_score`, `quality_cutoff_not_met`,
  `download_id`, `author_name`, and `book_title`; raw `data` dictionaries,
  `sourceTitle`, and nested library paths/URLs are omitted.
- `chaptarr_ignored` reads `GET /api/v1/ignored` (`page`, `page_size`,
  `sort_key`, `sort_direction`) and returns `total_records` and `records` with
  `source_title` sanitized to strip directory components, URLs/magnet links, and
  credential markers.
- `chaptarr_manual_import_candidates` reads `GET /api/v1/manualimport`
  (`folder`, `download_id`, `author_id`, `filter_existing_files`,
  `replace_existing_files`, `media_type`, `book_file_ids`) and returns
  `total_count` and `candidates` with basename-sanitized `name`, `path_sha256`,
  catalog/suggested match identifiers, `quality_name`, `has_rejections`,
  `rejection_count`, and `warning_count`. Candidate paths, raw rejection
  reasons, warnings, and embedded file tags are omitted.
- `chaptarr_pending_author_import` supports `operation` values:
  - `list` (default): `GET /api/v1/pendingauthorimport` -> `items`
  - `get`: `GET /api/v1/pendingauthorimport/{id}` (requires `pending_id`) -> `items`
  - `exists`: `GET /api/v1/pendingauthorimport/author/exists/{providerId}`
    (requires `provider_id`) -> `existence`
  - `profile_options`: `GET /api/v1/pendingauthorimport/profiles/options` ->
    `profile_options` (`audiobook`, `ebook`, `all`)
  Server root-folder paths, raw `lastError` messages (replaced by boolean
  `has_error`), `requestedBy`, and `correlationId` are omitted.
- `chaptarr_cutoff_wanted` (`GET /api/v1/wanted/cutoff` and `/{id}`) and
  `chaptarr_missing` (`GET /api/v1/wanted/missing` and `/{id}`) accept either
  `book_id` for a single lookup or list filters (`page`, `page_size`,
  `sort_key`, `sort_direction`, `include_author`, `monitored`, `media_type`) and
  return `total_records` and `records` without nested file paths, `bookFiles`,
  or external cover/catalog URLs.

## Discovery data sources

- `chaptarr_languages` reads the language catalog or one `language_id`.
- `chaptarr_localization` reads the active localization dictionary.
- `chaptarr_search`, `chaptarr_library_search`, and `chaptarr_parse` perform
  escaped, bounded catalog queries and return canonical `result_json`.
- `chaptarr_calendar` reads a date range or one `calendar_id`.
- `chaptarr_system_routes` fingerprints all or duplicate development-mode
  routes without storing route graph/details in state.
- `chaptarr_commands` reads background command state or one `command_id` without
  enqueueing or cancelling commands.
- `chaptarr_blocklist` reads paged blocklist entries without removing records.
- `chaptarr_tasks` reads all tasks or one `task_id` without running it.
- `chaptarr_updates` observes update metadata without installing anything.
- `chaptarr_disk_space` returns current disk-space observations.
- `chaptarr_queue` reads queued items and queue paging state without modifying
  the queue.
- `chaptarr_queue_details` reads detailed queue items without modifying download
  progress or queue state.
- `chaptarr_queue_status` reads queue status and queue health counts without
  modifying queue state.
- `chaptarr_file_system` supports `contents`, `type`, and `media_files` lookups.
  Its path is query-escaped and no file operation is performed.

`result_json` is validated and canonicalized but can contain library names or
filesystem paths returned by the selected discovery endpoint. Treat the state
file with the same access controls as the Chaptarr instance. Search terms and
filter paths are also sent to Chaptarr; never place credentials or other secrets
in query inputs.

## Binary and feed responses

`chaptarr_media_cover` validates a single filename and returns only
`content_type`, `content_length`, and `sha256`; image bytes are not stored. Set
`legacy_route = true` when probing the compatibility media-cover routes
(`/api/v1/mediacover/{authorId}/{filename}` and
`/api/v1/mediacover/books/{bookId}/{filename}`).
`chaptarr_calendar_feed` likewise stores only metadata and a fingerprint, not
the feed or its book titles. Use `tags` for tag filtering;
`legacy_tag_list` maps to the compatibility-only upstream `tagList` parameter.
Derived identifiers are hashed and do not contain filenames, tags, search
terms, or filesystem paths.

## Imperative endpoints excluded from data sources

- `POST /api/v1/bookshelf` mutates author and book monitoring state and remains
  `action-only` / `excluded`.
- `GET /api/v1/release` triggers live external indexer searches and returns
  passkey-bearing download URLs, so it is classified `action-only` / `excluded`.
- `POST /api/v1/series/{seriesId}/narrators/discover` and
  `POST /api/v1/series/{seriesId}/narrators/search` are `POST` operations that
  trigger external provider lookups and remain `action-only` / `excluded`.
