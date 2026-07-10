// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bridge

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/approval"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/tools"
)

func TestBridgeValidatesAndPublishesOnlyStructuredCandidate(t *testing.T) {
	validator := &stubValidator{report: session.ValidationReport{Valid: true, Message: "valid", QueryType: "MEASURE"}}
	toolBridge := New(Config{Validator: validator, Executor: &stubExecutor{}})
	toolBridge.SetSession(&session.QuerySession{SchemaSnapshot: session.SchemaSnapshot{Type: session.ResourceTypeMeasure}})
	query := "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"

	result := toolBridge.Call(context.Background(), Call{Name: ToolValidateBydbQL, Arguments: map[string]any{"query": query}})
	if result.Err != nil {
		t.Fatalf("validate call failed: %v", result.Err)
	}
	if !strings.Contains(result.Content, `"valid":true`) {
		t.Fatalf("unexpected result: %s", result.Content)
	}
	event := receiveEvent(t, toolBridge.Events())
	if event.Kind != agent.EventKindToolCall || event.ToolName != ToolValidateBydbQL {
		t.Fatalf("unexpected tool event: %+v", event)
	}
	event = receiveEvent(t, toolBridge.Events())
	if event.Kind != agent.EventKindCandidate || event.Candidate != query {
		t.Fatalf("expected a structured candidate event, got %+v", event)
	}
}

func TestBridgeExecutesOnlyAfterExactApprovalAndDoesNotReturnRows(t *testing.T) {
	approvals := approval.NewController()
	executor := &stubExecutor{result: session.ExecutionResult{Rows: 3, Summary: "three rows", Response: "secret result row"}}
	toolBridge := New(Config{
		Approvals: approvals,
		Executor:  executor,
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, Message: "valid", QueryType: "MEASURE"}},
	})
	toolBridge.SetSession(&session.QuerySession{
		ResourceType: session.ResourceTypeMeasure,
		ResourceName: "latency",
		Groups:       []string{"production"},
		SchemaSnapshot: session.SchemaSnapshot{
			Type: session.ResourceTypeMeasure,
		},
	})
	query := "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- toolBridge.Call(context.Background(), Call{Name: ToolExecuteBydbQL, Arguments: map[string]any{"query": query}})
	}()

	request := receiveRequest(t, approvals.Requests())
	if request.Query != query || request.Source != approval.SourceAgentTool {
		t.Fatalf("unexpected approval request: %+v", request)
	}
	if executor.executeCount != 0 {
		t.Fatal("query executed before approval")
	}
	if resolveErr := approvals.Resolve(request.ID, approval.Decision{Approved: true}); resolveErr != nil {
		t.Fatalf("failed to approve request: %v", resolveErr)
	}
	result := <-resultCh
	if result.Err != nil {
		t.Fatalf("execute call failed: %v", result.Err)
	}
	if executor.executeCount != 1 {
		t.Fatalf("expected one execution, got %d", executor.executeCount)
	}
	if strings.Contains(result.Content, "secret result row") {
		t.Fatalf("raw response must not cross the agent boundary: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"rows":3`) {
		t.Fatalf("unexpected execute summary: %s", result.Content)
	}
}

func TestBridgeDoesNotExecuteWhenApprovalIsRejected(t *testing.T) {
	approvals := approval.NewController()
	executor := &stubExecutor{}
	toolBridge := New(Config{
		Approvals: approvals,
		Executor:  executor,
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, Message: "valid", QueryType: "MEASURE"}},
	})
	toolBridge.SetSession(&session.QuerySession{SchemaSnapshot: session.SchemaSnapshot{Type: session.ResourceTypeMeasure}})
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- toolBridge.Call(context.Background(), Call{
			Name:      ToolExecuteBydbQL,
			Arguments: map[string]any{"query": "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"},
		})
	}()
	request := receiveRequest(t, approvals.Requests())
	if resolveErr := approvals.Resolve(request.ID, approval.Decision{}); resolveErr != nil {
		t.Fatalf("failed to reject request: %v", resolveErr)
	}
	if result := <-resultCh; result.Err == nil {
		t.Fatal("expected rejected execution to return an error")
	}
	if executor.executeCount != 0 {
		t.Fatalf("expected rejected execution not to call BanyanDB, got %d calls", executor.executeCount)
	}
}

func TestBridgeCancelsAnAlreadySentQuery(t *testing.T) {
	approvals := approval.NewController()
	executor := &cancellableExecutor{started: make(chan struct{})}
	toolBridge := New(Config{
		Approvals: approvals,
		Executor:  executor,
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, Message: "valid", QueryType: "MEASURE"}},
	})
	toolBridge.SetSession(&session.QuerySession{SchemaSnapshot: session.SchemaSnapshot{Type: session.ResourceTypeMeasure}})
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- toolBridge.Call(context.Background(), Call{
			Name:      ToolExecuteBydbQL,
			Arguments: map[string]any{"query": "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"},
		})
	}()
	request := receiveRequest(t, approvals.Requests())
	if resolveErr := approvals.Resolve(request.ID, approval.Decision{Approved: true}); resolveErr != nil {
		t.Fatalf("failed to approve request: %v", resolveErr)
	}
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for query request")
	}
	toolBridge.Cancel()
	if result := <-resultCh; result.Err == nil {
		t.Fatal("expected cancelled query to fail")
	}
}

func TestServeMCPExposesOnlyControlledToolDefinitions(t *testing.T) {
	toolBridge := New(Config{
		Executor:  &stubExecutor{},
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, Message: "valid", QueryType: "MEASURE"}},
	})
	toolBridge.SetSession(&session.QuerySession{SchemaSnapshot: session.SchemaSnapshot{Type: session.ResourceTypeMeasure}})
	socketServer, startErr := StartSocketServer(toolBridge)
	if startErr != nil {
		t.Fatalf("failed to start private socket server: %v", startErr)
	}
	defer func() {
		_ = socketServer.Close()
	}()
	validateRequest := `{"jsonrpc":"2.0","id":"validate","method":"tools/call","params":{"name":"validate_bydbql","arguments":{"query":` +
		`"SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"}}}`
	input := strings.NewReader(strings.Join([]string{
		"{\"jsonrpc\":\"2.0\",\"id\":\"initialize\",\"method\":\"initialize\",\"params\":{}}",
		"{\"jsonrpc\":\"2.0\",\"id\":\"tools\",\"method\":\"tools/list\",\"params\":{}}",
		validateRequest,
	}, "\n"))
	var output bytes.Buffer
	if serveErr := ServeMCP(socketServer.Path(), input, &output); serveErr != nil {
		t.Fatalf("ServeMCP failed: %v", serveErr)
	}
	response := output.String()
	for _, expected := range []string{"list_groups_schemas", "describe_schema", "validate_bydbql", "execute_bydbql", "\\\"valid\\\":true"} {
		if !strings.Contains(response, expected) {
			t.Fatalf("MCP response missing %q:\n%s", expected, response)
		}
	}
	if strings.Contains(response, "username") || strings.Contains(response, "password") || strings.Contains(response, "localhost") {
		t.Fatalf("MCP response must not expose connection details:\n%s", response)
	}
}

func receiveEvent(t *testing.T, events <-chan agent.Event) agent.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bridge event")
		return agent.Event{}
	}
}

func receiveRequest(t *testing.T, requests <-chan approval.Request) approval.Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval request")
		return approval.Request{}
	}
}

type stubValidator struct {
	report session.ValidationReport
	err    error
}

func (validator *stubValidator) Validate(_ context.Context, _ string, _ *session.SchemaSnapshot) (session.ValidationReport, error) {
	return validator.report, validator.err
}

type stubExecutor struct {
	result       session.ExecutionResult
	executeCount int
}

type cancellableExecutor struct {
	started chan struct{}
}

func (executor *cancellableExecutor) DiscoverCatalog(_ context.Context) (session.SchemaCatalog, error) {
	return session.SchemaCatalog{}, nil
}

func (executor *cancellableExecutor) DiscoverSchema(_ context.Context, _ tools.SchemaRequest) (session.SchemaSnapshot, error) {
	return session.SchemaSnapshot{}, nil
}

func (executor *cancellableExecutor) Execute(ctx context.Context, _ *session.QuerySession, _ string) (session.ExecutionResult, error) {
	close(executor.started)
	<-ctx.Done()
	return session.ExecutionResult{Error: ctx.Err().Error()}, ctx.Err()
}

func (executor *stubExecutor) DiscoverCatalog(_ context.Context) (session.SchemaCatalog, error) {
	return session.SchemaCatalog{Groups: []string{"production"}}, nil
}

func (executor *stubExecutor) DiscoverSchema(_ context.Context, request tools.SchemaRequest) (session.SchemaSnapshot, error) {
	return session.SchemaSnapshot{Type: request.Type, Name: request.Name, Groups: request.Groups}, nil
}

func (executor *stubExecutor) Execute(_ context.Context, _ *session.QuerySession, _ string) (session.ExecutionResult, error) {
	executor.executeCount++
	return executor.result, nil
}
