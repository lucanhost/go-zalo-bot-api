//go:build integration

package zalobot

import (
	"os"
	"testing"
)

// Live tests make real network calls; the standard library's HTTP client
// keeps connection-pool goroutines that are not this SDK's to leak, so the
// goleak check used by the hermetic suite (testmain_test.go) is omitted here.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
