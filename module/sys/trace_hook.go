// Hook for wiring vm.RegisterSysTraceBuiltins into the sys module.
// vm sets this in its init() to avoid an import cycle (vm imports module/sys).
//
// CPython: Python/sysmodule.c:3360 sys_methods (settrace / setprofile entries)

package sys

import "github.com/tamnd/gopy/objects"

// SysTraceBuiltinsHook is installed by vm so that sys.settrace,
// sys.setprofile, sys.gettrace, and sys.getprofile delegate to the
// real monitoring-aware implementations instead of the stub no-ops
// registered during module init.
var SysTraceBuiltinsHook func(d *objects.Dict) error
