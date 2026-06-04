//go:build windows

// CPython: Modules/posixmodule.c:10755 os_times_impl (MS_WINDOWS branch)

package os

import (
	"syscall"
	"time"
)

// processTimes returns the five os.times fields. Windows CPython calls
// GetProcessTimes and reports kernel/user time; child times are always
// zero there. Go's syscall.Rusage on Windows carries CreationTime,
// ExitTime, KernelTime and UserTime as Filetime (100-ns units), so we
// scale by 1e-7 to seconds, matching the FILETIME math in posixmodule.c.
//
// CPython: Modules/posixmodule.c:10755 os_times_impl
func processTimes() (user, system, childUser, childSystem, elapsed float64) {
	var ru syscall.Rusage
	h, err := syscall.GetCurrentProcess()
	if err == nil {
		_ = syscall.GetProcessTimes(h, &ru.CreationTime, &ru.ExitTime, &ru.KernelTime, &ru.UserTime)
		user = filetimeSeconds(ru.UserTime)
		system = filetimeSeconds(ru.KernelTime)
	}
	elapsed = float64(time.Now().UnixNano()) / 1e9
	return
}

// filetimeSeconds converts a Windows FILETIME (100-ns units) to seconds.
func filetimeSeconds(ft syscall.Filetime) float64 {
	return float64(ft.HighDateTime)*429.4967296 + float64(ft.LowDateTime)*1e-7
}
