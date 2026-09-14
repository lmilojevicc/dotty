//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type coordinatorProcessConfig struct {
	Root               string
	RootIdentity       Identity
	Repository         string
	RepositoryIdentity Identity
}

func startCoordinatorProcess(t *testing.T, root, repository string) *flockProcess {
	t.Helper()
	rootDir, repoDir := openDir(t, root), openDir(t, repository)
	config := coordinatorProcessConfig{
		Root:               root,
		RootIdentity:       rootDir.state.leaf().identity,
		Repository:         repository,
		RepositoryIdentity: repoDir.state.leaf().identity,
	}
	// Test config selection is intentionally separate from native coordination.
	// This file is inside the child's repository, not the parent's selected one.
	data, err := json.Marshal(repository)
	must(t, err)
	write(t, filepath.Join(repository, "choice.json"), string(data))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	input, writer, err := os.Pipe()
	must(t, err)
	t.Cleanup(func() { _ = input.Close(); _ = writer.Close() })
	reader, output, err := os.Pipe()
	must(t, err)
	t.Cleanup(func() { _ = reader.Close(); _ = output.Close() })
	executable, err := os.Executable()
	must(t, err)
	cmd := exec.CommandContext(
		ctx,
		executable,
		"-test.run=^TestCoordinatorProcess$",
		"-test.v",
		"-test.timeout=15s",
	)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin, cmd.ExtraFiles = input, []*os.File{output}
	var captured unsupportedBuildOutput
	cmd.Stdout, cmd.Stderr = &captured, &captured
	cmd.Env = append(os.Environ(), "DOTTY_COORDINATOR_TEST_CHILD=1")
	must(t, cmd.Start())
	// Own kill/reap and bounded output before any post-Start fatal assertion.
	events, done := make(chan string, 8), make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	child := &flockProcess{ctx: ctx, input: writer, events: events, done: done, output: &captured}
	t.Cleanup(func() {
		_ = writer.Close()
		cancel()
		if !child.finished {
			<-done
		}
		_ = reader.Close()
	})
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 128), 1024)
		for scanner.Scan() {
			select {
			case events <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case events <- "pipe-error: " + err.Error():
			case <-ctx.Done():
			}
		}
	}()
	must(t, input.Close())
	must(t, output.Close())
	data, err = json.Marshal(config)
	must(t, err)
	child.send(t, string(data))
	child.expect(t, "ready")
	return child
}

// Helper-only opt-in; no production override. All authority is validated from
// this parent's bounded private pipe before applying the _test.go adapter.
func TestCoordinatorProcess(t *testing.T) {
	if os.Getenv("DOTTY_COORDINATOR_TEST_CHILD") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// exec passes a blocking ExtraFiles descriptor. Validate it as a pipe and
	// make this child-owned endpoint pollable before NewFile, so event writes
	// have real deadlines rather than relying on an unsupported File deadline.
	var eventStat unix.Stat_t
	must(t, unix.Fstat(3, &eventStat))
	if statIdentity(&eventStat).Kind != unix.S_IFIFO {
		t.Fatal("event channel is not a pipe")
	}
	must(t, unix.SetNonblock(3, true))
	events := os.NewFile(3, "private-coordinator-events")
	if events == nil {
		t.Fatal("missing event pipe")
	}
	defer func() { _ = events.Close() }()
	for _, file := range []*os.File{events, os.Stdin} {
		info, err := file.Stat()
		must(t, err)
		if info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatal("IPC is not a pipe")
		}
	}
	lines := make(chan string, 4)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 1024), 8192)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	var config coordinatorProcessConfig
	must(t, json.Unmarshal([]byte(awaitFlock(t, ctx, lines)), &config))
	canonical := filepath.Dir(canonicalUserLockPath())
	if config.Root == "/" || config.Root == systemTemporaryRoot || config.Root == canonical ||
		strings.HasPrefix(config.Root, canonical+"/") ||
		config.Repository != filepath.Join(config.Root, "z") ||
		!config.RootIdentity.Valid ||
		!config.RepositoryIdentity.Valid {
		t.Fatal("invalid private child authority")
	}
	root := openDir(t, config.Root)
	if root.state.leaf().identity != config.RootIdentity {
		t.Fatal("private root identity changed")
	}
	facts, err := readAuthorityFacts(root.state.leaf().fd, systemAuthorityCalls())
	must(t, err)
	must(t, validateAuthority(config.Root, facts, RolePrivateAnchor, uint32(unix.Geteuid())))
	if err := root.state.guard("coordinator-child-authority"); err != nil {
		t.Fatal(err)
	}
	repo := openDir(t, config.Repository)
	if repo.state.leaf().identity != config.RepositoryIdentity {
		t.Fatal("private repository identity changed")
	}
	_, err = fixtureAuthority(repo, config.Root, RoleRepositoryDirectory, systemAuthorityCalls())
	must(t, err)
	t.Setenv("HOME", config.Repository)
	t.Setenv("XDG_CONFIG_HOME", config.Repository)
	t.Setenv("DOTTY_REPO", config.Repository)
	signal := func(value string) {
		must(t, events.SetWriteDeadline(time.Now().Add(5*time.Second)))
		_, err := fmt.Fprintln(events, value)
		must(t, err)
	}
	calls := flockCalls{private: privateFixtureCalls(config.Root), flock: unix.Flock}
	var once sync.Once
	calls.flock = func(fd, flags int) error {
		err := unix.Flock(fd, flags)
		if flags == unix.LOCK_EX|unix.LOCK_NB && errors.Is(err, unix.EWOULDBLOCK) {
			once.Do(func() { signal("contended") })
		}
		return err
	}
	signal("ready")
	if awaitFlock(t, ctx, lines) != "go" {
		t.Fatal("invalid acquisition handshake")
	}
	lease, err := acquireUserAt(ctx, config.Root, calls)
	if lease != nil {
		defer func() { _ = lease.Release() }()
	}
	must(t, err)
	if lease == nil {
		t.Fatal("missing child user lease")
	}
	// Deliberately read a test config only AFTER AcquireUser, unlike the parent
	// which selects its different repository explicitly between stages.
	data, err := os.ReadFile(filepath.Join(config.Repository, "choice.json"))
	must(t, err)
	var selected string
	must(t, json.Unmarshal(data, &selected))
	if selected != config.Repository {
		t.Fatal("config selection changed")
	}
	must(t, lease.LockRepositories(ctx, []string{selected}))
	bindings := lease.RepositoryBindings()
	if len(bindings) != 1 || bindings[0].Identity() != config.RepositoryIdentity {
		t.Fatal("child repository binding changed")
	}
	signal("acquired")
	if awaitFlock(t, ctx, lines) != "release" {
		t.Fatal("invalid release handshake")
	}
	must(t, lease.Release())
	signal("released")
	t.Logf(
		"bounded coordinator child root=%q identity=%+v facts=%+v config-selection=%q binding=%+v",
		config.Root,
		config.RootIdentity,
		facts,
		selected,
		bindings[0],
	)
}
