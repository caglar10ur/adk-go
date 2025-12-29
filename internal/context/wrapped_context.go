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

package context

import (
	"context"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

type WrappedContext struct {
	context.Context
	InvocationContext agent.InvocationContext
}

func NewWrappedContext(ctx context.Context, ictx agent.InvocationContext) *WrappedContext {
	return &WrappedContext{
		Context:           ctx,
		InvocationContext: ictx,
	}
}

func (w *WrappedContext) Done() <-chan struct{}             { return w.Context.Done() }
func (w *WrappedContext) Err() error                        { return w.Context.Err() }
func (w *WrappedContext) Deadline() (time.Time, bool)       { return w.Context.Deadline() }
func (w *WrappedContext) Value(key interface{}) interface{} { return w.Context.Value(key) }

func (w *WrappedContext) Artifacts() agent.Artifacts {
	return w.InvocationContext.Artifacts()
}

func (w *WrappedContext) Agent() agent.Agent {
	return w.InvocationContext.Agent()
}

func (w *WrappedContext) Branch() string {
	return w.InvocationContext.Branch()
}

func (w *WrappedContext) InvocationID() string {
	return w.InvocationContext.InvocationID()
}

func (w *WrappedContext) Memory() agent.Memory {
	return w.InvocationContext.Memory()
}

func (w *WrappedContext) Session() session.Session {
	return w.InvocationContext.Session()
}

func (w *WrappedContext) UserContent() *genai.Content {
	return w.InvocationContext.UserContent()
}

func (w *WrappedContext) RunConfig() *agent.RunConfig {
	return w.InvocationContext.RunConfig()
}

func (w *WrappedContext) EndInvocation() {
	w.InvocationContext.EndInvocation()
}

func (w *WrappedContext) Ended() bool {
	return w.InvocationContext.Ended()
}
