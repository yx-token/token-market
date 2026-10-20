//go:build integration

package quality

import (
	"fmt"
	"os"
	"testing"

	"github.com/yx-token/idlicat/market/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := testutil.CleanupTemplates(); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup quality database templates: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
