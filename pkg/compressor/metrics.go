package compressor

import (
	"math"
	"sort"
	"sync"
	"time"
)

// =============================================================================
// Metrics — point-in-time metrics snapshot with flat field names
// =============================================================================

// Metrics is a snapshot of compressor operational metrics: latency percentiles,
// compression ratios, registry counters, and CCR cache statistics.
type Metrics struct {
	// Latency
	LatencyAvg time.Duration
	LatencyP50 time.Duration
	LatencyP90 time.Duration
	LatencyP99 time.Duration

	// Ratio
	RatioAvg      float64
	RatioMin      float64
	RatioMax      float64
	RatioLast10Avg float64

	// Registry
	RegisteredCompressors int
	CompressCount         int64
	CompressErrors        int64
	CompressRejections    int64

	// CCR
	CCRHitRate        float64
	CCRHitCount       int64
	CCRMissCount      int64
	CCRLLCFetchCount  int64
	CCRStorageBytes   int64
	CCRExpiredEntries int64
}

// =============================================================================
// MetricsCollector — thread-safe metrics aggregator
// =============================================================================

// MetricsCollector aggregates compressor performance metrics in a thread-safe
// manner with sliding windows for latency and ratio samples.
type MetricsCollector struct {
	mu sync.RWMutex

	// Registry counters
	registeredCompressors int
	compressCount         int64
	compressErrors        int64
	compressRejections    int64

	// Latency samples (sliding window, max maxSamples)
	latencySamples []time.Duration

	// Ratio samples (sliding window, max maxSamples)
	ratioSamples []float64

	// Last N ratios for rolling average
	lastNRatios []float64
	maxLastN    int

	// Max samples in sliding windows (prevents unbounded growth)
	maxSamples int

	// CCR counters
	ccrHitCount       int64
	ccrMissCount      int64
	ccrLLCFetchCount  int64
	ccrStorageBytes   int64
	ccrExpiredEntries int64
}

// NewMetricsCollector creates a new MetricsCollector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		maxLastN:   10,
		maxSamples: 10000, // 10K samples ≈ 80KB, prevents O(n log n) blowup
	}
}

// =============================================================================
// Recording methods
// =============================================================================

// RecordSuccess records a successful compression operation.
func (mc *MetricsCollector) RecordSuccess(latency time.Duration, originalSz, finalSz int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.compressCount++

	ratio := 1.0
	if originalSz > 0 {
		ratio = float64(finalSz) / float64(originalSz)
	}

	// Accumulate latency samples (sliding window)
	mc.latencySamples = append(mc.latencySamples, latency)
	if len(mc.latencySamples) > mc.maxSamples {
		mc.latencySamples = mc.latencySamples[1:]
	}

	// Accumulate ratio samples (sliding window)
	mc.ratioSamples = append(mc.ratioSamples, ratio)
	if len(mc.ratioSamples) > mc.maxSamples {
		mc.ratioSamples = mc.ratioSamples[1:]
	}

	// Last N ratios
	mc.lastNRatios = append(mc.lastNRatios, ratio)
	if len(mc.lastNRatios) > mc.maxLastN {
		mc.lastNRatios = mc.lastNRatios[1:]
	}
}

// RecordError records a failed compression.
func (mc *MetricsCollector) RecordError() {
	mc.mu.Lock()
	mc.compressErrors++
	mc.mu.Unlock()
}

// RecordRejection records a rejected compression.
func (mc *MetricsCollector) RecordRejection() {
	mc.mu.Lock()
	mc.compressRejections++
	mc.mu.Unlock()
}

// RecordRegistryAdd sets the registered compressor count.
func (mc *MetricsCollector) RecordRegistryAdd(delta int) {
	mc.mu.Lock()
	mc.registeredCompressors += delta
	mc.mu.Unlock()
}

// RecordCCRHit records a CCR cache hit.
func (mc *MetricsCollector) RecordCCRHit() {
	mc.mu.Lock()
	mc.ccrHitCount++
	mc.mu.Unlock()
}

// RecordCCRMiss records a CCR cache miss.
func (mc *MetricsCollector) RecordCCRMiss() {
	mc.mu.Lock()
	mc.ccrMissCount++
	mc.mu.Unlock()
}

// RecordCCRLLCFetch records a long-tail cache fetch.
func (mc *MetricsCollector) RecordCCRLLCFetch() {
	mc.mu.Lock()
	mc.ccrLLCFetchCount++
	mc.mu.Unlock()
}

// RecordCCRStorage updates CCR storage metadata.
func (mc *MetricsCollector) RecordCCRStorage(storageBytes, expiredEntries int64) {
	mc.mu.Lock()
	mc.ccrStorageBytes = storageBytes
	mc.ccrExpiredEntries = expiredEntries
	mc.mu.Unlock()
}

// =============================================================================
// Snapshot — thread-safe read
// =============================================================================

// Snapshot returns a point-in-time copy of all metrics.
func (mc *MetricsCollector) Snapshot() *Metrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	m := &Metrics{
		RegisteredCompressors: mc.registeredCompressors,
		CompressCount:         mc.compressCount,
		CompressErrors:        mc.compressErrors,
		CompressRejections:    mc.compressRejections,
		CCRHitCount:           mc.ccrHitCount,
		CCRMissCount:          mc.ccrMissCount,
		CCRLLCFetchCount:      mc.ccrLLCFetchCount,
		CCRStorageBytes:       mc.ccrStorageBytes,
		CCRExpiredEntries:     mc.ccrExpiredEntries,
	}

	// Latency percentiles
	if len(mc.latencySamples) > 0 {
		sorted := make([]time.Duration, len(mc.latencySamples))
		copy(sorted, mc.latencySamples)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

		m.LatencyAvg = avgDuration(sorted)
		m.LatencyP50 = percentileDuration(sorted, 0.50)
		m.LatencyP90 = percentileDuration(sorted, 0.90)
		m.LatencyP99 = percentileDuration(sorted, 0.99)
	}

	// Ratio statistics (from full sample history)
	if len(mc.ratioSamples) > 0 {
		sortedRatios := make([]float64, len(mc.ratioSamples))
		copy(sortedRatios, mc.ratioSamples)
		sort.Float64s(sortedRatios)

		m.RatioAvg = avgFloat64(sortedRatios)
		m.RatioMin = sortedRatios[0]
		m.RatioMax = sortedRatios[len(sortedRatios)-1]
	}

	// Last N ratio average
	if len(mc.lastNRatios) > 0 {
		m.RatioLast10Avg = avgFloat64(mc.lastNRatios)
	}

	// CCR hit rate
	total := mc.ccrHitCount + mc.ccrMissCount
	if total > 0 {
		m.CCRHitRate = float64(mc.ccrHitCount) / float64(total)
	}

	return m
}

// =============================================================================
// Reset
// =============================================================================

// Reset clears all counters and windows.
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.registeredCompressors = 0
	mc.compressCount = 0
	mc.compressErrors = 0
	mc.compressRejections = 0
	mc.latencySamples = mc.latencySamples[:0]
	mc.ratioSamples = mc.ratioSamples[:0]
	mc.lastNRatios = mc.lastNRatios[:0]
	mc.ccrHitCount = 0
	mc.ccrMissCount = 0
	mc.ccrLLCFetchCount = 0
	mc.ccrStorageBytes = 0
	mc.ccrExpiredEntries = 0
}

// =============================================================================
// Internal helpers
// =============================================================================

func avgDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	var sum int64
	for _, d := range ds {
		sum += int64(d)
	}
	return time.Duration(sum / int64(len(ds)))
}

func percentileDuration(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1.0 {
		return sorted[len(sorted)-1]
	}

	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo] + time.Duration(float64(sorted[hi]-sorted[lo])*frac)
}

func avgFloat64(fs []float64) float64 {
	if len(fs) == 0 {
		return 0
	}
	sum := 0.0
	for _, f := range fs {
		sum += f
	}
	return sum / float64(len(fs))
}
