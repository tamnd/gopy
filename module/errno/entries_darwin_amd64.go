//go:build darwin && amd64

package errno

// darwinArchEntries returns errno names that exist only on a specific
// darwin GOARCH. On amd64 nothing extra is published, so the slice is
// empty.
func darwinArchEntries() []errnoEntry {
	return nil
}
