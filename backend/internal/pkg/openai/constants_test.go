package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsContainsGPT6Astra(t *testing.T) {
	for _, model := range DefaultModels {
		if model.ID != "gpt-6-astra" {
			continue
		}

		require.Equal(t, "GPT-6 Astra", model.DisplayName)
		require.Equal(t, "openai", model.OwnedBy)
		require.Equal(t, "model", model.Object)
		return
	}

	t.Fatal("gpt-6-astra is missing from the default OpenAI model catalog")
}
