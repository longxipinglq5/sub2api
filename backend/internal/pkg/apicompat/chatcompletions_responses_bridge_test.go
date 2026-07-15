package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesInputToChatMessages_DeveloperRoleMapsToSystem(t *testing.T) {
	messages, err := responsesInputToChatMessages("", json.RawMessage(`[{"role":"developer","content":"follow project instructions"}]`))
	require.NoError(t, err)
	require.Len(t, messages, 1)

	assert.Equal(t, "system", messages[0].Role)
	assert.JSONEq(t, `"follow project instructions"`, string(messages[0].Content))
}

func TestResponsesInputToChatMessages_KeepsChatCompletionRoles(t *testing.T) {
	input := json.RawMessage(`[
		{"role":"system","content":"system message"},
		{"role":"user","content":"user message"},
		{"role":"assistant","content":"assistant message"},
		{"role":"tool","content":"tool message"}
	]`)

	messages, err := responsesInputToChatMessages("", input)
	require.NoError(t, err)
	require.Len(t, messages, 4)

	assert.Equal(t, []string{"system", "user", "assistant", "tool"}, chatMessageRoles(messages))
}

func TestResponsesInputToChatMessages_EmptyRoleFallsBackToUser(t *testing.T) {
	messages, err := responsesInputToChatMessages("", json.RawMessage(`[{"role":"","content":"hello"}]`))
	require.NoError(t, err)
	require.Len(t, messages, 1)

	assert.Equal(t, "user", messages[0].Role)
}

func TestResponsesInputToChatMessages_DeveloperRoleTrimAndCaseInsensitive(t *testing.T) {
	input := json.RawMessage(`[
		{"role":" Developer ","content":"one"},
		{"role":"\tDEVELOPER\n","content":"two"}
	]`)

	messages, err := responsesInputToChatMessages("", input)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	assert.Equal(t, []string{"system", "system"}, chatMessageRoles(messages))
}

func TestResponsesToChatCompletionsRequest_InstructionsAndInputDeveloperRole(t *testing.T) {
	req := &ResponsesRequest{
		Model:        "gpt-4o",
		Instructions: "Use concise answers.",
		Input: json.RawMessage(`[
			{"role":"developer","content":[{"type":"input_text","text":"Prefer JSON."}]},
			{"role":"user","content":"Hello"}
		]`),
	}

	out, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 3)

	assert.Equal(t, []string{"system", "system", "user"}, chatMessageRoles(out.Messages))
	assert.JSONEq(t, `"Use concise answers."`, string(out.Messages[0].Content))
	assert.JSONEq(t, `"Prefer JSON."`, string(out.Messages[1].Content))
	assert.JSONEq(t, `"Hello"`, string(out.Messages[2].Content))
}

func TestResponsesToChatCompletionsRequest_CustomToolBecomesFunction(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[{"role":"user","content":"Run pwd"}]`),
		Tools: []ResponsesTool{{
			Type:        "custom",
			Name:        "exec",
			Description: "Run code in the local tool runtime.",
			Format:      json.RawMessage(`{"type":"grammar","syntax":"lark","definition":"start: /.+/"}`),
		}},
		ToolChoice: json.RawMessage(`{"type":"custom","name":"exec"}`),
	}

	out, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)
	require.NotNil(t, out.Tools[0].Function)
	assert.Equal(t, "function", out.Tools[0].Type)
	assert.Equal(t, "exec", out.Tools[0].Function.Name)
	assert.Contains(t, out.Tools[0].Function.Description, "freeform Responses API tool")
	assert.JSONEq(t, `{
		"type":"object",
		"properties":{"input":{"type":"string","description":"Complete freeform input for this tool."}},
		"required":["input"],
		"additionalProperties":false
	}`, string(out.Tools[0].Function.Parameters))
	assert.JSONEq(t, `{"type":"function","function":{"name":"exec"}}`, string(out.ToolChoice))
}

func TestResponsesToChatCompletionsRequest_CustomToolHistoryRoundTrips(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_exec","name":"exec","input":"const r = await tools.exec_command({cmd: \"pwd\"}); text(r.output);"},
			{"type":"custom_tool_call_output","call_id":"call_exec","output":[{"type":"input_text","text":"Script completed\n"},{"type":"input_text","text":"/workspace\n"}]}
		]`),
	}

	out, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 2)
	require.Len(t, out.Messages[0].ToolCalls, 1)
	assert.Equal(t, "assistant", out.Messages[0].Role)
	assert.Equal(t, "call_exec", out.Messages[0].ToolCalls[0].ID)
	assert.Equal(t, "exec", out.Messages[0].ToolCalls[0].Function.Name)
	assert.JSONEq(t,
		`{"input":"const r = await tools.exec_command({cmd: \"pwd\"}); text(r.output);"}`,
		out.Messages[0].ToolCalls[0].Function.Arguments,
	)
	assert.Equal(t, "tool", out.Messages[1].Role)
	assert.Equal(t, "call_exec", out.Messages[1].ToolCallID)
	assert.JSONEq(t, `"Script completed\n\n\n/workspace\n"`, string(out.Messages[1].Content))
}

func TestChatCompletionsResponseToResponsesWithTools_CustomToolCall(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl_custom",
		Model: "gpt-5.6-sol",
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ChatToolCall{{
					ID:   "call_exec",
					Type: "function",
					Function: ChatFunctionCall{
						Name:      "exec",
						Arguments: `{"input":"const r = await tools.exec_command({cmd: \"pwd\"}); text(r.output);"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	out := ChatCompletionsResponseToResponsesWithTools(resp, "gpt-5.6-sol", []ResponsesTool{{Type: "custom", Name: "exec"}})

	require.Len(t, out.Output, 1)
	assert.Equal(t, "custom_tool_call", out.Output[0].Type)
	assert.Equal(t, "call_exec", out.Output[0].CallID)
	assert.Equal(t, "exec", out.Output[0].Name)
	assert.Equal(t, `const r = await tools.exec_command({cmd: "pwd"}); text(r.output);`, out.Output[0].Input)
	assert.Empty(t, out.Output[0].Arguments)
}

func chatMessageRoles(messages []ChatMessage) []string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message.Role)
	}
	return roles
}
