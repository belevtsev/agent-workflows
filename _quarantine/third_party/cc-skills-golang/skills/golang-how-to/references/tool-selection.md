# Choosing Go code and package tools

Use this comparison when the question spans tool boundaries. Prefer available tools; reading this reference does not install or register a server, run a scan, or load every named skill.


Four tools can answer "is this dependency OK to use," and they don't overlap as much as they look:

- **Context7** is a general-purpose, cross-language documentation fetcher — useful when no more specific source exists. For a Go package or module, `godig` is almost always the better choice: it pulls **structured, Go-specific data** straight from pkg.go.dev — exact versions, exported symbols with signatures, runnable examples, `imported-by`, and known vulnerabilities — rather than Context7's generic scraped/curated docs, which don't expose that structure and can lag or miss lesser-known Go modules. Reach for Context7 only when a dependency's documentation genuinely doesn't exist or isn't indexed on pkg.go.dev.
- **`godig`** answers questions about the **published ecosystem**: any Go package or module, whether or not it's in your `go.mod` yet — it calls the remote pkg.go.dev API and never touches your local checkout. Its `vulns` command reports CVEs known for a package/version in isolation, regardless of whether your build actually reaches the vulnerable code path.
- **`gopls`** (→ `samber/cc-skills-golang@golang-gopls`, via its MCP server, the native `LSP` tool, or its CLI) answers questions about **your specific build**: your code plus every dependency as resolved from `go.mod`, workspace settings, and replacements, including `replace` directives pointing at forks or local paths — neither `godig` nor Context7 can see that. Its `go_vulncheck` operation runs a single, on-demand reachability check against the workspace as it stands right now.
- **`govulncheck`** (the standalone CLI, wrapped by the `samber/cc-skills-golang@golang-security` skill) is the whole-tree audit: it walks the entire module's call graph to confirm which known vulnerabilities are actually reachable, and is the tool of record for CI gates and periodic security sweeps — `gopls`'s `go_vulncheck` is a lighter-weight, single-shot version of the same analysis for use mid-edit.

Pick by task:

| Task | Tool | How |
| --- | --- | --- |
| Find where a symbol is defined in your own repo | `gopls` | `samber/cc-skills-golang@golang-gopls` — `go_search`, then `go_file_context` |
| Understand a file's intra-package dependencies | `gopls` | `samber/cc-skills-golang@golang-gopls` — `go_file_context` |
| Jump into a dependency's exact resolved source (incl. forks/`replace`d versions) | `gopls` | `samber/cc-skills-golang@golang-gopls` — `go_package_api`, or the native `LSP` tool's `goToDefinition` |
| Find every call site in your own code that references a dependency's symbol | `gopls` | `samber/cc-skills-golang@golang-gopls` — `go_symbol_references` — `godig`'s `imported-by` only lists public _packages_, not call sites in your repo |
| Get compiler diagnostics right after an edit | `gopls` | `samber/cc-skills-golang@golang-gopls` — `go_diagnostics` (MCP), or automatic with the native `LSP` tool |
| Check whether your current build can reach a known vulnerability, mid-edit | `gopls` | `samber/cc-skills-golang@golang-gopls` — `go_vulncheck` |
| Rename, extract, inline, or otherwise refactor local code | `gopls` | `samber/cc-skills-golang@golang-gopls` — safe rename, `refactor.*` code actions |
| Whole-tree vulnerability audit across the module (CI, periodic sweep) | `govulncheck` | `samber/cc-skills-golang@golang-security` skill — `govulncheck ./...` |
| List available versions of a published package | `godig` | `godig versions <path>` |
| Check known CVEs for a package/version you haven't added yet | `godig` | `godig vulns <path>` |
| See exported symbols/signatures of a published package | `godig` | `godig symbols` / `symbol doc` |
| Get runnable code examples for a symbol | `godig` | `godig symbol examples` |
| Read a package's rendered README/docs | `godig` | `godig module readme` / `package doc` |
| See who imports a package across the whole public ecosystem | `godig` | `godig imported-by` |
| Search for a package or library candidate | `godig` | `godig search` |
| Check a package's or module's license | `godig` | `godig package licenses` / `module licenses` |
| Get docs for a non-Go library, or a Go module not indexed on pkg.go.dev | Context7 | `resolve-library-id` / `query-docs` |

See the `samber/cc-skills-golang@golang-pkg-go-dev` skill for the full `godig` command reference, and the `samber/cc-skills-golang@golang-security` skill for the whole-tree `govulncheck` remediation workflow.

