# Books, editions, and book files

`chaptarr_book_lookup` is GET-only. Select one complete result object and pass
that object as `lookup_json` to `chaptarr_book`. The lookup candidate is
apply-only and is never written to Terraform state. `foreign_book_id` must
match the candidate exactly, preventing fields from different provider
results from being combined.

The book resource owns monitored state and the chosen foreign edition identity.
When `any_edition_ok = false`, configure a provider-prefixed
`monitored_edition_id`; the provider submits exactly one monitored edition.
Refresh is GET-only and normalizes the edition catalog, narrator names, and
duration into typed state. Update reads the full current BookResource before
overlaying managed intent so metadata fields are preserved.

`chaptarr_edition` can instead own the monitored-edition selection for an
existing book. It reads the full book, selects exactly one local edition, and
updates through the supported Book endpoint. Do not manage the same edition
selection in both `chaptarr_book` and `chaptarr_edition`. Edition destroy only
forgets Terraform ownership; it does not select another edition or touch files.
Import uses `<book_id>/<edition_id>`.

`chaptarr_book_file`, `chaptarr_editions`,
`chaptarr_rename_book_preview`, and `chaptarr_retag_book_preview` are read-only data
sources. Book-file update/delete and bulk editor endpoints remain excluded
because they can move or delete media. Rename and retag support is preview-only.

Book destroy sends `deleteFiles=false`, `addImportListExclusion=false`, and
`applyToBothFormats=false` unless each behavior is explicitly configured.
Missing-book download/search endpoints are never called by refresh or plan.
