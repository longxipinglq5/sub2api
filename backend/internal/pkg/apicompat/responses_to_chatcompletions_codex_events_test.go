package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesStreamEventUnmarshal_ToolCallObjectDelta(t *testing.T) {
	var added ResponsesStreamEvent
	require.NoError(t, json.Unmarshal([]byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"tool_call","call_id":"call_1","name":"get_weather"}}`), &added))
	require.NotNil(t, added.Item)
	assert.Equal(t, "function_call", added.Item.Type)

	var delta ResponsesStreamEvent
	require.NoError(t, json.Unmarshal([]byte(`{"type":"response.output_item.delta","output_index":0,"delta":{"id":"call_1","type":"tool_call","arguments":"{\"city\":","call_id":"call_1"}}`), &delta))
	assert.Equal(t, "response.function_call_arguments.delta", delta.Type)
	assert.Equal(t, `{"city":`, delta.Delta)
	assert.Equal(t, "call_1", delta.CallID)
}

func TestResponsesEventToChatChunks_ToolArgumentSnapshotDeduplicated(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-5.6-sol"
	state.SentRole = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &ResponsesOutput{Type: "function_call", CallID: "call_1", Name: "get_weather"},
	}, state)
	require.Len(t, chunks, 1)

	var emitted string
	for _, piece := range []string{`{"`, `city`, `":"`, `Shenzhen`, `"}`, `{"city":"Shenzhen"}`} {
		chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
			Type:        "response.function_call_arguments.delta",
			OutputIndex: 0,
			Delta:       piece,
		}, state)
		for _, chunk := range chunks {
			emitted += chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments
		}
	}
	assert.Equal(t, `{"city":"Shenzhen"}`, emitted)
}

func TestBufferedResponseAccumulator_ToolArgumentSnapshotDeduplicated(t *testing.T) {
	acc := NewBufferedResponseAccumulator()
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &ResponsesOutput{Type: "function_call", CallID: "call_1", Name: "get_weather"},
	})
	for _, piece := range []string{`{"`, `city`, `":"`, `Shenzhen`, `"}`, `{"city":"Shenzhen"}`} {
		acc.ProcessEvent(&ResponsesStreamEvent{
			Type:        "response.function_call_arguments.delta",
			OutputIndex: 0,
			Delta:       piece,
		})
	}
	output := acc.BuildOutput()
	require.Len(t, output, 1)
	assert.Equal(t, `{"city":"Shenzhen"}`, output[0].Arguments)
}

// custom_tool_call（custom/freeform 工具，如新版 apply_patch）应像 function_call 一样
// 注册为工具调用，其 *_input.delta 增量映射到正确的工具索引。
func TestResponsesEventToChatChunks_CustomToolCallInputDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-5-codex"
	state.SentRole = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 1,
		Item: &ResponsesOutput{
			Type:   "custom_tool_call",
			CallID: "call_patch",
			Name:   "apply_patch",
		},
	}, state)
	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].Choices[0].Delta.ToolCalls, 1)
	tc := chunks[0].Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, "call_patch", tc.ID)
	assert.Equal(t, "apply_patch", tc.Function.Name)

	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.custom_tool_call_input.delta",
		OutputIndex: 1,
		Delta:       "*** Begin Patch",
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 0, *tc.Index)
	assert.Equal(t, "*** Begin Patch", tc.Function.Arguments)
}

// 原始推理文本增量 reasoning_text.delta 应像 reasoning_summary_text.delta 一样
// 映射为 reasoning_content。
func TestResponsesEventToChatChunks_ReasoningTextDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-5-codex"
	state.SentRole = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.reasoning_text.delta",
		Delta: "thinking step",
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].Delta.ReasoningContent)
	assert.Equal(t, "thinking step", *chunks[0].Choices[0].Delta.ReasoningContent)
}

// 缓冲（非流式）累加器同样需识别两类新事件。
func TestBufferedResponseAccumulator_CodexEvents(t *testing.T) {
	acc := NewBufferedResponseAccumulator()
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &ResponsesOutput{Type: "custom_tool_call", CallID: "c1", Name: "apply_patch"},
	})
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:        "response.custom_tool_call_input.delta",
		OutputIndex: 0,
		Delta:       "patch-body",
	})
	acc.ProcessEvent(&ResponsesStreamEvent{
		Type:  "response.reasoning_text.delta",
		Delta: "raw-reasoning",
	})
	require.True(t, acc.HasContent())
}
