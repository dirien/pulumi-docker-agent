// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Claude Code runs every shell command in a session of its own, so its commands are in
// neither the CLI's process group nor its session, and the group signals of harness_unix.go
// do not reach them; it also leaves the commands it ran in the background running when it
// exits. The same holds for a run child (child.go), whose tools run in groups of their own:
// it is tracked like a CLI, with childCookieVar. A procTree therefore finds the processes started under one CLI in two ways, and the
// cleanup of harness_unix.go stops them with the CLI's group, whatever the CLI does with them:
//
//   - the tree: every procPollInterval while the CLI runs, and at each cleanup step, it reads
//     the process table and records every process whose parent is the CLI or a recorded
//     process, by PID and start time. A record outlives the parent: a process whose parent
//     exited (it then belongs to init or launchd) stays recorded, and so do its children. A
//     record is dropped when its process is gone;
//   - the cookie: the CLI gets a random 128-bit value in DOCKER_AGENT_RUN_COOKIE, which every
//     process it starts inherits. At each cleanup step (not at the polls), the processes of
//     the provider's user that started after the CLI, outside the CLI's group and not
//     recorded yet, are recorded when the environment they were started with holds exactly
//     that variable and value. This finds what the tree cannot: a process whose parent exited
//     before a poll saw it (`cmd &` in a shell that exits at once, a double fork). The reads
//     are bounded in number and time;
//   - it signals a recorded process only while one with its PID and start time runs (checked
//     right before the signal; on Linux through a pidfd), so a reused PID is never signalled;
//   - it signals a process group, or the groups of a session, only when a recorded process
//     created it, as its leader, and while that ID was never seen without members or with
//     another leader since: the kernel does not reuse the ID of a group or session that still
//     has members, so the ID is still the one created under the CLI;
//   - it never signals PID 1, the provider, or the provider's process group or session.
//
// So it signals only processes recorded under the CLI, the groups and sessions they created,
// and processes of the provider's user that started after the CLI and carry the exact cookie.
//
// Limits: a process that left the CLI's group and session, whose parent exited before a poll
// saw it, and whose environment does not show the cookie is not found: one started with a
// cleared or replaced environment (`env -i`), or, on macOS 11 and later, one whose
// environment the system does not show to other processes (programs restricted by their code
// signature, such as some signed with entitlements, while System Integrity Protection is on). Tracking is best effort and bounded: when the
// process table cannot be read, or has more than maxProcTable processes, the cleanup stops
// the CLI's process group and what it can check one by one, and the reason is logged at
// debug level.

// Tracking limits. Variables, so that tests can change them.
var (
	// procPollInterval is how often the process table is read while the CLI runs. A slow read
	// (a large table) stretches it to procPollCost times the time the read took.
	procPollInterval = 200 * time.Millisecond
	// maxProcTable is the largest process table that is read.
	maxProcTable = 1 << 16
	// maxTrackedProcs is the most running processes one CLI's tree records.
	maxTrackedProcs = 4096
	// maxCookieReads is the most environments one cleanup step reads.
	maxCookieReads = 1024
	// cookieSweepTimeout bounds the time one cleanup step reads environments: a read can
	// block (Linux takes the process's memory lock, which a hung page fault holds).
	cookieSweepTimeout = time.Second
)

const procPollCost = 5

// Access to the process table (descendants_linux.go, descendants_darwin.go). Variables, so
// that tests can replace them.
var (
	// readProcTable returns the processes that started at or after since, a start time as
	// procKey has it (it may return older ones too).
	readProcTable = procTable
	// readProc returns the process with a PID, false when there is none.
	readProc = procByPID
	// readCookie reports whether a process has an entry in its environment, and whether the
	// system hid the environment (macOS).
	readCookie = procHasCookie
)

var errProcTableTooLarge = errors.New("the process table is too large")

// procKey identifies a process: its PID, and its start time (Linux: clock ticks since boot;
// macOS: microseconds since the epoch), which tells it apart from a later process that got
// the same PID.
type procKey struct {
	pid   int
	start uint64
}

// procEntry is a process of the process table.
type procEntry struct {
	procKey
	ppid, pgid int
	sid        int // -1 when unknown
	uid        int // effective user (Linux: the owner of /proc/<pid>; root for a process that is not dumpable)
	zombie     bool
	kthread    bool // a Linux kernel thread
}

// newRunCookie returns a random value for runCookieVar.
func newRunCookie() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// withRunCookie returns the environment with runCookieVar set to cookie, replacing a value
// inherited from an outer run. A nil environment is the provider's.
func withRunCookie(env []string, cookie string) []string {
	return withCookie(env, runCookieVar, cookie)
}

// withCookie returns the environment with the cookie variable name set to cookie, replacing
// a value inherited from an outer run. A nil environment is the provider's.
func withCookie(env []string, name, cookie string) []string {
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return append(out, name+"="+cookie)
}

// environHas reports whether a NUL-separated environment holds exactly the entry
// (NAME=value). Only complete, NUL-terminated entries count, so a value cut off by a short
// read never matches.
func environHas(env []byte, entry string) bool {
	for {
		i := bytes.IndexByte(env, 0)
		if i < 0 {
			return false
		}
		if string(env[:i]) == entry {
			return true
		}
		env = env[i+1:]
	}
}

// procargsHas reports whether the environment in a macOS KERN_PROCARGS2 buffer holds exactly
// the entry. The buffer holds argc (a 32-bit integer), the executable path, NUL padding, the
// argc arguments and then the environment, each NUL-terminated; what follows the
// environment (the "apple" strings) is searched too, which an exact entry cannot match by
// accident. Empty arguments right after the path are taken as padding, as XNU itself parses
// the buffer: that can only skip environment entries, never take an argument for one.
// hidden reports a buffer that ends after the arguments: macOS 11 and later leave out the
// environment, and the apple strings with it, of processes restricted by their code
// signature (cs_restricted) unless System Integrity Protection is off.
func procargsHas(b []byte, entry string) (found, hidden bool) {
	if len(b) < 4 {
		return false, false
	}
	argc := int(binary.NativeEndian.Uint32(b))
	b = b[4:]
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return false, false
	}
	b = b[i:]
	for len(b) > 0 && b[0] == 0 {
		b = b[1:]
	}
	for ; argc > 0; argc-- {
		i := bytes.IndexByte(b, 0)
		if i < 0 {
			return false, false
		}
		b = b[i+1:]
	}
	if bytes.IndexByte(b, 0) < 0 {
		return false, true
	}
	return environHas(b, entry), false
}

// procTree records the processes started under one CLI (or one run child: everything below
// says "the CLI" for the process at its root). Its methods do nothing on a nil tree (tracking
// is off).
type procTree struct {
	root   procKey
	name   string // the root process in messages: "claude" or "the agent run"
	cookie string // runCookieVar=value (or childCookieVar=value), "" without a cookie
	self   struct{ pid, pgid, sid, uid int }
	debug  func(string)

	mu sync.Mutex
	// procs are the recorded processes that ran at the last read of the table.
	procs map[procKey]bool
	// groups and sessions that a recorded process created, by ID, with their leader. An ID
	// that was seen without members or with another leader is deleted: it may be reused.
	groups, sessions map[int]procKey
	// termed: the processes and groups that got SIGTERM, which a later step does not repeat.
	termed       map[procKey]bool
	termedGroups map[int]bool
	logged       map[string]bool

	stopPoll chan struct{}
	pollDone chan struct{}
	stopOnce sync.Once
}

// newProcTree starts recording the processes started under the CLI with that PID, whose
// environment holds runCookieVar=cookie. It returns nil, after logging why, when the CLI
// cannot be found in the process table.
func newProcTree(pid int, cookie string, debug func(string)) *procTree {
	return newProcTreeOf(pid, claudeSupervision(debug), cookie)
}

// newProcTreeOf starts recording the processes started under the process with that PID, which
// s describes, whose environment holds s.cookieVar=cookie.
func newProcTreeOf(pid int, s supervision, cookie string) *procTree {
	root, ok := readProc(pid)
	if !ok {
		logDebug(s.debug, "tracking the processes of %s is off: %s (PID %d) is not in the process table", s.name, s.name, pid)
		return nil
	}
	t := &procTree{
		root: root.procKey, name: s.name, debug: s.debug,
		procs: map[procKey]bool{}, groups: map[int]procKey{}, sessions: map[int]procKey{},
		termed: map[procKey]bool{}, termedGroups: map[int]bool{}, logged: map[string]bool{},
		stopPoll: make(chan struct{}), pollDone: make(chan struct{}),
	}
	if cookie != "" {
		t.cookie = s.cookieVar + "=" + cookie
	}
	t.self.pid, t.self.pgid, t.self.uid = syscall.Getpid(), unix.Getpgrp(), os.Geteuid()
	t.self.sid, _ = unix.Getsid(0)
	go t.poll()
	return t
}

func logDebug(debug func(string), format string, args ...any) {
	if debug != nil {
		debug(fmt.Sprintf(format, args...))
	}
}

// poll reads the process table until stopPolling.
func (t *procTree) poll() {
	defer close(t.pollDone)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-t.stopPoll:
			return
		case <-timer.C:
		}
		began := time.Now()
		t.mu.Lock()
		if tbl := t.readLocked(); tbl != nil {
			t.walkLocked(tbl)
		}
		t.mu.Unlock()
		timer.Reset(max(procPollInterval, procPollCost*time.Since(began)))
	}
}

// stopPolling ends the polling and waits for it to end.
func (t *procTree) stopPolling() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() { close(t.stopPoll) })
	<-t.pollDone
}

// logOnce logs a tracking problem at debug level, once per tree.
func (t *procTree) logOnce(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if !t.logged[msg] {
		t.logged[msg] = true
		logDebug(t.debug, "%s", msg)
	}
}

// procTableView is one read of the process table.
type procTableView struct {
	byPID    map[int]*procEntry
	children map[int][]*procEntry
}

// readLocked reads the process table and forgets what is gone: the records of processes that
// ended, and the groups and sessions whose ID may have been reused. It returns nil when the
// table could not be read.
func (t *procTree) readLocked() *procTableView {
	entries, err := readProcTable(t.root.start)
	if err != nil {
		t.logOnce("tracking the processes of %s: reading the process table: %v", t.name, err)
		return nil
	}
	if len(entries) > maxProcTable {
		t.logOnce("tracking the processes of %s: %v (%d processes)", t.name, errProcTableTooLarge, len(entries))
		return nil
	}
	tbl := &procTableView{byPID: make(map[int]*procEntry, len(entries)), children: map[int][]*procEntry{}}
	pgids, sids := map[int]bool{}, map[int]bool{}
	for i := range entries {
		e := &entries[i]
		if e.pid <= 1 {
			continue
		}
		tbl.byPID[e.pid] = e
		tbl.children[e.ppid] = append(tbl.children[e.ppid], e)
		pgids[e.pgid], sids[e.sid] = true, true
	}
	running := func(k procKey) bool {
		e := tbl.byPID[k.pid]
		return e != nil && e.procKey == k
	}
	for k := range t.procs {
		if !running(k) {
			delete(t.procs, k)
		}
	}
	for k := range t.termed {
		if !running(k) {
			delete(t.termed, k)
		}
	}
	// A group or session whose leader's PID now belongs to another process, or that has
	// nothing in it any more.
	for _, m := range []struct {
		leaders map[int]procKey
		used    map[int]bool
		group   bool
	}{{t.groups, pgids, true}, {t.sessions, sids, false}} {
		for id, leader := range m.leaders {
			if e := tbl.byPID[id]; (e != nil && e.procKey != leader) || (e == nil && !m.used[id]) {
				delete(m.leaders, id)
				if m.group {
					delete(t.termedGroups, id)
				}
			}
		}
	}
	return tbl
}

// walkLocked records what runs below the CLI and below the recorded processes.
func (t *procTree) walkLocked(tbl *procTableView) {
	var stack []*procEntry
	if e := tbl.byPID[t.root.pid]; e != nil && e.procKey == t.root {
		stack = append(stack, e)
	}
	for k := range t.procs {
		if e := tbl.byPID[k.pid]; e != nil && e.procKey == k {
			stack = append(stack, e)
		}
	}
	visited := map[int]bool{}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[e.pid] {
			continue
		}
		visited[e.pid] = true
		if e.procKey != t.root && !t.record(e, false) {
			continue
		}
		for _, c := range tbl.children[e.pid] {
			// A child starts after its parent. One that started before is not a child of
			// this parent's: a process a debugger attached to (macOS reports the tracer as
			// its parent), or a reused PID read in one table.
			if !visited[c.pid] && c.start >= e.start {
				stack = append(stack, c)
			}
		}
	}
}

// record records a process started under the CLI, and the group and session it leads. It
// returns false when the tree is full (unless force) and the process is not recorded.
func (t *procTree) record(e *procEntry, force bool) bool {
	if !t.procs[e.procKey] {
		if len(t.procs) >= maxTrackedProcs && !force {
			t.logOnce("tracking the processes of %s: more than %d processes run, the others are not recorded",
				t.name, maxTrackedProcs)
			return false
		}
		t.procs[e.procKey] = true
	}
	if e.pgid == e.pid {
		t.groups[e.pid] = e.procKey
	}
	if e.sid == e.pid {
		t.sessions[e.pid] = e.procKey
	}
	return true
}

// sweepLocked records the processes whose environment holds the run's cookie, among those of
// the provider's user that started after the CLI, are not kernel threads, not in the CLI's
// group (its signals reach those) and not recorded yet. It reads at most maxCookieReads
// environments, for at most cookieSweepTimeout: a read that blocks is abandoned, and what it
// did not check is not found. It returns how many it recorded.
func (t *procTree) sweepLocked(tbl *procTableView) int {
	if t.cookie == "" {
		return 0
	}
	var cands []procEntry
	for _, e := range tbl.byPID {
		if e.zombie || e.kthread || e.uid != t.self.uid || e.pid == t.root.pid || e.pid == t.self.pid ||
			e.pgid == t.root.pid || t.procs[e.procKey] {
			continue
		}
		if len(cands) == maxCookieReads {
			t.logOnce("tracking the processes of %s: more than %d candidates for the run's cookie, the others "+
				"are not checked", t.name, maxCookieReads)
			break
		}
		cands = append(cands, *e)
	}
	if len(cands) == 0 {
		return 0
	}
	type result struct {
		i             int
		found, hidden bool
	}
	// The reads run in a goroutine of their own, so that a blocked one cannot hold up the
	// cleanup (which holds the locks and may have the CLI's group stopped). The channel holds
	// every result, so the goroutine never blocks on it after it was abandoned.
	results := make(chan result, len(cands))
	var abandon atomic.Bool
	entry, read := t.cookie, readCookie
	go func() {
		defer close(results)
		buf := make([]byte, environBufSize)
		for i := range cands {
			if abandon.Load() {
				return
			}
			found, hidden := read(&cands[i], entry, buf)
			results <- result{i, found, hidden}
		}
	}()
	timer := time.NewTimer(cookieSweepTimeout)
	defer timer.Stop()
	found, hidden := 0, 0
	for done := false; !done; {
		select {
		case r, ok := <-results:
			switch {
			case !ok:
				done = true
			case r.found:
				t.record(&cands[r.i], true)
				found++
			case r.hidden:
				hidden++
			}
		case <-timer.C:
			abandon.Store(true)
			t.logOnce("tracking the processes of %s: reading the environments of processes took longer than %v, "+
				"the others are not checked for the run's cookie", t.name, cookieSweepTimeout)
			done = true
		}
	}
	if hidden > 0 {
		t.logOnce("tracking the processes of %s: the system does not show the environment of some processes "+
			"(macOS hides it for code-signing restricted programs), so the run's cookie cannot find them", t.name)
	}
	return found
}

// procTarget is a recorded process that a cleanup step signals.
type procTarget struct {
	procKey
	pgid int
}

// groupTarget is a process group that a cleanup step signals: one that a recorded process
// created (session -1), or one in a session that a recorded process created.
type groupTarget struct {
	pgid, session int
}

// procTargets is what a cleanup step signals.
type procTargets struct {
	procs  []procTarget
	groups []groupTarget // with members
	// outside is how many processes run in them outside the CLI's group: what the CLI left
	// running, for the warning. The CLI's own group is stopped without a warning.
	outside int
}

func (tg procTargets) empty() bool { return len(tg.procs) == 0 && len(tg.groups) == 0 }

// mayGroup reports whether a group may be signalled: never the provider's group, or the
// group of the leader of the provider's session; the CLI's group is signalled by
// harness_unix.go itself.
func (t *procTree) mayGroup(g int) bool {
	return g > 1 && g != t.self.pgid && g != t.self.sid && g != t.self.pid && g != t.root.pid
}

// targetsLocked returns what runs under the CLI, from a read of the table.
func (t *procTree) targetsLocked(tbl *procTableView) procTargets {
	var tg procTargets
	outside := map[int]bool{}
	for k := range t.procs {
		if e := tbl.byPID[k.pid]; e != nil && e.procKey == k && !e.zombie && k.pid != t.self.pid {
			tg.procs = append(tg.procs, procTarget{k, e.pgid})
			if e.pgid != t.root.pid {
				outside[k.pid] = true
			}
		}
	}
	groups := map[int]groupTarget{}
	for g := range t.groups {
		if t.mayGroup(g) {
			groups[g] = groupTarget{pgid: g, session: -1}
		}
	}
	for _, e := range tbl.byPID {
		if _, ok := t.sessions[e.sid]; ok && e.sid != t.self.sid && t.mayGroup(e.pgid) {
			if _, dup := groups[e.pgid]; !dup {
				groups[e.pgid] = groupTarget{pgid: e.pgid, session: e.sid}
			}
		}
	}
	members := map[int]bool{}
	for _, e := range tbl.byPID {
		if _, ok := groups[e.pgid]; ok && !e.zombie {
			members[e.pgid] = true
			if e.pid != t.root.pid && e.pid != t.self.pid {
				outside[e.pid] = true
			}
		}
	}
	for g := range members {
		tg.groups = append(tg.groups, groups[g])
	}
	tg.outside = len(outside)
	return tg
}

// scan reads the process table for a cleanup step: it records what runs below the CLI and
// what carries the run's cookie, and returns what runs under the CLI. When the table cannot
// be read, the recorded processes and the groups of recorded leaders are checked one by one.
func (t *procTree) scan() procTargets {
	if t == nil {
		return procTargets{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if tbl := t.readLocked(); tbl != nil {
		t.walkLocked(tbl)
		if t.sweepLocked(tbl) > 0 {
			// Below the processes the cookie found.
			t.walkLocked(tbl)
		}
		return t.targetsLocked(tbl)
	}
	var tg procTargets
	for k := range t.procs {
		if e, ok := readProc(k.pid); ok && e.procKey == k && !e.zombie && k.pid != t.self.pid {
			tg.procs = append(tg.procs, procTarget{k, e.pgid})
			if e.pgid != t.root.pid {
				tg.outside++
			}
		}
	}
	for g, leader := range t.groups {
		if e, ok := readProc(g); ok && e.procKey == leader && t.mayGroup(g) {
			tg.groups = append(tg.groups, groupTarget{pgid: g, session: -1})
		}
	}
	return tg
}

// tracking reports whether the tree has recorded processes.
func (t *procTree) tracking() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.procs) > 0
}

// term sends SIGTERM and SIGCONT to the targets that did not get them from an earlier step:
// a second SIGTERM can make a program skip its graceful shutdown.
func (t *procTree) term(tg procTargets) { t.send(tg, true, syscall.SIGTERM, syscall.SIGCONT) }

// kill sends SIGKILL to the targets.
func (t *procTree) kill(tg procTargets) { t.send(tg, false, syscall.SIGKILL) }

// send sends the signals to the groups that are still the recorded ones, and to the
// processes that run with their PID and start time and are not in a group it signalled.
// once: only to what did not get them from an earlier call.
func (t *procTree) send(tg procTargets, once bool, sigs ...syscall.Signal) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	reached := map[int]bool{}
	for _, g := range tg.groups {
		if once && t.termedGroups[g.pgid] {
			reached[g.pgid] = true
			continue
		}
		if !t.groupStillOurs(g) {
			continue
		}
		for _, sig := range sigs {
			_ = syscall.Kill(-g.pgid, sig)
		}
		reached[g.pgid] = true
		if once {
			t.termedGroups[g.pgid] = true
		}
	}
	for _, p := range tg.procs {
		if p.pid <= 1 || p.pid == t.self.pid || reached[p.pgid] || (once && t.termed[p.procKey]) {
			continue
		}
		signalProc(p.procKey, sigs...)
		if once {
			t.termed[p.procKey] = true
		}
	}
}

// groupStillOurs checks, right before a group is signalled, that its ID is still recorded
// (a read of the table may have dropped it since the targets were taken), that it still has
// members, and that its leader, if it still runs, is the recorded one, or for a group of a
// recorded session, is still in that session.
func (t *procTree) groupStillOurs(g groupTarget) bool {
	if !t.mayGroup(g.pgid) {
		return false
	}
	var leader procKey
	var ok bool
	if g.session < 0 {
		leader, ok = t.groups[g.pgid]
	} else {
		_, ok = t.sessions[g.session]
	}
	if !ok || !groupAlive(g.pgid) {
		return false
	}
	e, found := readProc(g.pgid)
	switch {
	case !found:
		// The leader is gone; the ID stays reserved while the group has members.
		return true
	case g.session < 0:
		return e.procKey == leader
	default:
		return e.sid == g.session
	}
}

// running reports whether anything of the targets still runs.
func (t *procTree) running(tg procTargets) bool {
	if t == nil {
		return false
	}
	for _, g := range tg.groups {
		if groupAlive(g.pgid) {
			return true
		}
	}
	for _, p := range tg.procs {
		if e, ok := readProc(p.pid); ok && e.procKey == p.procKey && !e.zombie {
			return true
		}
	}
	return false
}
