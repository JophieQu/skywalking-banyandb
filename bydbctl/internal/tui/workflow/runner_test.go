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

package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent/fake"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

func TestSyncSessionUpdatesSlots(t *testing.T) {
	runner := NewRunner(Config{AgentGateway: fake.NewGateway()})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeMeasure,
		ResourceName: "service_latency",
		Groups:       []string{"default"},
		Goal:         "first goal",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	updatedSession, syncErr := runner.SyncSession(context.Background(), querySession, StartOptions{
		ResourceType:   session.ResourceTypeStream,
		ResourceName:   "sw",
		Groups:         []string{"production"},
		Goal:           "updated goal",
		TimeRange:      session.TimeRange{Start: "-1h"},
		NameProvided:   true,
		GroupsProvided: true,
		TypeProvided:   true,
	})
	if syncErr != nil {
		t.Fatalf("SyncSession returned error: %v", syncErr)
	}
	if updatedSession.ResourceName != "sw" {
		t.Fatalf("unexpected resource name: %s", updatedSession.ResourceName)
	}
	if updatedSession.ResourceType != session.ResourceTypeStream {
		t.Fatalf("unexpected resource type: %s", updatedSession.ResourceType)
	}
	if updatedSession.UserGoal != "updated goal" {
		t.Fatalf("unexpected goal: %s", updatedSession.UserGoal)
	}
	if len(updatedSession.Transcript) < 2 {
		t.Fatalf("expected refresh transcript entry: %+v", updatedSession.Transcript)
	}
}

func TestStartSessionDiscoversSchemaWithoutCandidate(t *testing.T) {
	runner := NewRunner(Config{AgentGateway: fake.NewGateway()})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeTopN,
		ResourceName: "service_latency",
		Groups:       []string{"production"},
		TimeRange:    session.TimeRange{Start: "-30m"},
		Goal:         "top slow endpoints",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	if querySession.CurrentCandidate() != nil {
		t.Fatalf("expected no candidate before agent generation: %+v", querySession.CurrentCandidate())
	}
	if querySession.Phase != session.PhaseIntent {
		t.Fatalf("unexpected phase: %s", querySession.Phase)
	}
	if querySession.SchemaSnapshot.Name != "service_latency" {
		t.Fatalf("unexpected schema snapshot: %+v", querySession.SchemaSnapshot)
	}
}

func TestReviseWithFakeAgentAndAccept(t *testing.T) {
	runner := NewRunner(Config{AgentGateway: fake.NewGateway()})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeMeasure,
		ResourceName: "service_latency",
		Groups:       []string{"production"},
		Goal:         "average latency",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	events, reviseErr := runner.ReviseWithAgent(context.Background(), querySession)
	if reviseErr != nil {
		t.Fatalf("ReviseWithAgent returned error: %v", reviseErr)
	}
	if len(events) == 0 {
		t.Fatal("expected agent events")
	}
	if querySession.Phase != session.PhaseReady {
		t.Fatalf("unexpected phase: %s", querySession.Phase)
	}
	if executeErr := runner.ExecuteCurrent(context.Background(), querySession); executeErr != nil {
		t.Fatalf("ExecuteCurrent returned error: %v", executeErr)
	}
	if acceptErr := runner.AcceptCurrent(querySession); acceptErr != nil {
		t.Fatalf("AcceptCurrent returned error: %v", acceptErr)
	}
	if querySession.AcceptedQuery == "" {
		t.Fatal("expected accepted query")
	}
}

func TestAcceptCurrentRejectsInvalidCandidate(t *testing.T) {
	runner := NewRunner(Config{AgentGateway: fake.NewGateway()})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeStream,
		ResourceName: "sw",
		Groups:       []string{"default"},
		Goal:         "find logs",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	if validateErr := runner.ValidateManualQuery(context.Background(), querySession, "SELECT FROM"); validateErr != nil {
		t.Fatalf("ValidateManualQuery returned error: %v", validateErr)
	}
	if acceptErr := runner.AcceptCurrent(querySession); acceptErr == nil {
		t.Fatal("expected invalid candidate to be rejected")
	}
}

func TestReviseWithAgentExtractsCandidateFromMessage(t *testing.T) {
	gateway := scriptedGateway{
		events: []agent.Event{
			{
				Kind:    agent.EventKindFinalResponse,
				Message: "```bydbql\nSELECT * FROM STREAM sw IN default TIME > '-30m' LIMIT 10\n```",
			},
		},
	}
	runner := NewRunner(Config{AgentGateway: gateway})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeStream,
		ResourceName: "sw",
		Groups:       []string{"default"},
		Goal:         "find logs",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	if _, reviseErr := runner.ReviseWithAgent(context.Background(), querySession); reviseErr != nil {
		t.Fatalf("ReviseWithAgent returned error: %v", reviseErr)
	}
	currentCandidate := querySession.CurrentCandidate()
	if currentCandidate == nil || !strings.Contains(currentCandidate.Query, "SELECT * FROM STREAM sw") {
		t.Fatalf("unexpected candidate: %+v", currentCandidate)
	}
}

func TestReviseWithAgentExtractsCandidateFromChunkedMessages(t *testing.T) {
	gateway := scriptedGateway{
		events: []agent.Event{
			{
				Kind:    agent.EventKindMessageDelta,
				Message: "Here is the final query:\n```bydbql\nSELECT *",
			},
			{
				Kind:    agent.EventKindMessageDelta,
				Message: "FROM STREAM sw IN default",
			},
			{
				Kind:    agent.EventKindMessageDelta,
				Message: "TIME > '-30m' LIMIT 10\n```",
			},
		},
	}
	runner := NewRunner(Config{AgentGateway: gateway})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeStream,
		ResourceName: "sw",
		Groups:       []string{"default"},
		Goal:         "find logs",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	if _, reviseErr := runner.ReviseWithAgent(context.Background(), querySession); reviseErr != nil {
		t.Fatalf("ReviseWithAgent returned error: %v", reviseErr)
	}
	currentCandidate := querySession.CurrentCandidate()
	if currentCandidate == nil || !strings.Contains(currentCandidate.Query, "TIME > '-30m' LIMIT 10") {
		t.Fatalf("unexpected candidate: %+v", currentCandidate)
	}
}

func TestReviseWithAgentExtractsCandidateFromFragmentedCodexACPOutput(t *testing.T) {
	gateway := scriptedGateway{
		events: []agent.Event{
			{Kind: agent.EventKindMessageDelta, Message: "```"},
			{Kind: agent.EventKindMessageDelta, Message: "by"},
			{Kind: agent.EventKindMessageDelta, Message: "db"},
			{Kind: agent.EventKindMessageDelta, Message: "ql"},
			{Kind: agent.EventKindMessageDelta, Message: "text"},
			{Kind: agent.EventKindMessageDelta, Message: "SELECT"},
			{Kind: agent.EventKindMessageDelta, Message: "*"},
			{Kind: agent.EventKindMessageDelta, Message: "FROM"},
			{Kind: agent.EventKindMessageDelta, Message: "ME"},
			{Kind: agent.EventKindMessageDelta, Message: "ASURE"},
			{Kind: agent.EventKindMessageDelta, Message: "service"},
			{Kind: agent.EventKindMessageDelta, Message: "_endpoint"},
			{Kind: agent.EventKindMessageDelta, Message: "_latency"},
			{Kind: agent.EventKindMessageDelta, Message: "IN"},
			{Kind: agent.EventKindMessageDelta, Message: "default"},
			{Kind: agent.EventKindMessageDelta, Message: "TIME"},
			{Kind: agent.EventKindMessageDelta, Message: ">"},
			{Kind: agent.EventKindMessageDelta, Message: "'-"},
			{Kind: agent.EventKindMessageDelta, Message: "30"},
			{Kind: agent.EventKindMessageDelta, Message: "m"},
			{Kind: agent.EventKindMessageDelta, Message: "'"},
			{Kind: agent.EventKindMessageDelta, Message: "LIMIT"},
			{Kind: agent.EventKindMessageDelta, Message: "text"},
			{Kind: agent.EventKindMessageDelta, Message: "100"},
			{Kind: agent.EventKindMessageDelta, Message: "text"},
			{Kind: agent.EventKindMessageDelta, Message: "```"},
		},
	}
	runner := NewRunner(Config{AgentGateway: gateway})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeMeasure,
		ResourceName: "service_endpoint_latency",
		Groups:       []string{"default"},
		Goal:         "生成正常的 bydbql, limit100",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	if _, reviseErr := runner.ReviseWithAgent(context.Background(), querySession); reviseErr != nil {
		t.Fatalf("ReviseWithAgent returned error: %v", reviseErr)
	}
	currentCandidate := querySession.CurrentCandidate()
	if currentCandidate == nil {
		t.Fatal("expected candidate")
	}
	expected := "SELECT * FROM MEASURE service_endpoint_latency IN default TIME > '-30m' LIMIT 100"
	if currentCandidate.Query != expected {
		t.Fatalf("unexpected candidate:\nwant: %s\n got: %s", expected, currentCandidate.Query)
	}
}

func TestNormalizeFragmentedAgentTextTimeRange(t *testing.T) {
	input := "TIME > '- 30 m '"
	got := normalizeFragmentedAgentText(input)
	want := "TIME > '-30m'"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRepairFragmentedQueryAggregateAVG(t *testing.T) {
	input := "SHOW TOP 10 FROM MEASURE service_endpoint_latency IN default TIME > '-30m' AGGREGATE BY AV G ORDER BY DESC"
	want := "SHOW TOP 10 FROM MEASURE service_endpoint_latency IN default TIME > '-30m' AGGREGATE BY AVG ORDER BY DESC"
	got := RepairFragmentedQuery(input)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFinalCandidateClaudeACPFragmentedShowTop(t *testing.T) {
	fragments := []string{
		"```", "b", "yd", "b", "ql", "text", "SH", "OW", "TOP", "text", "10", "FROM", "ME", "AS", "URE",
		"service", "_end", "point", "_l", "at", "ency", "IN", "sw", "_", "metrics", "TIME", ">", "'-", "30", "m", "'",
		"A", "GG", "REG", "ATE", "BY", "SUM", "ORDER", "BY", "DESC", "text", "```",
	}
	var events []agent.Event
	events = append(events, agent.Event{Kind: agent.EventKindPlanUpdate, Message: "available commands updated"})
	var buffered []string
	for _, fragment := range fragments {
		events = append(events, agent.Event{Kind: agent.EventKindMessageDelta, Message: fragment})
		buffered = append(buffered, fragment)
	}
	events = append(events, agent.Event{
		Kind:    agent.EventKindFinalResponse,
		Message: strings.Join(buffered, "\n"),
	})
	candidate := finalCandidate(events)
	if candidate == "" {
		t.Fatal("expected candidate from claude-acp fragmented SHOW TOP output")
	}
	expected := "SHOW TOP 10 FROM MEASURE service_endpoint_latency IN sw_metrics TIME > '-30m' AGGREGATE BY SUM ORDER BY DESC"
	if candidate != expected {
		t.Fatalf("unexpected candidate:\nwant: %s\n got: %s", expected, candidate)
	}
}

func TestReviseWithAgentExtractsCandidateFromJSONMessage(t *testing.T) {
	gateway := scriptedGateway{
		events: []agent.Event{
			{
				Kind: agent.EventKindFinalResponse,
				Message: `{
					"candidate": "SELECT * FROM MEASURE service_latency IN production TIME > '-30m' LIMIT 10"
				}`,
			},
		},
	}
	runner := NewRunner(Config{AgentGateway: gateway})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeMeasure,
		ResourceName: "service_latency",
		Groups:       []string{"production"},
		Goal:         "average latency",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	if _, reviseErr := runner.ReviseWithAgent(context.Background(), querySession); reviseErr != nil {
		t.Fatalf("ReviseWithAgent returned error: %v", reviseErr)
	}
	currentCandidate := querySession.CurrentCandidate()
	if currentCandidate == nil || !strings.Contains(currentCandidate.Query, "SELECT * FROM MEASURE service_latency") {
		t.Fatalf("unexpected candidate: %+v", currentCandidate)
	}
}

func TestReviseWithAgentReportsRawOutputWhenNoCandidate(t *testing.T) {
	gateway := scriptedGateway{
		events: []agent.Event{
			{
				Kind:    agent.EventKindFinalResponse,
				Message: "I need more information before writing a query.",
			},
		},
	}
	runner := NewRunner(Config{AgentGateway: gateway})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeMeasure,
		ResourceName: "service_latency",
		Groups:       []string{"production"},
		Goal:         "average latency",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	_, reviseErr := runner.ReviseWithAgent(context.Background(), querySession)
	if reviseErr == nil {
		t.Fatal("expected ReviseWithAgent to return error")
	}
	if !strings.Contains(reviseErr.Error(), "agent output: I need more information") {
		t.Fatalf("expected raw agent output in error, got: %v", reviseErr)
	}
}

func TestReviseWithAgentReportsInvalidCandidateDetails(t *testing.T) {
	gateway := scriptedGateway{
		events: []agent.Event{
			{
				Kind:      agent.EventKindFinalResponse,
				Candidate: "SELECT * FROM STREAM sw IN default WHERE",
			},
		},
	}
	runner := NewRunner(Config{AgentGateway: gateway, MaxRetries: 1})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeStream,
		ResourceName: "sw",
		Groups:       []string{"default"},
		Goal:         "find logs",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	_, reviseErr := runner.ReviseWithAgent(context.Background(), querySession)
	if reviseErr == nil {
		t.Fatal("expected ReviseWithAgent to return error")
	}
	if !strings.Contains(reviseErr.Error(), "last candidate: SELECT * FROM STREAM sw IN default WHERE") {
		t.Fatalf("expected invalid candidate in error, got: %v", reviseErr)
	}
	if !strings.Contains(querySession.Validation.Message, "syntax error") {
		t.Fatalf("expected validation detail, got: %s", querySession.Validation.Message)
	}
}

func TestReviseWithAgentIncludesExecutionSummary(t *testing.T) {
	var requests []agent.TurnRequest
	gateway := scriptedGateway{
		events: []agent.Event{
			{
				Kind:      agent.EventKindFinalResponse,
				Candidate: "SELECT * FROM MEASURE service_latency IN production TIME > '-30m' LIMIT 10",
			},
		},
		requests: &requests,
	}
	runner := NewRunner(Config{AgentGateway: gateway})
	querySession, startErr := runner.StartSession(context.Background(), StartOptions{
		ResourceType: session.ResourceTypeMeasure,
		ResourceName: "service_latency",
		Groups:       []string{"production"},
		Goal:         "average latency",
	})
	if startErr != nil {
		t.Fatalf("StartSession returned error: %v", startErr)
	}
	query := "SELECT * FROM MEASURE service_latency IN production TIME > '-30m' LIMIT 10"
	if validateErr := runner.ValidateManualQuery(context.Background(), querySession, query); validateErr != nil {
		t.Fatalf("ValidateManualQuery returned error: %v", validateErr)
	}
	if executeErr := runner.ExecuteCurrent(context.Background(), querySession); executeErr != nil {
		t.Fatalf("ExecuteCurrent returned error: %v", executeErr)
	}
	if _, reviseErr := runner.ReviseWithAgent(context.Background(), querySession); reviseErr != nil {
		t.Fatalf("ReviseWithAgent returned error: %v", reviseErr)
	}
	if len(requests) == 0 {
		t.Fatal("expected agent request")
	}
	payload := requests[0].Payload
	if payload.ExecutionSummary == nil {
		t.Fatal("expected execution summary in agent payload")
	}
	if payload.ExecutionSummary.Command != "POST /api/v1/bydbql/query" {
		t.Fatalf("unexpected command: %s", payload.ExecutionSummary.Command)
	}
	if !payload.Constraints.UserMustEditOrConfirmBeforeExecute {
		t.Fatal("expected execution confirmation constraint")
	}
}

type scriptedGateway struct {
	events   []agent.Event
	requests *[]agent.TurnRequest
}

func (gateway scriptedGateway) Start(_ context.Context, _ agent.StartRequest) (agent.Session, error) {
	return agent.Session{ID: "scripted", Provider: "scripted"}, nil
}

func (gateway scriptedGateway) Send(ctx context.Context, _ string, req agent.TurnRequest) (<-chan agent.Event, error) {
	if gateway.requests != nil {
		*gateway.requests = append(*gateway.requests, req)
	}
	events := make(chan agent.Event, len(gateway.events))
	go func() {
		defer close(events)
		for _, event := range gateway.events {
			select {
			case <-ctx.Done():
				return
			case events <- event:
			}
		}
	}()
	return events, nil
}

func (gateway scriptedGateway) Stop(_ context.Context, _ string) error {
	return nil
}
