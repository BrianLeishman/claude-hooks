package hooks

type TypeScriptHook struct{}

func (h *TypeScriptHook) PreEdit(files []string, verbose bool) error {
	return nil
}

func (h *TypeScriptHook) PostEdit(files []string, verbose bool) error {
	// Auto checks disabled for speed - run manually if needed
	return nil
}

func (h *TypeScriptHook) PostEditJSON(files []string, verbose bool) error {
	// Auto checks disabled for speed - run manually if needed
	return nil
}
