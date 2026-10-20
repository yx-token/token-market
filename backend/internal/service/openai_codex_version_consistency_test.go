//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/pkg/openai"
)

func TestCodexVersionConstants_Consistency(t *testing.T) {
	require.True(t, strings.Contains(codexCLIUserAgent, openai.CodexDefaultOriginator+"/"+codexCLIVersion),
		"codexCLIUserAgent must embed codexCLIVersion")

	require.True(t, strings.Contains(DefaultOpenAICodexUserAgent, codexCLIVersion),
		"DefaultOpenAICodexUserAgent must embed codexCLIVersion")
}
