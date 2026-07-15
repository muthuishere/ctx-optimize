# .NET — parse a `.sln` into multi-path modules

The .NET convention splits one logical module across `src/Foo/Foo.csproj` and
`tests/Foo.Tests/Foo.Tests.csproj` — two top-level folders, ONE module. Group
them so test→source calls resolve.

## 1. Find the solution

Look for `*.sln` at the repo root (or `*.slnx`, the newer XML form). No `.sln`?
Enumerate every `*.csproj`/`*.fsproj`/`*.vbproj` in the tree and treat each as a
project (skip the naming step's manifest, go straight to pairing by name).

## 2. List projects from the `.sln`

A `.sln` is a text file. Each project is a `Project(...)` line:

```
Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "Billing", "src\Billing\Billing.csproj", "{GUID}"
Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "Billing.Tests", "tests\Billing.Tests\Billing.Tests.csproj", "{GUID}"
```

Extract, per line: the **display name** (first quoted string) and the
**project path** (second quoted string). Normalize `\` → `/`. The project's
DIRECTORY is `dirname(projectPath)` (e.g. `src/Billing`). Ignore solution
folders — their "path" equals their name with no `.csproj` (no directory).

For a `.slnx`, projects are `<Project Path="src/Billing/Billing.csproj" />`
elements — same idea, read the `Path` attribute.

## 3. Pair source ↔ tests

Prefer the project reference graph: open each test `.csproj` and read its
`<ProjectReference Include="..\..\src\Billing\Billing.csproj" />` — the referenced
project is the source it tests. That's the authoritative pairing.

Fallback (no clear reference): match by name — `Billing.Tests`,
`Billing.Test`, `Billing.UnitTests`, `Billing.IntegrationTests` all pair with
`Billing`. Strip the `.Tests`/`.Test`/`.UnitTests`/`.IntegrationTests` suffix
and match the remainder.

## 4. Emit modules

One multi-path module per source project, folding in every test project that
references (or name-matches) it:

```json
{"name": "acme", "modules": [
  {"name": "Billing", "paths": ["src/Billing", "tests/Billing.Tests"]},
  {"name": "Orders",  "paths": ["src/Orders",  "tests/Orders.Tests", "tests/Orders.IntegrationTests"]},
  {"name": "Shared",  "path":  "src/Shared"}
]}
```

- Use the SOURCE project's display name as the module `name`.
- A source project with no tests → single-path `{"name","path"}`.
- Prefer directory paths (`src/Billing`), not the `.csproj` file path — the
  whole folder is gathered.
- If many tests sit under one dir, a glob works: `"tests/*.Tests"`.

## 5. Confirm and gather

Show the grouping, let the user fix it, write `.ctxoptimize/config.json`, then
`ctx-optimize add .`. Verify a test→source call resolves with a `query`.
