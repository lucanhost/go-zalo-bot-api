//go:build !integration

package zalobot

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain enforces goroutine hygiene for the hermetic suite. The live
// integration suite (//go:build integration) deliberately opts out: it makes
// real network calls, and the standard library's HTTP client keeps
// connection-pool goroutines that are not this SDK's to leak. See
// testmain_integration_test.go.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
