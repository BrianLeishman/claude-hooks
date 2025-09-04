package hooks

// Hook defines the interface for language-specific hooks
type Hook interface {
	// PostEdit runs after files have been edited
	PostEdit(files []string, verbose bool) error

	// PreEdit runs before files are edited
	PreEdit(files []string, verbose bool) error

	// PostEditJSON runs after files have been edited, suppressing stdout for JSON mode
	PostEditJSON(files []string, verbose bool) error
}

var registry = make(map[string]Hook)

// registry stores the mapping of file types to their corresponding hook implementations

func init() {
	// Register all hooks
	registry["go"] = &GoHook{}
	registry["typescript"] = &TypeScriptHook{}
	registry["javascript"] = &TypeScriptHook{} // Reuse TS hook for JS
}

// GetHook returns the hook for the given file type
func GetHook(fileType string) Hook {
	return registry[fileType]
}
