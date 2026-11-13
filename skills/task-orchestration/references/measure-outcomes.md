# Measure outcomes

Read this only when the user asks to measure tokens, cost or outcomes with the
experimental `cw metrics` commands: `schema`, `summarize`, `import` and
`history`. Each one's `--help` prints its flags.

- Record observations in the version-one schema that `cw metrics schema`
  prints, with project and workload identity, source revision or fingerprint,
  environment, evidence and the observation scope.
- Record real duration, usage and quality outcomes when available; leave
  missing values absent.
- Compare baseline and candidate only on comparable cases, and report missing
  pairs and source or environment differences.
- Synthetic acceptance cases establish those boundaries; actual project
  diagnostics establish the checked environments. Productivity, defect
  reduction and cost savings need representative comparable observations:
  neither model agreement nor a passing small fixture suite establishes them.
- Keep private reports and provider content out of repositories and public
  releases.
