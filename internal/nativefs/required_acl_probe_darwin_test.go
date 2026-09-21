//go:build darwin && cgo

package nativefs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type darwinRequiredACLProbeResult struct {
	Status         int
	Errno          int
	StatBefore     int
	StatAfter      int
	IdentityStable bool
	Device         uint64
	Inode          uint64
	Mode           uint32
	Capacity       uint32
	Length         uint32
	RefOffset      int32
	RefLength      uint32
	FieldsValid    bool
	Complete       bool
}

// This matches only the raw schema under investigation, NOT production ACL
// absence policy. See testdata/darwin-acl-probe/README.md for the public contract
// and source-version limitations. No variable-length ACL data is interpreted.
func (r darwinRequiredACLProbeResult) matchesAbsentSchema() bool {
	return r.Status == 0 && r.Errno == 0 && r.StatBefore == 0 && r.StatAfter == 0 &&
		r.IdentityStable && r.FieldsValid && r.Complete && r.Capacity >= 12 &&
		r.Length == 12 && r.RefOffset == 8 && r.RefLength == 0
}

func darwinRequiredACLProbeEnvironment(root string) []string {
	// Start empty: in particular no inherited GOCACHEPROG, compiler flags,
	// DYLD hooks, SDK overrides, module configuration, or user configuration.
	return []string{
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"CC=/usr/bin/clang",
		"CXX=/usr/bin/clang++",
		"HOME=" + root,
		"USERPROFILE=" + root,
		"TMPDIR=" + root,
		"GOTMPDIR=" + root,
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"DOTTY_REPO=" + filepath.Join(root, "repo"),
		"GOCACHE=" + filepath.Join(root, "cache"),
		"GOMODCACHE=" + filepath.Join(root, "modcache"),
		"GOPATH=" + filepath.Join(root, "gopath"),
		"GOCACHEPROG=",
		"GOENV=off",
		"GOWORK=off",
		"GO111MODULE=off",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOFLAGS=",
		"CGO_ENABLED=1",
		"GOOS=darwin",
		"GOARCH=" + runtime.GOARCH,
	}
}

func buildDarwinRequiredACLProbe(t *testing.T, root string) string {
	t.Helper()
	// Compile a private copy with the host toolchain, without module resolution
	// or writes under the checkout. Never use go run: fd 3 belongs to the final
	// executable, not an intermediate Go driver with unspecified inheritance.
	source, err := os.ReadFile(filepath.Join("testdata", "darwin-acl-probe", "main_darwin.go"))
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, "main_darwin.go"), source, 0o600))
	artifact := filepath.Join(root, "darwin-acl-probe")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	goDriver, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find selected host Go driver: %v", err)
	}
	goDriver, err = filepath.Abs(goDriver)
	if err != nil {
		t.Fatalf("resolve selected host Go driver path: %v", err)
	}
	// Resolve the selected driver's active root, not the test binary's build
	// root. The private environment prevents user config or toolchain downloads.
	env := darwinRequiredACLProbeEnvironment(root)
	query := exec.CommandContext(
		ctx,
		goDriver,
		"env",
		"-json",
		"GOROOT",
		"GOVERSION",
		"GOHOSTOS",
		"GOHOSTARCH",
	)
	query.Dir = root
	query.Env = env
	query.WaitDelay = time.Second
	var queryOutput, queryErrors unsupportedBuildOutput
	query.Stdout, query.Stderr = &queryOutput, &queryErrors
	if err := query.Run(); err != nil {
		t.Fatalf("query selected host Go driver: %v; context=%v; stderr=%s; stdout=%s",
			err, ctx.Err(), queryErrors.String(), queryOutput.String())
	}
	if queryErrors.String() != "" {
		t.Fatalf("unexpected host Go query stderr: %s", queryErrors.String())
	}
	var host struct {
		GOROOT     string
		GOVERSION  string
		GOHOSTOS   string
		GOHOSTARCH string
	}
	must(t, json.Unmarshal([]byte(queryOutput.String()), &host))
	if !filepath.IsAbs(host.GOROOT) || host.GOVERSION != "go1.26.6" ||
		host.GOHOSTOS != runtime.GOOS || host.GOHOSTARCH != runtime.GOARCH {
		t.Fatalf(
			"selected Go driver %q is not the required local host toolchain: %+v",
			goDriver,
			host,
		)
	}
	cmd := exec.CommandContext(ctx, goDriver, "build", "-o", artifact, "main_darwin.go")
	cmd.Dir = root
	env = append(env, "GOROOT="+host.GOROOT)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	var output unsupportedBuildOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		t.Fatalf(
			"build private Darwin ACL probe: %v; context=%v; output=%s",
			err,
			ctx.Err(),
			output.String(),
		)
	}
	return artifact
}

func runDarwinRequiredACLProbe(
	t *testing.T, root, artifact, path, variant string,
) darwinRequiredACLProbeResult {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsAbs(root) || !filepath.IsLocal(relative) {
		t.Fatal("probe fixture path not private")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	must(t, err)
	file := os.NewFile(uintptr(fd), "private-acl-fixture")
	defer func() { must(t, file.Close()) }()
	var before, after unix.Stat_t
	must(t, unix.Fstat(fd, &before))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, artifact, variant)
	cmd.Dir = root
	cmd.Env = darwinRequiredACLProbeEnvironment(root)
	cmd.ExtraFiles = []*os.File{file} // The final probe receives exactly fd 3.
	cmd.WaitDelay = time.Second
	var stdout, stderr unsupportedBuildOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	must(t, unix.Fstat(fd, &after))
	// Ignore access time: this is read-only observation, not restoration proof.
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode ||
		before.Uid != after.Uid || before.Gid != after.Gid || before.Nlink != after.Nlink ||
		before.Size != after.Size || before.Flags != after.Flags ||
		before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		t.Fatal("private fixture descriptor identity/metadata changed across probe")
	}
	if runErr != nil {
		t.Fatalf("private ACL probe %s: %v; context=%v; stderr=%s; stdout=%s",
			variant, runErr, ctx.Err(), stderr.String(), stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("unexpected probe stderr: %s", stderr.String())
	}
	var result darwinRequiredACLProbeResult
	must(t, json.Unmarshal([]byte(stdout.String()), &result))
	t.Logf("test-validation-only %s raw probe: %+v", variant, result)
	if result.StatBefore != 0 || result.StatAfter != 0 || !result.IdentityStable ||
		result.Device != uint64(
			before.Dev,
		) || result.Inode != before.Ino || result.Mode != uint32(before.Mode) {
		t.Fatalf("probe fd 3 did not retain the private fixture identity: %+v", result)
	}
	return result
}

func correlateDarwinRequiredACLGetter(t *testing.T, path string, present, inherited bool) {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	must(t, err)
	defer func() { must(t, unix.Close(fd)) }()
	calls := systemDarwinACLCalls()
	acl, getErr := calls.get(fd)
	if acl != nil {
		defer func() { must(t, calls.free(acl)) }()
	}
	var code unix.Errno
	_ = errors.As(getErr, &code)
	t.Logf("getter correlation only: nil=%v errno=%d error=%v", acl == nil, code, getErr)
	if !present {
		// NULL/ENOENT is only a correlation, never absence authority or fallback.
		return
	}
	must(t, getErr)
	if acl == nil {
		t.Fatal("real ACL fixture getter returned nil")
	}
	entry, err := darwinFirstEntry(acl)
	must(t, err)
	if !entry.present || entry.inherited != inherited {
		t.Fatalf(
			"real ACL fixture entry mismatch: present=%v inherited=%v",
			entry.present,
			entry.inherited,
		)
	}
}

func requireDarwinRequiredACLProbeFilesystem(t *testing.T, root string) {
	t.Helper()
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open private ACL probe fixture root %q: %v", root, err)
	}
	file := os.NewFile(uintptr(fd), "private-acl-probe-root")
	defer func() { must(t, file.Close()) }()
	before, err := file.Stat()
	must(t, err)
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		t.Fatalf("private ACL probe fixture root %q Fstatfs: %v", root, err)
	}
	// Pure filesystem classification only: do not invoke Authority or its ACL getter.
	if darwinFilesystemFacts(&st).State() != EvidencePresent {
		t.Fatalf(
			"private ACL probe fixture root %q requires local ownership-enforced non-union APFS; observed: %+v",
			root,
			st,
		)
	}
	after, err := os.Lstat(root)
	must(t, err)
	if !before.IsDir() || !after.IsDir() || !os.SameFile(before, after) {
		t.Fatalf(
			"private ACL probe fixture root %q path/descriptor identity changed during filesystem check",
			root,
		)
	}
	t.Logf(
		"test-validation-only private ACL probe fixture root %q descriptor filesystem: %+v",
		root,
		st,
	)
	must(t, unix.Fchmod(fd, 0o700))
}

func TestDarwinRequiredACLProbe(t *testing.T) {
	root := t.TempDir()
	requireDarwinRequiredACLProbeFilesystem(t, root)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("DOTTY_REPO", filepath.Join(root, "repo"))
	artifact := buildDarwinRequiredACLProbe(t, t.TempDir())
	t.Log(
		"test-validation-only: required-attribute raw schema, not production authority/absence verification",
	)
	for _, kind := range []string{"Directory", "RegularFile"} {
		for _, fixture := range []struct {
			name      string
			acl       string
			inherited bool
		}{
			{name: "NaturallyAbsent"},
			{name: "Allow", acl: "everyone allow readattr"},
			{name: "Deny", acl: "everyone deny writeextattr"},
			{name: "Inherited", acl: "everyone allow readattr,directory_inherit,file_inherit", inherited: true},
		} {
			t.Run(kind+"/"+fixture.name, func(t *testing.T) {
				parent := filepath.Join(root, kind+fixture.name)
				must(t, os.Mkdir(parent, 0o700))
				if fixture.inherited {
					chmodPrivateACL(t, root, parent, fixture.acl)
				}
				path := filepath.Join(parent, "object")
				if kind == "Directory" {
					must(t, os.Mkdir(path, 0o700))
				} else {
					must(t, os.WriteFile(path, []byte("private ACL probe fixture\n"), 0o600))
				}
				if fixture.acl != "" && !fixture.inherited {
					chmodPrivateACL(t, root, path, fixture.acl)
				}
				correlateDarwinRequiredACLGetter(t, path, fixture.acl != "", fixture.inherited)
				r := runDarwinRequiredACLProbe(t, root, artifact, path, "normal")
				if fixture.acl == "" {
					if !r.matchesAbsentSchema() {
						t.Fatalf("naturally absent fixture did not match raw 12/8/0 schema: %+v", r)
					}
				} else if r.Status != 0 || r.Errno != 0 || !r.FieldsValid || !r.Complete ||
					r.Length <= 12 || r.RefLength == 0 || r.matchesAbsentSchema() {
					t.Fatalf("real nonempty ACL must produce non-absence data: %+v", r)
				}
			})
		}
	}
	path := filepath.Join(root, "negative-fixture")
	must(t, os.WriteFile(path, nil, 0o600))
	for _, negative := range []struct {
		variant string
		errno   unix.Errno
	}{
		{variant: "invalid-fd", errno: unix.EBADF},
		{variant: "short-buffer", errno: unix.ERANGE},
		{variant: "invalid-request", errno: unix.EINVAL},
		{variant: "truncated-buffer"},
	} {
		t.Run(negative.variant, func(t *testing.T) {
			r := runDarwinRequiredACLProbe(t, root, artifact, path, negative.variant)
			if r.matchesAbsentSchema() || r.FieldsValid || r.Complete {
				t.Fatalf(
					"error/truncated response matched absence or exposed invalid fields: %+v",
					r,
				)
			}
			if negative.errno != 0 {
				if r.Status != -1 || r.Errno != int(negative.errno) {
					t.Fatalf("want syscall -1/errno %d: %+v", negative.errno, r)
				}
			} else if r.Status != 0 || r.Errno != 0 || r.Capacity != 4 || r.Length <= r.Capacity {
				t.Fatalf("FULLSIZE did not expose truncated fixed fields: %+v", r)
			}
		})
	}
}
