# Maintained Go bundle

This distribution retains the upstream skill names, licenses, technical
references, scripts, and assets. Local entrypoints use concrete task triggers
and load supporting guides only when they help the current question.

Unsupported upstream metadata remains under `metadata.upstream`. That includes
`allowed-tools`, because Claude Code would otherwise pre-approve those tools while
a skill runs, and skill selection must not grant permission. Keep routing
conditional, respect repository execution requirements and user authorization,
and avoid automatic configuration changes or publication requirements.

Validate metadata, local references, notices, and the model policy with the
[Go suite validator](../../docs/development.md) (`cw validate`). Device audits,
historical measurement snapshots, and evaluation outputs stay outside the
published suite.
