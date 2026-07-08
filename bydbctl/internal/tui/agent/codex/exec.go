// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Package codex provides Codex-backed agent adapters.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
)

const defaultCodexBin = "codex"

// ExecGateway runs codex exec --json as a single-turn fallback adapter.
type ExecGateway struct {
	now              func() time.Time
	binPath          string
	workingDirectory string
}

// ExecOption configures an ExecGateway.
type ExecOption func(*ExecGateway)

// WithBinPath sets the codex executable path.
func WithBinPath(binPath string) ExecOption {
	return func(gateway *ExecGateway) {
		if strings.TrimSpace(binPath) != "" {
			gateway.binPath = binPath
		}
	}
}

// WithWorkingDirectory sets the command working directory.
func WithWorkingDirectory(workingDirectory string) ExecOption {
	return func(gateway *ExecGateway) {
		gateway.workingDirectory = workingDirectory
	}
}

// NewExecGateway creates a Codex exec gateway.
func NewExecGateway(options ...ExecOption) *ExecGateway {
	gateway := &ExecGateway{
		now:     time.Now,
		binPath: defaultCodexBin,
	}
	for _, option := range options {
		option(gateway)
	}
	return gateway
}

// Start creates a local session record for codex exec turns.
func (gateway *ExecGateway) Start(_ context.Context, req agent.StartRequest) (agent.Session, error) {
	return agent.Session{
		ID:        "codex-exec-" + uuid.NewString(),
		Provider:  req.Provider,
		StartedAt: gateway.now(),
	}, nil
}

// Send runs codex exec --json and normalizes line-delimited JSON events.
func (gateway *ExecGateway) Send(ctx context.Context, _ string, req agent.TurnRequest) (<-chan agent.Event, error) {
	events := make(chan agent.Event, 16)
	prompt, promptErr := buildPrompt(req)
	if promptErr != nil {
		return nil, fmt.Errorf("failed to build codex prompt: %w", promptErr)
	}
	command := exec.CommandContext(ctx, gateway.binPath, "exec", "--json", prompt)
	if gateway.workingDirectory != "" {
		command.Dir = gateway.workingDirectory
	}
	stdoutPipe, stdoutErr := command.StdoutPipe()
	if stdoutErr != nil {
		return nil, fmt.Errorf("failed to open codex stdout: %w", stdoutErr)
	}
	stderrPipe, stderrErr := command.StderrPipe()
	if stderrErr != nil {
		return nil, fmt.Errorf("failed to open codex stderr: %w", stderrErr)
	}
	if startErr := command.Start(); startErr != nil {
		return nil, fmt.Errorf("failed to start codex exec: %w", startErr)
	}
	go gateway.stream(ctx, command, stdoutPipe, stderrPipe, events)
	return events, nil
}

// Stop stops a codex exec session.
func (gateway *ExecGateway) Stop(_ context.Context, _ string) error {
	return nil
}

type stderrResult struct {
	err  error
	text string
}

func (gateway *ExecGateway) stream(ctx context.Context, command *exec.Cmd, stdout io.Reader, stderr io.Reader, events chan<- agent.Event) {
	defer close(events)
	stderrCh := make(chan stderrResult, 1)
	go func() {
		stderrBytes, readErr := io.ReadAll(stderr)
		stderrCh <- stderrResult{
			err:  readErr,
			text: strings.TrimSpace(string(stderrBytes)),
		}
	}()
	var finalCandidate string
	var finalMessage string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		event := parseCodexLine(scanner.Bytes())
		if event.Kind == "" {
			continue
		}
		if event.Candidate != "" {
			finalCandidate = event.Candidate
		}
		if event.Message != "" {
			finalMessage = event.Message
		}
		if !send(ctx, events, event) {
			return
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		_ = send(ctx, events, agent.Event{
			Kind:    agent.EventKindError,
			Message: "failed to read codex output",
			Err:     scanErr,
		})
		return
	}
	waitErr := command.Wait()
	stderrData := <-stderrCh
	if stderrData.err != nil {
		_ = send(ctx, events, agent.Event{
			Kind:    agent.EventKindError,
			Message: "failed to read codex stderr",
			Err:     stderrData.err,
		})
		return
	}
	if waitErr != nil {
		message := strings.TrimSpace(stderrData.text)
		if message == "" {
			message = waitErr.Error()
		}
		_ = send(ctx, events, agent.Event{
			Kind:    agent.EventKindError,
			Message: message,
			Err:     waitErr,
		})
		return
	}
	if finalCandidate != "" {
		_ = send(ctx, events, agent.Event{
			Kind:      agent.EventKindFinalResponse,
			Message:   finalMessage,
			Candidate: finalCandidate,
		})
	}
}

func buildPrompt(req agent.TurnRequest) (string, error) {
	return agent.BuildBydbqlPrompt(req)
}

func parseCodexLine(line []byte) agent.Event {
	var raw map[string]any
	if unmarshalErr := json.Unmarshal(line, &raw); unmarshalErr != nil {
		text := strings.TrimSpace(string(line))
		if text == "" {
			return agent.Event{}
		}
		return agent.Event{
			Kind:    agent.EventKindMessageDelta,
			Message: text,
		}
	}
	eventType := strings.ToLower(stringValue(raw, "type", "event", "kind"))
	message := stringValue(raw, "message", "delta", "content", "text")
	candidate := stringValue(raw, "candidate", "bydbql", "query", "final")
	switch {
	case strings.Contains(eventType, "error"):
		return agent.Event{
			Kind:    agent.EventKindError,
			Message: message,
			Err:     fmt.Errorf("%s", message),
		}
	case strings.Contains(eventType, "permission"):
		return agent.Event{
			Kind:       agent.EventKindPermissionRequest,
			Message:    message,
			Permission: message,
		}
	case strings.Contains(eventType, "plan"):
		return agent.Event{
			Kind:    agent.EventKindPlanUpdate,
			Message: message,
		}
	case strings.Contains(eventType, "final"), candidate != "":
		return agent.Event{
			Kind:      agent.EventKindFinalResponse,
			Message:   message,
			Candidate: candidate,
		}
	default:
		return agent.Event{
			Kind:    agent.EventKindMessageDelta,
			Message: message,
		}
	}
}

func stringValue(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if text, textOK := value.(string); textOK {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func send(ctx context.Context, events chan<- agent.Event, event agent.Event) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- event:
		return true
	}
}
