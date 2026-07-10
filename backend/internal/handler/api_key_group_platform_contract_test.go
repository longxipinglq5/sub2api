package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIHandlersResolveEffectiveAPIKeyGroupByPlatform(t *testing.T) {
	files := []string{
		"openai_chat_completions.go",
		"openai_images.go",
	}

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
			require.NoError(t, err)

			require.True(t, fileCallsFunction(file, "resolveEffectiveAPIKeyGroupForPlatform"), "%s must route multi-group API keys by target platform", name)
		})
	}
}

func fileCallsFunction(file *ast.File, functionName string) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if ok && ident.Name == functionName {
			found = true
			return false
		}
		return true
	})
	return found
}
