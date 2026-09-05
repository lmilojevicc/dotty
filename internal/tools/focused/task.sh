#!/usr/bin/env bash
# Internal mise wrapper; the public interface remains PACKAGE PATTERN.
if [ "$#" -ne 2 ] || [ -z "$1" ] || [ -z "$2" ]; then
  echo 'usage: test:focused PACKAGE PATTERN' >&2
  exit 2
fi
case "$(uname -s)" in
  Linux|Darwin) ;;
  *) echo 'focused execution requires Linux or macOS for owned process-group cancellation' >&2; exit 1 ;;
esac

tmp=$(mktemp -d "${TMPDIR:-/tmp}/dotty-focused.XXXXXXXX") || exit "$?"
trap 'rm -rf "$tmp"' EXIT
mkfifo "$tmp/status" "$tmp/control" || exit "$?"
cancelled=0
build_ready=0
stage=build
cancel() {
  [ "$stage" = finished ] && return
  if [ "$cancelled" -eq 0 ]; then cancelled=$1; fi
  if [ "$stage" = build ] && [ "$build_ready" -eq 1 ]; then
    # Bash 3.2 can resume a trapped FIFO read instead of returning from it.
    # Forward here once BUILDING proves the anchor is ready, not after read.
    stop_build
  elif [ "$stage" = startup ]; then
    # No ACK has been sent: the runner cannot own a separate child group.
    # Abort here even if Bash resumes (or corrupts) the interrupted read.
    stop_startup
  elif [ "$stage" = runner ]; then
    # Always wake an outstanding ACK read, even if the write reported success
    # but delivered only a partial/malformed frame. STOP before or within ACK
    # forbids child startup; after a complete ACK the anchor discards it.
    # Only runner-managed shutdown is allowed once ACK may have been written.
    printf 'STOP\n' >&4
    kill -"$2" -- "-$anchor" 2>/dev/null
  fi
}
trap 'cancel 130 INT' INT
trap 'cancel 143 TERM' TERM
trap ':' PIPE
# Job control gives this anchor (not the wrapper or caller) a fresh group.
# Keep it alive through RUNNER_DONE until explicit RELEASE/EOF: Bash may reap
# the async runner before wait, so its numeric PID is NEVER a signal target.
set -m
(
  set +m
  interrupted=0
  trap 'interrupted=1' INT TERM
  trap ':' PIPE
  # Open status writer first, paired with the wrapper's status reader; then
  # pair control reader/writer. No self-held FIFO writer can hide control EOF.
  exec 3> "$tmp/status"
  exec 4< "$tmp/control"
  exec 5<&0
  read_control() {
    local part read_code
    control=
    while :; do
      IFS= read -r part <&4
      read_code=$?
      control=$control$part
      [ "$read_code" -eq 0 ] && return 0
      [ "$read_code" -gt 128 ] || return 1
    done
  }
  printf 'BUILDING\n' >&3
  go build -o "$tmp/focused" ./internal/tools/focused 3>&- 4<&- 5<&-
  code=$?
  printf 'BUILD_STATUS %s\n' "$code" >&3
  if read_control && [ "$control" = START ] && [ "$code" -eq 0 ]; then
    # Async Bash children inherit ignored INT with job control off. Go's
    # signal.Notify enables it before RUNNER_READY. Only the runner reads
    # control for ACK while we wait; it closes fd 4 before starting any work.
    DOTTY_FOCUSED_WRAPPER=1 "$tmp/focused" "$1" "$2" <&5 5<&- &
    runner=$!
    while :; do
      interrupted=0
      wait "$runner"
      code=$?
      [ "$interrupted" -eq 1 ] || break
    done
    printf 'RUNNER_DONE %s\n' "$code" >&3
  fi
  # Even a failed/malformed status or early runner exit must not drop our
  # group-ID reservation. Unconsumed ACK/STOP bytes (including partial-frame
  # tails) are ignored only after runner wait, without competing readers.
  # Build failure also retains ownership until explicit RELEASE/control EOF.
  while [ "$control" != RELEASE ] && read_control; do :; done
) &
anchor=$!
exec 3< "$tmp/status"
exec 4> "$tmp/control"

finish() {
  stage=finished
  # Disable forwarding BEFORE release/reap/cleanup, including on error paths.
  # Traps still catch signals so an interrupted wait can be retried.
  interrupted=0
  trap 'interrupted=1' INT TERM
  trap ':' PIPE
  if [ "${1:-}" != killed ]; then printf 'RELEASE\n' >&4; fi
  exec 4>&-
  exec 3<&-
  while :; do
    interrupted=0
    wait "$anchor" 2>/dev/null
    [ "$interrupted" -eq 1 ] || break
  done
  # EXIT removes only this invocation's private directory, synchronously.
}
stop_build() {
  # Mask repeated signals before forwarding, including trap-driven entry.
  trap ':' INT TERM
  if [ "$cancelled" -eq 130 ]; then
    kill -INT -- "-$anchor" 2>/dev/null
  else
    kill -TERM -- "-$anchor" 2>/dev/null
  fi
  # Repeated signals must not shorten the build group's escalation grace.
  sleep 0.25
  kill -KILL -- "-$anchor" 2>/dev/null
  # The anchor is deliberately killed here, so close rather than write RELEASE.
  finish killed
  exit "$cancelled"
}
stop_startup() {
  # Before ACK, INT may still be inherited as ignored. TERM then KILL stops
  # only our persistent outer group; no separately grouped work can exist.
  # Keep the anchor reserved through escalation, just as for build abort.
  trap ':' INT TERM
  kill -TERM -- "-$anchor" 2>/dev/null
  sleep 0.25
  kill -KILL -- "-$anchor" 2>/dev/null
  finish killed
  if [ "$cancelled" -ne 0 ]; then exit "$cancelled"; fi
  exit 1
}
protocol_error() {
  echo "focused: ${1:-invalid or incomplete wrapper status: $message}" >&2
  code=1
  if [ "$stage" != runner ]; then stop_startup; fi
  # ACK may already have launched a separate child group. The registered
  # runner, not outer-group KILL, must stop/reap that work before anchor wait.
  kill -TERM -- "-$anchor" 2>/dev/null
}
valid_status() {
  case "$1" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "${#1}" -le 3 ] && [ "$1" -le 255 ]
}
read_status() {
  local part read_code
  message=
  while :; do
    IFS= read -r part <&3
    read_code=$?
    message=$message$part
    [ "$read_code" -eq 0 ] && return 0
    # A caught signal can return >128 OR resume read on Bash 3.2. Damaged READY
    # was observed in an instrumented fixture; its cause is unproven. Never
    # infer READY from a prefix, regardless of how a malformed frame arose.
    # Startup cancellation aborts in the trap without awaiting read.
    [ "$read_code" -gt 128 ] && continue
    protocol_error
    return 1
  done
}
# BUILDING is sent only after the anchor installed traps and opened both FIFOs.
# Defer early cancellation until this acknowledgment establishes readiness.
code=1
while read_status; do
  case "$message" in
    BUILDING)
      if [ "$stage" != build ] || [ "$build_ready" -ne 0 ]; then protocol_error; break; fi
      build_ready=1
      if [ "$cancelled" -ne 0 ]; then stop_build; fi
      ;;
    'BUILD_STATUS '*)
      code=${message#BUILD_STATUS }
      if [ "$stage" != build ] || [ "$build_ready" -ne 1 ] || ! valid_status "$code"; then
        protocol_error; break
      fi
      if [ "$cancelled" -ne 0 ]; then stop_build; fi
      if [ "$code" -ne 0 ]; then break; fi
      code=1
      stage=startup
      # No separately grouped work may start until a complete READY and ACK.
      if [ "$cancelled" -ne 0 ]; then stop_startup; fi
      printf 'START\n' >&4 || { protocol_error 'cannot write runner start'; break; }
      ;;
    RUNNER_READY)
      if [ "$stage" != startup ]; then protocol_error; break; fi
      # Enable runner-managed forwarding BEFORE ACK can launch child work.
      # A signal in this transition is queued by the registered runner; if
      # already observed here, abort without ACK while outer KILL is safe.
      stage=runner
      if [ "$cancelled" -ne 0 ]; then stop_startup; fi
      # Even a write error must not assume that no ACK bytes were delivered.
      # Error cleanup closes control to unblock an incomplete ACK reader and
      # leaves any acknowledged child shutdown to the registered runner.
      printf 'ACK\n' >&4 || { protocol_error 'cannot write wrapper acknowledgment'; break; }
      ;;
    'RUNNER_DONE '*)
      code=${message#RUNNER_DONE }
      if { [ "$stage" != startup ] && [ "$stage" != runner ]; } || ! valid_status "$code"; then
        protocol_error; break
      fi
      # Startup failure without READY is valid only as a nonzero exit.
      if [ "$stage" = startup ] && [ "$code" -eq 0 ]; then protocol_error; fi
      break
      ;;
    *) protocol_error; break ;;
  esac
done
finish
if [ "$cancelled" -ne 0 ]; then exit "$cancelled"; fi
exit "$code"
