# Maintained Go bundle

This distribution retains the upstream skill names, licenses, technical
references, scripts, and assets. Local entrypoints use concrete task triggers
and load supporting guides only when they help the current question.

Unsupported upstream metadata remains under `metadata.upstream`. That includes
`allowed-tools`, because Claude Code would otherwise pre-approve those tools while
a skill runs, and skill selection must not grant permission. Keep routing
conditional, respect repository execution requirements and user authorization,
and avoid automatic configuration changes or publication requirements.

Library-specific skills that most projects never need (`golang-samber-*`,
`golang-uber-fx`, `golang-uber-dig`, `golang-google-wire`, `golang-swagger`,
`golang-graphql` and `golang-spf13-viper`) set `disable-model-invocation: true`
and an `agents/openai.yaml` with `policy.allow_implicit_invocation: false`. They
stay out of the automatic skill listing on Claude Code and Codex and run only
when invoked by name. Keep the two settings in agreement for each skill.
`golang-how-to` tells the agent to read a named-only library skill from its
sibling directory when routing reaches one.

Validate metadata, local references, notices, and the model policy with the
[Go suite validator](../../docs/development.md) (`cw validate`). Device audits,
historical measurement snapshots, and evaluation outputs stay outside the
published suite.
