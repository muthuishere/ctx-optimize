# Ingest from external systems — SharePoint, OneDrive, Drive, wikis

Fetch → convert (references/ingest-docs.md) → land under
`docs/imported/<source>/` with provenance frontmatter → `add .`.
The binary never fetches; the agent does, with the user's existing brokers.

## SharePoint / OneDrive / Microsoft 365

Use the `apl` broker (`npm i -g @deemwarhq/apl`; accounts via `apl accounts`,
`ms:<key>` handles) to list and download files — it owns the OAuth tokens,
you never touch credentials. Flow:

1. `apl` → search/download the file(s) from the site/library the user names.
2. Convert (docx/pdf/pptx → markdown).
3. Land + stamp `source: sharepoint://…` + `fetched_at` + the drive item id
   (the id makes sync exact instead of name-matching).
4. `ctx-optimize add .`

## Google Drive

Same shape via `apl` `google:<key>` handles (export Google Docs as docx/html,
then convert).

## Confluence / internal wikis / anything behind a login

`browser-bridge` (the user's logged-in browser) fetches the page body;
convert HTML → markdown. Never scrape credentials; the bridge IS the auth.

## Rules

- One source document = one markdown file; re-fetch overwrites the same file
  (stable path ⇒ stable node ids ⇒ clean diffs, clean re-add).
- Provenance frontmatter is MANDATORY — it is what makes sync possible.
- Ask once which sites/folders matter; record the answer in the fetch script
  (references/sync.md) so refresh needs no memory.
