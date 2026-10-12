// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bytes"
	"errors"
	"math"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// procTable reads /proc/<pid>/stat of every process. It skips processes that ended while it
// read, that it may not read (hidepid), and those that started before since. A failure of
// the reader itself (no file descriptor or memory left) fails the whole read: a process it
// could not read would otherwise lose its record.
func procTable(since uint64) ([]procEntry, error) {
	d, err := os.Open("/proc")
	if err != nil {
		return nil, err
	}
	names, err := d.Readdirnames(-1)
	_ = d.Close()
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(names))
	for _, n := range names {
		if pid, err := strconv.Atoi(n); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	if len(pids) > maxProcTable {
		return nil, errProcTableTooLarge
	}
	var buf [statBufSize]byte
	out := make([]procEntry, 0, 64)
	for _, pid := range pids {
		e, ok, err := readStat(pid, buf[:])
		if err != nil {
			return nil, err
		}
		if ok && e.start >= since {
			out = append(out, e)
		}
	}
	return out, nil
}

// procByPID reads /proc/<pid>/stat.
func procByPID(pid int) (procEntry, bool) {
	var buf [statBufSize]byte
	e, ok, _ := readStat(pid, buf[:])
	return e, ok
}

// statBufSize holds a stat line: 52 numbers and a command name of at most 64 bytes.
const statBufSize = 2048

// openProcFile opens a file of /proc for reading; tests replace it.
var openProcFile = func(path string) (int, error) {
	return syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
}

// readerFailure reports whether an error is the reader's own (no file descriptor or memory
// left) rather than the process's (gone, not readable).
func readerFailure(err error) bool {
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) || errors.Is(err, syscall.ENOMEM)
}

// readStat reads and parses /proc/<pid>/stat: "pid (comm) state ppid pgrp session ... flags
// ... starttime ...", and takes the process's user from the file's owner (its effective user,
// or root when it is not dumpable, whose environment cannot be read either). ok is false
// when the process is gone or not readable; err is a reader failure.
func readStat(pid int, buf []byte) (e procEntry, ok bool, err error) {
	fd, err := openProcFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if readerFailure(err) {
			return procEntry{}, false, err
		}
		return procEntry{}, false, nil
	}
	defer syscall.Close(fd) //nolint:errcheck
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return procEntry{}, false, nil
	}
	n := 0
	for {
		n, err = syscall.Read(fd, buf)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil || n <= 0 {
		if err != nil && readerFailure(err) {
			return procEntry{}, false, err
		}
		return procEntry{}, false, nil
	}
	e, ok = parseStat(pid, buf[:n])
	e.uid = int(st.Uid)
	return e, ok, nil
}

// pfKthread is PF_KTHREAD of the stat flags: a kernel thread.
const pfKthread = 0x00200000

// parseStat parses a stat line. The command name may contain spaces and parentheses: the
// fields start after the last ')'.
func parseStat(pid int, line []byte) (procEntry, bool) {
	i := bytes.LastIndexByte(line, ')')
	if i < 0 {
		return procEntry{}, false
	}
	// The fields after the name, from 0: state, ppid, pgrp, session, ..., flags (6), ...,
	// starttime (19).
	var state byte
	var nums [20]uint64
	field := 0
	for j := i + 1; j < len(line) && field <= 19; field++ {
		for j < len(line) && line[j] == ' ' {
			j++
		}
		start := j
		for j < len(line) && line[j] != ' ' && line[j] != '\n' {
			j++
		}
		switch {
		case start == j:
			return procEntry{}, false
		case field == 0:
			state = line[start]
		case field <= 3 || field == 6 || field == 19:
			for _, c := range line[start:j] {
				if c < '0' || c > '9' {
					return procEntry{}, false
				}
				nums[field] = nums[field]*10 + uint64(c-'0')
			}
		}
	}
	if field <= 19 || nums[1] > math.MaxInt32 || nums[2] > math.MaxInt32 || nums[3] > math.MaxInt32 {
		return procEntry{}, false
	}
	return procEntry{
		procKey: procKey{pid: pid, start: nums[19]},
		ppid:    int(nums[1]),
		pgid:    int(nums[2]),
		sid:     int(nums[3]),
		uid:     -1,
		zombie:  state == 'Z' || state == 'X',
		kthread: nums[6]&pfKthread != 0,
	}, true
}

// environBufSize is the most of a process's environment that procHasCookie reads: ARG_MAX
// with an 8 MiB stack.
const environBufSize = 2 << 20

// procHasCookie reports whether a process has the entry in its environment
// (/proc/<pid>/environ: the environment it was started with). An unreadable environment
// (another user's process, one that ended) does not. Linux never hides the environment from
// its user.
func procHasCookie(e *procEntry, entry string, buf []byte) (found, hidden bool) {
	fd, err := openProcFile("/proc/" + strconv.Itoa(e.pid) + "/environ")
	if err != nil {
		return false, false
	}
	defer syscall.Close(fd) //nolint:errcheck
	n := 0
	for n < len(buf) {
		m, err := syscall.Read(fd, buf[n:])
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || m <= 0 {
			break
		}
		n += m
	}
	return environHas(buf[:n], entry), false
}

// signalProc sends the signals to the process with that key, if it still runs. Through a
// pidfd, the check and the signals reach the same process even if its PID were reused in
// between; without one (a kernel before 5.3, a seccomp filter), the check comes right before
// the signals.
func signalProc(k procKey, sigs ...syscall.Signal) {
	fd, err := unix.PidfdOpen(k.pid, 0)
	if err == nil {
		defer unix.Close(fd) //nolint:errcheck
	} else if errors.Is(err, unix.ESRCH) {
		return
	}
	if e, ok := readProc(k.pid); !ok || e.procKey != k || e.zombie {
		return
	}
	for _, sig := range sigs {
		if err == nil {
			_ = unix.PidfdSendSignal(fd, sig, nil, 0)
		} else {
			_ = syscall.Kill(k.pid, sig)
		}
	}
}
