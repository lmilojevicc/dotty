# Share one protected repository inventory classifier

Dotty will use one anchored, no-follow ownership classifier for every repository inventory consumer. It returns tracked sources, selectable untracked content, protected system entries, and transaction-owned artifacts from one traversal so planning, status, completion, and mutation cannot disagree about ownership.

Protected system entries include `dotty.toml`, `.git`, user and repository locks, staging paths, rollback backups, and Dotty temporary artifacts at any depth. Explicit selection does not change classification. The classifier returns protected descendants and their identities to callers; `CONTEXT.md` defines how each command reports or refuses those classifications.

Transaction cleanup is the sole bounded exception to ordinary protection. It may act only on artifacts created by that transaction and recorded in the identity-bound owned-tree inventory defined by ADR 0007. Name or classifier membership never authorizes deletion. Anchored revalidation retains counterfeit, missing, unrecorded, or changed descendants and reports the appropriate cleanup warning or uncertain state.

Command-specific omission, suggestion, refusal, and retention behavior belongs to `CONTEXT.md`; this ADR owns only the shared classifier and transaction-provenance mechanism.

## Considered Options

- One shared classifier with transaction-provenance cleanup
- Separate status, completion, and mutation scans
- Name-based cleanup of Dotty-looking files
- Allow explicit selection to override protected entries
