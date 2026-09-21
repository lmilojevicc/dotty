//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func testPrivateUmask(t *testing.T) {
	for _, mask := range []string{"000", "077", "200", "700", "777"} {
		t.Run(mask, func(t *testing.T) {
			root := fixture(t)
			executable, err := os.Executable()
			must(t, err)
			t.Setenv("DOTTY_PRIVATE_UMASK_CHILD", mask)
			t.Setenv("TMPDIR", root)
			t.Setenv("TMP", root)
			t.Setenv("TEMP", root)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(
				ctx,
				executable,
				"-test.run=^TestPrivateUmaskProcess$",
				"-test.v",
				"-test.timeout=20s",
			)
			cmd.WaitDelay = 5 * time.Second
			var output unsupportedBuildOutput
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Run(); err != nil {
				t.Fatalf("umask %s: %v context=%v\n%s", mask, err, ctx.Err(), output.String())
			}
			t.Logf("isolated umask %s:\n%s", mask, output.String())
		})
	}
}

func TestPrivateUmaskProcess(t *testing.T) {
	maskText := os.Getenv("DOTTY_PRIVATE_UMASK_CHILD")
	if maskText == "" {
		return
	}
	mask, err := strconv.ParseUint(maskText, 8, 9)
	must(t, err)
	// All fixture setup precedes the process-local umask change. This subprocess
	// runs only this helper and exits; no runner-global umask or fixture repair.
	r, d, calls := privateFixture(t)
	unix.Umask(int(mask))
	f, record, err := d.lockFile(component(t, "lock"), true, calls)
	must(t, err)
	must(t, f.Close())
	info, err := os.Lstat(filepath.Join(r, "lock"))
	must(t, err)
	if !record.Created() || info.Mode().Perm() != 0o600 {
		t.Fatal("exclusive mode not established")
	}
	child, record, err := d.privateDir(component(t, "anchor"), true, calls)
	want := os.FileMode(0o700 &^ mask)
	if want == 0o700 {
		must(t, err)
		must(t, child.Close())
	} else {
		if child != nil {
			t.Fatal("restrictive directory accepted")
		}
		retainedCreation(t, record, err, filepath.Join(r, "anchor"))
	}
	info, statErr := os.Lstat(filepath.Join(r, "anchor"))
	must(t, statErr)
	if info.Mode().Perm() != want {
		t.Fatalf("directory repaired: got %o want %o", info.Mode().Perm(), want)
	}
	t.Logf(
		"native umask %s file=0600 directory=%04o retained=%t",
		maskText,
		want,
		record.Created(),
	)
}
