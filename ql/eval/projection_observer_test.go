package eval

// SetProjectBindingsObserver exposes the package-level projectBindings test
// hook to external (eval_test) test packages. Pass nil to clear. The test
// is responsible for restoring the previous value (use t.Cleanup with
// GetProjectBindingsObserver).
func SetProjectBindingsObserver(fn func(width int)) {
	projectBindingsObserver = fn
}

// GetProjectBindingsObserver returns the current observer (may be nil).
// Tests should snapshot this before SetProjectBindingsObserver and restore
// it via t.Cleanup.
func GetProjectBindingsObserver() func(width int) {
	return projectBindingsObserver
}
