package hooks

type GoHook struct{}

func (h *GoHook) PreEdit(files []string, verbose bool) error {
	// Pre-edit: could run go vet or other checks
	return nil
}

func (h *GoHook) PostEdit(files []string, verbose bool) error {
	// Auto checks disabled for speed - run manually if needed
	return nil
}

func (h *GoHook) PostEditJSON(files []string, verbose bool) error {
	// Auto checks disabled for speed - run manually if needed
	return nil
}
