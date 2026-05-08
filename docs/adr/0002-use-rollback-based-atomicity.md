# Use rollback-based atomicity

Dotty will treat filesystem operations as atomic at the operation level: it stages changes, writes durable state via temporary files and rename where possible, and rolls back changes it already made when a normal error occurs. It will not promise full crash-proof transactions in v1; a crash-resistant journal was rejected for now because it adds significant complexity beyond the required success-or-fail behavior for ordinary command failures.

## Considered Options

- Rollback-based atomicity
- Crash-resistant operation journal
- Best-effort ordered changes
