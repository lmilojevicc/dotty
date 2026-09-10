//go:build darwin && cgo

package nativefs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var requiredDarwinAuthorityCases = []string{
	"LocalAPFSOwnershipEnforced",
	"OwnershipDisabledRefused",
	"NaturalDirectoryACLAbsentConfirmed",
	"NaturalRegularFileACLAbsentConfirmed",
	"AllocatedEmptyACLNoEntriesVerified",
	"ACLGetterConfirmationDispatch",
	"RequiredACLConfirmationErrorsRefused",
	"RequiredACLConfirmationMalformedRefused",
	"AllowACLRefused",
	"DenyOnlyACLRefused",
	"InheritedACLRefused",
	"DeferredInheritanceRefused",
	"ACLValidationFailureRefused",
	"ACLAllocationFreedOnAllPaths",
}

func TestDarwinAuthority(t *testing.T) {
	runAuthorityInventory(t, requiredDarwinAuthorityCases, map[string]func(*testing.T){
		"LocalAPFSOwnershipEnforced":              testDarwinFilesystem,
		"OwnershipDisabledRefused":                testDarwinOwnershipDisabled,
		"NaturalDirectoryACLAbsentConfirmed":      func(t *testing.T) { testDarwinNaturalAbsence(t, true) },
		"NaturalRegularFileACLAbsentConfirmed":    func(t *testing.T) { testDarwinNaturalAbsence(t, false) },
		"AllocatedEmptyACLNoEntriesVerified":      testDarwinEmptyACL,
		"ACLGetterConfirmationDispatch":           testDarwinConfirmationDispatch,
		"RequiredACLConfirmationErrorsRefused":    testDarwinConfirmationErrors,
		"RequiredACLConfirmationMalformedRefused": testDarwinConfirmationMalformed,
		"AllowACLRefused":                         func(t *testing.T) { testDarwinPresentACL(t, "everyone allow readattr", false) },
		"DenyOnlyACLRefused":                      func(t *testing.T) { testDarwinPresentACL(t, "everyone deny writeextattr", false) },
		"InheritedACLRefused": func(t *testing.T) {
			testDarwinPresentACL(t, "everyone allow readattr,directory_inherit,file_inherit", true)
		},
		"DeferredInheritanceRefused":   testDarwinDeferredACL,
		"ACLValidationFailureRefused":  testDarwinInvalidACL,
		"ACLAllocationFreedOnAllPaths": testDarwinACLFree,
	})
}

func testDarwinFilesystem(t *testing.T) {
	_, d, f := authorityFixture(t)
	var st unix.Statfs_t
	must(t, unix.Fstatfs(d.state.leaf().fd, &st))
	if unix.ByteSliceToString(st.Fstypename[:]) != "apfs" || st.Flags&unix.MNT_LOCAL == 0 ||
		st.Flags&unix.MNT_IGNORE_OWNERSHIP != 0 {
		t.Fatalf("native positive fixture requires local ownership-enforced APFS: %+v", st)
	}
	if f.Filesystem().Type() != uint64(st.Type) || f.Filesystem().Flags() != uint64(st.Flags) ||
		f.Filesystem().State() != EvidencePresent {
		t.Fatalf("filesystem observation mismatch: %+v / %+v", f.Filesystem(), st)
	}
	t.Logf("real native APFS descriptor facts: %+v", f.Filesystem())
}

func testDarwinOwnershipDisabled(t *testing.T) {
	_, d, _ := authorityFixture(t)
	var native unix.Statfs_t
	must(t, unix.Fstatfs(d.state.leaf().fd, &native))
	for _, flag := range []uint32{unix.MNT_IGNORE_OWNERSHIP, unix.MNT_UNION} {
		st := native
		st.Flags |= flag
		if f := darwinFilesystemFacts(&st); f.State() != EvidenceUnsupported {
			t.Fatalf("injected unsafe mount accepted: %+v", f)
		}
	}
	st := native
	st.Flags &^= unix.MNT_LOCAL
	if f := darwinFilesystemFacts(&st); f.State() != EvidenceUnsupported {
		t.Fatalf("injected non-local mount accepted: %+v", f)
	}
	st = native
	copy(st.Fstypename[:], []byte("unknown\x00"))
	if f := darwinFilesystemFacts(&st); f.State() != EvidenceUnsupported {
		t.Fatalf("injected unknown filesystem accepted: %+v", f)
	}
}

func testDarwinEmptyACL(t *testing.T) {
	calls := systemDarwinACLCalls()
	// A real heap ACL, not an assumption about a naturally ACL-absent inode.
	calls.get = func(int) (*darwinACL, error) { return allocateDarwinEmptyACLForTest() }
	calls.confirm = func(int) (darwinRequiredACLResult, error) {
		t.Fatal("allocated ACL used required-attribute confirmation")
		return darwinRequiredACLResult{}, nil
	}
	valid, first, free := calls.valid, calls.first, calls.free
	freed := 0
	calls.free = func(acl *darwinACL) error {
		freed++
		return free(acl)
	}
	validated, enumerated := false, false
	calls.valid = func(acl *darwinACL) error {
		err := valid(acl)
		validated = err == nil
		return err
	}
	calls.first = func(acl *darwinACL) (bool, error) {
		if !validated {
			t.Fatal("enumeration before validation")
		}
		enumerated = true
		present, err := first(acl)
		if present || err != nil {
			t.Fatalf("real empty ACL enumeration: %v, %v", present, err)
		}
		return present, err
	}
	evidence, err := readDarwinSecurity(-1, calls)
	must(t, err)
	if !validated || !enumerated || freed != 1 || evidence.State() != EvidenceAbsent {
		t.Fatalf("empty ACL not validated/enumerated: %+v", evidence)
	}
}

func chmodPrivateACL(t *testing.T, root, path, acl string) {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsAbs(root) || !filepath.IsAbs(path) || !filepath.IsLocal(relative) {
		t.Fatal("ACL fixture path not private")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/chmod", "+a", acl, path)
	cmd.WaitDelay = time.Second
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + root,
		"TMPDIR=" + root,
		"XDG_CONFIG_HOME=" + filepath.Join(
			root,
			"config",
		),
		"DOTTY_REPO=" + filepath.Join(root, "repo"),
	}
	var output unsupportedBuildOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		t.Fatalf(
			"private ACL fixture setup: %v; context=%v; output=%s",
			err,
			ctx.Err(),
			output.String(),
		)
	}
}

func testDarwinPresentACL(t *testing.T, acl string, inherited bool) {
	r, _, _ := authorityFixture(t)
	parent := filepath.Join(r, "acl-parent")
	mkdir(t, parent)
	chmodPrivateACL(t, r, parent, acl)
	path := parent
	if inherited {
		path = filepath.Join(parent, "inherited")
		must(t, os.Mkdir(path, 0o700))
	}
	d := openDir(t, path)
	calls := systemDarwinACLCalls()
	if inherited {
		// Inspect the actual native entry flag, not chmod output or an injected
		// security state. No mutation helper is exposed by the package.
		observed, err := calls.get(d.state.leaf().fd)
		must(t, err)
		entry, entryErr := darwinFirstEntry(observed)
		freeErr := calls.free(observed)
		must(t, errors.Join(entryErr, freeErr))
		if !entry.present || !entry.inherited {
			t.Fatal("native child ACL entry was not inherited")
		}
	}
	_, err := fixtureAuthority(d, path, RolePrivateAnchor, systemAuthorityCalls())
	e := authorityReason(t, err, ReasonAuthorityPolicy, path)
	if e.Observed.Security().State() != EvidencePresent {
		t.Fatalf("real ACL was not detected as present: %+v", e)
	}
	t.Logf("real Darwin libc ACL evidence for %q, inherited=%v", acl, inherited)
}

func testDarwinDeferredACL(t *testing.T) {
	r, d, _ := authorityFixture(t)
	calls := allocatedDarwinACLFixture(t, r)
	valid := calls.valid
	validated := false
	calls.valid = func(acl *darwinACL) error {
		err := valid(acl)
		validated = err == nil
		return err
	}
	calls.deferred = func(*darwinACL) (bool, error) {
		if !validated {
			t.Fatal("deferred check before real ACL validation")
		}
		return true, nil
	}
	security, err := readDarwinSecurity(d.state.leaf().fd, calls)
	must(t, err)
	if security.State() != EvidencePresent {
		t.Fatal("deferred inheritance accepted")
	}
	facts, err := readAuthorityFacts(d.state.leaf().fd, systemAuthorityCalls())
	must(t, err)
	facts.security = security
	_ = authorityReason(
		t,
		validateAuthority(r, facts, RolePrivateAnchor, uint32(os.Geteuid())),
		ReasonAuthorityPolicy,
		r,
	)
	t.Log(
		"INJECTED defer flag on genuinely retrieved/validated ACL; not native flag creation/detection or round-trip evidence",
	)
}

func testDarwinInvalidACL(t *testing.T) {
	// Public Libc's getter does not set/clear errno for its valid 0/1 results.
	// Inject stale errno to exercise mapping, not a claimed native error path.
	for _, value := range []int{0, 1} {
		for _, stale := range []unix.Errno{unix.EINVAL, unix.EIO} {
			present, err := darwinFlagResult(value, stale)
			must(t, err)
			if present != (value == 1) {
				t.Fatalf("flag value %d with stale errno %v mapped incorrectly", value, stale)
			}
		}
	}
	_, unexpected := darwinFlagResult(-1, unix.EINVAL)
	if !errors.Is(unexpected, unix.EIO) || errors.Is(unexpected, unix.EINVAL) {
		t.Fatalf("unexpected injected getter result treated as documented errno: %v", unexpected)
	}
	r, d, _ := authorityFixture(t)
	calls := allocatedDarwinACLFixture(t, r)
	calls.valid = func(*darwinACL) error { return unix.EINVAL }
	calls.first = func(*darwinACL) (bool, error) {
		t.Fatal("invalid ACL enumerated")
		return false, nil
	}
	security, err := readDarwinSecurity(d.state.leaf().fd, calls)
	if !errors.Is(err, unix.EINVAL) || security.State() != EvidenceFailed {
		t.Fatalf("injected invalid ACL became absence: %+v %v", security, err)
	}
}

func testDarwinACLFree(t *testing.T) {
	r, d, _ := authorityFixture(t)
	base := allocatedDarwinACLFixture(t, r)
	for _, stage := range []string{"success", "get", "allocated-get-error", "valid", "deferred", "deferred-present", "first", "present", "free", "valid-and-free"} {
		calls := base
		free := calls.free
		freed := 0
		calls.free = func(acl *darwinACL) error {
			freed++
			err := free(acl)
			if stage == "free" || stage == "valid-and-free" {
				return errors.Join(err, unix.EIO)
			}
			return err
		}
		switch stage {
		case "get":
			calls.get = func(int) (*darwinACL, error) { return nil, unix.EACCES }
		case "allocated-get-error":
			calls.get = func(fd int) (*darwinACL, error) {
				acl, err := base.get(fd)
				return acl, errors.Join(err, unix.EIO)
			}
		case "valid", "valid-and-free":
			calls.valid = func(*darwinACL) error { return unix.EINVAL }
		case "deferred":
			calls.deferred = func(*darwinACL) (bool, error) { return false, unix.EINVAL }
		case "deferred-present":
			calls.deferred = func(*darwinACL) (bool, error) { return true, nil }
		case "first":
			calls.first = func(*darwinACL) (bool, error) { return false, unix.EIO }
		case "present":
			calls.first = func(*darwinACL) (bool, error) { return true, nil }
		}
		security, err := readDarwinSecurity(d.state.leaf().fd, calls)
		wantFreed := 1
		if stage == "get" {
			wantFreed = 0
		}
		if freed != wantFreed {
			t.Fatalf("%s: freed %d times, want %d", stage, freed, wantFreed)
		}
		if stage == "success" || stage == "present" || stage == "deferred-present" {
			must(t, err)
		} else if err == nil || security.State() != EvidenceFailed {
			t.Fatalf("%s: lost failure: %+v %v", stage, security, err)
		}
		if stage == "valid-and-free" &&
			(!errors.Is(err, unix.EINVAL) || !errors.Is(err, unix.EIO)) {
			t.Fatalf("ACL validation/free errors not joined: %v", err)
		}
	}
	t.Log("real ACL allocations/frees; injected per-call failure paths")
}

func allocatedDarwinACLFixture(t *testing.T, root string) darwinACLCalls {
	t.Helper()
	requireDarwinRequiredACLProbeFilesystem(t, root)
	path := filepath.Join(root, "allocated-acl")
	mkdir(t, path)
	chmodPrivateACL(t, root, path, "everyone allow readattr")
	d := openDir(t, path)
	calls := systemDarwinACLCalls()
	get := calls.get
	calls.get = func(int) (*darwinACL, error) {
		acl, err := get(d.state.leaf().fd)
		if err != nil || acl == nil {
			t.Fatalf("real allocated ACL fixture: %v, %v", acl, err)
		}
		return acl, nil
	}
	calls.confirm = func(int) (darwinRequiredACLResult, error) {
		t.Fatal("allocated ACL used required-attribute confirmation")
		return darwinRequiredACLResult{}, nil
	}
	return calls
}

func testDarwinNaturalAbsence(t *testing.T, directory bool) {
	root := fixture(t)
	// Gate the actual private root before any ACL observation or setup.
	requireDarwinRequiredACLProbeFilesystem(t, root)
	path := filepath.Join(root, "naturally-absent")
	if directory {
		must(t, os.Mkdir(path, 0o700))
	} else {
		must(t, os.WriteFile(path, nil, 0o600))
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	must(t, err)
	defer func() { must(t, unix.Close(fd)) }()
	calls := systemDarwinACLCalls()
	acl, err := calls.get(fd)
	if acl != nil {
		must(t, calls.free(acl))
	}
	if acl != nil || !errors.Is(err, unix.ENOENT) {
		t.Fatalf("natural fixture requires NULL/ENOENT getter correlation: %v, %v", acl, err)
	}
	// No injected reader: exercise the production entrypoint and its C query.
	security, err := nativeSecurity(fd, directory)
	must(t, err)
	if security.State() != EvidenceAbsent || security.Mechanism() != darwinRequiredACLMechanism {
		t.Fatalf("natural absence not independently confirmed: %+v", security)
	}
	facts, err := readAuthorityFacts(fd, systemAuthorityCalls())
	must(t, err)
	role := RoleLockFile
	if directory {
		role = RolePrivateAnchor
	}
	must(t, validateAuthority(path, facts, role, uint32(os.Geteuid())))
}

func absentDarwinRequiredACLResult() darwinRequiredACLResult {
	return darwinRequiredACLResult{
		capacity: 4096, length: 12, offset: 8, refLength: 0,
		header: true, fields: true,
	}
}

func testDarwinConfirmationDispatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cause   error
		confirm bool
	}{
		{"NULLENOENT", unix.ENOENT, true},
		{"NULLEINVAL", unix.EINVAL, false},
		{"NULLENOTSUP", unix.ENOTSUP, false},
		{"NULLEBADF", unix.EBADF, false},
		{"NULLNilError", nil, false},
	} {
		calls := systemDarwinACLCalls()
		calls.get = func(int) (*darwinACL, error) { return nil, tc.cause }
		confirmed := 0
		calls.confirm = func(fd int) (darwinRequiredACLResult, error) {
			if fd != 37 {
				t.Fatalf("confirmation changed held fd: %d", fd)
			}
			confirmed++
			return absentDarwinRequiredACLResult(), nil
		}
		security, err := readDarwinSecurity(37, calls)
		if tc.confirm {
			must(t, err)
			if confirmed != 1 || security.State() != EvidenceAbsent {
				t.Fatalf("%s: %+v, calls=%d", tc.name, security, confirmed)
			}
		} else if confirmed != 0 || err == nil || security.State() == EvidenceAbsent {
			t.Fatalf("%s: %+v, %v, calls=%d", tc.name, security, err, confirmed)
		}
		if !tc.confirm && tc.cause != nil && !errors.Is(err, tc.cause) {
			t.Fatalf("%s lost cause: %v", tc.name, err)
		}
	}
	root := fixture(t)
	calls := allocatedDarwinACLFixture(t, root)
	get := calls.get
	calls.get = func(fd int) (*darwinACL, error) {
		acl, err := get(fd)
		must(t, err)
		// Exercise the same result mapper as the C getter with real storage.
		return darwinACLOpenResult("acl_get_fd_np", acl.value, int(unix.ENOENT))
	}
	security, err := readDarwinSecurity(-1, calls)
	must(t, err)
	if security.State() != EvidencePresent {
		t.Fatalf("allocated getter success with stale errno: %+v", security)
	}
}

func testDarwinConfirmationErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  EvidenceState
	}{
		{"ENOENT", unix.ENOENT, EvidenceFailed},
		{"EINVAL", unix.EINVAL, EvidenceFailed},
		{"ENOTSUP", unix.ENOTSUP, EvidenceUnsupported},
		{"EOPNOTSUPP", unix.EOPNOTSUPP, EvidenceUnsupported},
		{"ENOSYS", unix.ENOSYS, EvidenceUnsupported},
		{"EBADF", unix.EBADF, EvidenceFailed},
		{"ERANGE", unix.ERANGE, EvidenceFailed},
		{"EACCES", unix.EACCES, EvidenceFailed},
		{"EIO", unix.EIO, EvidenceFailed},
	} {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/wrapped=%t", tc.name, wrapped), func(t *testing.T) {
				cause := tc.cause
				if wrapped {
					cause = fmt.Errorf("fgetattrlist required extended security: %w", cause)
				}
				calls := systemDarwinACLCalls()
				calls.get = func(int) (*darwinACL, error) { return nil, unix.ENOENT }
				confirmed := 0
				calls.confirm = func(fd int) (darwinRequiredACLResult, error) {
					if fd != 37 {
						t.Fatalf("confirmation changed held fd: %d", fd)
					}
					confirmed++
					return absentDarwinRequiredACLResult(), cause
				}
				security, err := readDarwinSecurity(37, calls)
				if !errors.Is(err, cause) || !errors.Is(err, tc.cause) ||
					security.State() != tc.want ||
					security.Mechanism() != darwinRequiredACLMechanism ||
					confirmed != 1 {
					t.Fatalf(
						"confirmation %v became %+v, %v, calls=%d",
						cause,
						security,
						err,
						confirmed,
					)
				}
				// Capability failures from the getter must refuse without using
				// the ENOENT-only confirmation route, wrapped or otherwise.
				if tc.want == EvidenceUnsupported {
					cause = tc.cause
					if wrapped {
						cause = fmt.Errorf("acl_get_fd_np: %w", cause)
					}
					calls.get = func(int) (*darwinACL, error) { return nil, cause }
					confirmed = 0
					security, err = readDarwinSecurity(37, calls)
					if !errors.Is(err, cause) || !errors.Is(err, tc.cause) ||
						security.State() != tc.want ||
						security.Mechanism() != "acl_get_fd_np:extended" ||
						confirmed != 0 {
						t.Fatalf(
							"getter %v became %+v, %v, calls=%d",
							cause,
							security,
							err,
							confirmed,
						)
					}
				}
			})
		}
	}
}

func testDarwinConfirmationMalformed(t *testing.T) {
	cases := []struct {
		name   string
		change func(*darwinRequiredACLResult)
	}{
		{"MissingCapacity", func(r *darwinRequiredACLResult) { r.capacity = 0 }},
		{"MissingHeader", func(r *darwinRequiredACLResult) { r.header = false }},
		{"MissingFixedFields", func(r *darwinRequiredACLResult) { r.fields = false }},
		{"ReportedShort", func(r *darwinRequiredACLResult) { r.length = 4 }},
		{"ReportedOversize", func(r *darwinRequiredACLResult) { r.length = 4097 }},
		{"WrongLength", func(r *darwinRequiredACLResult) { r.length = 16 }},
		{"NegativeOffset", func(r *darwinRequiredACLResult) { r.offset = -8 }},
		{"WrongOffset", func(r *darwinRequiredACLResult) { r.offset = 4 }},
		{"NonzeroReferenceLength", func(r *darwinRequiredACLResult) { r.refLength = 68 }},
		{
			"SuccessfulTruncation",
			func(r *darwinRequiredACLResult) { r.capacity = 4; r.fields = false },
		},
	}
	for _, tc := range cases {
		calls := systemDarwinACLCalls()
		calls.get = func(int) (*darwinACL, error) { return nil, unix.ENOENT }
		calls.confirm = func(int) (darwinRequiredACLResult, error) {
			r := absentDarwinRequiredACLResult()
			tc.change(&r)
			return r, nil
		}
		security, err := readDarwinSecurity(-1, calls)
		if !errors.Is(err, unix.EIO) || security.State() != EvidenceFailed {
			t.Fatalf("%s accepted: %+v, %v", tc.name, security, err)
		}
	}
}
