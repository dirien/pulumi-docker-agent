// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// childSysProcAttr starts a run child in a process group of its own.
func childSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// childJob is the job object of a run child: closing it kills the processes in it, also when
// the provider dies. A child that could not be put into one (a job of the provider's that
// forbids it) runs without, and debug says why.
type childJob struct {
	h     windows.Handle
	debug func(string)
}

func newChildJob(debug func(string)) childJob {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		logDebug(debug, "the agent run runs without a job object: %v", err)
		return childJob{debug: debug}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		logDebug(debug, "the agent run runs without a job object: %v", err)
		_ = windows.CloseHandle(h)
		return childJob{debug: debug}
	}
	return childJob{h: h, debug: debug}
}

// assign puts the started child into the job.
func (j *childJob) assign(p *os.Process) {
	if j.h == 0 {
		return
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid)) //nolint:gosec // a PID
	if err == nil {
		err = windows.AssignProcessToJobObject(j.h, ph)
		_ = windows.CloseHandle(ph)
	}
	if err != nil {
		logDebug(j.debug, "the agent run runs without a job object: %v", err)
		j.close()
	}
}

// jobAccounting is JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}

// finish stops what the exited child left running in the job, and returns how many processes
// that were.
func (j *childJob) finish() int {
	if j.h == 0 {
		return 0
	}
	defer j.close()
	var acct jobAccounting
	if err := windows.QueryInformationJobObject(j.h, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&acct)), uint32(unsafe.Sizeof(acct)), nil); err != nil {
		logDebug(j.debug, "reading the job object of the agent run: %v", err)
		return 0
	}
	if acct.ActiveProcesses == 0 {
		return 0
	}
	if err := windows.TerminateJobObject(j.h, 1); err != nil {
		logDebug(j.debug, "stopping what the agent run left running: %v", err)
	}
	return int(acct.ActiveProcesses)
}

// close closes the job, which kills what still runs in it.
func (j *childJob) close() {
	if j.h != 0 {
		_ = windows.CloseHandle(j.h)
		j.h = 0
	}
}

// redirectStdHandles makes the process's standard output handle stderr and its standard
// input the null device, for code that reads the handles instead of os.Stdout.
func redirectStdHandles(null *os.File) {
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(os.Stderr.Fd()))
	_ = windows.SetStdHandle(windows.STD_INPUT_HANDLE, windows.Handle(null.Fd()))
}

// logDebug sends a formatted debug message.
func logDebug(debug func(string), format string, args ...any) {
	if debug != nil {
		debug(fmt.Sprintf(format, args...))
	}
}
