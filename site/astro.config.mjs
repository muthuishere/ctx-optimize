// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLlmsTxt from 'starlight-llms-txt';

// Project GitHub Pages: https://muthuishere.github.io/ctx-optimize/
// The previous site was six hand-written HTML files, each carrying its own copy
// of the same 180-line <style> block, and its evidence pages dead-ended in raw
// .md served as plain text (`.nojekyll` is on, so Pages never rendered them).
// Everything under public/ is carried over verbatim so published asset URLs —
// media/, proof/, bench/, examples/ — keep resolving.
export default defineConfig({
	site: 'https://muthuishere.github.io',
	base: '/ctx-optimize',
	// The old site was six .html files at the root. Published links to them must
	// not 404, so every retired page redirects to the page that replaced it.
	redirects: {
		'/docs.html': '/ctx-optimize/cli/',
		'/concepts.html': '/ctx-optimize/concepts/',
		'/cookbook.html': '/ctx-optimize/cookbook/',
		'/compare.html': '/ctx-optimize/compare/',
		'/use-cases.html': '/ctx-optimize/use-cases/',
		// NOTE: no '/index.html' entry — it collides with the generated index.
	},
	integrations: [
		starlight({
			title: 'ctx-optimize',
			tableOfContents: false,
			description:
				'A deterministic code knowledge graph for coding agents. Ask who calls this, what breaks if I change it, and what this system talks to — every answer a resolved symbol with its real file:line. One binary, no LLM, no database, no MCP.',
			plugins: [
				starlightLlmsTxt({
					projectName: 'ctx-optimize',
					description:
						'A deterministic code knowledge graph for coding agents: one static binary that gathers a repo into a plain-ndjson graph and answers structural questions — query, card, change-plan, affected, path, boundaries — each with an exact file:line.',
					details:
						'ctx-optimize parses code with tree-sitter compiled to WASM (hosted by wazero, 12 languages embedded, others as grammar packs), stores a sorted newline-delimited JSON graph with a content-hash manifest, and answers with resolved symbols rather than matching lines. Every edge carries a confidence tier: EXTRACTED is parsed fact, INFERRED is name-matched, AMBIGUOUS is surfaced as a shortlist rather than guessed into the graph. The boundaries lane models a repo external surface — env reads, spawned binaries, HTTP hosts, served routes — as port nodes with direction, transport and credential flagging by NAME. The binary makes no LLM calls, holds no credentials at rest, and reaches the network only when explicitly asked.',
				}),
			],
			customCss: ['@fontsource-variable/inter', './src/styles/ctx.css'],
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/muthuishere/ctx-optimize' },
			],
			sidebar: [
				{
					label: 'Start here',
					items: [
						{ label: 'Quick start', slug: 'quickstart' },
						{ label: 'What it is', slug: 'concepts' },
					],
				},
				{
					label: 'Using it',
					items: [
						{ label: 'CLI reference', slug: 'cli' },
						{ label: 'Cookbook', slug: 'cookbook' },
						{ label: 'Use cases', slug: 'use-cases' },
						{ label: 'Boundaries — the external surface', slug: 'boundaries' },
					],
				},
				{
					label: 'Evidence',
					items: [
						{ label: 'Benchmarks', slug: 'benchmarks' },
						{ label: 'Compared with other tools', slug: 'compare' },
						{ label: 'What we do not claim', slug: 'limits' },
					],
				},
			],
		}),
	],
});
