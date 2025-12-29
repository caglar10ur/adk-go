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

package services

import (
	"context"
	"slices"
	"strings"
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// APIServerSpanExporter is a custom SpanExporter that stores relevant span data.
// Stores attributes of specific spans (call_llm, send_data, execute_tool) keyed by `gcp.vertex.agent.event_id`.
// This is used for debugging individual events.
// APIServerSpanExporter implements sdktrace.SpanExporter interface.
type APIServerSpanExporter struct {
	mu sync.RWMutex

	traceDict map[string]map[string]string

	session2TraceID map[string][]string
	spans           []sdktrace.ReadOnlySpan
}

// NewAPIServerSpanExporter returns a APIServerSpanExporter instance
func NewAPIServerSpanExporter() *APIServerSpanExporter {
	return &APIServerSpanExporter{
		traceDict:       make(map[string]map[string]string),
		session2TraceID: make(map[string][]string),
	}
}

// GetTraceDict returns stored trace informations
func (s *APIServerSpanExporter) GetTraceDict() map[string]map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.traceDict
}

// ExportSpans implements custom export function for sdktrace.SpanExporter.
func (s *APIServerSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.spans = append(s.spans, spans...)

	for _, span := range spans {
		spanAttributes := span.Attributes()
		attributes := make(map[string]string)
		for _, attribute := range spanAttributes {
			key := string(attribute.Key)
			attributes[key] = attribute.Value.AsString()
		}
		if span.Name() == "call_llm" || span.Name() == "send_data" || strings.HasPrefix(span.Name(), "execute_tool") {
			attributes["trace_id"] = span.SpanContext().TraceID().String()
			attributes["span_id"] = span.SpanContext().SpanID().String()

			if eventID, ok := attributes["gcp.vertex.agent.event_id"]; ok {
				s.traceDict[eventID] = attributes
			}
		}
		// collect trace ids for each session from spans so that later we can construct the whole trace.
		if sessionID, ok := attributes["gcp.vertex.agent.session_id"]; ok && span.SpanContext().HasTraceID() {
			traceID := span.SpanContext().TraceID().String()
			if !slices.Contains(s.session2TraceID[sessionID], traceID) {
				s.session2TraceID[sessionID] = append(s.session2TraceID[sessionID], traceID)
			}
		}
	}
	return nil
}

func (s *APIServerSpanExporter) ExportSpansBySession(sessionID string) []sdktrace.ReadOnlySpan {
	s.mu.RLock()
	defer s.mu.RUnlock()

	traceIDs := s.session2TraceID[sessionID]
	if len(traceIDs) == 0 {
		return nil
	}

	var filteredSpans []sdktrace.ReadOnlySpan
	for _, span := range s.spans {
		if slices.Contains(traceIDs, span.SpanContext().TraceID().String()) {
			filteredSpans = append(filteredSpans, span)
		}
	}
	return filteredSpans
}

// Shutdown is a function that sdktrace.SpanExporter has, should close the span exporter connections.
// Since APIServerSpanExporter holds only in-memory dictionary, no additional logic required.
func (s *APIServerSpanExporter) Shutdown(ctx context.Context) error {
	return nil
}

var _ sdktrace.SpanExporter = (*APIServerSpanExporter)(nil)
