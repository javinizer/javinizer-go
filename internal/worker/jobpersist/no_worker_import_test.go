package jobpersist

// This test file exists to assert that the jobpersist package can be exercised
// end-to-end (Encode + Decode + ParseResultsJSON) without importing the worker
// package. The import list of codec_test.go and the parse test files in this
// package contains only:
//   - github.com/javinizer/javinizer-go/internal/models
//   - github.com/javinizer/javinizer-go/internal/operationmode
//   - github.com/javinizer/javinizer-go/internal/worker/resultstore
//   - stdlib + testify
//
// If `go list -deps ./internal/worker/jobpersist` ever includes
// `internal/worker`, the codec extraction has regressed. This is verified by
// the CI `go vet`/build step: any import of internal/worker from this package
// would be an import cycle (worker imports jobpersist), which the Go toolchain
// rejects at compile time.
