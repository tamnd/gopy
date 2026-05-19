// Watcher-install hook fired from specialize.Enable. CPython installs
// the builtins dict watcher in pylifecycle.c:1380 at interpreter init
// and the globals + type watchers lazily inside
// optimizer_analysis.c:175 the first time remove_globals visits a
// trace. gopy collapses both points onto specialize.Enable(): every
// Code-creation site (pythonrun, vm.liftNestedCode,
// marshal.unmarshalCode) routes through Enable, so calling the
// installer on every Enable invocation gives the runtime as many
// retries as it needs to install the watcher once the main
// interpreter has been minted. The installer itself owns the
// idempotency latch.
//
// The optimizer package registers its installer via SetWatcherInstaller
// in its package-init. Until that registration runs (e.g. specialize
// unit tests that do not import optimizer), the hook is a no-op.
//
// CPython: Python/pylifecycle.c:1378-1383 builtins watcher install /
// Python/optimizer_analysis.c:175-180 globals + type watcher install

package specialize

// watcherInstaller is the optimizer-supplied callback that wires the
// dict / type watcher slots. nil means "no optimizer linked into the
// binary"; the hook then just elides the install entirely. The
// callback owns its own idempotency latch so Enable can fire it on
// every Code creation without coordinating.
var watcherInstaller func()

// SetWatcherInstaller registers fn as the watcher-install callback.
// Called from optimizer.init so the hook is in place by the time the
// runtime mints its first Code object. Tests can pass nil or a no-op
// to suppress the install.
func SetWatcherInstaller(fn func()) { watcherInstaller = fn }

// ensureWatchersInstalled fires the registered installer. Cheap on
// every invocation because the installer owns the latch that decides
// whether the actual wiring runs.
//
// CPython: Python/pylifecycle.c:1378-1383 (the equivalent
// install-once site at interpreter init).
func ensureWatchersInstalled() {
	fn := watcherInstaller
	if fn != nil {
		fn()
	}
}
