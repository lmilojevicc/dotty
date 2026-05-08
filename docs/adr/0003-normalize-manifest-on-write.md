# Normalize the manifest on write

Dotty will rewrite the TOML manifest in a deterministic normalized format when commands such as `add` modify it, rather than preserving user comments and formatting. This keeps v1 simple and testable; comment-preserving TOML edits were rejected for now because they require a more complex document-editing layer.

## Considered Options

- Normalize the manifest on write
- Preserve comments and formatting
- Avoid editing the manifest and print snippets instead
