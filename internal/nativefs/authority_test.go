//go:build linux || (darwin && cgo)

package nativefs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

// Declared before implementation; future 31b/31c inventories remain in the plan.
// These leaf names must all complete on each native supported security backend.
var requiredAuthorityCases = []string{
	"ZeroEvidenceInvalid",
	"IdentityNotAuthority",
	"DescriptorFactsUIDGIDModeSpecialNlink",
	"RolePolicyMatrix",
	"ACLAbsentVerified",
	"ACLPresentRefused",
	"ACLReadFailureRefused",
	"FilesystemMountUnknownRefused",
	"SecurityDriftSameInode",
	"FullIdentityTopologyAncestry",
	"PrivateSecurityBoundaryOnly",
	"UnboundedProductionPolicyRefusal",
}

func runAuthorityInventory(t *testing.T, inventory []string, cases map[string]func(*testing.T)) {
	t.Helper()
	if len(inventory) != len(cases) {
		t.Fatalf(
			"authority inventory mismatch: %d required, %d implemented",
			len(inventory),
			len(cases),
		)
	}
	seen := map[string]bool{}
	for _, name := range inventory {
		fn, ok := cases[name]
		if !ok || seen[name] {
			t.Fatalf("missing or duplicate authority case %q", name)
		}
		seen[name] = true
		completed := false
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() {
				if t.Skipped() {
					t.Errorf("required authority case skipped: %s", name)
				}
			})
			fn(t)
			completed = true
		})
		if !completed {
			t.Errorf("required authority case missing, filtered, or incomplete: %s", name)
		}
	}
}

func TestAuthorityBoundary(t *testing.T) {
	runAuthorityInventory(t, requiredAuthorityCases, map[string]func(*testing.T){
		"ZeroEvidenceInvalid":                   testAuthorityZero,
		"IdentityNotAuthority":                  testIdentityNotAuthority,
		"DescriptorFactsUIDGIDModeSpecialNlink": testAuthorityDescriptorFacts,
		"RolePolicyMatrix":                      testAuthorityRoles,
		"ACLAbsentVerified":                     testAuthorityAbsent,
		"ACLPresentRefused":                     testAuthorityPresent,
		"ACLReadFailureRefused":                 testAuthorityReadFailure,
		"FilesystemMountUnknownRefused":         testAuthorityFilesystemFailure,
		"SecurityDriftSameInode":                testAuthoritySecurityDrift,
		"FullIdentityTopologyAncestry":          testAuthorityTopology,
		"PrivateSecurityBoundaryOnly":           testAuthorityPrivateBoundary,
		"UnboundedProductionPolicyRefusal":      testAuthorityUnboundedRefusal,
	})
}

// Only this _test.go helper cuts the security-ancestry boundary. Production has
// no boundary selector. Full physical topology guards always cover / through
// the leaf, even when security observations start at the private fixture root.
func fixtureAuthority(
	d *Dir,
	root string,
	role AuthorityRole,
	calls authorityCalls,
) (AuthorityFacts, error) {
	release, err := acquire(d)
	if err != nil {
		return AuthorityFacts{}, err
	}
	defer release()
	start := -1
	for i, node := range d.state.nodes {
		if node.path == root {
			start = i
		}
	}
	if start < 0 {
		return AuthorityFacts{}, errors.New("private authority root not pinned")
	}
	if err := d.state.guard("test-authority"); err != nil {
		return AuthorityFacts{}, err
	}
	nodes := d.state.nodes[start:]
	before, err := observeAuthorityNodes(nodes, role, calls)
	if err != nil {
		return AuthorityFacts{}, err
	}
	if err := d.state.guard("test-authority"); err != nil {
		return AuthorityFacts{}, err
	}
	after, err := observeAuthorityNodes(nodes, role, calls)
	if err != nil {
		return AuthorityFacts{}, err
	}
	if err := compareAuthorityNodes(nodes, role, before, after); err != nil {
		return AuthorityFacts{}, err
	}
	if err := d.state.guard("test-authority"); err != nil {
		return AuthorityFacts{}, err
	}
	return after[len(after)-1], nil
}

func authorityFixture(t *testing.T) (string, *Dir, AuthorityFacts) {
	t.Helper()
	r := fixture(t)
	d := openDir(t, r)
	f, err := fixtureAuthority(d, r, RolePrivateAnchor, systemAuthorityCalls())
	must(t, err) // Unsupported real filesystem/ACL evidence is a gate failure, never a skip.
	t.Logf(
		"bounded native fixture os=%s arch=%s path=%s facts=%+v",
		runtime.GOOS,
		runtime.GOARCH,
		r,
		f,
	)
	return r, d, f
}

func authorityReason(t *testing.T, err error, want Reason, path string) *AuthorityError {
	t.Helper()
	var e *AuthorityError
	if !errors.As(err, &e) || e.Reason != want || e.Path != path || e.Remediation == "" {
		t.Fatalf("want %s at %q with remediation, got %v", want, path, err)
	}
	return e
}

func testAuthorityZero(t *testing.T) {
	_, _, f := authorityFixture(t)
	for _, state := range []EvidenceState{EvidenceInvalid, EvidencePresent, EvidenceUnsupported, EvidenceFailed} {
		bad := f
		bad.security.state = state
		if err := validateAuthority(
			"private",
			bad,
			RolePrivateAnchor,
			uint32(os.Geteuid()),
		); err == nil {
			t.Fatalf("accepted security evidence %v", state)
		}
	}
	if err := validateAuthority(
		"private",
		AuthorityFacts{},
		RolePrivateAnchor,
		uint32(os.Geteuid()),
	); err == nil {
		t.Fatal("zero facts accepted")
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[AuthorityFacts](), reflect.TypeFor[SecurityEvidence](), reflect.TypeFor[FilesystemFacts]()} {
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).IsExported() {
				t.Fatalf("mutable exported authority field: %s.%s", typ, typ.Field(i).Name)
			}
		}
	}
}

func testIdentityNotAuthority(t *testing.T) {
	r, d, f := authorityFixture(t)
	must(t, os.Chmod(r, 0o750))
	id, err := d.Identity()
	must(t, err)
	if id != f.Identity() {
		t.Fatal("mode change changed identity")
	}
	_, err = fixtureAuthority(d, r, RolePrivateAnchor, systemAuthorityCalls())
	_ = authorityReason(t, err, ReasonAuthorityPolicy, r)
	if f.Mode() != 0o700 {
		t.Fatal("previous immutable facts changed")
	}
}

func testAuthorityDescriptorFacts(t *testing.T) {
	r, _, _ := authorityFixture(t)
	path := filepath.Join(r, "facts")
	write(t, path, "private")
	must(t, unix.Chmod(path, 0o4600))
	must(t, os.Link(path, filepath.Join(r, "alias")))
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	must(t, err)
	defer func() { must(t, unix.Close(fd)) }()
	f, err := readAuthorityFacts(fd, systemAuthorityCalls())
	must(t, err)
	var st unix.Stat_t
	must(t, unix.Fstat(fd, &st))
	if f.Identity() != statIdentity(&st) || f.UID() != st.Uid || f.GID() != st.Gid ||
		f.Mode() != 0o4600 ||
		f.LinkCount() != 2 {
		t.Fatalf("descriptor facts differ: %+v stat=%+v", f, st)
	}
	if err := validateAuthority(path, f, RoleLockFile, uint32(os.Geteuid())); err == nil {
		t.Fatal("file with special bits and hard links accepted")
	}
}

func testAuthorityRoles(t *testing.T) {
	_, _, baseline := authorityFixture(t)
	uid := uint32(os.Geteuid())
	// Synthetic role variants of a real descriptor observation: policy coverage,
	// not evidence that a wrong-owner fixture was created by an unprivileged user.
	cases := []struct {
		role  AuthorityRole
		kind  uint32
		owner uint32
		mode  uint32
		links uint64
		path  string
		ok    bool
	}{
		{RolePrivateAnchor, unix.S_IFDIR, uid, 0o700, 2, "private", true},
		{RolePrivateAnchor, unix.S_IFDIR, uid, 0o750, 2, "private", false},
		{RolePrivateAnchor, unix.S_IFDIR, uid + 1, 0o700, 2, "private", false},
		{RolePrivateAnchor, unix.S_IFDIR, uid, 0o1700, 2, "private", false},
		{RolePrivateAnchor, unix.S_IFREG, uid, 0o700, 1, "private", false},
		{RolePrivateAnchor, unix.S_IFLNK, uid, 0o700, 1, "private", false},
		{RolePrivateAnchor, unix.S_IFIFO, uid, 0o700, 1, "private", false},
		{RoleLockFile, unix.S_IFREG, uid, 0o600, 1, "lock", true},
		{RoleLockFile, unix.S_IFREG, uid, 0o600, 2, "lock", false},
		{RoleLockFile, unix.S_IFREG, uid + 1, 0o600, 1, "lock", false},
		{RoleLockFile, unix.S_IFREG, uid, 0o640, 1, "lock", false},
		{RoleLockFile, unix.S_IFREG, uid, 0o2600, 1, "lock", false},
		{RoleLockFile, unix.S_IFDIR, uid, 0o600, 1, "lock", false},
		{RoleRepositoryDirectory, unix.S_IFDIR, uid, 0o755, 2, "repo", true},
		{RoleRepositoryDirectory, unix.S_IFDIR, uid, 0o775, 2, "repo", false},
		{RoleRepositoryDirectory, unix.S_IFDIR, uid, 0o707, 2, "repo", false},
		{RoleRepositoryDirectory, unix.S_IFDIR, uid, 0o2700, 2, "repo", false},
		{RoleRepositoryDirectory, unix.S_IFDIR, uid + 1, 0o700, 2, "repo", false},
		{RoleSystemAncestor, unix.S_IFDIR, 0, 0o755, 2, "/", true},
		{RoleSystemAncestor, unix.S_IFDIR, uid, 0o700, 2, "home", true},
		{RoleSystemAncestor, unix.S_IFDIR, uid + 1, 0o755, 2, "foreign", false},
		{RoleSystemAncestor, unix.S_IFDIR, 0, 0o1777, 2, systemTemporaryRoot, true},
		{RoleSystemAncestor, unix.S_IFDIR, 0, 0o777, 2, systemTemporaryRoot, false},
		{RoleSystemAncestor, unix.S_IFDIR, 0, 0o1777, 2, "arbitrary", false},
		{AuthorityRole(0), unix.S_IFDIR, uid, 0o700, 2, "invalid", false},
	}
	for _, tc := range cases {
		f := baseline
		f.identity.Kind, f.uid, f.mode, f.nlink = tc.kind, tc.owner, tc.mode, tc.links
		err := validateAuthority(tc.path, f, tc.role, uid)
		if (err == nil) != tc.ok {
			t.Fatalf("role case %+v: %v", tc, err)
		}
	}
}

func testAuthorityAbsent(t *testing.T) {
	_, _, f := authorityFixture(t)
	if f.Security().State() != EvidenceAbsent || f.Security().Mechanism() == "" ||
		f.Filesystem().State() != EvidencePresent {
		t.Fatalf("absence not verified: %+v", f)
	}
}

func testAuthorityPresent(t *testing.T) {
	r, d, _ := authorityFixture(t)
	calls := systemAuthorityCalls()
	calls.security = func(int, bool) (SecurityEvidence, error) {
		return SecurityEvidence{state: EvidencePresent, mechanism: "injected-present"}, nil
	}
	_, err := fixtureAuthority(d, r, RolePrivateAnchor, calls)
	e := authorityReason(t, err, ReasonAuthorityPolicy, r)
	if e.Observed.Security().State() != EvidencePresent {
		t.Fatal("present evidence lost")
	}
	t.Log("injected present evidence; platform inventory separately requires real ACLs")
}

func testAuthorityReadFailure(t *testing.T) {
	r, d, _ := authorityFixture(t)
	for _, cause := range []error{unix.EACCES, unix.EIO, unix.ENOENT, unix.EINVAL, unix.EOPNOTSUPP, unix.EBADF, unix.ERANGE} {
		calls := systemAuthorityCalls()
		calls.security = func(int, bool) (SecurityEvidence, error) {
			return SecurityEvidence{state: EvidenceFailed, mechanism: "injected-failure"}, cause
		}
		_, err := fixtureAuthority(d, r, RolePrivateAnchor, calls)
		if !errors.Is(err, cause) {
			t.Fatalf("lost ACL failure %v: %v", cause, err)
		}
		var e *AuthorityError
		if !errors.As(err, &e) || e.Path != r || e.Role != RolePrivateAnchor ||
			e.Observed.Security().State() != EvidenceFailed || e.Remediation == "" ||
			e.Expected.Identity() != d.state.leaf().identity ||
			e.Observed.Identity() != d.state.leaf().identity {
			t.Fatalf("lost failed evidence/path/role/identity/remediation: %v", err)
		}
		if e.Reason == ReasonMissing || (errors.Is(cause, unix.ENOENT) && e.Reason != ReasonIO) {
			t.Fatalf("descriptor security read failure diagnosed as pathname absence: %v", err)
		}
	}
}

func testAuthorityFilesystemFailure(t *testing.T) {
	r, d, _ := authorityFixture(t)
	for _, state := range []EvidenceState{EvidenceInvalid, EvidenceUnsupported, EvidenceFailed} {
		calls := systemAuthorityCalls()
		calls.filesystem = func(int) (FilesystemFacts, error) {
			return FilesystemFacts{state: state, model: "injected-unknown"}, nil
		}
		_, err := fixtureAuthority(d, r, RolePrivateAnchor, calls)
		_ = authorityReason(t, err, ReasonUnsupported, r)
	}
	calls := systemAuthorityCalls()
	calls.filesystem = func(int) (FilesystemFacts, error) {
		return FilesystemFacts{state: EvidenceFailed}, unix.EIO
	}
	_, err := fixtureAuthority(d, r, RolePrivateAnchor, calls)
	if !errors.Is(err, unix.EIO) {
		t.Fatalf("filesystem read failure lost: %v", err)
	}
}

func testAuthoritySecurityDrift(t *testing.T) {
	r, d, _ := authorityFixture(t)
	calls := systemAuthorityCalls()
	read := calls.security
	reads := 0
	calls.security = func(fd int, directory bool) (SecurityEvidence, error) {
		security, err := read(fd, directory)
		reads++
		if reads == 1 {
			must(t, unix.Fchmod(fd, 0o750))
		}
		return security, err
	}
	_, err := fixtureAuthority(d, r, RolePrivateAnchor, calls)
	_ = authorityReason(t, err, ReasonAuthorityChanged, r)

	for _, tc := range []struct {
		name string
		role AuthorityRole
	}{
		{"RepositoryLeafBetweenObservations", RoleRepositoryDirectory},
		{"AncestorBetweenObservations", RoleSystemAncestor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, _ := authorityFixture(t)
			leaf := filepath.Join(root, "repository")
			mkdir(t, leaf)
			dir := openDir(t, leaf)
			node := dir.state.leaf()
			if tc.role == RoleSystemAncestor {
				node = dir.state.nodes[len(dir.state.nodes)-2]
			}
			calls := systemAuthorityCalls()
			stat := calls.stat
			reads := 0
			calls.stat = func(fd int, st *unix.Stat_t) error {
				if fd == node.fd {
					reads++
					// Each observation has two stat reads. Change mode before
					// the second observation so both are stable and policy-valid.
					if reads == 3 {
						must(t, unix.Fchmod(fd, 0o750))
					}
				}
				return stat(fd, st)
			}
			_, err := fixtureAuthority(dir, root, RoleRepositoryDirectory, calls)
			e := authorityReason(t, err, ReasonAuthorityChanged, node.path)
			if e.Role != tc.role || e.Expected.Mode() != 0o700 || e.Observed.Mode() != 0o750 ||
				e.Expected.Identity() != e.Observed.Identity() {
				t.Fatalf("inter-observation drift lost role or facts: %+v", e)
			}
			if reads != 4 || e.Detail != "authority changed between guarded observations" {
				t.Fatalf("want two stable observations, got %d stat reads: %v", reads, e)
			}
		})
	}
}

func testAuthorityTopology(t *testing.T) {
	r, _, _ := authorityFixture(t)
	parent := filepath.Join(r, "ancestor")
	leaf := filepath.Join(parent, "leaf")
	mkdir(t, leaf)
	d := openDir(t, leaf)
	calls := systemAuthorityCalls()
	read := calls.security
	swapped := false
	calls.security = func(fd int, directory bool) (SecurityEvidence, error) {
		security, err := read(fd, directory)
		if !swapped {
			swapped = true
			must(t, os.Rename(parent, filepath.Join(r, "retained")))
			mkdir(t, leaf)
		}
		return security, err
	}
	_, err := fixtureAuthority(d, leaf, RolePrivateAnchor, calls)
	// The swapped ancestor is ABOVE the test security boundary, but it remains
	// covered by the full native topology guard.
	e := reason(t, err, ReasonIdentityChanged)
	if e.Path != parent {
		t.Fatalf("wrong affected ancestry path: %v", err)
	}
}

func testAuthorityPrivateBoundary(t *testing.T) {
	r, d, _ := authorityFixture(t)
	calls := systemAuthorityCalls()
	read := calls.filesystem
	seen := 0
	calls.filesystem = func(fd int) (FilesystemFacts, error) {
		if fd != d.state.leaf().fd {
			t.Fatal("bounded test inspected security above private root")
		}
		seen++
		return read(fd)
	}
	_, err := fixtureAuthority(d, r, RolePrivateAnchor, calls)
	must(t, err)
	if seen != 2 {
		t.Fatalf("want two real descriptor observations, got %d", seen)
	}
}

func testAuthorityUnboundedRefusal(t *testing.T) {
	r, d, _ := authorityFixture(t)
	calls := systemAuthorityCalls()
	read := calls.filesystem
	rootFD := d.state.nodes[0].fd
	calls.filesystem = func(fd int) (FilesystemFacts, error) {
		if fd == rootFD {
			return FilesystemFacts{
				state: EvidenceUnsupported,
				model: "injected-unsupported-ancestor",
			}, nil
		}
		return read(fd)
	}
	_, err := d.authority(RolePrivateAnchor, calls)
	_ = authorityReason(t, err, ReasonUnsupported, "/")
	t.Log("unbounded production policy: injected unsupported / refused")
	_, err = d.Authority(RolePrivateAnchor)
	if err != nil {
		var e *AuthorityError
		if !errors.As(err, &e) {
			t.Fatalf("unexpected observed production refusal: %v", err)
		}
		t.Logf("unbounded observed production-policy refusal (not injected): %v", err)
	} else {
		t.Logf(
			"unbounded observed production policy supported on this host for private fixture %s; unsupported-ancestor refusal above is injected",
			r,
		)
	}
}
