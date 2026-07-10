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

// Package bridge provides the private, bydbctl-owned tool set exposed to ACP agents.
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/approval"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/tools"
)

const (
	eventBufferSize       = 64
	ToolListGroupsSchemas = "list_groups_schemas"
	ToolDescribeSchema    = "describe_schema"
	ToolValidateBydbQL    = "validate_bydbql"
	ToolExecuteBydbQL     = "execute_bydbql"
)

// Validator checks a BYDBQL query without executing it.
type Validator interface {
	Validate(ctx context.Context, query string, schema *session.SchemaSnapshot) (session.ValidationReport, error)
}

// Config creates a private tool bridge.
type Config struct {
	Approvals *approval.Controller
	Executor  tools.Executor
	Validator Validator
}

// Call is one structured MCP tool request.
type Call struct {
	Arguments map[string]any
	Name      string
}

// Result is the compact, provider-safe result of a tool request.
type Result struct {
	Content string
	Err     error
}

// ToolBridge holds all tool policy, execution, and visible lifecycle events behind a small interface.
type ToolBridge struct {
	approvals *approval.Controller
	executor  tools.Executor
	validator Validator
	now       func() time.Time
	events    chan agent.Event

	mu           sync.RWMutex
	querySession *session.QuerySession
	executionMu  sync.Mutex
	cancelQuery  context.CancelFunc
}

// New creates a private tool bridge. The bridge never receives server credentials.
func New(config Config) *ToolBridge {
	approvals := config.Approvals
	if approvals == nil {
		approvals = approval.NewController()
	}
	return &ToolBridge{
		approvals: approvals,
		executor:  config.Executor,
		validator: config.Validator,
		now:       time.Now,
		events:    make(chan agent.Event, eventBufferSize),
	}
}

// SetSession sets the TUI-owned session used by subsequent tool calls.
func (toolBridge *ToolBridge) SetSession(querySession *session.QuerySession) {
	toolBridge.mu.Lock()
	toolBridge.querySession = querySession
	toolBridge.mu.Unlock()
}

// Events returns visible tool lifecycle updates for the TUI.
func (toolBridge *ToolBridge) Events() <-chan agent.Event {
	return toolBridge.events
}

// Cancel rejects pending approvals and makes a best effort to cancel the active query request.
func (toolBridge *ToolBridge) Cancel() {
	if toolBridge == nil {
		return
	}
	toolBridge.approvals.Cancel()
	toolBridge.executionMu.Lock()
	cancelQuery := toolBridge.cancelQuery
	toolBridge.executionMu.Unlock()
	if cancelQuery != nil {
		cancelQuery()
	}
}

// Call dispatches only the closed, registered bydbctl tool set.
func (toolBridge *ToolBridge) Call(ctx context.Context, call Call) Result {
	toolName := strings.TrimSpace(call.Name)
	callID := uuid.NewString()
	toolBridge.emit(agent.Event{
		ID:           callID,
		Kind:         agent.EventKindToolCall,
		ToolName:     toolName,
		InputSummary: summarizeArguments(call.Arguments),
		Status:       agent.EventStatusRunning,
		StartedAt:    toolBridge.now(),
	})
	var result Result
	switch toolName {
	case ToolListGroupsSchemas:
		result = toolBridge.listGroupsSchemas(ctx)
	case ToolDescribeSchema:
		result = toolBridge.describeSchema(ctx, call.Arguments)
	case ToolValidateBydbQL:
		result = toolBridge.validateBydbQL(ctx, callID, call.Arguments)
	case ToolExecuteBydbQL:
		result = toolBridge.executeBydbQL(ctx, callID, call.Arguments)
	default:
		result = Result{Err: fmt.Errorf("tool %q is not registered", toolName)}
	}
	toolBridge.emitResult(callID, toolName, result)
	return result
}

func (toolBridge *ToolBridge) listGroupsSchemas(ctx context.Context) Result {
	if toolBridge.executor == nil {
		return Result{Err: fmt.Errorf("schema executor is not configured")}
	}
	catalog, catalogErr := toolBridge.executor.DiscoverCatalog(ctx)
	if catalogErr != nil {
		return Result{Err: fmt.Errorf("group and schema discovery failed")}
	}
	return jsonResult(map[string]any{
		"groups":    catalog.Groups,
		"resources": catalog.Entries,
	})
}

func (toolBridge *ToolBridge) describeSchema(ctx context.Context, arguments map[string]any) Result {
	if toolBridge.executor == nil {
		return Result{Err: fmt.Errorf("schema executor is not configured")}
	}
	querySession := toolBridge.session()
	resourceType := session.NormalizeResourceType(stringArgument(arguments, "type"))
	resourceName := stringArgument(arguments, "name")
	groups := stringSliceArgument(arguments, "groups")
	if querySession != nil {
		if resourceName == "" {
			resourceName = querySession.ResourceName
		}
		if len(groups) == 0 {
			groups = append([]string(nil), querySession.Groups...)
		}
		if stringArgument(arguments, "type") == "" {
			resourceType = querySession.ResourceType
		}
	}
	snapshot, schemaErr := toolBridge.executor.DiscoverSchema(ctx, tools.SchemaRequest{
		Type:   resourceType,
		Name:   resourceName,
		Groups: groups,
	})
	if schemaErr != nil {
		return Result{Err: fmt.Errorf("schema description failed")}
	}
	return jsonResult(map[string]any{
		"type":           snapshot.Type,
		"name":           snapshot.Name,
		"groups":         snapshot.Groups,
		"tags":           snapshot.Tags,
		"fields":         snapshot.Fields,
		"indexed_fields": snapshot.IndexedFields,
	})
}

func (toolBridge *ToolBridge) validateBydbQL(ctx context.Context, callID string, arguments map[string]any) Result {
	if toolBridge.validator == nil {
		return Result{Err: fmt.Errorf("BYDBQL validator is not configured")}
	}
	query := stringArgument(arguments, "query")
	querySession := toolBridge.session()
	var schemaSnapshot *session.SchemaSnapshot
	if querySession != nil {
		schemaSnapshot = &querySession.SchemaSnapshot
	}
	validation, validateErr := toolBridge.validator.Validate(ctx, query, schemaSnapshot)
	if validateErr != nil {
		return Result{Err: fmt.Errorf("failed to validate BYDBQL: %w", validateErr)}
	}
	if validation.Valid {
		toolBridge.emit(agent.Event{
			ID:          callID,
			Kind:        agent.EventKindCandidate,
			Candidate:   strings.TrimSpace(query),
			Message:     "candidate validated through controlled tool",
			Status:      agent.EventStatusSucceeded,
			CompletedAt: toolBridge.now(),
			ToolName:    ToolValidateBydbQL,
		})
	}
	return jsonResult(map[string]any{
		"valid":      validation.Valid,
		"message":    validation.Message,
		"query_type": validation.QueryType,
	})
}

func (toolBridge *ToolBridge) executeBydbQL(ctx context.Context, callID string, arguments map[string]any) Result {
	if toolBridge.validator == nil || toolBridge.executor == nil {
		return Result{Err: fmt.Errorf("BYDBQL execution bridge is not configured")}
	}
	querySession := toolBridge.session()
	if querySession == nil {
		return Result{Err: fmt.Errorf("query session is not configured")}
	}
	query := stringArgument(arguments, "query")
	validation, validationErr := toolBridge.validator.Validate(ctx, query, &querySession.SchemaSnapshot)
	if validationErr != nil {
		return Result{Err: fmt.Errorf("failed to validate execution query: %w", validationErr)}
	}
	if !validation.Valid {
		return jsonResult(map[string]any{"valid": false, "message": validation.Message})
	}
	toolBridge.emit(agent.Event{
		ID:           callID,
		Kind:         agent.EventKindApproval,
		ToolName:     ToolExecuteBydbQL,
		Message:      "execution approval required",
		InputSummary: "exact BYDBQL statement awaiting user decision",
		Status:       agent.EventStatusWaiting,
		StartedAt:    toolBridge.now(),
	})
	limits := tools.Limits(toolBridge.executor)
	request := approval.WithLimits(
		approval.NewRequest(
			query,
			fmt.Sprintf("%s/%s", querySession.ResourceType, querySession.ResourceName),
			querySession.Groups,
			approval.SourceAgentTool,
		),
		limits.Timeout,
		limits.PreviewRows,
	)
	decision, approvalErr := toolBridge.approvals.Request(ctx, request)
	if approvalErr != nil {
		return Result{Err: fmt.Errorf("execution approval did not complete: %w", approvalErr)}
	}
	if !decision.Approved {
		toolBridge.emit(agent.Event{
			ID:          callID,
			Kind:        agent.EventKindCancelled,
			ToolName:    ToolExecuteBydbQL,
			Message:     "execution rejected",
			Status:      agent.EventStatusCancelled,
			CompletedAt: toolBridge.now(),
		})
		return Result{Err: fmt.Errorf("execution rejected")}
	}
	validation, validationErr = toolBridge.validator.Validate(ctx, query, &querySession.SchemaSnapshot)
	if validationErr != nil {
		return Result{Err: fmt.Errorf("failed to revalidate approved query: %w", validationErr)}
	}
	if !validation.Valid {
		return Result{Err: fmt.Errorf("approved query failed revalidation: %s", validation.Message)}
	}
	executionCtx, cancelQuery := context.WithCancel(ctx)
	toolBridge.setExecutionCancel(cancelQuery)
	executionResult, executeErr := toolBridge.executor.Execute(executionCtx, querySession, query)
	cancelQuery()
	toolBridge.clearExecutionCancel()
	querySession.ExecutionResult = executionResult
	if executeErr != nil {
		return Result{Err: fmt.Errorf("BYDBQL execution failed")}
	}
	return jsonResult(map[string]any{
		"rows":    executionResult.Rows,
		"summary": executionResult.Summary,
		"error":   providerError(executionResult.Error),
	})
}

func providerError(executionError string) string {
	if strings.TrimSpace(executionError) == "" {
		return ""
	}
	return "BYDBQL execution failed"
}

func (toolBridge *ToolBridge) setExecutionCancel(cancelQuery context.CancelFunc) {
	toolBridge.executionMu.Lock()
	toolBridge.cancelQuery = cancelQuery
	toolBridge.executionMu.Unlock()
}

func (toolBridge *ToolBridge) clearExecutionCancel() {
	toolBridge.executionMu.Lock()
	toolBridge.cancelQuery = nil
	toolBridge.executionMu.Unlock()
}

func (toolBridge *ToolBridge) session() *session.QuerySession {
	toolBridge.mu.RLock()
	defer toolBridge.mu.RUnlock()
	return toolBridge.querySession
}

func (toolBridge *ToolBridge) emitResult(callID, toolName string, result Result) {
	status := agent.EventStatusSucceeded
	message := "tool completed"
	if result.Err != nil {
		status = agent.EventStatusFailed
		message = result.Err.Error()
	}
	toolBridge.emit(agent.Event{
		ID:            callID,
		Kind:          agent.EventKindToolResult,
		ToolName:      toolName,
		Message:       message,
		OutputSummary: summarizeResult(result),
		Status:        status,
		CompletedAt:   toolBridge.now(),
		Err:           result.Err,
	})
}

func (toolBridge *ToolBridge) emit(event agent.Event) {
	select {
	case toolBridge.events <- event:
	default:
	}
}

func jsonResult(value any) Result {
	encodedValue, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return Result{Err: fmt.Errorf("failed to encode tool result: %w", marshalErr)}
	}
	return Result{Content: string(encodedValue)}
}

func stringArgument(arguments map[string]any, name string) string {
	if arguments == nil {
		return ""
	}
	value, ok := arguments[name].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func stringSliceArgument(arguments map[string]any, name string) []string {
	if arguments == nil {
		return nil
	}
	switch value := arguments[name].(type) {
	case string:
		return compactStrings(strings.Split(value, ","))
	case []string:
		return compactStrings(value)
	case []any:
		groups := make([]string, 0, len(value))
		for _, item := range value {
			if group, ok := item.(string); ok {
				groups = append(groups, group)
			}
		}
		return compactStrings(groups)
	default:
		return nil
	}
}

func compactStrings(values []string) []string {
	compactedValues := make([]string, 0, len(values))
	for _, value := range values {
		if trimmedValue := strings.TrimSpace(value); trimmedValue != "" {
			compactedValues = append(compactedValues, trimmedValue)
		}
	}
	return compactedValues
}

func summarizeArguments(arguments map[string]any) string {
	if len(arguments) == 0 {
		return "no parameters"
	}
	if query := stringArgument(arguments, "query"); query != "" {
		return fmt.Sprintf("query=%d characters", len([]rune(query)))
	}
	keys := make([]string, 0, len(arguments))
	for key := range arguments {
		keys = append(keys, key)
	}
	return "parameters=" + strings.Join(keys, ",")
}

func summarizeResult(result Result) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	if result.Content == "" {
		return "completed"
	}
	return fmt.Sprintf("result=%d characters", len([]rune(result.Content)))
}
