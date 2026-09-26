package telemetry

// Histogram bucket families. Every histogram in the catalog picks one of these instead of the
// SDK default (which stops at 10 and so lumps every 1–60s call, and every token count, into
// the overflow bucket).
//
// Cost note: each bucket is a series per label set, and each series is a sample per export, so
// keep families short. Boundaries are upper bounds; the SDK adds the +Inf bucket.
var (
	// BucketsHTTP covers API request latency: mostly fast, with slower uploads and exports.
	BucketsHTTP = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

	// BucketsFast covers database queries and other sub-second dependencies.
	BucketsFast = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

	// BucketsSlow covers outbound calls, LLM calls and chat turn stages, which mostly land in
	// 1–60s, with long streamed generations and retries out to 5 minutes.
	BucketsSlow = []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 7.5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 300}

	// BucketsJob covers whole async jobs and scheduled runs: seconds to an hour.
	BucketsJob = []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300, 600, 1200, 1800, 3600}

	// BucketsTokens covers token counts per call, from short side calls to 1M-token contexts.
	BucketsTokens = []float64{16, 64, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, 1048576}

	// BucketsBytes covers file and payload sizes from 1 KiB to 256 MiB.
	BucketsBytes = []float64{1 << 10, 16 << 10, 64 << 10, 256 << 10, 1 << 20, 4 << 20, 16 << 20, 64 << 20, 256 << 20}

	// BucketsCount covers small counts such as tool calls per turn or items per import.
	BucketsCount = []float64{0, 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 500, 1000, 5000, 10000}
)
