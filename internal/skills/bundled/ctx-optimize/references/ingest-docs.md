# Ingest documents — ANY format → markdown → `add .`

The binary only reads `.md`/`.txt` (plus code). Everything else enters by
YOU converting it to markdown inside the repo, then running a normal add.
No LLM lane, no fetcher in the binary — the agent is the converter.

## The flow

1. **Convert** the source to markdown (converters below).
2. **Land it** under `docs/imported/<source-kind>/<name>.md` — committed,
   diffable, re-addable. One file per source document.
3. **Stamp provenance** as frontmatter so sync (references/sync.md) can
   refresh it later:

   ```markdown
   ---
   source: sharepoint://contoso.sharepoint.com/sites/eng/Shared%20Documents/arch.docx
   fetched_at: 2026-07-13T12:00:00Z
   converter: pandoc
   ---
   ```

4. `ctx-optimize add .` — the markdown producer picks it up like any doc.

## Converters (pick by extension; all local, no service)

| Source | Convert with |
|---|---|
| .docx | `pandoc -f docx -t gfm` (or the docx skill when structure is complex) |
| .pdf | pdf skill / `pdftotext` for text-first PDFs; OCR lane for scans |
| .pptx | pptx skill → slide-per-section markdown |
| .xlsx | xlsx skill → tables as markdown |
| .html / URL | `pandoc -f html -t gfm` on the fetched body |
| .rst / .adoc | `pandoc` |

Rules: keep headings (they become section nodes), keep tables, drop nav/
boilerplate. Big binary blobs (images) stay out — link them, don't inline.

## When NOT to convert

Content that is really a SYSTEM (a DB, a queue, a live API) — write an
adapter instead (references/adapters.md). Convert-to-markdown is for
human-authored documents.
