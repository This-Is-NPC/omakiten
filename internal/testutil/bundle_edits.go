package testutil

// DirectBundleEdits publishes edits directly for in-memory bundle stores.
type DirectBundleEdits struct{}

func (DirectBundleEdits) EditBundle(path string, edit func(string) error) error {
	return edit(path)
}
