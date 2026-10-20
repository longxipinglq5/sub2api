package apicompat

import (
	"bytes"
	"encoding/json"
	"strings"
)

// UnmarshalJSON accepts both the official Responses SSE schema and the
// output_item.delta tool-call variant emitted by some compatible upstreams.
func (e *ResponsesStreamEvent) UnmarshalJSON(data []byte) error {
	type eventAlias ResponsesStreamEvent
	var wire struct {
		eventAlias
		Delta json.RawMessage `json:"delta"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	*e = ResponsesStreamEvent(wire.eventAlias)
	normalizeResponsesToolCallOutput(e.Item)
	if e.Response != nil {
		for i := range e.Response.Output {
			normalizeResponsesToolCallOutput(&e.Response.Output[i])
		}
	}

	delta := bytes.TrimSpace(wire.Delta)
	if len(delta) == 0 || bytes.Equal(delta, []byte("null")) {
		return nil
	}
	if delta[0] == '"' {
		return json.Unmarshal(delta, &e.Delta)
	}

	var objectDelta struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		Arguments string `json:"arguments"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(delta, &objectDelta); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(objectDelta.Type), "tool_call") {
		if e.Type == "response.output_item.delta" {
			e.Type = "response.function_call_arguments.delta"
		}
		e.Delta = objectDelta.Arguments
		e.CallID = objectDelta.CallID
		if e.CallID == "" {
			e.CallID = objectDelta.ID
		}
		e.Name = objectDelta.Name
	}
	return nil
}

func normalizeResponsesToolCallOutput(output *ResponsesOutput) {
	if output != nil && strings.EqualFold(strings.TrimSpace(output.Type), "tool_call") {
		output.Type = "function_call"
	}
}
