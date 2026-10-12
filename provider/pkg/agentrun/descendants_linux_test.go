// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"os"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStat(t *testing.T) {
	e, ok := parseStat(42, []byte("42 (a) b (c) S 7 42 40 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 123456 1000 10\n"))
	require.True(t, ok)
	assert.Equal(t, procEntry{procKey: procKey{pid: 42, start: 123456}, ppid: 7, pgid: 42, sid: 40, uid: -1}, e)
	e, ok = parseStat(43, []byte("43 (z) Z 1 43 43 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 99 0 0"))
	require.True(t, ok)
	assert.True(t, e.zombie)
	e, ok = parseStat(2, []byte("2 (kthreadd) S 0 0 0 0 -1 2129984 0 0 0 0 0 0 0 0 20 0 1 0 5 0 0"))
	require.True(t, ok)
	assert.True(t, e.kthread, "PF_KTHREAD")
	for _, bad := range []string{"", "42 (x", "42 (x) S 1 2", "42 (x) S a 2 3 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 99"} {
		_, ok := parseStat(1, []byte(bad))
		assert.False(t, ok, "%q", bad)
	}
}

// TestReadStatOwner: the user of a process comes from the owner of its stat file.
func TestReadStatOwner(t *testing.T) {
	e, ok := procByPID(os.Getpid())
	require.True(t, ok)
	assert.Equal(t, os.Geteuid(), e.uid)
	assert.False(t, e.kthread)
}

// TestProcTreeKeepsRecordsWhenTheTableFails: when the reader itself fails (no file
// descriptor left), the read fails as a whole, and the records stay; they are not dropped as
// if their processes had ended.
func TestProcTreeKeepsRecordsWhenTheTableFails(t *testing.T) {
	cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
	child := startSleep(t, &syscall.SysProcAttr{Setsid: true})
	cliEntry, ok := readProc(cli.Process.Pid)
	require.True(t, ok)
	childEntry, ok := readProc(child.Process.Pid)
	require.True(t, ok)
	childEntry.ppid = cli.Process.Pid
	prev := readProcTable
	var fail atomic.Bool
	readProcTable = func(since uint64) ([]procEntry, error) {
		if fail.Load() {
			// The real reader, with every open of the child's stat failing with EMFILE.
			prevOpen := openProcFile
			defer func() { openProcFile = prevOpen }()
			openProcFile = func(path string) (int, error) {
				if path == "/proc/"+strconv.Itoa(child.Process.Pid)+"/stat" {
					return -1, syscall.EMFILE
				}
				return prevOpen(path)
			}
			return procTable(since)
		}
		return []procEntry{cliEntry, childEntry}, nil
	}
	t.Cleanup(func() { readProcTable = prev })
	tree := newProcTree(cli.Process.Pid, "", nil)
	require.NotNil(t, tree)
	tree.stopPolling()
	require.Equal(t, []procKey{childEntry.procKey}, procKeys(tree.scan()))
	fail.Store(true)
	assert.Equal(t, []procKey{childEntry.procKey}, procKeys(tree.scan()), "the record survives a failed read")
	assert.True(t, tree.tracking())
}
