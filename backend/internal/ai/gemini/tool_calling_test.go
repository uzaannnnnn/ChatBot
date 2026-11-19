package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
)

func TestClient_StreamChat_ToolCallingAndFunctionResult(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: interaction.start\ndata: {\"event_type\":\"interaction.start\",\"interaction\":{\"id\":\"inter_123\"}}\n\n"))
		_, _ = w.Write([]byte("event: step.start\ndata: {\"event_type\":\"step.start\",\"index\":0,\"step\":{\"type\":\"function_call\",\"id\":\"call_1\",\"name\":\"generate_image\"}}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"arguments_delta\":\"{\\\"prompt\\\":\\\"a neon\"}}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"arguments_delta\":\" fox\\\"}\"}}\n\n"))
		_, _ = w.Write([]byte("event: step.end\ndata: {\"event_type\":\"step.end\",\"index\":0}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	stream, err := client.StreamChat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "Draw a neon fox"}},
		Tools: []ai.ToolDefinition{{
			Name:        "generate_image",
			Description: "Generate an image",
			Parameters:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}
	defer stream.Close()

	if !strings.Contains(capturedBody, `"tools":[{"type":"function","name":"generate_image"`) {
		t.Errorf("expected tools schema in payload, got: %s", capturedBody)
	}

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv error: %v", err)
	}
	if chunk.InteractionID != "inter_123" {
		t.Errorf("expected interaction ID inter_123, got %q", chunk.InteractionID)
	}
	if len(chunk.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chunk.ToolCalls))
	}
	tc := chunk.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "generate_image" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	var args map[string]string
	if err := json.Unmarshal(tc.Arguments, &args); err != nil || args["prompt"] != "a neon fox" {
		t.Errorf("unexpected arguments %s: %v", string(tc.Arguments), err)
	}

	_, err = stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestClient_BuildPayload_ToolResults(t *testing.T) {
	c := NewClient(ClientOptions{APIKey: "k"})
	payload := c.buildInteractionPayload(ai.ChatRequest{
		PreviousInteractionID: "inter_123",
		ToolResults: []ai.ToolResult{{
			CallID: "call_1",
			Name:   "generate_image",
			Result: map[string]any{"status": "generated"},
		}},
	}, true)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"previous_interaction_id":"inter_123"`) {
		t.Errorf("missing previous_interaction_id: %s", s)
	}
	if !strings.Contains(s, `"type":"function_result"`) || !strings.Contains(s, `"call_id":"call_1"`) {
		t.Errorf("missing function_result input: %s", s)
	}
}

func TestContainsPseudoToolJSON(t *testing.T) {
	if !ai.ContainsPseudoToolJSON(`{"action": "dalle.text2im", "action_input": "{\"prompt\": \"cat\"}"}`) {
		t.Errorf("expected dalle.text2im payload to be detected")
	}
	if !ai.ContainsPseudoToolJSON("```json\n{\"tool\":\"generate_image\",\"prompt\":\"cat\"}\n```") {
		t.Errorf("expected fenced pseudo-tool JSON to be detected")
	}
	if ai.ContainsPseudoToolJSON("Here is a normal explanation about JSON objects.") {
		t.Errorf("normal prose should not be flagged")
	}
}

func TestClient_StreamChat_StepStopAndDeduplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: interaction.start\ndata: {\"event_type\":\"interaction.start\",\"interaction\":{\"id\":\"inter_stop_1\"}}\n\n"))
		_, _ = w.Write([]byte("event: step.start\ndata: {\"event_type\":\"step.start\",\"index\":0,\"step\":{\"type\":\"function_call\",\"call_id\":\"call_stop_1\",\"name\":\"generate_image\"}}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"arguments_delta\":\"{\\\"prompt\\\":\\\"alpine\"}}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"arguments_delta\":\" sunrise\\\"}\"}}\n\n"))
		_, _ = w.Write([]byte("event: step.stop\ndata: {\"event_type\":\"step.stop\",\"index\":0}\n\n"))
		// Final interaction.completed repeats the same function_call; must NOT be emitted twice
		_, _ = w.Write([]byte("event: interaction.completed\ndata: {\"event_type\":\"interaction.completed\",\"interaction\":{\"id\":\"inter_stop_1\",\"steps\":[{\"type\":\"function_call\",\"call_id\":\"call_stop_1\",\"name\":\"generate_image\",\"arguments\":{\"prompt\":\"alpine sunrise\"}}]}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	stream, err := client.StreamChat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "Draw an alpine sunrise"}},
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}
	defer stream.Close()

	var calls []ai.ToolCall
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv unexpected error: %v", err)
		}
		calls = append(calls, chunk.ToolCalls...)
	}

	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 deduplicated tool call across step.stop + interaction.completed, got %d: %+v", len(calls), calls)
	}
	if calls[0].ID != "call_stop_1" || calls[0].Name != "generate_image" {
		t.Errorf("unexpected tool call metadata: %+v", calls[0])
	}
	var args map[string]string
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil || args["prompt"] != "alpine sunrise" {
		t.Errorf("unexpected flushed arguments %s: %v", string(calls[0].Arguments), err)
	}
}
