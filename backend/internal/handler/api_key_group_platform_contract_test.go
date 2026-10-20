package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
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

func TestOpenAIResponsesHandlersResolveEffectiveAPIKeyGroupByPlatform(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(".", "openai_gateway_handler.go"), nil, 0)
	require.NoError(t, err)

	for _, methodName := range []string{"Responses", "ResponsesWebSocket"} {
		t.Run(methodName, func(t *testing.T) {
			require.True(
				t,
				methodCallsFunction(file, methodName, "resolveEffectiveAPIKeyGroupForPlatform"),
				"%s must route multi-group API keys by target platform",
				methodName,
			)
		})
	}
}

func TestOpenAIResponsesTargetPlatform(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   *service.APIKey
		model    string
		expected string
	}{
		{
			name: "claude primary with openai secondary uses openai for gpt",
			apiKey: &service.APIKey{
				Group: &service.Group{ID: 6, Platform: service.PlatformAnthropic},
				Groups: []service.Group{
					{ID: 6, Platform: service.PlatformAnthropic},
					{ID: 2, Platform: service.PlatformOpenAI},
				},
			},
			model:    "gpt-5.5",
			expected: service.PlatformOpenAI,
		},
		{
			name: "claude primary with grok secondary uses grok for grok model",
			apiKey: &service.APIKey{
				Group: &service.Group{ID: 6, Platform: service.PlatformAnthropic},
				Groups: []service.Group{
					{ID: 6, Platform: service.PlatformAnthropic},
					{ID: 7, Platform: service.PlatformGrok},
				},
			},
			model:    "grok-4",
			expected: service.PlatformGrok,
		},
		{
			name: "grok only key falls back to grok",
			apiKey: &service.APIKey{
				Group:  &service.Group{ID: 7, Platform: service.PlatformGrok},
				Groups: []service.Group{{ID: 7, Platform: service.PlatformGrok}},
			},
			model:    "gpt-5.5",
			expected: service.PlatformGrok,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, openAIResponsesTargetPlatform(tt.apiKey, tt.model))
		})
	}
}

func methodCallsFunction(file *ast.File, methodName, functionName string) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != methodName || fn.Body == nil {
			continue
		}
		return nodeCallsFunction(fn.Body, functionName)
	}
	return false
}

func fileCallsFunction(file *ast.File, functionName string) bool {
	return nodeCallsFunction(file, functionName)
}

func nodeCallsFunction(node ast.Node, functionName string) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
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
