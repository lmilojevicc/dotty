# Core Lifecycle Implementation Plan

## Status and authority

**Status:** Milestone 0 remains accepted for substantive documentation baseline `7eaec6c`. Milestone 1 is In progress: its FIRST focused-test harness prerequisite is accepted at `2669cac5cd51bf8a20f30dd61258506c97fd096f` ([receipt](#milestone-1-first-focused-test-harness-acceptance-receipt)), strict TOML persistence at `854a4165951cbdc8b812f7b28ccc6584b6636e2b` ([receipt](#milestone-1-strict-toml-persistence-acceptance-receipt)), additive C4 vocabulary and final CLI transport only at `b4b89802f3853e8d4d8ea7790dfdc5e846747664` ([receipt](#milestone-1-c4-vocabulary-and-transport-acceptance-receipt)), and the production-unused native-handle boundary at `8c5104ce422bacb5c1566a35ad2ef22be5332a4f` ([receipt](#milestone-1-native-handle-boundary-acceptance-receipt)) with Darwin/arm64 and Linux/arm64 evidence for that slice only; full Milestone 1 is not accepted. Typed reports are producer claims, not filesystem verification; legacy transaction and caller classification remains unchanged. User coordination, full anchored observations and artifact authority, protected-inventory classification, relocation fidelity, EXDEV handling, all caller migrations, and broader Milestone 1 native filesystem gates remain pending. Milestones 2–7 remain planned. The user authorizes the full remaining plan in prerequisite order, subject to its acceptance gates; limited authorizations in earlier receipts are historical, not current scope restrictions. The next bounded step is native authority/file/flock extensions and the canonical coordinator, initially unused by production, per oracle `cd94ac45`; dependent production integration waits for release-outcome and isolation readiness. This update records status/evidence only and implements no next slice.

[CONTEXT.md](../../CONTEXT.md) is the sole normative user-visible product contract in this working tree. This roadmap owns only delivery order, compatibility notes, acceptance gates, and evidence links; it does not define runtime semantics. Fresh acceptance of this baseline's Milestone 0 commit gates every source milestone.

Historical commits `57dddcb` (contracts) and `cb846c3` (evidence) identify provenance from the reference-only `docs/lifecycle-design` worktree. They are not evidence of acceptance, checks, or implementation progress in this baseline. Its recovered product decisions and ADRs are retained, subject to the fresh review gate; old source code and execution evidence are not imported.

- The canonical roadmap path is `docs/plans/lifecycle-implementation-plan.md`; the former `docs/lifecycle-implementation-plan.md` path must not be recreated.
- No source milestone may begin until the Milestone 0 commit containing the reviewed `CONTEXT.md` and ADRs is accepted.
- [AGENTS.md](../../AGENTS.md) owns repository-wide engineering and verification policy.
- [ADRs](../adr/) own durable architecture mechanisms and rationale only.
- `README.md`, CLI help, and completions describe behavior only when it ships.
- ADR 0002 remains authoritative: Dotty rolls back ordinary failures but does not promise crash-proof transactions.

## Serial delivery policy

1. Execute one milestone at a time in prerequisite order, with one writer and one worktree.
2. Split milestones into serial pull requests when useful; do not begin a dependent milestone until the preceding acceptance gate passes.
3. Define tests before implementation and retain one deterministic regression for every repaired integrity defect.
4. Use `mise` tasks for project checks. Run `mise run verify` at every milestone gate and `mise run vuln` for dependency, persistence, destructive-filesystem, or security-sensitive work.
5. Focused checks are iteration evidence, not acceptance by themselves. Before relying on a focused pattern, add or use a harness that reports the matched, non-skipped executed tests and fails when the full selection executes zero such tests, including a missing requested subtest beneath a matching parent. A parent-only match or test listing is not execution evidence; every gate also runs `mise run test` through `mise run verify`.
6. Require fresh-context integrity, test, and CLI review. Apply accepted fixes with one writer and rerun affected gates.
7. Record accepted commits or pull requests in the status table; do not replace evidence links with progress prose.

## Contract and architecture index

This table is non-normative traceability for milestone acceptance. `CONTEXT.md` owns each user-visible contract; the named ADRs own mechanisms and rationale. Milestone checklists below test those authorities and must not redefine them.

| ID | User-visible authority | Architecture authority |
| --- | --- | --- |
| C1 — integrity and concurrency | [Transaction outcomes and concurrency boundary](../../CONTEXT.md#transaction-outcomes-and-concurrency-boundary) | [ADR 0006](../adr/0006-use-a-user-scoped-mutation-lock.md) user lock; [ADR 0007](../adr/0007-use-anchored-filesystem-mutations.md) anchored mutations |
| C2 — planning, dry-run, confirmation | [Planning, dry-run, and destructive confirmation](../../CONTEXT.md#planning-dry-run-and-destructive-confirmation) | [ADR 0005](../adr/0005-use-immutable-operation-plans.md) immutable plans |
| C3 — streams and exits | [Planning, dry-run, and destructive confirmation](../../CONTEXT.md#planning-dry-run-and-destructive-confirmation) | [ADR 0005](../adr/0005-use-immutable-operation-plans.md) structured confirmation/exit seam |
| C4 — transaction outcomes | [Transaction outcomes and concurrency boundary](../../CONTEXT.md#transaction-outcomes-and-concurrency-boundary) | [ADR 0002](../adr/0002-use-rollback-based-atomicity.md) rollback boundary; [ADR 0007](../adr/0007-use-anchored-filesystem-mutations.md) and [ADR 0008](../adr/0008-preserve-relocation-fidelity.md) recovery |
| C5 — relative-symlink behavior | [Relocation, metadata, and hardlinks](../../CONTEXT.md#relocation-metadata-and-hardlinks) | [ADR 0008](../adr/0008-preserve-relocation-fidelity.md) tree-relative equivalence |
| C5a — metadata and hardlinks | [Relocation, metadata, and hardlinks](../../CONTEXT.md#relocation-metadata-and-hardlinks) | [ADR 0008](../adr/0008-preserve-relocation-fidelity.md) fidelity mechanisms |
| C5b — cross-filesystem outcomes | [Transaction outcomes and concurrency boundary](../../CONTEXT.md#transaction-outcomes-and-concurrency-boundary) | [ADR 0007](../adr/0007-use-anchored-filesystem-mutations.md) and [ADR 0008](../adr/0008-preserve-relocation-fidelity.md) staging, install, rollback, cleanup |
| C6 — Remove/Prune grammar | [Remove selection and behavior](../../CONTEXT.md#remove-selection-and-behavior) and [Prune selection and behavior](../../CONTEXT.md#prune-selection-and-behavior) | CLI implementation follows the normative grammar |
| C7 — Add/Force Add | [Add relationships](../../CONTEXT.md#relationships) and [lifecycle contracts](../../CONTEXT.md#lifecycle-command-contracts) | [ADR 0005](../adr/0005-use-immutable-operation-plans.md), [ADR 0007](../adr/0007-use-anchored-filesystem-mutations.md), and [ADR 0008](../adr/0008-preserve-relocation-fidelity.md) shared mechanisms |
| C8 — protected inventory | [Protected repository inventory](../../CONTEXT.md#protected-repository-inventory) | [ADR 0009](../adr/0009-share-protected-repository-inventory.md) classifier and transaction provenance |
| C9 — strict Manifest/config decoding | [Manifest and config decoding](../../CONTEXT.md#manifest-and-config-decoding) | [ADR 0003](../adr/0003-normalize-manifest-on-write.md) normalized persistence boundary |

Milestone 0 accepts this index only after product-contract and architecture reviews confirm that behavior appears solely in `CONTEXT.md`, mechanisms solely in ADRs, and every implementation checklist below links back to one of those authorities.

## Compatibility ledger

| Change | Impact and migration |
| --- | --- |
| Strict TOML decoding | Follow `CONTEXT.md` **Manifest and config decoding**. Migration requires removing unknown or misspelled fields; third-party extensions remain unsupported. |
| TOML/mode fixes | Correct invalid scalar output and permission drift while retaining normalized formatting from ADR 0003. |
| Deterministic Add | Existing-Package directory calls may refuse at an occupied/overlapping root or succeed at the newly inferred root. Use explicit `PACKAGE/BASENAME` to preserve old nested placement. |
| Explicit Add placement | `PACKAGE/SOURCE` becomes valid; `PACKAGE/.` remains invalid. |
| `init --dry-run` | Adds preview without changing ordinary Init. |
| Plan-driven Force Link confirmation | Destructive non-TTY scripts add `--yes`; no-op Force Link remains non-interactive. |
| Huh v2 | Adds a direct dependency and explicit TTY/cancellation behavior. |
| Remove | New managed teardown command; docs stop using “remove” as an informal Unlink synonym. |
| Prune | New deletion of classified Untracked Repository Content with explicit/scoped selection. |
| Bounded Force Add | Exact tracked sources may change content seen at several Target Paths; full dependency plan and confirmation are required. |
| Empty Collection warnings | `status` and `list` gain successful warnings on stderr. |
| Structured diagnostics | Exact error text changes intentionally; real-execution preflight plans/prompts stay off stdout while dry-run plans remain on stdout. |
| Relocation validation | Add, Leave Copy Unlink, Remove restoration, and Force Add may newly refuse external, changing, cyclic, dangling, or otherwise unprovable embedded links; users must replace them with contained stable links or manage that content outside Dotty. |
| Destructive-copy external hardlinks | Existing successful Add copy-and-delete behavior with aliases outside the captured tree becomes refusal, rather than silently splitting hardlink topology. Diagnostics identify the source and observed alias-count/topology mismatch, name known aliases, and state when aliases cannot be located; users must explicitly reorganize the content into a verifiable contained tree or manage it outside Dotty. Never silently break hardlinks or expand the selection. Leave Copy Unlink retains its source, so an external source alias alone remains permitted; copied metadata and internal topology still require verification. |

Release notes must include old/new Add examples, the destructive-copy external-hardlink refusal and safe remediation, and the `link --force --yes` automation migration.

## Universal validation and review

- Every mutation test snapshots Target Paths, Package Sources, Manifest bytes/mode, and relevant config before execution and asserts exact success, rollback, or uncertain-state outcomes.
- Every filesystem-destructive operation injects failures at every applicable capture, copy, install, persistence, rollback, and cleanup seam; required-case manifests fail on missing or skipped cases and distinguish destructive work from prompt-requiring work.
- Dry-run tests assert zero writes and no reads from prompt input.
- Race seams cover swaps at every path-component resolution/capture/install boundary, same-inode content/mode mutation, symlink target and parent swaps, and directory descendant insertion/removal.
- Same-filesystem rename is preferred. Every copy path satisfies C5a/C5b, including internal hardlink topology and platform file flags. Destructive copy-and-delete refuses external aliases before source deletion; Leave Copy Unlink must not refuse solely because its retained source has an external alias. Required metadata that cannot be preserved or verified refuses before destructive change.
- Native, non-skipped Linux and Darwin jobs record OS, architecture, and commit SHA for locking, metadata, no-replace, capture, and symlink behavior; cross-compilation does not count. Required-case manifests include negative native-capability and unsupported-filesystem cases and prohibit unsafe fallback.
- Each public command milestone ships exact `Use`, `Long`, rendered `Example`, flags, output, and Bash/zsh/fish/PowerShell completion-state tests. The custom help template must render `Examples:`.
- Each milestone records required-case matches, `mise run verify`, required `mise run vuln`, fresh reviews, platform evidence, and residual risks.

## Milestone 0 — Governance and normative contracts

### Goal

Make the confirmed design authoritative before source work.

### Contract/docs delta

- Put only user-visible terminology, commands, selection/flag behavior, I/O/exits, relocation refusals, transaction outcomes, Remove, Prune, bounded Force Add, Empty Package cleanup, and Empty Collection warnings in `CONTEXT.md`.
- Reconcile the Add relationships with deterministic directory/file inference, explicit nested placement, and invalid `PACKAGE/.`.
- Reconcile the language entry that currently treats “remove” as an avoided Unlink synonym; Remove becomes a distinct command.
- Define typed transaction outcomes and CLI retry semantics from C3/C4, user-visible relative-link outcomes from C5, and strict Manifest/config rejection behavior from C9.
- Add separate named ADRs for: immutable planning/structured diagnostics/confirmation; canonical user-lock identity and secure acquisition; descriptor-relative capture/no-replace; metadata/hardlink/EXDEV mechanisms; and the shared protected-inventory classifier with transaction-provenance cleanup.
- `CONTEXT.md` cross-links those ADRs but does not repeat their mechanisms; ADRs do not redefine command behavior.
- The lock ADR fixes its identity independently of repository/config contents, defines trusted-parent/FD/inode validation and user-lock-before-repository-lock ordering, and rejects unsupported native capabilities without fallback.
- Preserve ADR 0002's no-crash-durability boundary.

### Tests/checks

- Contract review uses examples for Add/Track/Link/Remove/Unlink/Untrack/Prune and all selection forms.
- Architecture review proves each mechanism has exactly one named ADR and each user-visible guarantee exactly one `CONTEXT.md` definition.
- Lock review covers repository aliases, `--repo`/environment/config resolution, concurrent config writes, lock symlink/path/inode replacement, and two repositories sharing a Target Path.
- Documentation review confirms the roadmap is no longer used to infer runtime semantics.
- Record a manual Markdown/terminology attestation because `mise run fmt:check` does not validate Markdown.
- Obtain a fresh oracle consultation on the reconciled baseline; historical READY verdicts and checks do not satisfy this gate.

```bash
mise run verify
```

### Acceptance/evidence

- Product-contract, architecture, and documentation reviews find no ambiguity.
- Revised `CONTEXT.md` is accepted before Milestone 1 begins.
- No runtime behavior or README claim ships here.

## Milestone 1 — Persistence and filesystem-integrity foundations

### Goal

Provide safe shared primitives and migrate every existing mutating caller before new destructive commands depend on them.

### First serial slices

1. **Focused-test harness only:** make `mise run test:focused` fail when the full requested selection executes zero non-skipped tests, including a requested subtest that does not exist even though its parent runs. Report the matched, non-skipped executed test names, preserve package/pattern and supported argument forwarding without reinterpretation, and propagate underlying command/test failures. Define positive, zero-match, subtest-mismatch, argument-preservation, and error-propagation regressions before implementation. Use the full `mise run verify` gate rather than trusting the harness to certify itself. This slice changes only the focused-check path and its tests; it does not begin runtime migration.
2. **Strict TOML persistence:** after the harness slice is independently reviewed and verified, implement C9 strict Manifest/config decoding and TOML-compatible scalar encoding, with regression coverage for unknown nested fields, malformed scalars, and round trips. Preserve existing rewrite modes; this slice does not claim completion of the later anchored persistence migration. Run `mise run verify` and `mise run vuln` for this persistence slice before proceeding.

Both first serial slices are accepted: the [FIRST focused-test harness prerequisite](#milestone-1-first-focused-test-harness-acceptance-receipt) and [strict TOML persistence](#milestone-1-strict-toml-persistence-acceptance-receipt). The subsequent [additive C4 vocabulary and final CLI transport slice](#milestone-1-c4-vocabulary-and-transport-acceptance-receipt) and [production-unused native-handle boundary](#milestone-1-native-handle-boundary-acceptance-receipt) are also accepted, not a completed typed transaction migration or Milestone 1. Within the authorized full remaining plan, the next bounded step is native authority/file/flock extensions and the canonical coordinator, initially unused by production, per oracle `cd94ac45`. Dependent production integration waits for release-outcome and isolation readiness. Adversarial tests must use private anchors, never the host canonical user-lock anchor; a VM is not approved, and later unmodified Darwin CLI coverage awaits an explicit isolation strategy. The implementation slice and gates below still apply; do not import old-worktree source or execution evidence. Anchored persistence and the remaining runtime integrity/caller migrations are pending, not implemented by this status update.

### Implementation slice

- Complete the first serial slices above before relying on focused acceptance patterns or beginning filesystem migration.
- Implement the canonical user-scoped mutation lock, secure FD acquisition/identity checks, repository locking, and deterministic order from C1.
- Preserve successful Manifest/config rewrite modes. Prefer rename; every copy/delete path enforces C5a metadata/hardlink preservation or fail-closed refusal.
- Implement the exact C5b `EXDEV` phase/outcome matrix so captured staging remains intact through every rollback-requiring step and only post-commit cleanup can warn successfully.
- Implement descriptor-relative pinned-parent capture and anchored identity/content/metadata/tree snapshots for existing callers (observations, not durable filesystem snapshots). Compare captured objects with those preflight snapshots, use no-replace installation, and attempt identity-bound no-replace restoration to the original anchored parent and name after a capture mismatch. Retain recoverable staging only when restoration cannot be proven. Missing native capabilities refuse without check-then-rename fallback.
- Implement typed C4 transaction outcomes and minimal CLI transport through `cmd/dotty/main.go` now: verified rollback means the requested state did not commit, Dotty's changes were reversed, and restoration was verified, preserving external edits rather than recreating the earlier planned state; exit 1. Rollback uncertainty also exits 1 and reports every uncertain/staged path with recovery guidance; committed cleanup warnings exit 0, report retained artifacts on stderr, and say not to retry. Pre-mutation refusal says no changes; post-capture failure reports verified restoration or uncertainty, never a false no-change claim. Full semantic planning and interaction remain Milestone 2; truthful outcome transport does not.
- Implement concrete C5 relative-link validation, C5a hardlink/metadata fidelity, and the central C8 classifier/consumer/owned-cleanup policy.
- Migrate Init, Add, Track, Untrack, Link/Force Link, `link --track`, Unlink/Leave Copy, `unlink --untrack`, Manifest/config writes, and cleanup paths. Prohibit direct replacing or check-then-use filesystem calls outside the safe primitive boundary.

### Milestone-specific tests

- Unknown top-level/nested TOML fields and round trips for quotes, slashes, Unicode, BEL, vertical tab, and newlines.
- Native file/directory metadata matrices under umask `0077`: modes/special bits, file flags, timestamps, ownership constraints, ACLs, and xattrs are either preserved and verified or detected and refused before deletion.
- Internal hardlink graphs preserve topology. Destructive copy-and-delete with external aliases, unsupported destination topology, or missing metadata capabilities refuses before replacement/deletion. A positive Leave Copy Unlink case with an external source alias succeeds without breaking that alias and verifies copied metadata and internal hardlinks; external aliases alone must not trigger its refusal.
- C5b `EXDEV` matrix covers first/middle/last descendant copy, verification, no-replace install, Target/Manifest/config persistence, rollback-copy, rollback destination collision/wrong identity, and partial post-commit cleanup with exact staged/source/destination states.
- Swaps at each component/capture/install boundary distinguish three outcomes: drift detected before capture refuses without mutation; capture mismatch with proven no-replace restoration returns the captured object to its original anchored parent and name and reports restored state; collision or unprovable restoration retains recoverable staging and reports every uncertain/staged path with recovery guidance. Fixtures must preserve externally changed captured content rather than expect restoration of the plan's earlier version.
- Planned-absent destination races, same-inode content/chmod drift, and directory descendant drift.
- C5 file/directory/nested-chain/cross-filesystem cases, absolute-changing links, and Add plus primitive-level restoration fixtures for future Remove; command-level Remove restoration tests are deferred to Milestone 4.
- Protected artifacts at root/nested depths, primitive-level replacement fixtures modeling future Force Add refusal, counterfeit artifact names, post-creation swaps, identity-bound owned cleanup, and the C8 consumer-policy matrix; command-level Force Add tests are deferred to Milestone 6.
- Repository aliases, alternate `--repo`/environment/config resolution, lock symlink/path/inode swaps, two-repository Target Path races, and concurrent config writes prove one canonical user lock; external-writer limits remain explicit.
- Runtime `ENOSYS`/`ENOTSUP`/`EOPNOTSUPP` equivalents for anchored traversal, no-replace, metadata, file flags, and hardlink topology refuse without unsafe fallback.
- Unit and built-binary tests assert typed transport for rolled-back failure and rollback failure/uncertain state (exit 1), plus committed cleanup warning (exit 0, retained paths on stderr, explicit no-retry guidance). Assert that only pre-mutation refusal claims no changes and that post-capture failures distinguish verified restoration from uncertainty.
- Command-level fault injection covers Init, Add, Track, Untrack, ordinary/Force Link, `link --track`, Unlink/Leave Copy, `unlink --untrack`, Manifest/config writes, and cleanup paths. A required-case manifest fails on missing/skipped subcases, and a static audit proves no legacy replacement seam remains.

### Focused checks

Use named milestone aggregators or a zero-match-failing focused harness, then:

```bash
mise run test:focused ./internal/dotty 'TestLifecycleIntegrity'
mise run test:focused ./internal/cli 'TestLifecycleIntegrityDiagnostics'
mise run verify
mise run vuln
```

### Acceptance/evidence

- Non-skipped native Linux and Darwin runtime jobs record OS/architecture/commit SHA and exercise locks, metadata/file flags, hardlinks, no-replace, capture, and symlinks. Negative capability/filesystem cases prove fail-closed behavior; cross-builds and skipped native cases do not satisfy the gate.
- Integrity, persistence, and rollback reviewers approve exact pre/post states and required-case coverage.
- No existing mutating caller uses replacing/check-then-use primitives outside the approved best-effort boundary.
- Every existing mutating caller has the shared anchored snapshots and truthful typed outcome/exit transport. Milestone 2 reuses these foundations inside full semantic plans and structured interaction; it must not introduce a second snapshot or transaction-outcome authority.

## Milestone 2 — Immutable planning, structured diagnostics, and confirmation

### Goal

Establish one planning and interaction boundary before new lifecycle commands.

### Implementation slice

`internal/dotty`:

- Full immutable semantic operation plans with selected actions, identities, dependencies, destructive classification, and expected outcomes, reusing Milestone 1's anchored snapshots and typed transaction outcomes rather than creating parallel authority models.
- Exact locked replan comparison and shared dry-run/execution action representation.
- Migrate Add, Track, Untrack, ordinary/Force Link, `link --track`, Unlink/Leave Copy, and `unlink --untrack` so dry-run and execution consume the same C2 semantic plan. Init joins this boundary in Milestone 3 when its dry-run is added.
- Full structured diagnostics for summary, labeled paths/states, dependencies, remediation, and mutation outcome, extending the truthful minimal outcome transport already required by Milestone 1.

`internal/cli`:

- Inject input, output, prompt writer, and TTY capability.
- Render diagnostics without parsing hints from error strings.
- Integrate `charm.land/huh/v2` inline Confirm, default No, never fullscreen.
- Extend Milestone 1's typed exit transport through `cmd/dotty/main.go` with cumulative C2/C3 interaction outcomes: full stderr preflight, stdout dry-run, `--yes`, selecting No (exit 0), Ctrl-C/Escape (exit 130), and EOF/non-TTY refusal or confirmed-plan drift (exit 1). Preserve the already required rollback/uncertainty exits and committed-warning/no-retry behavior.
- Apply plan-driven confirmation to destructive `link --force`; no-op Force Link does not prompt.
- Update the custom help template to render `Examples:`; Link ships its `--yes` help and completion changes here.
- Completion paths never invoke confirmation or runtime diagnostics.

### Milestone-specific tests

- Force Link no-op does not prompt/read stdin; destructive Force Link shows every replacement.
- Dry-run remains on stdout with empty prompt stderr; real preflight/Huh remains on stderr with clean stdout until success.
- Prompted Yes/No, `--yes`, Ctrl-C/Escape, EOF, non-TTY, drift, rollback, and committed-warning cases assert cumulative streams and exact exits from C3 in unit and built-binary tests.
- `--yes` and `--yes --dry-run` obey C2/C3.
- Drift detected after confirmation but before capture refuses without mutation. A capture mismatch instead attempts identity-bound no-replace restoration to the original anchored parent and name; verify successful restoration separately from collision/unprovable restoration that retains staging and reports uncertainty. Do not expect Dotty to undo external edits already present in the captured object.
- Structured remediation safely quotes every supported path.
- Existing automation migration has an exact-output regression.
- Per-caller semantic-plan parity tests cover Add, Track, Untrack, ordinary/Force Link, `link --track`, Unlink/Leave Copy, and `unlink --untrack`; required-case evidence lists every matched non-skipped case rather than only an aggregator name.

### Focused checks

```bash
mise run test:focused ./internal/dotty 'TestMilestone2Planning'
mise run test:focused ./internal/cli 'TestMilestone2Confirmation'
mise run verify
mise run vuln
```

### Acceptance/evidence

- Integrity review validates prompt/lock/replan/capture order.
- CLI review exercises real TTY and non-TTY flows.
- Dependency review records Huh and vulnerability evidence.
- Planner review proves every existing dry-run-capable mutating caller is migrated; Init is the only documented exception until Milestone 3.

## Milestone 3 — Deterministic Add and Init dry-run

### Goal

Ship the non-overwriting Add grammar/inference and complete the dry-run invariant.

### Implementation slice

- Parse `add TARGET PACKAGE[/SOURCE]` as placement grammar, not a general selector.
- Implement C7 type-based inference and explicit nested source validation.
- Keep ordinary Add non-overwriting and precisely classify exact tracked, exact untracked, unequal overlap, protected, and same-content destinations.
- Preserve in-place and symlink-adoption safety.
- Migrate Init to C2 and plan repository, Manifest, and config creation once for both dry-run and execution.
- Ship Add/Init `Use`, `Long`, `Example`, flags, exact output, release migration examples, and completion in this milestone.
- Do not suggest unshipped Force Add, Remove, or Prune remediation.

### Milestone-specific tests

```text
add ~/.zshrc zsh                 -> source .zshrc
add ~/.config/nvim nvim          -> source .
add ~/.config/nvim/lua nvim/lua  -> source lua
```

- Run each against absent, Empty, root-occupied, and nested-source Packages. An existing Empty Package may succeed at the newly inferred root; occupied/overlapping roots refuse with explicit old-layout migration commands.
- Symlink-to-directory infers `.`, symlink-to-file uses Target Path basename.
- Explicit traversal, trailing empty source, and `PACKAGE/.` refuse.
- Init dry-run missing/existing/invalid repository cases match real validation and write nothing.
- Help and completion cover positional counts, Package/source prefixes, and new flags without runtime diagnostics.

### Focused checks

```bash
mise run test:focused ./internal/dotty 'TestMilestone3AddInit'
mise run test:focused ./internal/cli 'TestMilestone3AddInitCLI'
mise run verify
mise run vuln
```

### Acceptance/evidence

- Compatibility, filesystem, and CLI reviewers approve inference, migration, output, and completions.
- Ordinary Add never overwrites or advertises unavailable force behavior.

## Milestone 4 — Remove

### Goal

Add mapping-first managed teardown without conflating Remove, Unlink, or Untrack.

### Implementation slice

- Implement C6 Remove grammar and normalization.
- Safe Remove restores selected targets, removes selected mappings, and removes only eligible Package Sources.
- `--purge` leaves selected Target Paths absent and prompts only when the computed plan is destructive.
- Unexpected regular content, wrong links, blocked targets, stale identities, or ambiguous root state abort atomically.
- Retain equal/shared or ancestor/descendant sources needed by remaining mappings and explain why.
- Safe Remove restores Source `.` by copying it to selected Target Paths. Both safe Remove and Purge retain the Package Root when remaining dependencies require it; Purge leaves selected Target Paths absent.
- Remove an Empty Package only when its root is empty and no Collection references it. Otherwise retain Package Root and Manifest metadata, report the exact content/Collections, and make no implicit metadata change.
- Ship complete help, examples contrasting Unlink/Untrack, exact output, and inventory-aware completions. Completion suggests selectors/Collections/mapped targets and suppresses incompatible choices after `--all`, `--collection`, multiple selectors, or invalid `--target` scope.

### Milestone-specific tests

- Expected/absent targets under safe and purge modes.
- Unexpected/wrong/blocked targets preserve all state.
- Shared equal and ancestor/descendant sources never widen selection.
- Root source with dependencies retains root; cleanup succeeds only for empty/no-Collection Packages. Root-nonempty and Collection-referenced cases assert exact retained filesystem/Manifest state and diagnostics.
- Purge of selected Source `.` mappings with a remaining shared dependency leaves selected Target Paths absent without copying, retains the Package Root and unselected mappings/targets, and reports the exact dependency preventing source deletion.
- Positional selectors, repeatable Collections, `--all`, and single-selector repeated `--target` obey C6; no selection and every invalid combination refuse; duplicate selection collapses.
- Failure after each target/source/mapping stage verifies reversal of Dotty's changes while preserving external edits rather than recreating the earlier planned state; rollback uncertainty and committed cleanup warning follow C4.
- Dry-run/`--yes`/No/TTY/non-TTY and completions follow shared contracts.

### Focused checks

```bash
mise run test:focused ./internal/dotty 'TestMilestone4Remove'
mise run test:focused ./internal/cli 'TestMilestone4RemoveCLI'
mise run verify
mise run vuln
```

### Acceptance/evidence

- Integrity review proves no unintended last-copy loss and exact rollback states. Safe Remove verifies target restoration before eligible source deletion; explicitly authorized selected-scope Purge may intentionally remove the last copy. Both retain the dependency/protected-content rules and never widen selection.
- Selector and UX reviews prove no implicit expansion and clear lifecycle distinctions.

## Milestone 5 — Prune

### Goal

Delete only classified Untracked Repository Content without touching targets or Manifest state.

### Implementation slice

- Implement exact C6 Prune grammar, atomic multi-path selection, recursion rules, and repository containment.
- Reuse C8 classification for Status, completion, and execution.
- Never follow parents outside the repository or delete tracked/protected equal/ancestor/descendant content.
- Prune leaves Target Paths and Manifest bytes/mode unchanged.
- Ship complete help, including the global `--repo PATH` plus local `--repo-wide` example, exact output, and state-aware completion across explicit, Package `--all`, and repository-wide scopes; incompatible alternatives are suppressed.

### Milestone-specific tests

- Multiple explicit files are deduplicated and pruned atomically.
- Explicit ancestor+descendant normalization never adds siblings and requires recursion for covered directories.
- Exact `Use`, flag cardinality/shorthands, cross-Package explicit paths, and explicit/Package/repository-wide forms reject every incompatible combination from C6.
- Non-empty directory refuses without `--recursive`; redundant recursion on files succeeds.
- Package scope cannot reach another Package/top-level content.
- Repository scope protects `dotty.toml`, `.git`, locks, temp/staging/backup artifacts at any depth, and all tracked dependencies.
- Source `.` content remains tracked.
- Symlink parents, parent swaps, prompt drift, stage failure, rollback failure, and cleanup warning preserve/report exact state.
- Completion excludes tracked/protected content and fails quietly on inventory errors.

### Focused checks

```bash
mise run test:focused ./internal/dotty 'TestMilestone5Prune'
mise run test:focused ./internal/cli 'TestMilestone5PruneCLI'
mise run verify
mise run vuln
```

### Acceptance/evidence

- Filesystem/status reviewers approve containment, no-follow classification, recursion, rollback, and performance evidence.
- CLI review approves `--repo-wide` clarity and large-plan behavior.

## Milestone 6 — Bounded Force Add

### Goal

Ship exact-source replacement only after Remove and Prune make every remediation actionable.

### Shared implementation

- Ordinary Add refuses first and suggests `--force --dry-run` only for eligible exact sources.
- Apply C7 selected-target exception: the chosen mapped target may be the identity-bound edited input; every other dependent target must be expected or absent.
- Preserve exact-source mappings and Collections.
- Use C1 capture/concurrency, C2 planning/confirmation, C3 I/O, C4 outcomes, C5 symlink equivalence, C5a metadata/hardlink fidelity, C5b cross-filesystem phases, C7 Add dependencies, and C8 protected-inventory rules.
- Ship Force Add help, flags, exact plan/result wording, automation notes, and force-aware completion with the relevant slice.

### Slice A — Exact non-root sources

- Replace exact untracked or exact tracked file/symlink sources and add the selected mapping only when absent.
- Reject unequal source overlaps and ineligible dependent target states.
- Test selected mapped recovery, unmapped adoption, shared dependents, duplicate-mapping refusal, internal hardlink preservation, external-alias refusal, relative/chained/dangling symlinks, metadata/file flags, same-inode drift, every applicable C5b `EXDEV` phase, rollback collision, negative native-capability paths, and committed cleanup warning. Inject failures at every applicable capture/install/Manifest/rollback/cleanup seam.

```bash
mise run test:focused ./internal/dotty 'TestMilestone6ForceAddExact'
mise run test:focused ./internal/cli 'TestMilestone6ForceAddExactCLI'
mise run verify
mise run vuln
```

**Slice A gate:** separate `mise run verify`/`mise run vuln`, independent integrity/UX/compatibility review, required-case manifest, and non-skipped Linux/Darwin runtime evidence before Slice B begins.

### Slice B — Directories and Package Root

- Require `--force --recursive` for non-empty trees.
- Allow Package Root replacement only for absent, Empty, or exact-root-only Packages.
- Reject nested non-root mappings.
- Preserve root mappings/Collections and add selected target mapping only when absent.
- Enumerate every replaced repository path and dependent Target Path.
- Test absent/Empty/root-only roots, nested-source and protected-descendant refusal, selected mapped directory input, tree metadata/file flags, relative-link chains, internal/external hardlink cases, descendant/parent drift, every C5b partial-tree `EXDEV` phase, rollback collision/failure, negative native-capability paths, and committed cleanup warnings.

```bash
mise run test:focused ./internal/dotty 'TestMilestone6ForceAddRoot'
mise run test:focused ./internal/cli 'TestMilestone6ForceAddRootCLI'
mise run verify
mise run vuln
```

**Slice B gate:** separate `mise run verify`/`mise run vuln`, required-case manifest, native runtime evidence, and fresh integrity/tree-relocation/UX/compatibility review distinct from Slice A.

### Program acceptance

- Every eligible/refused Force Add class has a named regression.
- Existing mappings/Collections remain exact.
- Plans list all affected paths, and no overlap, dependent conflict, or observed drift widens replacement.

## Milestone 7 — Empty Collection warnings and cross-command UX audit

### Goal

Finish warning semantics and audit consistency; do not defer feature-specific help or completion here.

### Implementation slice

- Keep Empty Collections valid.
- Show successful warnings in `status` and `list` on stderr with exit zero.
- Keep Empty Package and Empty Collection terminology distinct.
- Audit lifecycle comparison text, styles, diagnostics, prompt wording, and completion behavior across shipped commands.
- Synchronize final README command matrix and examples without introducing new semantics.

### Milestone-specific tests

- Empty Collection warnings preserve normal stdout data and exit zero.
- Exact ANSI/no-border rendering remains intact.
- Help examples use only shipped syntax and distinguish Add/Track/Link/Remove/Unlink/Untrack/Prune.
- All generated shells and repository overrides remain valid.
- Completion never prompts, mutates, or leaks runtime diagnostics.

```bash
mise run test:focused ./internal/dotty 'TestMilestone7Warnings'
mise run test:focused ./internal/cli 'TestMilestone7UXAudit'
mise run verify
```

Run `mise run vuln` if dependencies or security-sensitive scanning change.

### Acceptance/evidence

- CLI UX, documentation, and regression reviews find no contract drift.
- README, `CONTEXT.md`, help, completion, and runtime behavior agree.

## Deferred scope

- Package or Package Source rename/move.
- Link Mapping source/target retargeting.
- Collection CRUD.
- Default Repository unset, Manifest teardown, or complete repository teardown.
- Package Root replacement with nested non-root sources or general `replace-package` behavior.
- Force as an override for unsupported content, unsafe topology, or stale identity.
- Full-screen UI.
- Crash-resistant journaling, snapshots, or crash-proof transactions.
- Third-party Manifest extensions.

Deferred work requires a new product decision and contract update; no milestone may absorb it incidentally.

## Known risks

- Uncooperative external writers and open descriptors cannot be fully excluded.
- Platform-specific capture/no-replace behavior needs maintained Linux/Darwin evidence.
- Large recursive plans intentionally produce large output because every path is shown.
- Strict TOML decoding may expose previously ignored invalid data.
- Prompting destructive Force Link intentionally breaks scripts that omit `--yes`.
- Existing Manifest normalization still does not preserve comments or hand formatting.

## Status and evidence

Update Status during execution. Populate Evidence only after the acceptance gate passes; link the accepted commit/PR, required-case matches, `verify`/`vuln` results, reviews, native platform evidence, and residual risks.

| Milestone | Status | Evidence |
| --- | --- | --- |
| 0. Governance and normative contracts | Accepted (documentation only) | [Receipt](#milestone-0-acceptance-receipt) |
| 1. Persistence/filesystem foundations | In progress; focused-test harness, strict TOML persistence, additive C4 vocabulary/final CLI transport, and production-unused native-handle boundary accepted; remaining foundations and all caller migrations pending | [Harness receipt](#milestone-1-first-focused-test-harness-acceptance-receipt); [strict persistence receipt](#milestone-1-strict-toml-persistence-acceptance-receipt); [C4 vocabulary/transport receipt](#milestone-1-c4-vocabulary-and-transport-acceptance-receipt); [native boundary receipt](#milestone-1-native-handle-boundary-acceptance-receipt) (Darwin/arm64 and Linux/arm64); full M1 and broader native filesystem gates pending |
| 2. Planning, diagnostics, confirmation | Planned | — |
| 3. Deterministic Add and Init dry-run | Planned | — |
| 4. Remove | Planned | — |
| 5. Prune | Planned | — |
| 6A. Force Add exact sources | Planned | — |
| 6B. Force Add directories/root | Planned | — |
| 7. Empty Collection warnings/UX audit | Planned | — |

### Milestone 0 acceptance receipt

- Reviewed baseline: `7eaec6c4e1f54e39fa2b8ef87494a0512bff964f`. Independent oracle `1f7115f7-f38c-4667-a636-688462a3aaca` reviewed the full baseline: READY, no documentation blocker.
- Parent verification `proc_e7df` at that baseline: `env MISE_GO_VERSION=1.26.6 GOTOOLCHAIN=local mise run verify` SUCCEEDED, exit 0 after 5s; fmt:check, lint, vet, tests, and build passed. No lifecycle runtime or native-platform evidence is claimed.
- Provenance only: earlier oracle `0883364b` was BLOCKED; its two wording defects were corrected in `7eaec6c`. `proc_0a3d` passed before those edits; `proc_aae2` failed lint with default Homebrew Go 1.27.1 versus linter build Go 1.26.2. Mise does not pin Go; the passing override was process-local, not a repaired vanilla environment.
- Only the first focused-test harness slice is authorized; independent review and full `mise run verify` must pass before strict TOML persistence. Neither slice nor Milestone 1 is accepted here.

### Milestone 1 FIRST focused-test harness acceptance receipt

- Accepted scope: FIRST focused-test harness prerequisite only, substantive commit `2669cac5cd51bf8a20f30dd61258506c97fd096f`; at that acceptance, code was unchanged since the passing checks below. The later ACK correction is recorded in the native-handle boundary receipt. Runtime lifecycle commands are unchanged. This is not full Milestone 1 acceptance.
- Review sequence: fresh full-source reviewer `e23aa3fb9a954af6b462e3f3066e02f6` initially returned structural PASS, but runtime tests then exposed a build-cancellation regression; that initial review alone did not accept the slice. Final narrow independent reviewer `37a091b0-1d21-40b6-839f-9b788c9c964b` returned PASS on the evidence-based Bash 3.2 fix, resolving the old runtime blocker structurally.
- Parent `proc_ecc7` SUCCEEDED, exit 0 after 94s, using `/private/tmp/dotty-checks.SwvUbuw3/launch` for `fmt` → `diagnostic` → `verify`. Actual tasks: `mise run fmt`, `mise run test:focused ./internal/tools/focused 'TestFocusedCancellation/wrapper_build|TestFocusedTaskProtocol/completion'`, and `mise run verify`. The diagnostic explicitly passed INT/TERM build cancellation and completion tests. Required fmt:check, lint (0 issues), vet, all tests, and build passed; full focused-package tests took 70.285s.
- Parent `proc_aa73` ran `launch vuln` (`mise run vuln`): SUCCEEDED, exit 0 after 5s. No reachable/code/imported-package vulnerabilities were found. The scanner also reported one advisory at the required-module level, with no affected imported packages or reachable calls identified; this is not a claim of zero advisories globally.
- All final checks used an `env -i` allowlist with private HOME, XDG, DOTTY_REPO, TMPDIR, Go and mise state/cache, installed Go 1.26.6, `GOTOOLCHAIN=local`, and PATH excluding installed Dotty. Launcher review `b70b6cd8` returned PASS before the fixed vuln-only action was added. This protects against accidental live Dotty resolution; it is not a filesystem sandbox. The private launcher and artifacts are retained locally, not committed or claimed portable. Earlier pre-clean-environment failures are diagnostic history, not current acceptance evidence.
- Boundaries: native execution evidence is current Darwin with `/bin/bash` 3.2, not Linux. Linux and full native filesystem gates remain pending. Go remains unpinned in mise; clean-environment process selection is a workaround, not a toolchain configuration fix. Milestone 0 remains accepted at `7eaec6c`; strict TOML and all runtime integrity/command slices are not started; Milestones 2–7 remain planned. The next bounded strict TOML slice requires separate implementation/review/verify/vuln and is neither authorized nor started by this receipt.

### Milestone 1 strict TOML persistence acceptance receipt

- Accepted strict-code commit: `854a4165951cbdc8b812f7b28ccc6584b6636e2b`. Scope: unknown-field and invalid UTF-8 refusal, TOML basic-string encoding, existing-document preflight, and preservation of existing rewrite permission/special bits. Full independent review `27586e73`: PASS; fixture-only independent review `b227b498`: PASS.
- Parent clean-environment process `proc_9adc` SUCCEEDED after 12s through `/private/tmp/dotty-strict-checks.syochCeQ/launch`: `mise run fmt` → `mise run verify` → `mise run vuln`. Format check, lint (0 issues), vet, all tests, and build passed. Govulncheck found no affected code, imported packages, or reachable calls; it reported one required-module advisory. This does not mean the required module itself is unimported or that no advisory exists.
- Earlier `proc_71de` failed during setgid fixture setup, before `WriteFileTx`. The fixture's test-root group was corrected without skips or weakened assertions; production code was unchanged. Fresh passing core tests include the mode cases. The earlier failure is diagnostic history, not passing evidence.
- Final checks used an `env -i` allowlist with private HOME, XDG, DOTTY_REPO, temporary directories, cache, and state, installed Go 1.26.6, and no installed Dotty on PATH. This is environment isolation, not a security sandbox. Native execution evidence is Darwin only; no Linux or full Milestone 1 acceptance is claimed.
- Boundaries: strict persistence does not establish anchored safety, ownership/relocation fidelity, or crash durability. Native filesystem gates, anchored mutations/persistence, typed outcomes, locks, protected inventory, metadata/hardlink/relative-link fidelity, EXDEV handling, and existing caller migrations remain pending. Current authorization covers the full remaining plan, with next foundation planning governed by Milestone 1 above and its existing ADRs, not old-worktree code. Milestones 2–7 remain planned; semantic decisions are unchanged.

### Milestone 1 C4 vocabulary and transport acceptance receipt

- Accepted code commit: `b4b89802f3853e8d4d8ea7790dfdc5e846747664`, for additive C4 vocabulary and final CLI transport only. Success is data-only within the four-outcome vocabulary; pre-mutation refusal is separate. Malformed error graphs fail closed with available quoted metadata. Typed reports are producer claims, not filesystem verification.
- Review sequence: initial reviewer `cc7023f0` found cycles, lost causes, and nil multi-error issues; subsequent reviewer `1aa138c0` found terminal/golden issues. All were fixed; final narrow independent review `952ed4b9` returned PASS. Earlier reviews are diagnostic history, not passing acceptance evidence.
- Parent clean-environment process `proc_2f9b` SUCCEEDED after 12s through `/private/tmp/dotty-outcome-checks.HJyyrveV/launch`: `mise run fmt` → `mise run verify` → `mise run vuln`. Format check, lint (0 issues), vet, all tests, and build passed. Govulncheck reported no affected imported packages or reachable calls and one required-module advisory; this is not a claim of zero advisories.
- Checks used private HOME, XDG, DOTTY_REPO, and cache, Go 1.26.6, and PATH without installed Dotty. This is clean-environment isolation, not a filesystem sandbox. The local launcher is evidence provenance, not a committed portable helper. Execution evidence is Darwin only. Linux infrastructure attempts did not pass project tests and supply no Linux C4 acceptance evidence.
- Boundaries: legacy `RunAtomic`/`Tx`, locks, services, and caller classification are unchanged. There is no production verified-restoration producer and no promotion of legacy cleanup errors into committed cleanup warnings. Synthetic entrypoint tests demonstrate transport, not real-filesystem restoration or cleanup evidence. This receipt does not accept typed transaction migration or full Milestone 1.
- At this receipt's acceptance, remaining Milestone 1 work included native handles, user coordination, anchored observations, artifact authority, the protected-inventory classifier, relocation fidelity, EXDEV handling, all caller migrations, and Darwin/Linux native filesystem gates; the next bounded primitive was the native-handle boundary per oracle `c7d421a3`. That next-slice status is historical, superseded by the native receipt below. Full-plan authorization continues in prerequisite order. This receipt changes no semantics, source, helpers, or runtime behavior and implements no next slice.

### Milestone 1 native-handle boundary acceptance receipt

- Accepted exact candidate: `8c5104ce422bacb5c1566a35ad2ef22be5332a4f`, including `85af381` (focused-harness ACK correction), `59bf875` (native boundary), and `8c5104c` (unsupported-target compile coverage). Independent source reviews `d4bcac3a` (native), `61cd9f71` (compile safeguards), and `b89a474a` (ACK correction): PASS. The earlier harness receipt remains historical evidence; the later ACK correction is included in this accepted candidate. Unrelated tests and typed C4 producer behavior remain unchanged.
- Accepted scope only: opaque validated components, physical no-follow directory pinning and device/inode/kind identity observations, operation leases and close-once lifetime, and native exclusive rename. `internal/nativefs` remains **unused by production**. No user lock, full snapshot/artifact authority, ACL/relocation fidelity, EXDEV engine, capture/rollback, caller migration, or full Milestone 1 acceptance. The final observation/syscall race remains best-effort, not inode compare-and-swap.
- Durable evidence root **R**: `/Users/milo/Worktrees/dotty/agent-validation/native-8c5104c-run2-028d44efd602ed77`. Final saved-evidence audit `c2774145`: PASS, recorded in the local, uncommitted artifact `R/records/native-boundary-acceptance-audit.md`. It independently confirms all 113 included source-file hashes/modes unchanged; sole extra `source/dotty` is retained expected build output. Original source/input-artifact bindings remain intact; the normal guard was neither rerun post-build nor weakened. Audit hashes establish local integrity, not independent Git authenticity; source reviews and host outer exit statuses/timings are parent-supplied provenance, not newly repeated by the audit.
- Native required cases: the same exact 40 leaves each RUN/PASS/`executed:` once in each platform's focused run and RUN/PASS once in each full verify, with no FAIL/SKIP. The complete inventory is in `R/records/linux-native-candidate-review.md`; the final audit confirms the matching Darwin set. Cases cover path/component refusals, no-follow ancestry and identity swaps, exclusive-rename collisions/topology, leases/close ownership, cleanup, and fail-closed capabilities. Capability/EXDEV cases use injected errors, and FD-zero ownership is simulated: no native cross-filesystem, missing-capability filesystem, or real-stdin claim.
- Linux RUN2 `proc_ade4`: PASS, approximately 90s; Go 1.26.6, Linux/arm64, kernel `7.0.12-linuxkit`, mise 2026.9.1; image `sha256:553b171cfad8288c9d230a5dbdf992a140f77551242344b19ac450ccae66259a`. Focused command: `mise run test:focused ./internal/nativefs '^TestNativeHandleBoundary$/.'`; full gate: `GOFLAGS="-v -count=1" mise run verify`. Full suite: 1,003 RUN/PASS nodes across five packages, including parents/subtests/fuzz seeds, no FAIL/SKIP, not 1,003 unique leaves. Formatting, lint (0 issues), vet, tests, and build passed. Runtime was isolated, nonroot, offline, read-only-root with writable private workspace/state volumes, no host binds, dropped capabilities and no-new-privileges; acquisition networking was separate.
- Fresh Darwin host sequence: `proc_89f5` environment preflight → `proc_b808` explicit private acquisition → `proc_fdfe` `mise run fmt:check` → `proc_1dc1` the same exact40 focused command → `proc_2103` `mise run vuln` → **last**, `proc_bb93` `GOFLAGS="-v -count=1" mise run verify`: all PASS; final verify approximately 116s. Full suite: the same 1,003 RUN/PASS nodes, no FAIL/SKIP; formatting, lint (0 issues), vet, tests, and build passed. Pinned Go 1.26.6, Darwin/arm64 kernel 25.3.0, host mise 2026.9.3; `env -i` with private HOME/XDG/DOTTY_REPO/caches and no installed Dotty on PATH. This is environment isolation, not a security sandbox. Govulncheck: zero affected imported packages/reachable vulnerabilities, one required-module advisory; not zero advisories globally. No separate Linux vulnerability scan is established.
- Foreign coverage is **compile-only**: maintained FreeBSD/amd64 and Windows/amd64 cases pass on both hosts. Output-cap and inherited-environment regressions pass. No foreign runtime evidence is claimed; successful nested compiler streams are not exported in full.
- Evidence collection `proc_18a3` and owned cleanup `proc_829a`: PASS. Final audit records the specific saved receipt for removal of journaled RUN2 containers/volumes, not a fresh daemon probe or global-daemon-emptiness claim. Images, shared/host caches, source, build output, and evidence remain intentionally retained. The earlier Linux review's pending host/cleanup statements are historical and superseded by this final audit. Failed kits, refusals, and repeated log collection are not additional acceptance runs.
- Next bounded step remains native authority/file/flock extensions plus the canonical coordinator, initially production-unused, per oracle `cd94ac45`; dependent production integration requires release-outcome and isolation readiness. Use only private adversarial anchors, never the host canonical anchor. VM execution is not approved; later unmodified Darwin CLI coverage awaits an explicit strategy. Full remaining-plan authorization continues, without changing semantics or implementing the next slice here.

## Completion definition

The lifecycle program is complete only when milestones pass in order; compatibility items have migration evidence; focused matches, `mise run verify`, and required `mise run vuln` checks pass; fresh integrity/test/UX reviews have no blocker; final docs/help/completion/runtime agree; and deferred scope was not implemented implicitly.
