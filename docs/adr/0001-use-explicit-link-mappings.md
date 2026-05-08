# Use explicit link mappings in the manifest

Dotty will represent every package as one or more explicit source-to-target link mappings in package-keyed TOML manifest tables. A whole-directory package is a package with a single mapping, while multi-file or multi-target packages use multiple mappings; this avoids stow-style inference in core state and makes status, conflict detection, unlink, and migration behavior deterministic. The manifest may also define named collections as lists of packages for bulk link/unlink workflows.

## Considered Options

- Explicit link mappings
- Separate directory and file package types
- Inferring targets from repository layout
