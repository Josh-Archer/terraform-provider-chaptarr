# Authors and series

Use `chaptarr_author_lookup` and `chaptarr_series_lookup` to inspect the pinned
metadata-provider results before choosing a provider-prefixed identity such as
`hc:191785`. Lookup is GET-only. Its canonical JSON result is bounded by the
provider client, and the data-source ID is a hash that does not retain the raw
query.

`chaptarr_author` owns one local author and its audiobook/ebook monitoring,
profile, root-folder, and tag configuration. Create disables queued/pending
imports and missing-book search unless `search_for_missing_books = true`.
Update first reads the full server document and overlays only managed fields so
server-owned metadata is preserved. `move_files_on_update` defaults to false.
Destroy sends `deleteFiles=false` and does not add an import-list exclusion
unless the corresponding explicit controls are true. Refresh only performs
`GET /api/v1/author/{id}` and never downloads or moves media.

`chaptarr_series` owns series monitoring intent and, when configured, the
preferred narrator and explicit narrator-variant removals. Chaptarr exposes
`POST /api/v1/series/add`, narrator and variant reads,
`PUT /api/v1/series/{id}/narrators/preferred`, and
`DELETE /api/v1/series/{id}/variants/{variantId}`. There is still no general
series update or delete endpoint. The provider reapplies the explicit
selected-book monitoring intent on update. Refresh is GET-only: it reads the
series document, narrator analysis, narrator inheritance, and variants.

`preferred_narrator` is optional. When it is set, create and update trim it and
send it to the preferred-narrator endpoint. Omit it to observe the narrator
decoded from the series document without changing it.
`apply_narrator_to_books` and `override_existing_book_narrators` default to
false. Applying a narrator changes book narrator state and requires
`preferred_narrator`. Override replaces narrators already stored on books and
requires apply. Both run only during create or update, never during refresh or
destroy.

`remove_variant_ids` defaults to empty. Create and update delete only IDs that
are present in this series' variant list. The managed series ID is rejected.
An ID that is not a current variant of this series is left untouched, and a
variant that disappears before deletion is success. Destroy does not delete
variants.

`narrator_analysis`, `narrator_inheritance`, and `variants` are computed.
Variant state keeps identity, media type, narrator, and work counts. It omits
images, links, descriptions, and book payloads so refresh does not retain media
URLs.

Import both resources with the positive local numeric ID. Author refresh can
reconstruct all managed server state. Series import cannot reconstruct the
original selected-book request, profile or root references, or variant-removal
intent, so those attributes start conservatively and must be configured before
an update. Conflict and provider-ambiguity responses direct users to lookup and
the unique local numeric import ID.

No plan or refresh path calls download-media, file-move, image-load, narrator
discovery, narrator search, or delete-files operations. Discover and search
remain unregistered because they are POST operations and must not run during
plan or refresh.
