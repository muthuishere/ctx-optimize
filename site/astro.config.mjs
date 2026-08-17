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
				'Fast answers on a cheaper model. A bigger model with less context for the whole job — not one grep. One local Go binary gathers nodes, edges, and boundaries — no model, no MCP. Same store for the architect.',
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
			components: {
				// Puts top-level nav at the right end of the header. The splash
				// homepage has no sidebar, so without these the docs are
				// reachable only from the hero button.
				Header: './src/components/Header.astro',
				SiteTitle: './src/components/SiteTitle.astro',
			},
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/muthuishere/ctx-optimize' },
			],
			sidebar: [
				{
					label: 'Start here',
					items: [
						{ label: 'Quick start', slug: 'quickstart' },
						{ label: 'How to use it', slug: 'guide' },
						{ label: 'What it is', slug: 'concepts' },
					],
				},
				{
					label: 'CLI',
					items: [
						{ label: 'Overview', slug: 'cli' },
						{
							label: 'Build',
							collapsed: true,
							items: [
								{ label: 'up', link: 'cli/#up' },
								{ label: 'init', link: 'cli/#init' },
								{ label: 'scan', link: 'cli/#scan' },
								{ label: 'add', link: 'cli/#add' },
								{ label: 'sync', link: 'cli/#sync' },
								{ label: 'wiki', link: 'cli/#wiki' },
							],
						},
						{
							label: 'Ask',
							collapsed: true,
							items: [
								{ label: 'query', link: 'cli/#query' },
								{ label: 'card', link: 'cli/#card' },
								{ label: 'change-plan', link: 'cli/#change-plan' },
								{ label: 'explain', link: 'cli/#explain' },
								{ label: 'affected', link: 'cli/#affected' },
								{ label: 'path', link: 'cli/#path' },
								{ label: 'hubs', link: 'cli/#hubs' },
								{ label: 'report', link: 'cli/#report' },
								{ label: 'verify', link: 'cli/#verify' },
								{ label: 'status', link: 'cli/#status' },
								{ label: 'fresh', link: 'cli/#fresh' },
								{ label: 'serve', link: 'cli/#serve' },
							],
						},
						{
							label: 'List',
							collapsed: true,
							items: [
								{ label: 'nodes', link: 'cli/#nodes' },
								{ label: 'edges', link: 'cli/#edges' },
								{ label: 'deps', link: 'cli/#deps' },
							],
						},
						{
							label: 'Manage',
							collapsed: true,
							items: [
								{ label: 'config', link: 'cli/#config' },
								{ label: 'remote', link: 'cli/#remote' },
								{ label: 'merge', link: 'cli/#merge' },
								{ label: 'export', link: 'cli/#export' },
								{ label: 'store', link: 'cli/#store' },
								{ label: 'log', link: 'cli/#log' },
								{ label: 'languages', link: 'cli/#languages' },
								{ label: 'routes', link: 'cli/#routes' },
								{ label: 'manifests', link: 'cli/#manifests' },
								{ label: 'install', link: 'cli/#install' },
								{ label: 'update', link: 'cli/#update' },
								{ label: 'version', link: 'cli/#version' },
							],
						},
					],
				},
				{
					label: 'Adapters',
					items: [
						{ label: 'How', link: 'cli/#adapters' },
						{ label: 'capture', link: 'cli/#capture' },
						{ label: 'postgres', link: 'cli/#postgres' },
						{ label: 'mysql', link: 'cli/#mysql' },
						{ label: 'mssql', link: 'cli/#mssql' },
						{ label: 'mongodb', link: 'cli/#mongodb' },
						{ label: 'redis', link: 'cli/#redis' },
						{ label: 'kafka', link: 'cli/#kafka' },
						{ label: 'nats', link: 'cli/#nats' },
						{ label: 's3', link: 'cli/#s3' },
						{ label: 'openapi', link: 'cli/#openapi' },
						{ label: 'drop-in script', link: 'cli/#drop-in' },
					],
				},
				{
					label: 'Boundaries',
					items: [
						{ label: 'Overview', slug: 'boundaries' },
						{ label: 'config.env', link: 'boundaries/#configenv' },
						{ label: 'network.http', link: 'boundaries/#networkhttp' },
						{ label: 'process.exec', link: 'boundaries/#processexec' },
						{ label: 'Flags', link: 'boundaries/#narrowing-it' },
						{ label: 'drift', link: 'boundaries/#drift' },
						{ label: 'services', link: 'boundaries/#services' },
						{ label: 'Authoring', link: 'boundaries/#authoring-your-own' },
					],
				},
				{
					label: 'See also',
					items: [
						{ label: 'Cookbook', slug: 'cookbook' },
						{ label: 'Use cases', slug: 'use-cases' },
						{ label: 'See the architecture', slug: 'see' },
						{ label: 'Dashboard', slug: 'dashboard' },
					],
				},
				{
					label: 'Under the hood',
					items: [
						{ label: 'How it works', slug: 'how-it-works' },
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
