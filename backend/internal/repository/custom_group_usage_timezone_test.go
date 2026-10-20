package repository

import (
	"testing"

	appTimezone "github.com/yx-token/token-market/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func useGroupUsageRepositoryTestTimezone(t *testing.T, name string) {
	t.Helper()

	previousName := appTimezone.Name()
	require.NoError(t, appTimezone.Init(name))
	t.Cleanup(func() { require.NoError(t, appTimezone.Init(previousName)) })
}
