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

`chaptarr_series` owns series monitoring intent. Chaptarr exposes
`POST /api/v1/series/add` plus read endpoints, but no general series update or
delete endpoint. The provider therefore reapplies the explicit selected-book
monitoring intent on update and uses GET-only refresh. Destroy removes the
object from Terraform state and emits a warning; it deliberately leaves the
series, authors, books, files, and last monitoring state in Chaptarr.

Import both resources with the positive local numeric ID. Author refresh can
reconstruct all managed server state. Series import cannot reconstruct the
original selected-book request or profile/root references from the Series API,
so those attributes start conservatively and must be configured before an
update. Conflict and provider-ambiguity responses direct users to lookup and
the unique local numeric import ID.

No plan or refresh path calls download-media, file-move, image-load, narrator
discovery, or delete-files operations.
