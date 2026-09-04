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
  elif [ "$stage" = runner ]; then
    kill -"$2" -- "-$anchor" 2>/dev/null
  fi
}
trap 'cancel 130 INT' INT
trap 'cancel 143 TERM' TERM
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
    # signal.Notify explicitly enables it before writing RUNNER_READY on fd 3.
    DOTTY_FOCUSED_WRAPPER=1 "$tmp/focused" "$1" "$2" <&5 4<&- 5<&- &
    runner=$!
    while :; do
      interrupted=0
      wait "$runner"
      code=$?
      [ "$interrupted" -eq 1 ] || break
    done
    printf 'RUNNER_DONE %s\n' "$code" >&3
    # Even a failed status write must not drop our group-ID reservation.
    # Only the wrapper's release or closed control stream ends ownership.
    while read_control; do
      [ "$control" = RELEASE ] && break
    done
  fi
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
read_status() {
  local part read_code
  message=
  while :; do
    IFS= read -r part <&3
    read_code=$?
    message=$message$part
    [ "$read_code" -eq 0 ] && return 0
    # Bash read returns >128 when a caught signal interrupts it. EOF is 1.
    # Keep any partial record across interruptions; do not retry ordinary EOF.
    [ "$read_code" -gt 128 ] && continue
    code=1
    return 1
  done
}
# BUILDING is sent only after the anchor installed traps and opened both FIFOs.
# Defer early cancellation until this acknowledgment establishes readiness.
code=1
while read_status; do
  case "$message" in
    BUILDING)
      build_ready=1
      if [ "$cancelled" -ne 0 ]; then stop_build; fi
      ;;
    'BUILD_STATUS '*)
      code=${message#BUILD_STATUS }
      if [ "$cancelled" -ne 0 ]; then stop_build; fi
      if [ "$code" -ne 0 ]; then break; fi
      code=1
      stage=startup
      # Cancellation in this transition is recorded, not sent to a runner
      # that has merely launched. RUNNER_READY makes forwarding safe.
      printf 'START\n' >&4 || break
      ;;
    RUNNER_READY)
      stage=runner
      if [ "$cancelled" -eq 130 ]; then cancel 130 INT; fi
      if [ "$cancelled" -eq 143 ]; then cancel 143 TERM; fi
      ;;
    'RUNNER_DONE '*)
      code=${message#RUNNER_DONE }
      break
      ;;
    *) code=1; break ;;
  esac
done
finish
if [ "$cancelled" -ne 0 ]; then exit "$cancelled"; fi
exit "$code"
