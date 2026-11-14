# Selecting security evidence

Use the repository's maintained commands, configuration, tool versions, and execution environment. Record exact code/input identity and the provenance of reused CI; a green unrelated workflow does not cover the boundary being reviewed.

| Claim | Useful evidence | Limit to preserve |
| --- | --- | --- |
| Untrusted input reaches a dangerous operation | Source trace through actual callers, controls, and effect; focused negative case if execution is permitted | A test-only path or isolated sink is not runtime reachability |
| Authorization protects a resource | Caller identity to authority to resource-owner comparison, including retries and alternate entrypoints | Successful authentication or a trusted transport does not prove authorization |
| A Go dependency vulnerability is reachable | Existing `govulncheck` output tied to module/build settings and traced vulnerable calls | A module advisory match alone does not prove a vulnerable call executes |
| A Go source pattern warrants investigation | Existing `gosec` or repository-configured CI scan, followed by source verification | Rule matches and suppression comments need contextual evaluation |
| A fix rejects an attack | Repository-appropriate regression/integration evidence at the risky boundary | Local fixtures do not prove installed configuration or production exposure |

Check availability before choosing a tool. `govulncheck`, `gosec`, race tests, fuzzing, and CI scans are options within the user's scope, not mandatory steps for every review. Preserve repository security gates when changing code. A missing tool, unavailable credential-free CI read, or explicit no-tests constraint is a reportable limit, not an invitation to auto-install, run `go get`, fetch a latest tool, or substitute an unauthorized environment. Continue source analysis and use available evidence.

For each candidate, retain the initiating input, prerequisite attacker capability, decisive control or missing check, and observable consequence. Distinguish a defect introduced by a diff from a pre-existing issue. Reject false positives when callers constrain input, a control blocks the path, or the operation is an intentionally authorized capability; explain the decisive evidence briefly when useful. Lack of evidence for exploitability does not prove safety.

When specialist guidance is useful, dynamically resolve an installed skill and read only its applicable reference. Existing Go security references cover input injection, filesystem, network, secrets, logging, memory, and cryptography. Optional differential review can help assess changed attack surface; API-footgun review can assess misuse-prone interfaces. Neither replaces the source trace or authorizes broader execution.

Report in the user's requested format. A compact finding can be: severity; path/line at exact SHA; input/trigger; impact; evidence; fix direction; verification limit. Keep dependency advisory status, scanner observations, confirmed runtime defects, and deployment qualification distinct.
