// content_router_ext.go — maintained by Client B 🟩
//
// This file extends the ContentRouter's available compressor set by
// registering LogCompressor and SearchCompressor during package init.
// It is intentionally separate from content_router.go to isolate
// multi-client development and avoid merge conflicts.

package compressor

func init() {
	// Register log and search compressors into the global registry.
	Register("log_compressor", NewLogCompressor())
	Register("search_compressor", NewSearchCompressor())
}
