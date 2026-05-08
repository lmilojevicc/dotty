# Classify non-symlink targets as conflicts

Dotty will infer package state from the filesystem and will treat any non-symlink content at a manifest target path as a conflict, including copies left by a soft `unlink`. This means relinking after a soft unlink requires an explicit destructive `--force`, but it avoids maintaining hidden unlink metadata or performing expensive content comparisons to guess whether a target copy is safe to replace.

## Considered Options

- Treat all non-symlink targets as conflicts
- Compare target content to package sources
- Record unlink-state metadata
