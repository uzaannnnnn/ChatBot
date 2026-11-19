package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
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

func TestClient_StreamChat_LiveArgumentsDeltaStringPayload(t *testing.T) {
	// Exact live SSE sequence observed from Gemini 3.1 Flash-Lite Interactions API
	const wantPrompt = "A serene and calm blue ocean wave captured at sunrise."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: interaction.created\ndata: {\"interaction\":{\"id\":\"v1_live_inter_1\",\"object\":\"interaction\",\"model\":\"gemini-3.1-flash-lite\"},\"event_type\":\"interaction.created\"}\n\n"))
		_, _ = w.Write([]byte("event: interaction.status_update\ndata: {\"interaction_id\":\"v1_live_inter_1\",\"status\":\"in_progress\",\"event_type\":\"interaction.status_update\"}\n\n"))
		_, _ = w.Write([]byte("event: step.start\ndata: {\"index\":0,\"step\":{\"type\":\"thought\"},\"event_type\":\"step.start\"}\n\n"))
		_, _ = w.Write([]byte("event: step.stop\ndata: {\"index\":0,\"event_type\":\"step.stop\"}\n\n"))
		_, _ = w.Write([]byte("event: step.start\ndata: {\"index\":1,\"step\":{\"id\":\"call_6883402666266341304\",\"type\":\"function_call\",\"name\":\"generate_image\",\"arguments\":{}},\"event_type\":\"step.start\"}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":1,\"delta\":{\"arguments\":\"{\\\"prompt\\\":\\\"A serene and calm blue ocean wave \",\"type\":\"arguments_delta\"},\"event_type\":\"step.delta\"}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":1,\"delta\":{\"arguments\":\"captured at sunrise.\\\"}\",\"type\":\"arguments_delta\"},\"event_type\":\"step.delta\"}\n\n"))
		_, _ = w.Write([]byte("event: step.stop\ndata: {\"index\":1,\"event_type\":\"step.stop\"}\n\n"))
		_, _ = w.Write([]byte("event: interaction.completed\ndata: {\"interaction\":{\"id\":\"v1_live_inter_1\",\"status\":\"requires_action\",\"steps\":[{\"id\":\"call_6883402666266341304\",\"type\":\"function_call\",\"name\":\"generate_image\",\"arguments\":{\"prompt\":\"A serene and calm blue ocean wave captured at sunrise.\"}}]},\"event_type\":\"interaction.completed\"}\n\n"))
		_, _ = w.Write([]byte("event: done\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	stream, err := client.StreamChat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "Generate an ocean wave"}},
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}
	defer stream.Close()

	var (
		calls         []ai.ToolCall
		interactionID string
	)
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv unexpected error: %v", err)
		}
		if chunk.InteractionID != "" {
			interactionID = chunk.InteractionID
		}
		calls = append(calls, chunk.ToolCalls...)
	}

	if interactionID != "v1_live_inter_1" {
		t.Errorf("expected interaction ID v1_live_inter_1, got %q", interactionID)
	}
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 deduplicated tool call, got %d: %+v", len(calls), calls)
	}
	tc := calls[0]
	if tc.ID != "call_6883402666266341304" {
		t.Errorf("expected call ID call_6883402666266341304, got %q", tc.ID)
	}
	if tc.Name != "generate_image" {
		t.Errorf("expected name generate_image, got %q", tc.Name)
	}
	if !json.Valid(tc.Arguments) {
		t.Fatalf("emitted arguments are not valid JSON: %s", string(tc.Arguments))
	}
	var parsed map[string]string
	if err := json.Unmarshal(tc.Arguments, &parsed); err != nil {
		t.Fatalf("unmarshal arguments failed: %v", err)
	}
	if parsed["prompt"] != wantPrompt {
		t.Errorf("expected prompt %q, got %q", wantPrompt, parsed["prompt"])
	}
}

func TestClient_StreamChat_MalformedFunctionCallArgumentsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: step.start\ndata: {\"index\":1,\"step\":{\"id\":\"call_bad\",\"type\":\"function_call\",\"name\":\"generate_image\",\"arguments\":{}},\"event_type\":\"step.start\"}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":1,\"delta\":{\"arguments\":\"{\\\"prompt\\\":\\\"unterminated\",\"type\":\"arguments_delta\"},\"event_type\":\"step.delta\"}\n\n"))
		_, _ = w.Write([]byte("event: step.stop\ndata: {\"index\":1,\"event_type\":\"step.stop\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{BaseURL: server.URL, APIKey: "test-key"})
	stream, err := client.StreamChat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "Draw something"}},
	})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err == nil || !strings.Contains(err.Error(), "malformed function_call arguments") {
		t.Fatalf("expected malformed function_call arguments error, got: %v", err)
	}
}

func TestClient_StreamChat_MultipleIndexedFunctionCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: step.start\ndata: {\"index\":1,\"step\":{\"id\":\"call_a\",\"type\":\"function_call\",\"name\":\"generate_image\",\"arguments\":{}},\"event_type\":\"step.start\"}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":1,\"delta\":{\"arguments\":\"{\\\"prompt\\\":\\\"first\\\"}\",\"type\":\"arguments_delta\"},\"event_type\":\"step.delta\"}\n\n"))
		_, _ = w.Write([]byte("event: step.start\ndata: {\"index\":2,\"step\":{\"id\":\"call_b\",\"type\":\"function_call\",\"name\":\"generate_image\",\"arguments\":{}},\"event_type\":\"step.start\"}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":2,\"delta\":{\"arguments\":\"{\\\"prompt\\\":\\\"second\\\"}\",\"type\":\"arguments_delta\"},\"event_type\":\"step.delta\"}\n\n"))
		_, _ = w.Write([]byte("event: interaction.completed\ndata: {\"interaction\":{\"id\":\"inter_multi\"},\"event_type\":\"interaction.completed\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{BaseURL: server.URL, APIKey: "test-key"})
	stream, err := client.StreamChat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "Draw two images"}},
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

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls in deterministic index order, got %d: %+v", len(calls), calls)
	}
	if calls[0].ID != "call_a" || calls[1].ID != "call_b" {
		t.Errorf("unexpected call order: %+v", calls)
	}
}

func TestClient_StreamChat_LiveGeminiFunctionCallParser(t *testing.T) {
	if os.Getenv("RUN_LIVE_GEMINI_PARSER_TEST") != "1" {
		t.Skip("skipping live Gemini test (set RUN_LIVE_GEMINI_PARSER_TEST=1 to enable)")
	}

	_ = godotenv.Load("../../../.env")
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" || strings.Contains(apiKey, "your_gemini") {
		t.Skip("GEMINI_API_KEY not configured")
	}
	baseURL := strings.TrimSpace(os.Getenv("GEMINI_BASE_URL"))
	modelID := strings.TrimSpace(os.Getenv("GEMINI_MODEL_ID"))
	if modelID == "" {
		modelID = DefaultModel
	}

	client := NewClient(ClientOptions{
		BaseURL:      baseURL,
		APIKey:       apiKey,
		DefaultModel: modelID,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := client.StreamChat(ctx, ai.ChatRequest{
		Model: modelID,
		Messages: []ai.Message{
			{Role: "system", Content: "You are DOUBLEU AI. When the user asks to generate an image, always invoke the generate_image tool."},
			{Role: "user", Content: "Generate an image of a calm blue ocean wave at sunrise."},
		},
		Tools: []ai.ToolDefinition{{
			Name:        "generate_image",
			Description: "Generate a single original image from a detailed text prompt.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "A vivid description of the image to generate.",
					},
				},
				"required": []string{"prompt"},
			},
		}},
		MaxTokens:   256,
		Temperature: 0.2,
		Stream:      true,
	})
	if err != nil {
		t.Fatalf("live StreamChat error: %v", err)
	}
	defer stream.Close()

	var (
		interactionID string
		calls         []ai.ToolCall
	)
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("live Recv error: %v", err)
		}
		if chunk.InteractionID != "" {
			interactionID = chunk.InteractionID
		}
		calls = append(calls, chunk.ToolCalls...)
	}

	if interactionID == "" {
		t.Errorf("expected non-empty interaction ID from live stream")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 parsed tool call from live stream, got %d: %+v", len(calls), calls)
	}
	tc := calls[0]
	if tc.ID == "" || tc.Name != "generate_image" {
		t.Fatalf("unexpected live tool call metadata: id=%q name=%q", tc.ID, tc.Name)
	}
	if !json.Valid(tc.Arguments) {
		t.Fatalf("live parsed arguments are not valid JSON: %s", string(tc.Arguments))
	}
	var parsed map[string]string
	if err := json.Unmarshal(tc.Arguments, &parsed); err != nil || strings.TrimSpace(parsed["prompt"]) == "" {
		t.Fatalf("expected non-empty prompt in live parsed arguments, got %s (err=%v)", string(tc.Arguments), err)
	}
	t.Logf("LIVE PARSE VERIFIED: model=%s interaction_id_present=%v call_id=%s name=%s prompt_len=%d",
		modelID, interactionID != "", tc.ID, tc.Name, len(parsed["prompt"]))
}

func TestClient_StreamChat_FunctionResultRoundTripUnit(t *testing.T) {
	var capturedPayloads []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedPayloads = append(capturedPayloads, string(b))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if len(capturedPayloads) == 1 {
			_, _ = w.Write([]byte("event: interaction.created\ndata: {\"interaction\":{\"id\":\"inter_rt_1\"},\"event_type\":\"interaction.created\"}\n\n"))
			_, _ = w.Write([]byte("event: step.start\ndata: {\"index\":1,\"step\":{\"id\":\"call_rt_1\",\"type\":\"function_call\",\"name\":\"generate_image\",\"arguments\":{}},\"event_type\":\"step.start\"}\n\n"))
			_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":1,\"delta\":{\"arguments\":\"{\\\"prompt\\\":\\\"calm ocean wave\\\"}\",\"type\":\"arguments_delta\"},\"event_type\":\"step.delta\"}\n\n"))
			_, _ = w.Write([]byte("event: step.stop\ndata: {\"index\":1,\"event_type\":\"step.stop\"}\n\n"))
			_, _ = w.Write([]byte("event: interaction.completed\ndata: {\"interaction\":{\"id\":\"inter_rt_1\",\"status\":\"requires_action\",\"steps\":[{\"id\":\"call_rt_1\",\"type\":\"function_call\",\"name\":\"generate_image\",\"arguments\":{\"prompt\":\"calm ocean wave\"}}]},\"event_type\":\"interaction.completed\"}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}

		// Turn 2: continuation after function_result
		_, _ = w.Write([]byte("event: interaction.created\ndata: {\"interaction\":{\"id\":\"inter_rt_2\"},\"event_type\":\"interaction.created\"}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":0,\"delta\":{\"text\":\"Here is your \",\"type\":\"text\"},\"event_type\":\"step.delta\"}\n\n"))
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":0,\"delta\":{\"text\":\"calm ocean wave!\",\"type\":\"text\"},\"event_type\":\"step.delta\"}\n\n"))
		_, _ = w.Write([]byte("event: interaction.completed\ndata: {\"interaction\":{\"id\":\"inter_rt_2\",\"status\":\"completed\"},\"event_type\":\"interaction.completed\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{BaseURL: server.URL, APIKey: "test-key"})

	// Turn 1
	s1, err := client.StreamChat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "Generate a calm ocean wave"}},
		Tools: []ai.ToolDefinition{{
			Name:        "generate_image",
			Description: "Generate an image",
			Parameters:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("turn 1 StreamChat error: %v", err)
	}
	var interID string
	var calls []ai.ToolCall
	for {
		c, err := s1.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("turn 1 Recv error: %v", err)
		}
		if c.InteractionID != "" {
			interID = c.InteractionID
		}
		calls = append(calls, c.ToolCalls...)
	}
	_ = s1.Close()

	if interID != "inter_rt_1" || len(calls) != 1 || calls[0].ID != "call_rt_1" {
		t.Fatalf("unexpected turn 1 result: interID=%q calls=%+v", interID, calls)
	}

	// Turn 2: submit simulated function_result with previous_interaction_id
	s2, err := client.StreamChat(context.Background(), ai.ChatRequest{
		PreviousInteractionID: interID,
		ToolResults: []ai.ToolResult{{
			CallID: calls[0].ID,
			Name:   calls[0].Name,
			Result: map[string]any{
				"status":    "simulated_success",
				"simulated": true,
				"note":      "Synthetic test result for round-trip verification; no image generated.",
			},
		}},
	})
	if err != nil {
		t.Fatalf("turn 2 StreamChat error: %v", err)
	}
	var contText strings.Builder
	var turn2Calls []ai.ToolCall
	for {
		c, err := s2.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("turn 2 Recv error: %v", err)
		}
		contText.WriteString(c.Delta)
		turn2Calls = append(turn2Calls, c.ToolCalls...)
	}
	_ = s2.Close()

	if len(capturedPayloads) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(capturedPayloads))
	}
	if !strings.Contains(capturedPayloads[1], `"previous_interaction_id":"inter_rt_1"`) {
		t.Errorf("turn 2 payload missing previous_interaction_id: %s", capturedPayloads[1])
	}
	if !strings.Contains(capturedPayloads[1], `"call_id":"call_rt_1"`) || !strings.Contains(capturedPayloads[1], `"type":"function_result"`) {
		t.Errorf("turn 2 payload missing function_result/call_id: %s", capturedPayloads[1])
	}
	if contText.String() != "Here is your calm ocean wave!" {
		t.Errorf("unexpected continuation text: %q", contText.String())
	}
	if len(turn2Calls) != 0 {
		t.Errorf("expected 0 duplicate tool calls in turn 2, got %d", len(turn2Calls))
	}
}

func TestClient_StreamChat_LiveGeminiFunctionResultRoundTrip(t *testing.T) {
	if os.Getenv("RUN_LIVE_GEMINI_ROUNDTRIP_TEST") != "1" {
		t.Skip("skipping live Gemini round-trip test (set RUN_LIVE_GEMINI_ROUNDTRIP_TEST=1 to enable)")
	}

	_ = godotenv.Load("../../../.env")
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" || strings.Contains(apiKey, "your_gemini") {
		t.Skip("GEMINI_API_KEY not configured")
	}
	baseURL := strings.TrimSpace(os.Getenv("GEMINI_BASE_URL"))
	modelID := strings.TrimSpace(os.Getenv("GEMINI_MODEL_ID"))
	if modelID == "" {
		modelID = DefaultModel
	}

	client := NewClient(ClientOptions{
		BaseURL:      baseURL,
		APIKey:       apiKey,
		DefaultModel: modelID,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Request 1: trigger generate_image function call
	s1, err := client.StreamChat(ctx, ai.ChatRequest{
		Model: modelID,
		Messages: []ai.Message{
			{Role: "system", Content: "You are DOUBLEU AI. When asked to generate an image, invoke the generate_image tool."},
			{Role: "user", Content: "Generate an image of a quiet mountain cabin at dusk."},
		},
		Tools: []ai.ToolDefinition{{
			Name:        "generate_image",
			Description: "Generate a single original image from a detailed text prompt.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "A vivid description of the image to generate.",
					},
				},
				"required": []string{"prompt"},
			},
		}},
		MaxTokens:   256,
		Temperature: 0.2,
		Stream:      true,
	})
	if err != nil {
		t.Fatalf("live turn 1 StreamChat error: %v", err)
	}
	var (
		turn1InteractionID string
		turn1Calls         []ai.ToolCall
	)
	for {
		c, err := s1.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = s1.Close()
			t.Fatalf("live turn 1 Recv error: %v", err)
		}
		if c.InteractionID != "" {
			turn1InteractionID = c.InteractionID
		}
		turn1Calls = append(turn1Calls, c.ToolCalls...)
	}
	_ = s1.Close()

	if turn1InteractionID == "" {
		t.Fatalf("live turn 1 returned empty interaction ID")
	}
	if len(turn1Calls) != 1 {
		t.Fatalf("live turn 1 expected 1 function_call, got %d", len(turn1Calls))
	}
	tc := turn1Calls[0]
	if tc.ID == "" || tc.Name != "generate_image" || !json.Valid(tc.Arguments) {
		t.Fatalf("live turn 1 invalid function_call: id=%q name=%q valid_json=%v", tc.ID, tc.Name, json.Valid(tc.Arguments))
	}
	t.Logf("LIVE TURN 1 PASS: model=%s interaction_id_present=true call_id=%s name=%s args_len=%d",
		modelID, tc.ID, tc.Name, len(tc.Arguments))

	// Request 2: submit synthetic simulated function_result (NO real image generated or uploaded)
	s2, err := client.StreamChat(ctx, ai.ChatRequest{
		Model:                 modelID,
		PreviousInteractionID: turn1InteractionID,
		ToolResults: []ai.ToolResult{{
			CallID: tc.ID,
			Name:   tc.Name,
			Result: map[string]any{
				"status":    "simulated_generated",
				"simulated": true,
				"message":   "Simulated image generation succeeded for round-trip verification. Provide a brief one-sentence confirmation.",
			},
		}},
		MaxTokens:   128,
		Temperature: 0.2,
		Stream:      true,
	})
	if err != nil {
		t.Fatalf("live turn 2 StreamChat error: %v", err)
	}
	var (
		turn2InteractionID string
		contText           strings.Builder
		turn2Calls         []ai.ToolCall
	)
	for {
		c, err := s2.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = s2.Close()
			t.Fatalf("live turn 2 Recv error: %v", err)
		}
		if c.InteractionID != "" {
			turn2InteractionID = c.InteractionID
		}
		contText.WriteString(c.Delta)
		turn2Calls = append(turn2Calls, c.ToolCalls...)
	}
	_ = s2.Close()

	if len(turn2Calls) != 0 {
		t.Fatalf("live turn 2 unexpectedly emitted %d duplicate tool calls", len(turn2Calls))
	}
	if strings.TrimSpace(contText.String()) == "" {
		t.Fatalf("live turn 2 returned empty continuation text")
	}
	t.Logf("LIVE TURN 2 PASS: turn2_interaction_id_present=%v continuation_len=%d duplicate_tool_calls=%d",
		turn2InteractionID != "", len(strings.TrimSpace(contText.String())), len(turn2Calls))
}
