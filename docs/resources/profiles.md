# Profile resources

Issue #4 adds typed resources for Chaptarr quality, metadata, release, and delay profiles plus deliberate management of built-in quality definitions.

## Quality profiles

Use `chaptarr_quality_profile_schema` with `media_type = "audiobook"` or `"ebook"` before creating a profile. It returns the server's ordered quality tree and complete applicable custom-format list. Copy each group's ID and name plus the ordered quality IDs, allowed flags, format IDs, and scores into `chaptarr_quality_profile`. Direct-quality IDs/names and format names/built-in keys remain server-owned.

The provider supports Chaptarr's current two-level tree (groups containing quality leaves). Refresh fails safely if a future Chaptarr version returns deeper nesting instead of silently flattening it. `convert_to_quality_id` is optional; omitting it disables conversion. `prefer_custom_formats_over_quality` is rejected for ebook profiles because upstream ignores it outside audiobook profiles.

## Metadata profiles

`chaptarr_metadata_profile_schema` returns server defaults plus current language names/codes and special unknown-language values. `allowed_languages` and `ignored_terms` are case-insensitive sets normalized into deterministic order. Changes to filtering fields can make Chaptarr queue author metadata refresh commands during apply; changing only the profile name does not.

## Release and delay profiles

Release-profile required and ignored terms remain ordered lists. At least one list must be non-empty. An enabled profile with a nonzero `indexer_id` is also validated by Chaptarr against its configured indexers.

Delay profiles require Usenet or Torrent, non-negative delays, and tags for non-global profiles. Chaptarr reserves delay-profile ID `1`: it must have no tags and cannot be destroyed through the provider. Profile ordering remains server-owned and observed through computed `order`; the imperative reorder endpoint is intentionally not called during plan or refresh.

## Quality definitions

Quality definitions are built into Chaptarr, so they do not have create or delete endpoints. Creating `chaptarr_quality_definition` adopts the definition matching `quality_id`, preserves its server-owned title/group/weight/default fields, and updates only configured size thresholds. Import uses the definition record ID. Destroy removes the object from Terraform state without deleting or resetting the Chaptarr definition. The bulk-update route remains unused so one failed definition cannot obscure which managed object failed.

All profile API calls use the provider's header-only authentication. These schemas contain no credential attributes and no response bodies are written to diagnostics.
