//go:build linux || (darwin && cgo)

package nativefs

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

type coordinatorRepository struct {
	binding  RepositoryBinding
	dir      *Dir
	baseline []AuthorityFacts
	lease    *Lease
}

type coordinatorState struct {
	// Immutable after AcquireUser; resources are not exposed to callers.
	root      *Dir
	anchor    *Dir
	user      *Lease
	userFacts AuthorityFacts
	baseline  []AuthorityFacts
	records   []CreationRecord
	calls     flockCalls
	path      string

	// Only the winning batch changes resources. Release waits for batchDone
	// before accessing them; cleanup never calls Release or waits for itself.
	repositories []coordinatorRepository
	cleanupOnce  sync.Once
	cleanupErr   error

	mu             sync.Mutex
	batchStarted   bool
	batchRunning   bool
	batchContext   context.Context
	batchCancel    context.CancelFunc
	batchDone      chan struct{}
	bindings       []RepositoryBinding
	closing        bool
	releaseStarted bool
	releaseDone    chan struct{}
	releaseErr     error
}

func canonicalUserLockPath() string {
	return entryPath(
		entryPath(systemTemporaryRoot, userAnchorName()),
		Component{name: "mutation.lock"},
	)
}

func userAnchorName() Component {
	return Component{name: "dotty-" + strconv.FormatUint(uint64(uint32(unix.Geteuid())), 10)}
}

// AcquireUser establishes the fixed platform temporary-root/effective-UID lock,
// independently of environment, config and repository choices. Full native
// authority is required before creation. Anchors are never repaired or removed.
func AcquireUser(ctx context.Context) (*UserLease, error) {
	return acquireUserAt(ctx, systemTemporaryRoot, systemFlockCalls())
}

func zeroCreationRecord(record CreationRecord) bool {
	return record.path == "" && record.kind == 0 && !record.created && record.observations == nil
}

// Only the exact exclusive-create syscall failure permits a separate existing
// open. A wrapped/joined errno, extra diagnostic field or partial record refuses.
func exactCreationCollision(
	err error,
	record CreationRecord,
	op, path string,
	name Component,
) bool {
	e, ok := err.(*Error) //nolint:errorlint // Exact diagnostic node, never a matching wrapped descendant.
	return ok && e != nil && zeroCreationRecord(record) && e.Op == op && e.Path == path &&
		e.Component == name.name && e.Reason == ReasonExists && e.Err == unix.EEXIST && //nolint:errorlint // Require the sole raw syscall errno, never joined-cause membership.
		e.SourcePath == "" && e.DestinationPath == "" && e.OtherComponent == "" &&
		e.Expected == (Identity{}) && e.Observed == (Identity{})
}

// Per-call private root/security seams follow the primitive test boundary.
// The public entry always supplies the fixed root and full native observers;
// only _test.go callers can supply a bounded private fixture adapter.
func acquireUserAt(
	ctx context.Context,
	rootPath string,
	calls flockCalls,
) (result *UserLease, resultErr error) {
	name, lockName := userAnchorName(), Component{name: "mutation.lock"}
	anchorPath := entryPath(rootPath, name)
	s := &coordinatorState{
		calls:       calls,
		path:        entryPath(anchorPath, lockName),
		releaseDone: make(chan struct{}),
	}
	var bootstrap *File
	defer func() {
		// Bootstrap retains its original parent token until comparison, including
		// failures. Close it before releasing the user and closing the anchor.
		if bootstrap != nil {
			resultErr = errors.Join(resultErr, bootstrap.Close())
		}
		if result == nil {
			resultErr = errors.Join(resultErr, s.cleanup())
			resultErr = coordinatorError("", s.path, s.records, resultErr)
		}
	}()
	if err := flockContextError(ctx, s.path); err != nil {
		return nil, err
	}
	var err error
	s.root, err = openPhysicalDir(rootPath, calls.private.acquisition)
	if err != nil {
		return nil, err
	}
	if _, err := privateParent(s.root.state, RoleSystemAncestor, calls.private); err != nil {
		return nil, err
	}
	var record CreationRecord
	s.anchor, record, err = s.root.privateDir(name, true, calls.private)
	if !zeroCreationRecord(record) {
		s.records = append(s.records, record)
	}
	if exactCreationCollision(err, record, "create-private-directory", anchorPath, name) {
		s.anchor, _, err = s.root.privateDir(name, false, calls.private)
	}
	if err != nil {
		return nil, err
	}
	bootstrap, record, err = s.anchor.lockFile(lockName, true, calls.private)
	if !zeroCreationRecord(record) {
		s.records = append(s.records, record)
	}
	if exactCreationCollision(err, record, "open-lock-file", s.path, lockName) {
		bootstrap, _, err = s.anchor.lockFile(lockName, false, calls.private)
	}
	if err != nil {
		return nil, err
	}
	s.userFacts = bootstrap.state.authority
	// Creation transitions are over. These strict full-ancestry facts survive
	// the independent flock wait and every subsequent repository wait.
	s.baseline, err = privateParent(s.anchor.state, RolePrivateAnchor, calls.private)
	if err != nil {
		return nil, err
	}
	s.user, err = s.anchor.lockFileLease(ctx, lockName, calls)
	if err != nil {
		return nil, err
	}
	// Both descriptions must still be bound to the bootstrap inode and full
	// authority baseline. Merely acquiring another valid rendezvous is not enough.
	if err := s.validateUserFD(bootstrap.state.fd); err != nil {
		return nil, err
	}
	if err := s.validateUser(); err != nil {
		return nil, err
	}
	err = bootstrap.Close()
	bootstrap = nil // Never retry an ambiguous native close.
	if err != nil {
		return nil, err
	}
	if err := flockContextError(ctx, s.path); err != nil {
		return nil, err
	}
	return &UserLease{state: s}, nil
}

func (s *coordinatorState) validateUserFD(fd int) error {
	if err := s.root.state.guard("coordinator-root"); err != nil {
		return err
	}
	node := pinnedDir{
		fd:       fd,
		path:     s.path,
		name:     Component{name: "mutation.lock"},
		identity: s.userFacts.identity,
	}
	for observation := 0; observation < 2; observation++ {
		facts, err := boundFile(
			s.anchor.state,
			node,
			s.baseline,
			s.calls.private,
			&CreationRecord{},
		)
		if err != nil {
			return err
		}
		if err := validateAuthority(
			s.path,
			facts,
			RoleLockFile,
			uint32(unix.Geteuid()),
		); err != nil {
			return err
		}
		if err := compareAuthorityNodes(
			[]pinnedDir{node},
			RoleLockFile,
			[]AuthorityFacts{s.userFacts},
			[]AuthorityFacts{facts},
		); err != nil {
			return err
		}
	}
	if err := s.root.state.guard("coordinator-root"); err != nil {
		return err
	}
	return nil
}

func (s *coordinatorState) validateUser() error { return s.validateUserFD(s.user.state.fd) }

// CreationRecords returns immutable bootstrap evidence, also after Release.
func (u *UserLease) CreationRecords() []CreationRecord {
	if u == nil || u.state == nil {
		return nil
	}
	return slices.Clone(u.state.records)
}

// RepositoryBindings returns a copy in original logical-selection order. It is
// empty until successful publication and is evidence, not an owned-handle API.
func (u *UserLease) RepositoryBindings() []RepositoryBinding {
	if u == nil || u.state == nil {
		return nil
	}
	s := u.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.bindings)
}

func resolveRepository(logical string) (string, error) {
	if !filepath.IsAbs(logical) || strings.ContainsRune(logical, '\x00') {
		return "", &Error{Op: "resolve-repository", Path: logical, Reason: ReasonInvalidPath}
	}
	// EvalSymlinks must see the original spelling, especially symlink/.. .
	physical, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return "", err
	}
	if _, err := physicalComponents(physical); err != nil {
		return "", err
	}
	return physical, nil
}

// Return first-selection indices in physical byte order. There is deliberately
// no topology-equivalence prover for distinct physical paths sharing an inode.
func orderedRepositories(bindings []RepositoryBinding) ([]int, error) {
	paths := make(map[string]int)
	identities := make(map[Identity]int)
	var order []int
	for i, binding := range bindings {
		if prior, ok := paths[binding.physical]; ok {
			if bindings[prior].identity != binding.identity {
				return nil, coordinatorError(
					binding.logical,
					binding.physical,
					nil,
					mismatch(
						"repository-batch",
						binding.physical,
						Component{},
						bindings[prior].identity,
						binding.identity,
					),
				)
			}
			continue
		}
		if prior, ok := identities[binding.identity]; ok {
			return nil, coordinatorError(binding.logical, binding.physical, nil, &Error{
				Op:              "repository-batch",
				Path:            binding.physical,
				SourcePath:      bindings[prior].physical,
				DestinationPath: binding.physical,
				Reason:          ReasonTopology,
				Expected:        bindings[prior].identity,
				Observed:        binding.identity,
			})
		}
		paths[binding.physical], identities[binding.identity] = i, i
		order = append(order, i)
	}
	slices.SortFunc(
		order,
		func(a, b int) int { return strings.Compare(bindings[a].physical, bindings[b].physical) },
	)
	return order, nil
}

func validateRepository(repo coordinatorRepository, calls privateCalls) error {
	facts, err := privateParent(repo.dir.state, RoleRepositoryDirectory, calls)
	if err != nil {
		return err
	}
	return compareAuthorityNodes(
		repo.dir.state.nodes,
		RoleRepositoryDirectory,
		repo.baseline,
		facts,
	)
}

func validateAlias(binding RepositoryBinding) error {
	physical, err := resolveRepository(binding.logical)
	if err == nil && physical != binding.physical {
		err = &Error{
			Op:              "resolve-repository",
			Path:            binding.logical,
			SourcePath:      binding.physical,
			DestinationPath: physical,
			Reason:          ReasonTopology,
			Expected:        binding.identity,
		}
	}
	return coordinatorError(binding.logical, binding.physical, nil, err)
}

func (s *coordinatorState) lockRepositories(
	ctx context.Context,
	logicalPaths []string,
) ([]RepositoryBinding, error) {
	if err := flockContextError(ctx, s.path); err != nil {
		return nil, err
	}
	// Check between synchronous filesystem operations: an in-flight native
	// call or alias resolution is not interruptible and has no hard I/O deadline.
	checkContext := func(logical, physical string) error {
		path := physical
		if path == "" {
			path = logical
		}
		return coordinatorError(logical, physical, nil, flockContextError(ctx, path))
	}
	bindings := make([]RepositoryBinding, 0, len(logicalPaths))
	byPath := make(map[string]int)
	for _, logical := range logicalPaths {
		if err := checkContext(logical, ""); err != nil {
			return nil, err
		}
		physical, err := resolveRepository(logical)
		if err != nil {
			return nil, coordinatorError(logical, physical, nil, err)
		}
		if err := checkContext(logical, physical); err != nil {
			return nil, err
		}
		binding := RepositoryBinding{logical: logical, physical: physical}
		if prior, ok := byPath[physical]; ok {
			repo := s.repositories[prior]
			if err := validateRepository(repo, s.calls.private); err != nil {
				return nil, coordinatorError(logical, physical, nil, err)
			}
			binding.identity = repo.binding.identity
		} else {
			if err := checkContext(logical, physical); err != nil {
				return nil, err
			}
			dir, err := openPhysicalDir(physical, s.calls.private.acquisition)
			if err != nil {
				return nil, coordinatorError(logical, physical, nil, err)
			}
			binding.identity = dir.state.leaf().identity
			// Own the pin immediately, even when the first authority observation fails.
			byPath[physical] = len(s.repositories)
			s.repositories = append(
				s.repositories,
				coordinatorRepository{binding: binding, dir: dir},
			)
			repo := &s.repositories[len(s.repositories)-1]
			if err := checkContext(logical, physical); err != nil {
				return nil, err
			}
			repo.baseline, err = privateParent(dir.state, RoleRepositoryDirectory, s.calls.private)
			if err != nil {
				return nil, coordinatorError(logical, physical, nil, err)
			}
		}
		if err := checkContext(logical, physical); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	order, err := orderedRepositories(bindings)
	if err != nil {
		return nil, err
	}
	ordered := make([]coordinatorRepository, 0, len(order))
	for _, index := range order {
		ordered = append(ordered, s.repositories[byPath[bindings[index].physical]])
	}
	s.repositories = ordered
	for i := range s.repositories {
		repo := &s.repositories[i]
		for _, binding := range bindings {
			if err := checkContext(binding.logical, binding.physical); err != nil {
				return nil, err
			}
			if binding.physical == repo.binding.physical {
				if err := validateAlias(binding); err != nil {
					return nil, err
				}
				if err := checkContext(binding.logical, binding.physical); err != nil {
					return nil, err
				}
			}
		}
		if err := checkContext(repo.binding.logical, repo.binding.physical); err != nil {
			return nil, err
		}
		if err := validateRepository(*repo, s.calls.private); err != nil {
			return nil, coordinatorError(repo.binding.logical, repo.binding.physical, nil, err)
		}
		if err := checkContext(repo.binding.logical, repo.binding.physical); err != nil {
			return nil, err
		}
		var err error
		repo.lease, err = repo.dir.lockDirectory(ctx, s.calls)
		if err != nil {
			return nil, coordinatorError(repo.binding.logical, repo.binding.physical, nil, err)
		}
		for _, binding := range bindings {
			if err := checkContext(binding.logical, binding.physical); err != nil {
				return nil, err
			}
			if binding.physical == repo.binding.physical {
				if err := validateAlias(binding); err != nil {
					return nil, err
				}
				if err := checkContext(binding.logical, binding.physical); err != nil {
					return nil, err
				}
			}
		}
	}
	// After the LAST wait, recheck every alias, the user rendezvous/root/anchor,
	// and every retained repository. Empty batches still check user authority.
	for _, binding := range bindings {
		if err := checkContext(binding.logical, binding.physical); err != nil {
			return nil, err
		}
		if err := validateAlias(binding); err != nil {
			return nil, err
		}
		if err := checkContext(binding.logical, binding.physical); err != nil {
			return nil, err
		}
	}
	if err := flockContextError(ctx, s.path); err != nil {
		return nil, err
	}
	if err := s.validateUser(); err != nil {
		return nil, err
	}
	if err := flockContextError(ctx, s.path); err != nil {
		return nil, err
	}
	for _, repo := range s.repositories {
		if err := checkContext(repo.binding.logical, repo.binding.physical); err != nil {
			return nil, err
		}
		if err := validateRepository(repo, s.calls.private); err != nil {
			return nil, coordinatorError(repo.binding.logical, repo.binding.physical, nil, err)
		}
		if err := checkContext(repo.binding.logical, repo.binding.physical); err != nil {
			return nil, err
		}
	}
	return bindings, nil
}

// LockRepositories accepts exactly one complete existing-directory batch,
// including empty. Concurrent losing attempts cannot cancel the winner. A
// winning failure is terminal and releases all resources before returning.
func (u *UserLease) LockRepositories(ctx context.Context, absoluteLogicalPaths []string) error {
	if u == nil || u.state == nil {
		return coordinatorError("", "", nil, &Error{Op: "repository-batch", Reason: ReasonClosed})
	}
	s := u.state
	s.mu.Lock()
	if s.closing || s.batchStarted {
		s.mu.Unlock()
		return coordinatorError(
			"",
			s.path,
			s.records,
			&Error{Op: "repository-batch", Path: s.path, Reason: ReasonInvalidOperation},
		)
	}
	s.batchStarted, s.batchRunning = true, true
	s.batchDone = make(chan struct{})
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	s.batchContext, s.batchCancel = context.WithCancel(parent)
	batchCtx := s.batchContext
	s.mu.Unlock()
	defer s.batchCancel()
	var bindings []RepositoryBinding
	err := flockContextError(ctx, s.path)
	if err == nil {
		bindings, err = s.lockRepositories(batchCtx, slices.Clone(absoluteLogicalPaths))
	}
	s.mu.Lock()
	if err == nil {
		err = flockContextError(batchCtx, s.path)
		if s.closing && err == nil {
			err = &Error{Op: "repository-batch", Path: s.path, Reason: ReasonClosed}
		}
	}
	if err == nil {
		s.bindings = bindings
	} else {
		s.closing = true
	}
	s.mu.Unlock()
	if err != nil {
		err = coordinatorError("", s.path, s.records, errors.Join(err, s.cleanup()))
	}
	s.mu.Lock()
	s.batchRunning = false
	close(s.batchDone)
	s.mu.Unlock()
	return err
}

func (s *coordinatorState) cleanup() error {
	s.cleanupOnce.Do(func() {
		var errs []error
		for i := len(s.repositories) - 1; i >= 0; i-- {
			repo := s.repositories[i]
			errs = append(
				errs,
				coordinatorError(
					repo.binding.logical,
					repo.binding.physical,
					nil,
					repo.lease.Release(),
				),
			)
		}
		for i := len(s.repositories) - 1; i >= 0; i-- {
			repo := s.repositories[i]
			errs = append(
				errs,
				coordinatorError(
					repo.binding.logical,
					repo.binding.physical,
					nil,
					repo.dir.Close(),
				),
			)
		}
		errs = append(errs, s.user.Release())
		if s.anchor != nil {
			errs = append(errs, s.anchor.Close())
		}
		if s.root != nil {
			errs = append(errs, s.root.Close())
		}
		s.cleanupErr = errors.Join(errs...)
	})
	return s.cleanupErr
}

// Release serializes with publication, cancels pending work and waits outside
// the mutex. Every dependent release precedes pin/anchor closure; no parent is
// reacquired and neither close nor unlock is retried after an ambiguous failure.
func (u *UserLease) Release() error {
	if u == nil || u.state == nil {
		return nil
	}
	s := u.state
	s.mu.Lock()
	if s.releaseStarted {
		done := s.releaseDone
		s.mu.Unlock()
		<-done
		return s.releaseErr
	}
	s.releaseStarted, s.closing = true, true
	cancel := s.batchCancel
	var done chan struct{}
	if s.batchRunning {
		done = s.batchDone
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	err := coordinatorError("", s.path, s.records, s.cleanup())
	s.mu.Lock()
	s.releaseErr = err
	close(s.releaseDone)
	s.mu.Unlock()
	return err
}
