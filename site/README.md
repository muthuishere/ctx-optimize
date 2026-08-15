# site/ — the docs and marketing site

Astro + Starlight, markdown-authored, deployed to
<https://muthuishere.github.io/ctx-optimize/> by `.github/workflows/pages.yml`.

```bash
npm install
npm run dev      # http://localhost:4321/ctx-optimize/
npm run build    # -> dist/
```

## Layout

| path | what |
|---|---|
| `src/content/docs/index.mdx` | the splash homepage (`template: splash`) |
| `src/content/docs/*.md` | one file per docs page; the sidebar is declared in `astro.config.mjs` |
| `src/styles/ctx.css` | the theme — one accent, charcoal dark / neutral light, wide content column for benchmark tables |
| `public/` | carried over verbatim from the previous site: `media/`, `proof/`, `bench/`, `examples/`. Published asset URLs must keep resolving |

## Why it is not six HTML files any more

It was: `index.html`, `docs.html`, `concepts.html`, `cookbook.html`, `compare.html` and
`use-cases.html`, each carrying its own copy of the same ~180-line `<style>` block. The
evidence pages under `proof/` linked to raw `.md` files, and with `.nojekyll` on, GitHub
Pages served those as plain text — the most credibility-critical links on the site
dead-ended in unrendered markdown.

Old URLs are preserved as redirects in `astro.config.mjs`; do not remove them.

## House rules for this site

- **Every number is measured, and says where from.** Competitor numbers carry their
  provenance, including when it is weak — the benchmark arena predates the committed
  `setup.py`, so no competitor row is pin-verified and the page says so.
- **The losses stay on the page.** `limits.md` is not an appendix; grep beating us on
  "where is X" is in the benchmark table itself.
- Node runs here only. The Go binary, `task ci` and `go install` need no toolchain.
