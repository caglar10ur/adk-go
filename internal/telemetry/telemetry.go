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

// Package telemetry sets up the open telemetry exporters to the ADK.
//
// WARNING: telemetry provided by ADK (internaltelemetry package) may change (e.g. attributes and their names)
// because we're in process to standardize and unify telemetry across all ADKs.
package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	"google.golang.org/genai"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
)

type tracerProviderHolder struct {
	tp trace.TracerProvider
}

type tracerProviderConfig struct {
	spanProcessors []sdktrace.SpanProcessor
	mu             *sync.RWMutex
}

var (
	once              sync.Once
	localTracer       tracerProviderHolder
	localTracerConfig = tracerProviderConfig{
		spanProcessors: []sdktrace.SpanProcessor{},
		mu:             &sync.RWMutex{},
	}
)

const (
	systemName                         = "gcp.vertex.agent"
	executeToolName                    = "execute_tool"
	mergeToolName                      = "(merged tools)"
	adkCaptureMessageContentInSpansEnv = "ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS"

	genAiOperationName           = "gen_ai.operation.name"
	genAiAgentDescription        = "gen_ai.agent.description"
	genAiAgentName               = "gen_ai.agent.name"
	genAiConversationID          = "gen_ai.conversation.id"
	genAiToolDescription         = "gen_ai.tool.description"
	genAiToolName                = "gen_ai.tool.name"
	genAiToolCallID              = "gen_ai.tool.call.id"
	genAiToolType                = "gen_ai.tool.type"
	genAiSystemName              = "gen_ai.system"
	genAiRequestModelName        = "gen_ai.request.model"
	genAiRequestTopP             = "gen_ai.request.top_p"
	genAiRequestMaxTokens        = "gen_ai.request.max_tokens"
	genAiResponseFinishReasons   = "gen_ai.response.finish_reasons"
	genAiUsageInputTokens        = "gen_ai.usage.input_tokens"
	genAiUsageOutputTokens       = "gen_ai.usage.output_tokens"
	genAiResponseTotalTokenCount = "gen_ai.response.total_token_count"

	gcpVertexAgentLLMRequestName   = "gcp.vertex.agent.llm_request"
	gcpVertexAgentToolCallArgsName = "gcp.vertex.agent.tool_call_args"
	gcpVertexAgentEventID          = "gcp.vertex.agent.event_id"
	gcpVertexAgentToolResponseName = "gcp.vertex.agent.tool_response"
	gcpVertexAgentLLMResponseName  = "gcp.vertex.agent.llm_response"
	gcpVertexAgentInvocationID     = "gcp.vertex.agent.invocation_id"
	gcpVertexAgentSessionID        = "gcp.vertex.agent.session_id"
	gcpVertexAgentDataName         = "gcp.vertex.agent.data"
)

// AddSpanProcessor adds a span processor to the local tracer config.
func AddSpanProcessor(processor sdktrace.SpanProcessor) {
	localTracerConfig.mu.Lock()
	defer localTracerConfig.mu.Unlock()

	localTracerConfig.spanProcessors = append(localTracerConfig.spanProcessors, processor)
}

// RegisterTelemetry sets up the local tracer that will be used to emit traces.
// We use local tracer to respect the global tracer configurations.
func RegisterTelemetry() {
	once.Do(func() {
		traceProvider := sdktrace.NewTracerProvider()

		localTracerConfig.mu.RLock()
		spanProcessors := localTracerConfig.spanProcessors
		localTracerConfig.mu.RUnlock()

		for _, processor := range spanProcessors {
			traceProvider.RegisterSpanProcessor(processor)
		}
		localTracer = tracerProviderHolder{tp: traceProvider}
	})
}

// multiTracer implements trace.Tracer and forwards calls to multiple tracers.
type multiTracer struct {
	embedded.Tracer

	local  trace.Tracer
	global trace.Tracer
}

func (m *multiTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, localSpan := m.local.Start(ctx, spanName, opts...)
	_, globalSpan := m.global.Start(ctx, spanName, opts...)
	// return the context from local tracer as it is the one that will be used to emit spans.
	return ctx, &multiSpan{spans: []trace.Span{localSpan, globalSpan}}
}

// multiSpan implements trace.Span and forwards calls to multiple spans.
type multiSpan struct {
	embedded.Span
	spans []trace.Span
}

func (m *multiSpan) End(options ...trace.SpanEndOption) {
	for _, s := range m.spans {
		s.End(options...)
	}
}

func (m *multiSpan) AddEvent(name string, options ...trace.EventOption) {
	for _, s := range m.spans {
		s.AddEvent(name, options...)
	}
}

func (m *multiSpan) AddLink(link trace.Link) {
	for _, s := range m.spans {
		s.AddLink(link)
	}
}

func (m *multiSpan) IsRecording() bool {
	for _, s := range m.spans {
		if s.IsRecording() {
			return true
		}
	}
	return false
}

func (m *multiSpan) RecordError(err error, options ...trace.EventOption) {
	for _, s := range m.spans {
		s.RecordError(err, options...)
	}
}

func (m *multiSpan) SpanContext() trace.SpanContext {
	if len(m.spans) > 0 {
		return m.spans[0].SpanContext()
	}
	return trace.SpanContext{}
}

func (m *multiSpan) SetStatus(code codes.Code, description string) {
	for _, s := range m.spans {
		s.SetStatus(code, description)
	}
}

func (m *multiSpan) SetName(name string) {
	for _, s := range m.spans {
		s.SetName(name)
	}
}

func (m *multiSpan) SetAttributes(kv ...attribute.KeyValue) {
	for _, s := range m.spans {
		s.SetAttributes(kv...)
	}
}

func (m *multiSpan) TracerProvider() trace.TracerProvider {
	if len(m.spans) > 0 {
		return m.spans[0].TracerProvider()
	}
	return nil
}

// If the global tracer is not set, the default NoopTracerProvider will be used.
// That means that the spans are NOT recording/exporting
// If the local tracer is not set, we'll set up tracer with all registered span processors.
func getTracer() trace.Tracer {
	if localTracer.tp == nil {
		RegisterTelemetry()
	}

	return &multiTracer{
		local:  localTracer.tp.Tracer(systemName),
		global: otel.GetTracerProvider().Tracer(systemName),
	}
}

// StartTrace returns a composite span to start emitting events, forwarding to both global and local tracers.
func StartTrace(ctx context.Context, traceName string) (context.Context, trace.Span) {
	return getTracer().Start(ctx, traceName)
}

// TraceAgentInvocation traces agent metadata.
func TraceAgentInvocation(span trace.Span, name, description, sessionID string) {
	attributes := []attribute.KeyValue{
		attribute.String(genAiOperationName, "invoke_agent"),
		attribute.String(genAiAgentDescription, description),
		attribute.String(genAiAgentName, name),
		attribute.String(genAiConversationID, sessionID),
	}
	span.SetAttributes(attributes...)
}

// TraceSendData traces the sending of data to the agent.
func TraceSendData(span trace.Span, invocationID string, eventID string, data []*genai.Content) {
	attributes := []attribute.KeyValue{
		attribute.String(gcpVertexAgentInvocationID, invocationID),
		attribute.String(gcpVertexAgentEventID, eventID),
	}
	if shouldAddRequestResponseToSpans() {
		serializedData := make([]map[string]any, len(data))
		for i, content := range data {
			serializedData[i] = map[string]any{
				"role":  content.Role,
				"parts": content.Parts,
			}
		}
		attributes = append(attributes, attribute.String(gcpVertexAgentDataName, safeSerialize(serializedData)))
	} else {
		attributes = append(attributes, attribute.String(gcpVertexAgentDataName, "{}"))
	}
	span.SetAttributes(attributes...)
}

// TraceMergedToolCalls traces the tool execution events.
func TraceMergedToolCalls(span trace.Span, fnResponseEvent *session.Event) {
	if fnResponseEvent == nil {
		return
	}
	attributes := []attribute.KeyValue{
		attribute.String(genAiOperationName, executeToolName),
		attribute.String(genAiToolName, mergeToolName),
		attribute.String(genAiToolDescription, mergeToolName),
		// Setting empty llm request and response (as UI expect these) while not
		// applicable for tool_response.
		attribute.String(gcpVertexAgentLLMRequestName, "{}"),
		attribute.String(gcpVertexAgentLLMResponseName, "{}"),
		attribute.String(gcpVertexAgentToolCallArgsName, "N/A"),
		attribute.String(gcpVertexAgentEventID, fnResponseEvent.ID),
	}

	if shouldAddRequestResponseToSpans() {
		attributes = append(attributes, attribute.String(gcpVertexAgentToolResponseName, safeSerialize(fnResponseEvent)))
	} else {
		attributes = append(attributes, attribute.String(gcpVertexAgentToolResponseName, "{}"))
	}

	span.SetAttributes(attributes...)
}

// TraceToolCall traces the tool execution events.
func TraceToolCall(span trace.Span, name, description, toolType string, fnArgs map[string]any, fnResponseEvent *session.Event) {
	if fnResponseEvent == nil {
		return
	}
	attributes := []attribute.KeyValue{
		attribute.String(genAiOperationName, executeToolName),
		attribute.String(genAiToolName, name),
		attribute.String(genAiToolDescription, description),
		// e.g. FunctionTool
		attribute.String(genAiToolType, toolType),

		// Setting empty llm request and response (as UI expect these) while not
		// applicable for tool_response.
		attribute.String(gcpVertexAgentLLMRequestName, "{}"),
		attribute.String(gcpVertexAgentLLMResponseName, "{}"),
		attribute.String(gcpVertexAgentEventID, fnResponseEvent.ID),
	}

	if shouldAddRequestResponseToSpans() {
		attributes = append(attributes, attribute.String(gcpVertexAgentToolCallArgsName, safeSerialize(fnArgs)))
	} else {
		attributes = append(attributes, attribute.String(gcpVertexAgentToolCallArgsName, "{}"))
	}

	toolCallID := "<not specified>"
	toolResponse := "<not specified>"

	if fnResponseEvent.LLMResponse.Content != nil {
		responseParts := fnResponseEvent.LLMResponse.Content.Parts

		if len(responseParts) > 0 {
			functionResponse := responseParts[0].FunctionResponse
			if functionResponse != nil {
				if functionResponse.ID != "" {
					toolCallID = functionResponse.ID
				}
				if functionResponse.Response != nil {
					toolResponse = safeSerialize(functionResponse.Response)
				}
			}
		}
	}

	attributes = append(attributes, attribute.String(genAiToolCallID, toolCallID))

	if shouldAddRequestResponseToSpans() {
		attributes = append(attributes, attribute.String(gcpVertexAgentToolResponseName, toolResponse))
	} else {
		attributes = append(attributes, attribute.String(gcpVertexAgentToolResponseName, "{}"))
	}

	span.SetAttributes(attributes...)
}

// TraceLLMCall fills the call_llm event details.
func TraceLLMCall(span trace.Span, sessionID string, llmRequest *model.LLMRequest, event *session.Event) {
	attributes := []attribute.KeyValue{
		attribute.String(genAiSystemName, systemName),
		attribute.String(genAiRequestModelName, llmRequest.Model),
		attribute.String(gcpVertexAgentInvocationID, event.InvocationID),
		attribute.String(gcpVertexAgentSessionID, sessionID),
		attribute.String(gcpVertexAgentEventID, event.ID),
	}

	if shouldAddRequestResponseToSpans() {
		attributes = append(attributes,
			attribute.String(gcpVertexAgentLLMRequestName, safeSerialize(llmRequestToTrace(llmRequest))),
			attribute.String(gcpVertexAgentLLMResponseName, safeSerialize(event.LLMResponse)),
		)
	} else {
		attributes = append(attributes,
			attribute.String(gcpVertexAgentLLMRequestName, "{}"),
			attribute.String(gcpVertexAgentLLMResponseName, "{}"),
		)
	}

	if llmRequest.Config.TopP != nil {
		attributes = append(attributes, attribute.Float64(genAiRequestTopP, float64(*llmRequest.Config.TopP)))
	}

	if llmRequest.Config.MaxOutputTokens != 0 {
		attributes = append(attributes, attribute.Int(genAiRequestMaxTokens, int(llmRequest.Config.MaxOutputTokens)))
	}
	if event.FinishReason != "" {
		attributes = append(attributes, attribute.StringSlice(genAiResponseFinishReasons, []string{string(event.FinishReason)}))
	}
	if event.UsageMetadata != nil {
		if event.UsageMetadata.PromptTokenCount > 0 {
			attributes = append(attributes, attribute.Int(genAiUsageInputTokens, int(event.UsageMetadata.PromptTokenCount)))
		}
		if event.UsageMetadata.CandidatesTokenCount > 0 {
			attributes = append(attributes, attribute.Int(genAiUsageOutputTokens, int(event.UsageMetadata.CandidatesTokenCount)))
		}
		if event.UsageMetadata.TotalTokenCount > 0 {
			attributes = append(attributes, attribute.Int(genAiResponseTotalTokenCount, int(event.UsageMetadata.TotalTokenCount)))
		}
	}

	span.SetAttributes(attributes...)
}

func safeSerialize(obj any) string {
	dump, err := json.Marshal(obj)
	if err != nil {
		return "<not serializable>"
	}
	return string(dump)
}

func shouldAddRequestResponseToSpans() bool {
	// Defaults to true for now to preserve backward compatibility.
	// Once prompt and response logging is well established in ADK, we might start
	// a deprecation of request/response content in spans by switching the default
	// to false.
	disabledViaEnvVar := strings.ToLower(os.Getenv(adkCaptureMessageContentInSpansEnv))
	return disabledViaEnvVar != "false" && disabledViaEnvVar != "0"
}

func llmRequestToTrace(llmRequest *model.LLMRequest) map[string]any {
	result := map[string]any{
		"config":  llmRequest.Config,
		"model":   llmRequest.Model,
		"content": []*genai.Content{},
	}
	for _, content := range llmRequest.Contents {
		parts := []*genai.Part{}
		// filter out InlineData part
		for _, part := range content.Parts {
			if part.InlineData != nil {
				continue
			}
			parts = append(parts, part)
		}
		filteredContent := &genai.Content{
			Role:  content.Role,
			Parts: parts,
		}
		result["content"] = append(result["content"].([]*genai.Content), filteredContent)
	}
	return result
}
