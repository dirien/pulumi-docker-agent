// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"os"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
)

// sZomb is SZOMB of <sys/proc.h>: the process exited and waits for its parent.
const sZomb = 5

// startSlack: macOS records the start of a process in wall-clock time, which can step
// backwards; procTable keeps processes that started up to a minute before since.
const startSlack = 60 * 1_000_000

// environBufSize: procHasCookie gets its buffer from sysctl.
const environBufSize = 0

// allProcs is set when kern.proc.ruid cannot be read: procTable then reads kern.proc.all.
var allProcs atomic.Bool

// procTable reads the processes of the provider's real user (sysctl kern.proc.ruid.<uid>;
// for root, or when that fails, kern.proc.all) and the session of each that started at or
// after since. Reading the user's processes only keeps the table, and the memory each read
// allocates, small; the provider cannot signal other users' processes anyway, unless it runs
// as root.
func procTable(since uint64) ([]procEntry, error) {
	var kps []unix.KinfoProc
	var err error
	uid := os.Getuid()
	if uid != 0 && !allProcs.Load() {
		if kps, err = unix.SysctlKinfoProcSlice("kern.proc.ruid", uid); err != nil {
			allProcs.Store(true)
		}
	}
	if uid == 0 || allProcs.Load() {
		if kps, err = unix.SysctlKinfoProcSlice("kern.proc.all"); err != nil {
			return nil, err
		}
	}
	if len(kps) > maxProcTable {
		return nil, errProcTableTooLarge
	}
	floor := since - min(since, startSlack)
	out := make([]procEntry, 0, 64)
	for i := range kps {
		e := kinfoEntry(&kps[i])
		if e.pid <= 0 || e.start < floor {
			continue
		}
		e.sid = sessionOf(e.pid)
		out = append(out, e)
	}
	return out, nil
}

// procByPID reads one process (sysctl kern.proc.pid.<pid>) and its session.
func procByPID(pid int) (procEntry, bool) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return procEntry{}, false
	}
	e := kinfoEntry(k)
	if e.pid != pid {
		return procEntry{}, false
	}
	e.sid = sessionOf(pid)
	return e, true
}

func kinfoEntry(k *unix.KinfoProc) procEntry {
	tv := k.Proc.P_starttime
	return procEntry{
		procKey: procKey{pid: int(k.Proc.P_pid), start: uint64(tv.Sec)*1_000_000 + uint64(tv.Usec)}, //nolint:gosec // not negative
		ppid:    int(k.Eproc.Ppid),
		pgid:    int(k.Eproc.Pgid),
		sid:     -1,
		uid:     int(k.Eproc.Ucred.Uid),
		zombie:  k.Proc.P_stat == sZomb,
	}
}

// sessionOf returns the session of a process, -1 when it cannot be read (the process ended,
// or is a zombie). XNU's getsid reads the session of any process, also of one in another
// session or of another user: it fails only with ESRCH, and does not use POSIX's permission to
// refuse a process of another session with EPERM (bsd/kern/kern_prot.c). kinfo_proc carries
// no session ID (only a kernel pointer, e_sess), so getsid is the source; a session that
// cannot be read leaves the process out of the run child's session sweep (descendants.go),
// which logs that at debug level.
func sessionOf(pid int) int {
	sid, err := unix.Getsid(pid)
	if err != nil {
		return -1
	}
	return sid
}

// procHasCookie reports whether a process of the provider's user has the entry in its
// environment (sysctl kern.procargs2.<pid>: the environment it was started with). The sysctl
// fails (EINVAL, EPERM) for other users' processes and for zombies, which then do not; hidden
// reports a process whose environment macOS leaves out (procargsHas).
func procHasCookie(e *procEntry, entry string, _ []byte) (found, hidden bool) {
	if e.uid != os.Geteuid() {
		return false, false
	}
	b, err := unix.SysctlRaw("kern.procargs2", e.pid)
	if err != nil {
		return false, false
	}
	return procargsHas(b, entry)
}

// signalProc sends the signals to the process with that key, if it still runs: the check
// comes right before the signals (macOS has no pidfd).
func signalProc(k procKey, sigs ...syscall.Signal) {
	if e, ok := readProc(k.pid); !ok || e.procKey != k || e.zombie {
		return
	}
	for _, sig := range sigs {
		_ = syscall.Kill(k.pid, sig)
	}
}
