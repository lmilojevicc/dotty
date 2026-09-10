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

type flockProcessConfig struct {
	Root             string
	RootIdentity     Identity
	Selected         string
	SelectedIdentity Identity
	Directory        bool
}

type flockProcess struct {
	ctx      context.Context
	input    *os.File
	events   <-chan string
	done     <-chan error
	output   *unsupportedBuildOutput
	finished bool
}

func startFlockProcess(t *testing.T, d *Dir, directory bool) *flockProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	input, writer, err := os.Pipe()
	must(t, err)
	t.Cleanup(func() { _ = input.Close(); _ = writer.Close() })
	reader, output, err := os.Pipe()
	if err != nil {
		_ = input.Close()
		_ = writer.Close()
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = output.Close() })
	executable, err := os.Executable()
	must(t, err)
	cmd := exec.CommandContext(
		ctx,
		executable,
		"-test.run=^TestFlockProcess$",
		"-test.v",
		"-test.timeout=15s",
	)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = input
	cmd.ExtraFiles = []*os.File{output}
	var captured unsupportedBuildOutput
	cmd.Stdout, cmd.Stderr = &captured, &captured
	// The opt-in is helper-only. Authority arrives over this process's private
	// control pipe, not a production environment/config boundary override.
	cmd.Env = append(os.Environ(), "DOTTY_FLOCK_TEST_CHILD=1")
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		_ = writer.Close()
		_ = reader.Close()
		_ = output.Close()
		cancel()
		t.Fatal(err)
	}
	// Every successful Start owns exactly one Wait and unconditional reaping
	// before any post-start assertion, including closing our child pipe ends.
	events := make(chan string, 8)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	child := &flockProcess{ctx: ctx, input: writer, events: events, done: done, output: &captured}
	t.Cleanup(func() {
		_ = writer.Close()
		cancel() // Kill on incomplete handshake, then reap; WaitDelay bounds output drain.
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
	root := filepath.Dir(d.state.leaf().path)
	var rootID Identity
	for _, node := range d.state.nodes {
		if node.path == root {
			rootID = node.identity
		}
	}
	selectedID := d.state.leaf().identity
	if !directory {
		var err error
		selectedID, err = statEntry(d.state.leaf().fd, Component{name: "lock"})
		must(t, err)
	}
	config := flockProcessConfig{
		Root:             root,
		RootIdentity:     rootID,
		Selected:         d.state.leaf().path,
		SelectedIdentity: selectedID,
		Directory:        directory,
	}
	data, err := json.Marshal(config)
	must(t, err)
	child.send(t, string(data))
	child.expect(t, "ready")
	return child
}

func (p *flockProcess) send(t *testing.T, value string) {
	t.Helper()
	// Small bounded messages on an otherwise empty pipe. A deadline prevents an
	// unresponsive child from trapping its parent in a write.
	must(t, p.input.SetWriteDeadline(time.Now().Add(5*time.Second)))
	_, err := fmt.Fprintln(p.input, value)
	must(t, err)
}

func (p *flockProcess) expect(t *testing.T, want string) {
	t.Helper()
	got := awaitFlock(t, p.ctx, p.events)
	if got != want {
		t.Fatalf("child event=%q want=%q\n%s", got, want, p.output.String())
	}
}

func (p *flockProcess) finish(t *testing.T) {
	t.Helper()
	err := awaitFlock(t, p.ctx, p.done)
	p.finished = true
	if err != nil {
		t.Fatalf("child failed: %v\n%s", err, p.output.String())
	}
	t.Logf("real process read-only flock proof:\n%s", p.output.String())
}

func testFlockProcesses(t *testing.T, directory bool) {
	_, d, calls := flockFixture(t, directory)
	ctx, cancel := flockContext(t)
	defer cancel()
	first := requireFlock(t, ctx, d, directory, calls)
	child := startFlockProcess(t, d, directory)
	child.send(t, "go")
	child.expect(t, "contended") // Actual native EWOULDBLOCK, never a sleep proof.
	must(t, first.Release())
	child.expect(t, "acquired")
	child.send(t, "release")
	child.expect(t, "released")
	child.finish(t)
	last := requireFlock(t, ctx, d, directory, calls)
	must(t, last.Release())
}

// Helper is outside the exact fifteen roots; it runs only on a bounded parent
// command and never accepts a host canonical-anchor path as fixture authority.
func TestFlockProcess(t *testing.T) {
	if os.Getenv("DOTTY_FLOCK_TEST_CHILD") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	events := os.NewFile(3, "private-flock-events")
	if events == nil {
		t.Fatal("missing event pipe")
	}
	defer func() { must(t, events.Close()) }()
	info, err := events.Stat()
	must(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("event channel is not a pipe")
	}
	info, err = os.Stdin.Stat()
	must(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("control channel is not a pipe")
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
	var config flockProcessConfig
	must(t, json.Unmarshal([]byte(awaitFlock(t, ctx, lines)), &config))
	canonical := filepath.Join(systemTemporaryRoot, fmt.Sprintf("dotty-%d", unix.Geteuid()))
	if config.Root == "/" ||
		config.Root == systemTemporaryRoot ||
		config.Root == canonical ||
		strings.HasPrefix(config.Root, canonical+"/") ||
		config.Selected != filepath.Join(config.Root, "selected") ||
		!config.RootIdentity.Valid ||
		!config.SelectedIdentity.Valid {
		t.Fatal("invalid private child authority")
	}
	// No alias cleaning. Prove parent-supplied identity, native private-root
	// ownership/mode/filesystem/ACL and full physical binding BEFORE the adapter.
	root := openDir(t, config.Root)
	if root.state.leaf().identity != config.RootIdentity {
		t.Fatal("private root identity changed")
	}
	facts, err := readAuthorityFacts(root.state.leaf().fd, systemAuthorityCalls())
	must(t, err)
	must(t, validateAuthority(config.Root, facts, RolePrivateAnchor, uint32(unix.Geteuid())))
	if err := root.state.guard("child-fixture-authority"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", config.Root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(config.Root, "config"))
	t.Setenv("DOTTY_REPO", filepath.Join(config.Root, "repo"))
	d := openDir(t, config.Selected)
	id := d.state.leaf().identity
	if !config.Directory {
		id, err = statEntry(d.state.leaf().fd, Component{name: "lock"})
		must(t, err)
	}
	if id != config.SelectedIdentity {
		t.Fatal("selected fixture identity changed")
	}
	calls := flockCalls{private: privateFixtureCalls(config.Root), flock: unix.Flock}
	var once sync.Once
	calls.flock = func(fd, flags int) error {
		actualFlags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			return err
		}
		if actualFlags&unix.O_ACCMODE != unix.O_RDONLY {
			return errors.New("flock description was not read-only")
		}
		err = unix.Flock(fd, flags)
		if flags == unix.LOCK_EX|unix.LOCK_NB && errors.Is(err, unix.EWOULDBLOCK) {
			once.Do(func() { _, writeErr := fmt.Fprintln(events, "contended"); must(t, writeErr) })
		}
		return err
	}
	signal := func(value string) { _, err := fmt.Fprintln(events, value); must(t, err) }
	signal("ready")
	if awaitFlock(t, ctx, lines) != "go" {
		t.Fatal("invalid acquisition handshake")
	}
	lease := requireFlock(t, ctx, d, config.Directory, calls)
	signal("acquired")
	if awaitFlock(t, ctx, lines) != "release" {
		t.Fatal("invalid release handshake")
	}
	must(t, lease.Release())
	signal("released")
	t.Logf("native child directory=%t root-facts=%+v selected=%+v", config.Directory, facts, id)
}
