//go:build integration

package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/yx-token/token-market/market/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := testutil.CleanupTemplates(); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup e2e database templates: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
