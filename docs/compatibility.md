# Chaptarr compatibility

This file is generated from `compatibility/chaptarr.json`. Run `go run ./tools/compatibility generate` after changing the matrix.

## Version evidence

| Chaptarr | API | Immutable image | Contract status | Acceptance status | Representative verified targets | Evidence |
|---|---|---|---|---|---|---|
| `0.9.929` | `v1` | `chaptarr/chaptarr:0.9.929@sha256:2f5409fad4b02386fdd57169d93f7533342eafd036357a2c2b7256df19cda7eb` | pinned-contract | live-verified | `chaptarr_tag`, `chaptarr_api_info`, `chaptarr_system_status` | Disposable tag CRUD/import plus API/system reads verified against this exact image digest; CI re-verifies each provider head. |
| `0.9.925` | `v1` | `chaptarr/chaptarr:0.9.925@sha256:8e29f4941acaf74c80bba4322237dfd2549816b3dd1b581f176b1be5d1ccb46b` | compatibility-candidate | live-verified | `chaptarr_tag`, `chaptarr_api_info`, `chaptarr_system_status` | Disposable tag CRUD/import plus API/system reads passed locally against this exact image digest on 2026-08-10; this version does not supply the pinned provider contract. |
| `0.9.914` | `v1` | `chaptarr/chaptarr:0.9.914@sha256:4780b36440dc055a404f86da089f124d41e3330d32eeb92837f65fb5c82a348c` | compatibility-candidate | live-verified | `chaptarr_tag`, `chaptarr_api_info`, `chaptarr_system_status` | Disposable tag CRUD/import plus API/system reads passed locally against this exact image digest on 2026-08-10; this version does not supply the pinned provider contract. |

`pinned-contract` means the checked-in OpenAPI artifact is authoritative for code generation. `compatibility-candidate` means only the representative acceptance lane is proposed. `candidate` is not a live-verified claim; change it only with exact-head disposable-environment evidence.

## Pinned contract coverage

The provider contract is Chaptarr `0.9.929` (`v0.9.929`, commit `537eb64f39ee1640f07ec0107aeeb8754402f0d8`). It records 30 implemented resource targets and 38 implemented data-source targets. Representative acceptance proves only tag lifecycle and safe API/system reads; it does not claim live coverage of every target.

Resources: `chaptarr_author`, `chaptarr_book`, `chaptarr_conversion_config`, `chaptarr_custom_filter`, `chaptarr_custom_format`, `chaptarr_delay_profile`, `chaptarr_development_config`, `chaptarr_download_client`, `chaptarr_download_client_config`, `chaptarr_hardcover_config`, `chaptarr_host_config`, `chaptarr_import_list`, `chaptarr_import_list_exclusion`, `chaptarr_indexer`, `chaptarr_indexer_config`, `chaptarr_media_management_config`, `chaptarr_metadata`, `chaptarr_metadata_profile`, `chaptarr_metadata_provider_config`, `chaptarr_naming_config`, `chaptarr_notification`, `chaptarr_proxy`, `chaptarr_quality_definition`, `chaptarr_quality_profile`, `chaptarr_release_profile`, `chaptarr_remote_path_mapping`, `chaptarr_root_folder`, `chaptarr_series`, `chaptarr_tag`, `chaptarr_ui_config`.

Data sources: `chaptarr_api_info`, `chaptarr_author_lookup`, `chaptarr_book_file`, `chaptarr_book_lookup`, `chaptarr_calendar`, `chaptarr_calendar_feed`, `chaptarr_custom_format_schema`, `chaptarr_disk_space`, `chaptarr_download_client_schema`, `chaptarr_editions`, `chaptarr_file_system`, `chaptarr_health`, `chaptarr_import_list_schema`, `chaptarr_indexer_flags`, `chaptarr_indexer_schema`, `chaptarr_languages`, `chaptarr_library_search`, `chaptarr_localization`, `chaptarr_media_cover`, `chaptarr_metadata_profile_schema`, `chaptarr_metadata_schema`, `chaptarr_naming_examples`, `chaptarr_naming_pattern`, `chaptarr_notification_schema`, `chaptarr_parse`, `chaptarr_quality_profile_schema`, `chaptarr_remote_path_mappings`, `chaptarr_rename_book_preview`, `chaptarr_retag_book_preview`, `chaptarr_root_folders`, `chaptarr_search`, `chaptarr_series_lookup`, `chaptarr_system_routes`, `chaptarr_system_statistics`, `chaptarr_system_status`, `chaptarr_tag_details`, `chaptarr_tasks`, `chaptarr_updates`.

## Verification boundary

Contract checks, unit tests, and generated documentation are offline evidence. The acceptance tier starts a disposable Chaptarr container exposed only on a random loopback port and uses a runtime-only synthetic API key. It attempts teardown on every exit, verifies that project containers, volumes, and networks are absent, and fails visibly if cleanup is incomplete. The dedicated Docker network is not an outbound-egress control. Production deployments, media, credentials, migration outcomes, and `terraform/arr-config` integration are outside this evidence.
