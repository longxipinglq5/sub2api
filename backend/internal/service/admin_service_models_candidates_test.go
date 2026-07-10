package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsListCandidateIDs_QwenIncludesWan27ImageModels(t *testing.T) {
	ids := defaultModelsListCandidateIDs(PlatformQwen)

	require.Contains(t, ids, "wan2.7-image")
	require.Contains(t, ids, "wan2.7-image-pro")
	require.Contains(t, ids, "wan2.6-t2i")
}
