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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/approval"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/tools"
)

func TestBridgeValidatesRawBydbQLWithoutPublishingAProviderCandidate(t *testing.T) {
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
	if event.Kind == agent.EventKindCandidate || event.ToolName != ToolValidateBydbQL {
		t.Fatalf("raw validation must not publish a candidate, got %+v", event)
	}
}

func TestBridgeCompilesStructuredQueryPlanBeforePublishingCandidate(t *testing.T) {
	schema := session.SchemaSnapshot{
		Type:   session.ResourceTypeMeasure,
		Name:   "service_latency",
		Groups: []string{"production"},
		Loaded: true,
		Columns: []session.SchemaColumn{
			{Name: "endpoint", Kind: session.SchemaColumnTag, Type: session.SchemaValueTypeString, Indexed: true},
			{Name: "status", Kind: session.SchemaColumnTag, Type: session.SchemaValueTypeInt},
			{Name: "latency", Kind: session.SchemaColumnField, Type: session.SchemaValueTypeFloat},
		},
	}
	toolBridge := New(Config{
		Executor:  &stubExecutor{schema: schema},
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, Message: "valid", QueryType: "MEASURE"}},
	})
	querySession := &session.QuerySession{SchemaSnapshot: session.SchemaSnapshot{
		AvailableGroups: []string{"production", "staging"},
		Catalog: []session.CatalogEntry{
			{Group: "production", Type: session.ResourceTypeMeasure, Name: "service_latency"},
		},
	}}
	toolBridge.SetSession(querySession)
	result := toolBridge.Call(context.Background(), Call{
		Name: ToolProposeQueryPlan,
		Arguments: map[string]any{
			"plan": map[string]any{
				"resource": map[string]any{"type": "MEASURE", "name": "service_latency", "groups": []any{"production"}},
				"projection": []any{
					map[string]any{"column": "endpoint"},
					map[string]any{"aggregate": map[string]any{"function": "MEAN", "column": "latency"}},
				},
				"filter":     map[string]any{"column": "status", "operator": "=", "value": 500},
				"group_by":   []any{"endpoint"},
				"order_by":   map[string]any{"column": "endpoint", "direction": "ASC"},
				"time_range": map[string]any{"start": "-30m"},
				"limit":      10,
			},
		},
	})
	if result.Err != nil {
		t.Fatalf("propose plan failed: %v", result.Err)
	}
	expectedQuery := "SELECT endpoint, MEAN(latency) FROM MEASURE service_latency IN production TIME > '-30m' " +
		"WHERE status = 500 GROUP BY endpoint ORDER BY endpoint ASC LIMIT 10"
	if !strings.Contains(result.Content, `"valid":true`) {
		t.Fatalf("unexpected compiled result: %s", result.Content)
	}
	_ = receiveEvent(t, toolBridge.Events())
	event := receiveEvent(t, toolBridge.Events())
	if event.Kind != agent.EventKindCandidate || event.ToolName != ToolProposeQueryPlan || event.Candidate != expectedQuery {
		t.Fatalf("expected compiled candidate event, got %+v", event)
	}
	if querySession.ResourceName != "service_latency" || querySession.ResourceType != session.ResourceTypeMeasure {
		t.Fatalf("expected proposed resource to update the session: %+v", querySession)
	}
	if len(querySession.SchemaSnapshot.AvailableGroups) != 2 || len(querySession.SchemaSnapshot.Catalog) != 1 {
		t.Fatalf("expected schema proposal to retain discovery context: %+v", querySession.SchemaSnapshot)
	}
}

func TestBridgeRejectsPlanResourceOutsideDiscoveredCatalog(t *testing.T) {
	toolBridge := New(Config{
		Executor:  &stubExecutor{},
		Validator: &stubValidator{report: session.ValidationReport{Valid: true}},
	})
	toolBridge.SetSession(&session.QuerySession{SchemaSnapshot: session.SchemaSnapshot{Catalog: []session.CatalogEntry{
		{Group: "production", Type: session.ResourceTypeMeasure, Name: "service_latency"},
	}}})
	toolBridge.SetRankedCandidates([]session.CatalogEntry{
		{Group: "production", Type: session.ResourceTypeMeasure, Name: "service_latency"},
	})
	result := toolBridge.Call(context.Background(), Call{Name: ToolProposeQueryPlan, Arguments: map[string]any{
		"plan": map[string]any{
			"resource": map[string]any{"type": "MEASURE", "name": "invented", "groups": []any{"production"}},
			"limit":    10,
		},
	}})
	if result.Err == nil || !strings.Contains(result.Err.Error(), "top five catalog candidates") {
		t.Fatalf("expected catalog rejection, got %+v", result)
	}
}

func TestBridgeLimitsSchemaInspectionsToRankedCatalogCandidates(t *testing.T) {
	catalog := session.SchemaCatalog{Entries: []session.CatalogEntry{
		{Group: "production", Type: session.ResourceTypeMeasure, Name: "latency"},
		{Group: "production", Type: session.ResourceTypeMeasure, Name: "alpha"},
		{Group: "production", Type: session.ResourceTypeMeasure, Name: "bravo"},
		{Group: "production", Type: session.ResourceTypeMeasure, Name: "charlie"},
		{Group: "production", Type: session.ResourceTypeMeasure, Name: "delta"},
		{Group: "production", Type: session.ResourceTypeMeasure, Name: "unrelated"},
	}}
	toolBridge := New(Config{Executor: &stubExecutor{catalog: catalog}})
	toolBridge.SetSession(&session.QuerySession{UserGoal: "slow latency"})
	result := toolBridge.Call(context.Background(), Call{Name: ToolListGroupsSchemas})
	if result.Err != nil || !strings.Contains(result.Content, `"candidate_limit":5`) || strings.Contains(result.Content, "unrelated") {
		t.Fatalf("expected a five-entry ranked catalog, got %+v", result)
	}
	result = toolBridge.Call(context.Background(), Call{
		Name: ToolDescribeSchema,
		Arguments: map[string]any{
			"type":   "MEASURE",
			"name":   "unrelated",
			"groups": []any{"production"},
		},
	})
	if result.Err == nil || !strings.Contains(result.Err.Error(), "top five") {
		t.Fatalf("expected detailed schema rejection outside ranked candidates, got %+v", result)
	}
}

func TestBridgeCompilesWorkflowPlanIntoIndividuallyApprovedSteps(t *testing.T) {
	schema := session.SchemaSnapshot{
		Type:   session.ResourceTypeStream,
		Name:   "logs",
		Groups: []string{"production"},
		Loaded: true,
		Columns: []session.SchemaColumn{
			{Name: "service", Kind: session.SchemaColumnTag, Type: session.SchemaValueTypeString},
			{Name: "status", Kind: session.SchemaColumnTag, Type: session.SchemaValueTypeInt},
		},
	}
	toolBridge := New(Config{
		Executor:  &stubExecutor{schema: schema},
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, QueryType: "STREAM"}},
	})
	querySession := &session.QuerySession{}
	toolBridge.SetSession(querySession)
	result := toolBridge.Call(context.Background(), Call{
		Name: ToolProposeQueryPlan,
		Arguments: map[string]any{
			"workflow": map[string]any{"steps": []any{
				map[string]any{
					"id":       "failed-requests",
					"resource": map[string]any{"type": "STREAM", "name": "logs", "groups": []any{"production"}},
					"filter":   map[string]any{"column": "status", "operator": "=", "value": 500},
					"limit":    10,
				},
				map[string]any{
					"id":       "payment-requests",
					"resource": map[string]any{"type": "STREAM", "name": "logs", "groups": []any{"production"}},
					"filter":   map[string]any{"column": "service", "operator": "=", "value": "payment"},
					"limit":    10,
				},
			}},
		},
	})
	if result.Err != nil {
		t.Fatalf("workflow proposal failed: %v", result.Err)
	}
	if len(querySession.PlannedQueries) != 2 || querySession.CurrentPlannedQuery() == nil {
		t.Fatalf("expected two individually approved steps, got %+v", querySession.PlannedQueries)
	}
	if querySession.CurrentPlannedQuery().ID != "failed-requests" {
		t.Fatalf("unexpected active workflow step: %+v", querySession.CurrentPlannedQuery())
	}
}

func TestBridgeAdvancesOnlyOneWorkflowStepAfterApproval(t *testing.T) {
	approvals := approval.NewController()
	schema := session.SchemaSnapshot{
		Type:   session.ResourceTypeStream,
		Name:   "logs",
		Groups: []string{"production"},
		Loaded: true,
		Columns: []session.SchemaColumn{
			{Name: "service", Kind: session.SchemaColumnTag, Type: session.SchemaValueTypeString},
			{Name: "status", Kind: session.SchemaColumnTag, Type: session.SchemaValueTypeInt},
		},
	}
	executor := &stubExecutor{schema: schema, result: session.ExecutionResult{Rows: 1, Columns: []string{"service"}, Preview: [][]string{{"payment"}}}}
	toolBridge := New(Config{
		Approvals: approvals,
		Executor:  executor,
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, QueryType: "STREAM"}},
	})
	querySession := &session.QuerySession{}
	toolBridge.SetSession(querySession)
	proposal := toolBridge.Call(context.Background(), Call{
		Name: ToolProposeQueryPlan,
		Arguments: map[string]any{"workflow": map[string]any{"steps": []any{
			map[string]any{
				"id":       "failed",
				"resource": map[string]any{"type": "STREAM", "name": "logs", "groups": []any{"production"}},
				"filter":   map[string]any{"column": "status", "operator": "=", "value": 500},
				"limit":    10,
			},
			map[string]any{
				"id":       "payment",
				"resource": map[string]any{"type": "STREAM", "name": "logs", "groups": []any{"production"}},
				"filter":   map[string]any{"column": "service", "operator": "=", "value": "payment"},
				"limit":    10,
			},
		}}},
	})
	if proposal.Err != nil {
		t.Fatalf("workflow proposal failed: %v", proposal.Err)
	}
	firstQuery := querySession.CurrentPlannedQuery()
	if firstQuery == nil {
		t.Fatal("expected first workflow step")
	}
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- toolBridge.Call(context.Background(), Call{Name: ToolExecuteBydbQL, Arguments: map[string]any{"query": firstQuery.Query}})
	}()
	request := receiveRequest(t, approvals.Requests())
	if resolveErr := approvals.Resolve(request.ID, approval.Decision{Approved: true}); resolveErr != nil {
		t.Fatalf("failed to approve first workflow step: %v", resolveErr)
	}
	result := <-resultCh
	if result.Err != nil {
		t.Fatalf("first workflow execution failed: %v", result.Err)
	}
	if executor.executeCount != 1 {
		t.Fatalf("expected exactly one approved execution, got %d", executor.executeCount)
	}
	if !strings.Contains(result.Content, "next_query") {
		t.Fatalf("expected the next exact query, got %s", result.Content)
	}
	nextQuery := querySession.CurrentPlannedQuery()
	if nextQuery == nil || nextQuery.ID != "payment" {
		t.Fatalf("expected second step awaiting its own approval, got %+v", nextQuery)
	}
}

func TestBridgeLimitsAutomaticPlanRepairsToTwoAfterTheInitialAttempt(t *testing.T) {
	schema := session.SchemaSnapshot{
		Type:   session.ResourceTypeMeasure,
		Name:   "service_latency",
		Groups: []string{"production"},
		Loaded: true,
		Columns: []session.SchemaColumn{
			{Name: "latency", Kind: session.SchemaColumnField, Type: session.SchemaValueTypeFloat},
		},
	}
	toolBridge := New(Config{Executor: &stubExecutor{schema: schema}, Validator: &stubValidator{report: session.ValidationReport{Valid: true}}})
	toolBridge.SetSession(&session.QuerySession{})
	invalidPlan := map[string]any{
		"resource": map[string]any{"type": "MEASURE", "name": "service_latency", "groups": []any{"production"}},
		"filter":   map[string]any{"column": "unknown", "operator": "=", "value": 1},
		"limit":    10,
	}
	for attempt := 0; attempt < 3; attempt++ {
		result := toolBridge.Call(context.Background(), Call{Name: ToolProposeQueryPlan, Arguments: map[string]any{"plan": invalidPlan}})
		if result.Err != nil || !strings.Contains(result.Content, `"valid":false`) || strings.Contains(result.Content, "repair limit") {
			t.Fatalf("expected repairable proposal failure at attempt %d, got %+v", attempt+1, result)
		}
	}
	result := toolBridge.Call(context.Background(), Call{Name: ToolProposeQueryPlan, Arguments: map[string]any{"plan": invalidPlan}})
	if result.Err != nil || !strings.Contains(result.Content, "repair limit") {
		t.Fatalf("expected repair limit after two automatic repairs, got %+v", result)
	}
}

func TestBridgeExecutesOnlyAfterExactApprovalAndDoesNotReturnRows(t *testing.T) {
	approvals := approval.NewController()
	executor := &stubExecutor{result: session.ExecutionResult{
		Rows:     3,
		Columns:  []string{"endpoint"},
		Preview:  [][]string{{"payment"}},
		Summary:  "three rows",
		Response: "secret result row",
	}}
	toolBridge := New(Config{
		Approvals: approvals,
		Executor:  executor,
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, Message: "valid", QueryType: "MEASURE"}},
	})
	query := "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"
	toolBridge.SetSession(&session.QuerySession{
		ResourceType: session.ResourceTypeMeasure,
		ResourceName: "latency",
		Groups:       []string{"production"},
		PlannedQueries: []session.PlannedQuery{{
			Query: query,
		}},
		SchemaSnapshot: session.SchemaSnapshot{
			Type: session.ResourceTypeMeasure,
		},
	})
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
	if !strings.Contains(result.Content, "payment") {
		t.Fatalf("expected bounded preview to cross the agent boundary: %s", result.Content)
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
	query := "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"
	toolBridge.SetSession(&session.QuerySession{
		PlannedQueries: []session.PlannedQuery{{Query: query}},
		SchemaSnapshot: session.SchemaSnapshot{Type: session.ResourceTypeMeasure},
	})
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- toolBridge.Call(context.Background(), Call{
			Name:      ToolExecuteBydbQL,
			Arguments: map[string]any{"query": query},
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

func TestBridgeReturnsSafeExecutionFailureForAgentRepair(t *testing.T) {
	approvals := approval.NewController()
	query := "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"
	executor := &stubExecutor{executeErr: fmt.Errorf("backend shard timeout")}
	toolBridge := New(Config{
		Approvals: approvals,
		Executor:  executor,
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, QueryType: "MEASURE"}},
	})
	toolBridge.SetSession(&session.QuerySession{
		PlannedQueries: []session.PlannedQuery{{
			Query:        query,
			ResourceType: session.ResourceTypeMeasure,
			Name:         "latency",
			Groups:       []string{"production"},
		}},
	})
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- toolBridge.Call(context.Background(), Call{Name: ToolExecuteBydbQL, Arguments: map[string]any{"query": query}})
	}()
	request := receiveRequest(t, approvals.Requests())
	if resolveErr := approvals.Resolve(request.ID, approval.Decision{Approved: true}); resolveErr != nil {
		t.Fatalf("failed to approve execution: %v", resolveErr)
	}
	result := <-resultCh
	if result.Err != nil || !strings.Contains(result.Content, `"error":"BYDBQL execution failed"`) {
		t.Fatalf("expected safe repair feedback, got %+v", result)
	}
	if strings.Contains(result.Content, "backend shard timeout") {
		t.Fatalf("execution detail must not cross the agent boundary: %s", result.Content)
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
	query := "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"
	toolBridge.SetSession(&session.QuerySession{
		PlannedQueries: []session.PlannedQuery{{Query: query}},
		SchemaSnapshot: session.SchemaSnapshot{Type: session.ResourceTypeMeasure},
	})
	resultCh := make(chan Result, 1)
	go func() {
		resultCh <- toolBridge.Call(context.Background(), Call{
			Name:      ToolExecuteBydbQL,
			Arguments: map[string]any{"query": query},
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

func TestBridgeRejectsExecutionOutsideACompiledPlan(t *testing.T) {
	toolBridge := New(Config{
		Executor:  &stubExecutor{},
		Validator: &stubValidator{report: session.ValidationReport{Valid: true, QueryType: "MEASURE"}},
	})
	toolBridge.SetSession(&session.QuerySession{SchemaSnapshot: session.SchemaSnapshot{Type: session.ResourceTypeMeasure}})
	result := toolBridge.Call(context.Background(), Call{
		Name:      ToolExecuteBydbQL,
		Arguments: map[string]any{"query": "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"},
	})
	if result.Err == nil || !strings.Contains(result.Err.Error(), "compiled query plan") {
		t.Fatalf("expected execution plan rejection, got %+v", result)
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
	for _, expected := range []string{
		"list_groups_schemas", "describe_schema", "propose_query_plan", "validate_bydbql", "execute_bydbql", "\\\"valid\\\":true",
	} {
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
	catalog      session.SchemaCatalog
	result       session.ExecutionResult
	schema       session.SchemaSnapshot
	executeErr   error
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
	if len(executor.catalog.Entries) != 0 {
		return executor.catalog, nil
	}
	return session.SchemaCatalog{Groups: []string{"production"}}, nil
}

func (executor *stubExecutor) DiscoverSchema(_ context.Context, request tools.SchemaRequest) (session.SchemaSnapshot, error) {
	if executor.schema.Name != "" {
		return executor.schema, nil
	}
	return session.SchemaSnapshot{Type: request.Type, Name: request.Name, Groups: request.Groups}, nil
}

func (executor *stubExecutor) Execute(_ context.Context, _ *session.QuerySession, _ string) (session.ExecutionResult, error) {
	executor.executeCount++
	return executor.result, executor.executeErr
}
