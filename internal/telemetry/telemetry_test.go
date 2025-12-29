// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package telemetry

import (
	"context"
	"os"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/genai"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
)

type capturingExporter struct {
	spans []sdktrace.ReadOnlySpan
}

func (e *capturingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *capturingExporter) Shutdown(ctx context.Context) error {
	return nil
}

func setupTestTracer() (*sdktrace.TracerProvider, *capturingExporter, trace.Tracer) {
	capturer := &capturingExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(capturer))
	tracer := tp.Tracer("test-tracer")
	return tp, capturer, tracer
}

func getSpanAttributes(span sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	attrs := make(map[attribute.Key]attribute.Value)
	for _, kv := range span.Attributes() {
		attrs[kv.Key] = kv.Value
	}
	return attrs
}

func TestTraceAgentInvocation(t *testing.T) {
	tp, capturer, tracer := setupTestTracer()
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test")

	TraceAgentInvocation(span, "agent-name", "agent-desc", "session-id")
	span.End()

	_ = tp.Shutdown(ctx)

	if len(capturer.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(capturer.spans))
	}

	attrs := getSpanAttributes(capturer.spans[0])

	if attrs[attribute.Key(genAiOperationName)].AsString() != "invoke_agent" {
		t.Errorf("expected invoke_agent, got %s", attrs[attribute.Key(genAiOperationName)].AsString())
	}
	if attrs[attribute.Key(genAiAgentName)].AsString() != "agent-name" {
		t.Errorf("expected agent-name, got %s", attrs[attribute.Key(genAiAgentName)].AsString())
	}
	if attrs[attribute.Key(genAiAgentDescription)].AsString() != "agent-desc" {
		t.Errorf("expected agent-desc, got %s", attrs[attribute.Key(genAiAgentDescription)].AsString())
	}
	if attrs[attribute.Key(genAiConversationID)].AsString() != "session-id" {
		t.Errorf("expected session-id, got %s", attrs[attribute.Key(genAiConversationID)].AsString())
	}
}

func TestTraceSendData(t *testing.T) {
	tp, capturer, tracer := setupTestTracer()
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test")

	data := []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hello"}}}}
	TraceSendData(span, "inv-id", "evt-id", data)
	span.End()

	_ = tp.Shutdown(ctx)

	attrs := getSpanAttributes(capturer.spans[0])

	if attrs[attribute.Key(gcpVertexAgentInvocationID)].AsString() != "inv-id" {
		t.Errorf("expected inv-id, got %s", attrs[attribute.Key(gcpVertexAgentInvocationID)].AsString())
	}
	if attrs[attribute.Key(gcpVertexAgentEventID)].AsString() != "evt-id" {
		t.Errorf("expected evt-id, got %s", attrs[attribute.Key(gcpVertexAgentEventID)].AsString())
	}
	// Check data attribute (should be present by default)
	if attrs[attribute.Key(gcpVertexAgentDataName)].AsString() == "" {
		t.Error("expected data attribute to be present")
	}
}

func TestTraceLLMCall(t *testing.T) {
	tp, capturer, tracer := setupTestTracer()
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test")

	req := &model.LLMRequest{
		Model: "gemini-pro",
		Config: &genai.GenerateContentConfig{
			TopP:            ptr(float32(0.9)),
			MaxOutputTokens: 100,
		},
	}

	event := &session.Event{
		InvocationID: "inv-id",
		ID:           "evt-id",
		LLMResponse: model.LLMResponse{
			FinishReason: genai.FinishReasonStop,
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     10,
				CandidatesTokenCount: 20,
				TotalTokenCount:      30,
			},
		},
	}

	TraceLLMCall(span, "sess-id", req, event)
	// span is ended inside TraceLLMCall

	_ = tp.Shutdown(ctx)

	attrs := getSpanAttributes(capturer.spans[0])

	if attrs[attribute.Key(genAiRequestModelName)].AsString() != "gemini-pro" {
		t.Errorf("expected gemini-pro, got %s", attrs[attribute.Key(genAiRequestModelName)].AsString())
	}
	if attrs[attribute.Key(genAiUsageInputTokens)].AsInt64() != 10 {
		t.Errorf("expected 10, got %d", attrs[attribute.Key(genAiUsageInputTokens)].AsInt64())
	}
	if attrs[attribute.Key(genAiUsageOutputTokens)].AsInt64() != 20 {
		t.Errorf("expected 20, got %d", attrs[attribute.Key(genAiUsageOutputTokens)].AsInt64())
	}
}

func TestShouldAddRequestResponseToSpans(t *testing.T) {
	// Test default behavior
	if !shouldAddRequestResponseToSpans() {
		t.Error("expected true by default")
	}

	// Test with env var false
	os.Setenv(adkCaptureMessageContentInSpansEnv, "false")
	if shouldAddRequestResponseToSpans() {
		t.Error("expected false when ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS is false")
	}

	// Test with env var 0
	os.Setenv(adkCaptureMessageContentInSpansEnv, "0")
	if shouldAddRequestResponseToSpans() {
		t.Error("expected false when ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS is 0")
	}

	// Test with env var true
	os.Setenv(adkCaptureMessageContentInSpansEnv, "true")
	if !shouldAddRequestResponseToSpans() {
		t.Error("expected true when ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS is true")
	}

	os.Unsetenv(adkCaptureMessageContentInSpansEnv)
}

func ptr[T any](v T) *T {
	return &v
}
