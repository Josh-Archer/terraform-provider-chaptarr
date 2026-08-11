# Storage resources

## `chaptarr_root_folder`

Registers an existing readable and writable library path. `name`, `path`, and
`folder_type` (`mixed`, `audiobook`, or `ebook`) are required. The resource also
supports default/per-media tags, quality and metadata profile IDs, monitoring
defaults, Audiobookshelf sidecar settings, and Calibre connection settings.

`path` cannot be edited by Chaptarr and therefore requires replacement.
Creating a root folder causes Chaptarr to queue its upstream initial scan; this
happens only during apply, never during plan or refresh.

Calibre `password` is sensitive and write-only. Chaptarr never returns it and
the provider never stores it in state. Supply it when creating an authenticated
Calibre root or when intentionally rotating it. An omitted password on update
uses Chaptarr's preserve-existing behavior.

Destroy is intentionally two-step. Set `allow_destroy = true` and apply before
removing the resource. Chaptarr deletes the root-folder database registration
and purges ingest-queue rows under the path. It does not move or delete library
files. Terraform lifecycle `prevent_destroy` remains recommended for important
libraries.

Import uses the positive numeric Chaptarr root-folder ID.

## `chaptarr_remote_path_mapping`

Maps a remote download-client path to an existing local Chaptarr path. Select
a configured client with `download_client_id`, or set `host` when the client ID
is zero. Import uses the positive numeric mapping ID.

`test_before_apply` defaults to false. When explicitly true, create/update
calls Chaptarr's mapping test before writing state. The probe can contact the
selected download client and inspect filesystem existence/writability. It
never runs during plan, refresh, or destroy, and state receives only a small
boolean/generic-error summary rather than observed item paths.

Destroy removes only the mapping record. It does not move or delete downloads
or library files.

## Read-only storage discovery

- `chaptarr_root_folders` returns canonical JSON for all root folders, or those
  compatible with `media_type = "audiobook"`/`"ebook"`; mixed roots appear in
  either filtered result.
- `chaptarr_remote_path_mappings` returns configured mappings without probing
  a download client.
- `chaptarr_file_system` and `chaptarr_disk_space` provide the bounded storage
  observations documented in the read-only data-source guide.

These discovery results may contain local/remote paths. Protect Terraform state
accordingly and never place credentials in path or host fields.
