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

// Package agent defines provider-neutral contracts for coding agent integration.
package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent/prompt"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

// AgentGateway starts sessions and streams provider-neutral agent events.
type AgentGateway interface {
	Start(ctx context.Context, req StartRequest) (Session, error)
	Send(ctx context.Context, sessionID string, req TurnRequest) (<-chan Event, error)
	Stop(ctx context.Context, sessionID string) error
}

// Gateway is an alias for AgentGateway.
type Gateway = AgentGateway

// StartRequest contains provider-neutral session startup data.
type StartRequest struct {
	Metadata         map[string]string
	Provider         string
	WorkingDirectory string
}

// AgentStartRequest is an alias for StartRequest.
type AgentStartRequest = StartRequest

// Session identifies a provider session.
type Session struct {
	StartedAt time.Time
	ID        string
	Provider  string
}

// AgentSession is an alias for Session.
type AgentSession = Session

// TurnRequest is a structured prompt sent to an agent.
type TurnRequest struct {
	Payload RequestPayload
	Task    string
	Prompt  string
}

// AgentTurnRequest is an alias for TurnRequest.
type AgentTurnRequest = TurnRequest

// RequestPayload is the JSON shape sent through ACP/Codex adapters.
type RequestPayload struct {
	Constraints      Constraints       `json:"constraints"`
	Schema           SchemaSummary     `json:"schema"`
	QueryHints       QueryHints        `json:"query_hints"`
	TimeRange        TimeRangePayload  `json:"time_range"`
	ExecutionSummary *ExecutionSummary `json:"execution_summary,omitempty"`
	ValidationError  *string           `json:"validation_error,omitempty"`
	Task             string            `json:"task"`
	Goal             string            `json:"goal"`
	Candidate        string            `json:"candidate"`
	TemplateHint     string            `json:"template_hint,omitempty"`
}

// Constraints are hard safety constraints owned by bydbctl.
type Constraints struct {
	FinalArtifact                      string `json:"final_artifact"`
	ReadOnly                           bool   `json:"read_only"`
	MustUseSchema                      bool   `json:"must_use_schema"`
	UserMustEditOrConfirmBeforeExecute bool   `json:"user_must_edit_or_confirm_before_execute"`
	MustNotExecuteTools                bool   `json:"must_not_execute_tools"`
}

// SchemaSummary is the schema subset exposed to an agent.
type SchemaSummary struct {
	Groups        []string `json:"groups"`
	Tags          []string `json:"tags"`
	Fields        []string `json:"fields"`
	IndexedFields []string `json:"indexed_fields,omitempty"`
	Type          string   `json:"type"`
	Name          string   `json:"name"`
}

// TimeRangePayload is the BYDBQL-compatible time range from TUI slots.
type TimeRangePayload struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// QueryHints carries rule-based intent classification for prompt guidance.
type QueryHints struct {
	PreferShowTop bool   `json:"prefer_show_top,omitempty"`
	TimeRangeHint string `json:"time_range_hint,omitempty"`
	LimitHint     int    `json:"limit_hint,omitempty"`
	UseSlots      bool   `json:"use_slots"`
}

// ExecutionSummary is the compact execution feedback exposed to the agent.
type ExecutionSummary struct {
	Rows    int    `json:"rows"`
	Summary string `json:"summary"`
	Query   string `json:"query"`
	Command string `json:"command"`
	Path    string `json:"path"`
	Error   string `json:"error,omitempty"`
}

// BuildBydbqlPrompt renders the provider prompt for BYDBQL generation.
func BuildBydbqlPrompt(req TurnRequest) (string, error) {
	payload, marshalErr := MarshalPayload(req.Payload)
	if marshalErr != nil {
		return "", marshalErr
	}
	return prompt.Build(prompt.Input{
		TaskPrompt:  req.Prompt,
		PayloadJSON: payload,
		Candidate:   req.Payload.Candidate,
	}), nil
}

// EventKind identifies a normalized event emitted by an agent adapter.
type EventKind string

// Normalized agent events.
const (
	EventKindMessageDelta      EventKind = "message_delta"
	EventKindPermissionRequest EventKind = "permission_request"
	EventKindPlanUpdate        EventKind = "plan_update"
	EventKindFinalResponse     EventKind = "final_response"
	EventKindError             EventKind = "error"
)

// Event is the provider-neutral stream item consumed by WorkflowRunner and the TUI.
type Event struct {
	Kind        EventKind
	Message     string
	Candidate   string
	Explanation string
	Permission  string
	Err         error
}

// AgentEvent is an alias for Event.
type AgentEvent = Event

// IsTerminal reports whether the event ends an agent turn.
func (event Event) IsTerminal() bool {
	return event.Kind == EventKindFinalResponse || event.Kind == EventKindError
}

// BuildReviseRequest builds the structured request used by the BYDBQL refinement workflow.
func BuildReviseRequest(querySession *session.QuerySession, hints QueryHints, templateHint string) RequestPayload {
	var candidate string
	if currentCandidate := querySession.CurrentCandidate(); currentCandidate != nil {
		candidate = currentCandidate.Query
	}
	var validationError *string
	if !querySession.Validation.Valid && querySession.Validation.Message != "" {
		message := querySession.Validation.Message
		validationError = &message
	}
	var executionSummary *ExecutionSummary
	if querySession.ExecutionResult.Summary != "" || querySession.ExecutionResult.Error != "" {
		executionSummary = &ExecutionSummary{
			Rows:    querySession.ExecutionResult.Rows,
			Summary: querySession.ExecutionResult.Summary,
			Query:   querySession.ExecutionResult.Query,
			Command: querySession.ExecutionResult.Command,
			Path:    querySession.ExecutionResult.Path,
			Error:   querySession.ExecutionResult.Error,
		}
	}
	return RequestPayload{
		Task:         "revise_bydbql",
		Goal:         querySession.UserGoal,
		Candidate:    candidate,
		TemplateHint: strings.TrimSpace(templateHint),
		QueryHints:   hints,
		TimeRange: TimeRangePayload{
			Start: strings.TrimSpace(querySession.TimeRange.Start),
			End:   strings.TrimSpace(querySession.TimeRange.End),
		},
		Constraints: Constraints{
			FinalArtifact:                      "BYDBQL",
			ReadOnly:                           true,
			MustUseSchema:                      true,
			UserMustEditOrConfirmBeforeExecute: true,
			MustNotExecuteTools:                true,
		},
		Schema: SchemaSummary{
			Type:          querySession.SchemaSnapshot.Type.String(),
			Name:          querySession.SchemaSnapshot.Name,
			Groups:        append([]string(nil), querySession.SchemaSnapshot.Groups...),
			Tags:          append([]string(nil), querySession.SchemaSnapshot.Tags...),
			Fields:        append([]string(nil), querySession.SchemaSnapshot.Fields...),
			IndexedFields: append([]string(nil), querySession.SchemaSnapshot.IndexedFields...),
		},
		ExecutionSummary: executionSummary,
		ValidationError:  validationError,
	}
}

// MarshalPayload renders a structured request for subprocess prompts.
func MarshalPayload(payload RequestPayload) (string, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
