package compressor

// =============================================================================
// KVCacheReuse — KV cache reuse measurement
// =============================================================================

// KVCacheReuse captures the KV cache reuse measurement between two content
// strings, typically before and after prefix stabilization via CacheAligner.
type KVCacheReuse struct {
	// ReuseRate is the estimated fraction of KV cache entries that can be
	// reused between the two inputs. Range: 0.0 to 1.0.
	ReuseRate float64

	// CommonPrefixLen is the length in bytes of the longest common prefix
	// shared by both inputs. This represents the maximum contiguous cache
	// reuse without recomputation.
	CommonPrefixLen int64

	// BeforeLen and AfterLen are the byte lengths of the two inputs.
	BeforeLen int64
	AfterLen  int64
}

// =============================================================================
// MeasureKVCacheReuse — primary entry point
// =============================================================================

// MeasureKVCacheReuse estimates the KV cache reuse rate between two content
// strings. It computes the longest common prefix, which represents the portion
// of the KV cache that can be reused without recomputation (relevant for
// Anthropic/OpenAI prompt caching).
//
// The reuse rate is computed as: CommonPrefixLen / max(BeforeLen, AfterLen)
//
// For best results, content should first be passed through CacheAligner to
// normalize timestamps, UUIDs, IPs, and other dynamic tokens into stable
// placeholders.
func MeasureKVCacheReuse(before, after string) *KVCacheReuse {
	beforeLen := int64(len(before))
	afterLen := int64(len(after))

	m := &KVCacheReuse{
		BeforeLen: beforeLen,
		AfterLen:  afterLen,
	}

	if beforeLen == 0 && afterLen == 0 {
		return m
	}

	// Compute longest common prefix
	m.CommonPrefixLen = int64(commonPrefixLen(before, after))

	// Reuse rate is the fraction of the longer content that is a common prefix
	maxLen := beforeLen
	if afterLen > maxLen {
		maxLen = afterLen
	}
	if maxLen > 0 {
		m.ReuseRate = float64(m.CommonPrefixLen) / float64(maxLen)
	}

	return m
}

// =============================================================================
// CommonPrefixLength — helper function
// =============================================================================

// CommonPrefixLength returns the length of the longest common prefix between
// two strings.
func CommonPrefixLength(a, b string) int {
	return commonPrefixLen(a, b)
}

// ---------------------------------------------------------------------------
// internal: commonPrefixLen
// ---------------------------------------------------------------------------

func commonPrefixLen(a, b string) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	i := 0
	// Compare in chunks for speed when dealing with long strings
	for i < minLen {
		chunkSize := 64
		if i+chunkSize > minLen {
			chunkSize = minLen - i
		}

		if a[i:i+chunkSize] != b[i:i+chunkSize] {
			// Byte-by-byte comparison for the divergent chunk
			for j := 0; j < chunkSize; j++ {
				if a[i+j] != b[i+j] {
					return i + j
				}
			}
		}
		i += chunkSize
	}
	return minLen
}

// =============================================================================
// MeasureKVCacheBatch — batch reuse measurement
// =============================================================================

// MeasureKVCacheBatch computes aggregate KV cache reuse metrics across a
// window of messages. Returns the average reuse rate and window size.
//
// This is useful for measuring the effectiveness of CacheAligner over a
// conversation session.
func MeasureKVCacheBatch(window []string) *KVCacheReuse {
	if len(window) < 2 {
		return &KVCacheReuse{}
	}

	totalRate := 0.0
	totalPrefix := int64(0)
	maxBeforeLen := int64(0)
	maxAfterLen := int64(0)

	for i := 1; i < len(window); i++ {
		m := MeasureKVCacheReuse(window[i-1], window[i])
		totalRate += m.ReuseRate
		totalPrefix += m.CommonPrefixLen
		if m.BeforeLen > maxBeforeLen {
			maxBeforeLen = m.BeforeLen
		}
		if m.AfterLen > maxAfterLen {
			maxAfterLen = m.AfterLen
		}
	}

	n := float64(len(window) - 1)
	return &KVCacheReuse{
		ReuseRate:       totalRate / n,
		CommonPrefixLen: totalPrefix / int64(len(window)-1),
		BeforeLen:       maxBeforeLen,
		AfterLen:        maxAfterLen,
	}
}
