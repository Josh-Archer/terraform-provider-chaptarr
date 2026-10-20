# Profile resources

Issue #4 adds typed resources for Chaptarr quality, metadata, release, and delay profiles plus deliberate management of built-in quality definitions.

## Quality profiles

Use `chaptarr_quality_profile_schema` with `media_type = "audiobook"` or `"ebook"` before creating a profile. It returns the server's ordered quality tree and complete applicable custom-format list. Copy each group's ID and name plus the ordered quality IDs, allowed flags, format IDs, and scores into `chaptarr_quality_profile`. Direct-quality IDs/names and format names/built-in keys remain server-owned.

Unknown Text is a first-class leaf with `quality_id = 0`. Ebook profiles may send an empty `format_items` list; the provider serializes that as JSON `[]`, not `null`. On update the provider GETs the current profile and merges server-owned quality names, conversion-target flags, format names, and `built_in_key` values before PUT so imported profiles do not 500 when computed names are unknown in plan. Those computed names are not preserved by list index, so reordering qualities or format items refreshes names from the server after apply.

The provider supports Chaptarr's current two-level tree (groups containing quality leaves). Refresh fails safely if a future Chaptarr version returns deeper nesting instead of silently flattening it. `convert_to_quality_id` is optional; omitting it disables conversion. `prefer_custom_formats_over_quality` is rejected for ebook profiles because upstream ignores it outside audiobook profiles.

## Metadata profiles

`chaptarr_metadata_profile_schema` returns server defaults plus current language names/codes and special unknown-language values. `allowed_languages` and `ignored_terms` are case-insensitive sets normalized into deterministic order. Changes to filtering fields can make Chaptarr queue author metadata refresh commands during apply; changing only the profile name does not.

## Release and delay profiles

Release-profile required and ignored terms remain ordered lists. At least one list must be non-empty. An enabled profile with a nonzero `indexer_id` is also validated by Chaptarr against its configured indexers.

Delay profiles require Usenet or Torrent, non-negative delays, and tags for non-global profiles. Chaptarr reserves delay-profile ID `1`: it must have no tags, cannot be destroyed through the provider, and cannot be reordered or used as an `after_id` target. Set optional `after_id` to move a profile after another existing profile; use `0` to place it first. The provider verifies that a nonzero target exists during plan (when the ID is known) and before apply. When an initial reorder fails during create, the delay profile preserves the planned `after_id` in state and marks the reorder as pending in private state with a warning diagnostic rather than an error, avoiding resource taint or recreation. Subsequent plan and read steps surface `after_id` as unset so the next apply retries the reorder in place against the existing profile ID. When an update reorder fails, prior state is preserved so Terraform can retry. Omitting `after_id` does not issue a reorder request, and unrelated profile updates do not re-send reorder requests, preserving manual adjustments made in Chaptarr's UI.

`after_id` records ordering intent; `order` is read from Chaptarr. A manual reorder in Chaptarr's UI can therefore change `order` without producing Terraform drift. Ordering cycles built with resource references (such as `tonumber(chaptarr_delay_profile.other.id)`) are rejected by Terraform during planning with a reference `Cycle` error. Literal-ID cycles cannot be detected across separate resources: each reorder runs in parallel, nondeterministic order rather than a defined sequence, and later calls may override earlier placement. Changing `after_id` on one profile re-asserts its placement. Avoid cyclic `after_id` references. If a create-time reorder is still pending and its target is then deleted in Chaptarr, planning fails with a "does not exist" error because the reorder cannot succeed; remove or change `after_id` to continue. Self-reference and setting `after_id` on the reserved global profile (ID 1) are rejected during planning when the profile ID is known.

## Quality definitions

Quality definitions are built into Chaptarr, so they do not have create or delete endpoints. Creating `chaptarr_quality_definition` adopts the definition matching `quality_id`, preserves its server-owned title/group/weight/default fields, and updates only configured size thresholds. Import uses the definition record ID. Destroy removes the object from Terraform state without deleting or resetting the Chaptarr definition. The bulk-update route remains unused so one failed definition cannot obscure which managed object failed.

All profile API calls use the provider's header-only authentication. These schemas contain no credential attributes and no response bodies are written to diagnostics.
