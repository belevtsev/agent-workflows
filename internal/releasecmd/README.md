# Release bundle tooling

The repository's release command assembles the six CLI archives, SBOMs, installers and plugin
into a flat bundle. It checks the complete checksum manifest and its contents before upload.

The publisher uses a GitHub App token to prepare a draft, confirm the tag's revision, and
compare GitHub's digests with the verified bundle. Run it only from the protected release
workflow after testing and attestation. The command ships separately from the Kitsu CLI.
