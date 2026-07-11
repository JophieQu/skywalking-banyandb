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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/approval"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/planner"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/tools"
)

const (
	eventBufferSize       = 64
	maxPlanAttempts       = 3
	maxSchemaDescriptions = 3
	ToolListGroupsSchemas = "list_groups_schemas"
	ToolDescribeSchema    = "describe_schema"
	ToolProposeQueryPlan  = "propose_query_plan"
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

	mu                 sync.RWMutex
	querySession       *session.QuerySession
	planAttempts       int
	schemaDescriptions int
	rankedCandidates   []session.CatalogEntry
	executionMu        sync.Mutex
	cancelQuery        context.CancelFunc
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
	toolBridge.planAttempts = 0
	toolBridge.schemaDescriptions = 0
	toolBridge.rankedCandidates = nil
	toolBridge.mu.Unlock()
}

// SetRankedCandidates pins the catalog shortlist used by describe_schema and propose_query_plan.
func (toolBridge *ToolBridge) SetRankedCandidates(candidates []session.CatalogEntry) {
	toolBridge.mu.Lock()
	toolBridge.rankedCandidates = append([]session.CatalogEntry(nil), candidates...)
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
	case ToolProposeQueryPlan:
		result = toolBridge.proposeQueryPlan(ctx, callID, call.Arguments)
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
	goal := ""
	if querySession := toolBridge.session(); querySession != nil {
		goal = querySession.UserGoal
	}
	candidates := toolBridge.rankedCatalogCandidates()
	if len(candidates) == 0 {
		candidates = rankCatalogCandidates(goal, catalog.Entries)
		toolBridge.setRankedCandidates(candidates)
	}
	return jsonResult(map[string]any{
		"groups":          catalog.Groups,
		"candidate_limit": maxCatalogCandidates,
		"resources":       candidates,
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
	if !resourceIsRanked(toolBridge.rankedCatalogCandidates(), resourceType, resourceName, groups) {
		return Result{Err: fmt.Errorf("schema description requires a resource from the top five catalog candidates")}
	}
	if !toolBridge.reserveSchemaDescription() {
		return Result{Err: fmt.Errorf("schema discovery limit reached after three detailed schema inspections")}
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
		"columns":        columnsForProvider(snapshot.Columns),
		"indexed_fields": snapshot.IndexedFields,
	})
}

const maxCatalogCandidates = 5

func (toolBridge *ToolBridge) setRankedCandidates(candidates []session.CatalogEntry) {
	toolBridge.mu.Lock()
	toolBridge.rankedCandidates = append([]session.CatalogEntry(nil), candidates...)
	toolBridge.mu.Unlock()
}

func (toolBridge *ToolBridge) rankedCatalogCandidates() []session.CatalogEntry {
	toolBridge.mu.RLock()
	defer toolBridge.mu.RUnlock()
	return append([]session.CatalogEntry(nil), toolBridge.rankedCandidates...)
}

func rankCatalogCandidates(goal string, entries []session.CatalogEntry) []session.CatalogEntry {
	goalTokens := strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(goal, "_", " ")), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	normalizedGoal := strings.ToLower(goal)
	type rankedEntry struct {
		entry session.CatalogEntry
		score int
	}
	ranked := make([]rankedEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" || strings.HasPrefix(entry.Group, "_") {
			continue
		}
		name := strings.ToLower(entry.Name)
		group := strings.ToLower(entry.Group)
		score := 0
		for _, token := range goalTokens {
			if strings.Contains(name, token) {
				score += 3
			}
			if strings.Contains(group, token) {
				score += 2
			}
		}
		if strings.Contains(normalizedGoal, name) {
			score += 20
		}
		if strings.Contains(normalizedGoal, group) {
			score += 10
		}
		if entry.Type == session.ResourceTypeMeasure && (strings.Contains(normalizedGoal, "metric") || strings.Contains(normalizedGoal, "latency") || strings.Contains(normalizedGoal, "endpoint")) {
			score += 4
		}
		if entry.Type == session.ResourceTypeStream && (strings.Contains(normalizedGoal, "log") || strings.Contains(normalizedGoal, "stream")) {
			score += 2
		}
		if entry.Type == session.ResourceTypeTrace && (strings.Contains(normalizedGoal, "trace") || strings.Contains(normalizedGoal, "span")) {
			score += 2
		}
		if strings.Contains(group, "metric") && (strings.Contains(normalizedGoal, "metric") || strings.Contains(normalizedGoal, "endpoint") || strings.Contains(normalizedGoal, "latency")) {
			score += 6
		}
		if strings.Contains(name, "latency") && strings.Contains(normalizedGoal, "slow") {
			score += 8
		}
		if strings.Contains(name, "endpoint") && (strings.Contains(normalizedGoal, "endpoint") || strings.Contains(normalizedGoal, "payment")) {
			score += 8
		}
		if group == "default" && len(entries) > 20 {
			score -= 8
		}
		ranked = append(ranked, rankedEntry{entry: entry, score: score})
	}
	sort.SliceStable(ranked, func(leftIndex, rightIndex int) bool {
		if ranked[leftIndex].score != ranked[rightIndex].score {
			return ranked[leftIndex].score > ranked[rightIndex].score
		}
		if ranked[leftIndex].entry.Group != ranked[rightIndex].entry.Group {
			return ranked[leftIndex].entry.Group < ranked[rightIndex].entry.Group
		}
		if ranked[leftIndex].entry.Type != ranked[rightIndex].entry.Type {
			return ranked[leftIndex].entry.Type < ranked[rightIndex].entry.Type
		}
		return ranked[leftIndex].entry.Name < ranked[rightIndex].entry.Name
	})
	if len(ranked) > maxCatalogCandidates {
		ranked = ranked[:maxCatalogCandidates]
	}
	candidates := make([]session.CatalogEntry, 0, len(ranked))
	for _, entry := range ranked {
		candidates = append(candidates, entry.entry)
	}
	return candidates
}

func resourceIsRanked(candidates []session.CatalogEntry, resourceType session.ResourceType, resourceName string, groups []string) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, group := range groups {
		found := false
		for _, entry := range candidates {
			if catalogTypesCompatible(resourceType, entry.Type) &&
				strings.EqualFold(entry.Name, resourceName) &&
				strings.EqualFold(entry.Group, group) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func catalogTypesCompatible(planType, catalogType session.ResourceType) bool {
	if planType == catalogType {
		return true
	}
	return planType == session.ResourceTypeTopN && catalogType == session.ResourceTypeMeasure
}

func (toolBridge *ToolBridge) reserveSchemaDescription() bool {
	toolBridge.mu.Lock()
	defer toolBridge.mu.Unlock()
	if toolBridge.schemaDescriptions >= maxSchemaDescriptions {
		return false
	}
	toolBridge.schemaDescriptions++
	return true
}

func columnsForProvider(columns []session.SchemaColumn) []map[string]any {
	if len(columns) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(columns))
	for _, column := range columns {
		result = append(result, map[string]any{
			"name":    column.Name,
			"kind":    column.Kind,
			"type":    column.Type,
			"indexed": column.Indexed,
		})
	}
	return result
}

func setSessionSchema(querySession *session.QuerySession, schemaSnapshot session.SchemaSnapshot) {
	if len(schemaSnapshot.AvailableGroups) == 0 {
		schemaSnapshot.AvailableGroups = append([]string(nil), querySession.SchemaSnapshot.AvailableGroups...)
	}
	if len(schemaSnapshot.Catalog) == 0 {
		schemaSnapshot.Catalog = append([]session.CatalogEntry(nil), querySession.SchemaSnapshot.Catalog...)
	}
	querySession.ResourceType = schemaSnapshot.Type
	querySession.ResourceName = schemaSnapshot.Name
	querySession.Groups = append([]string(nil), schemaSnapshot.Groups...)
	querySession.SchemaSnapshot = schemaSnapshot
}

func (toolBridge *ToolBridge) proposeQueryPlan(ctx context.Context, callID string, arguments map[string]any) Result {
	if toolBridge.executor == nil || toolBridge.validator == nil {
		return Result{Err: fmt.Errorf("query plan bridge is not configured")}
	}
	querySession := toolBridge.session()
	if querySession == nil {
		return Result{Err: fmt.Errorf("query session is not configured")}
	}
	plans, planErr := plannedQueries(arguments)
	if planErr != nil {
		return jsonResult(map[string]any{
			"valid":       false,
			"message":     planErr.Error(),
			"schema_hint": queryPlanSchemaHint(),
		})
	}
	attempt, allowed := toolBridge.reservePlanAttempt()
	if !allowed {
		return jsonResult(map[string]any{
			"valid":   false,
			"message": "automatic query plan repair limit reached after two repairs",
		})
	}
	if attempt > 1 {
		toolBridge.emit(agent.Event{
			ID:        callID,
			Kind:      agent.EventKindPlanUpdate,
			ToolName:  ToolProposeQueryPlan,
			Message:   fmt.Sprintf("repairing query plan (%d of %d attempts)", attempt, maxPlanAttempts),
			Status:    agent.EventStatusRunning,
			StartedAt: toolBridge.now(),
		})
	}
	compiledQueries := make([]planner.CompiledQuery, 0, len(plans))
	plannedQueries := make([]session.PlannedQuery, 0, len(plans))
	var selectedSnapshot session.SchemaSnapshot
	for planIndex, plan := range plans {
		if rankedCandidates := toolBridge.rankedCatalogCandidates(); len(rankedCandidates) != 0 &&
			!resourceIsRanked(rankedCandidates, plan.Resource.Type, plan.Resource.Name, plan.Resource.Groups) {
			return Result{Err: fmt.Errorf("query plan step %d selects a resource outside the top five catalog candidates", planIndex+1)}
		}
		if !resourceIsDiscoverable(querySession.SchemaSnapshot.Catalog, plan.Resource) {
			return Result{Err: fmt.Errorf("query plan step %d selects a resource outside the discovered catalog", planIndex+1)}
		}
		snapshot, schemaErr := toolBridge.executor.DiscoverSchema(ctx, schemaRequestForPlan(plan))
		if schemaErr != nil {
			return Result{Err: fmt.Errorf("failed to discover schema for plan step %d: %w", planIndex+1, schemaErr)}
		}
		compiled, compileErr := planner.Compile(plan, snapshot)
		if compileErr != nil {
			return jsonResult(map[string]any{
				"valid":       false,
				"message":     compileErr.Error(),
				"step":        planIndex + 1,
				"schema_hint": queryPlanSchemaHint(),
			})
		}
		validation, validationErr := toolBridge.validator.Validate(ctx, compiled.Query, &snapshot)
		if validationErr != nil {
			return Result{Err: fmt.Errorf("failed to validate query plan step %d: %w", planIndex+1, validationErr)}
		}
		if !validation.Valid {
			return jsonResult(map[string]any{
				"valid":   false,
				"message": validation.Message,
				"step":    planIndex + 1,
			})
		}
		if planIndex == 0 {
			selectedSnapshot = snapshot
		}
		compiledQueries = append(compiledQueries, compiled)
		plannedQueries = append(plannedQueries, session.PlannedQuery{
			ID:           compiled.ID,
			Query:        compiled.Query,
			ResourceType: compiled.Resource.Type,
			Name:         compiled.Resource.Name,
			Groups:       append([]string(nil), compiled.Resource.Groups...),
		})
	}
	setSessionSchema(querySession, selectedSnapshot)
	querySession.SetPlannedQueries(plannedQueries)
	firstQuery := compiledQueries[0]
	toolBridge.emit(agent.Event{
		ID:          callID,
		Kind:        agent.EventKindCandidate,
		ToolName:    ToolProposeQueryPlan,
		Candidate:   firstQuery.Query,
		Message:     "query plan compiled through controlled tool",
		Status:      agent.EventStatusSucceeded,
		CompletedAt: toolBridge.now(),
	})
	return jsonResult(map[string]any{
		"valid":        true,
		"query":        firstQuery.Query,
		"step_count":   len(compiledQueries),
		"resource":     firstQuery.Resource,
		"next_step_id": firstQuery.ID,
	})
}

func schemaRequestForPlan(plan planner.QueryPlan) tools.SchemaRequest {
	resourceType := plan.Resource.Type
	if resourceType == session.ResourceTypeTopN {
		resourceType = session.ResourceTypeMeasure
	}
	return tools.SchemaRequest{
		Type:   resourceType,
		Name:   plan.Resource.Name,
		Groups: plan.Resource.Groups,
	}
}

func resourceIsDiscoverable(catalog []session.CatalogEntry, resource planner.Resource) bool {
	if len(catalog) == 0 {
		return true
	}
	for _, group := range resource.Groups {
		found := false
		for _, entry := range catalog {
			if catalogTypesCompatible(resource.Type, entry.Type) &&
				strings.EqualFold(entry.Name, resource.Name) &&
				strings.EqualFold(entry.Group, group) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (toolBridge *ToolBridge) reservePlanAttempt() (int, bool) {
	toolBridge.mu.Lock()
	defer toolBridge.mu.Unlock()
	if toolBridge.planAttempts >= maxPlanAttempts {
		return toolBridge.planAttempts, false
	}
	toolBridge.planAttempts++
	return toolBridge.planAttempts, true
}

func plannedQueries(arguments map[string]any) ([]planner.QueryPlan, error) {
	planValue, hasPlan := arguments["plan"]
	workflowValue, hasWorkflow := arguments["workflow"]
	if hasPlan == hasWorkflow {
		return nil, fmt.Errorf("propose_query_plan requires exactly one of plan or workflow")
	}
	if hasPlan {
		var plan planner.QueryPlan
		if decodeErr := decodePlanArgument(normalizePlanArgument(planValue), &plan); decodeErr != nil {
			return nil, fmt.Errorf("invalid query plan: %w", decodeErr)
		}
		return []planner.QueryPlan{plan}, nil
	}
	var workflow planner.WorkflowPlan
	if decodeErr := decodePlanArgument(normalizePlanArgument(workflowValue), &workflow); decodeErr != nil {
		return nil, fmt.Errorf("invalid query workflow: %w", decodeErr)
	}
	if len(workflow.Steps) == 0 {
		return nil, fmt.Errorf("query workflow requires at least one step")
	}
	return workflow.Steps, nil
}

func decodePlanArgument(value any, target any) error {
	encodedValue, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return fmt.Errorf("failed to encode plan input: %w", marshalErr)
	}
	decoder := json.NewDecoder(bytes.NewReader(encodedValue))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if decodeErr := decoder.Decode(target); decodeErr != nil {
		return fmt.Errorf("failed to decode plan input: %w", decodeErr)
	}
	return nil
}

func (toolBridge *ToolBridge) validateBydbQL(ctx context.Context, _ string, arguments map[string]any) Result {
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
	plannedQuery := querySession.CurrentPlannedQuery()
	if plannedQuery == nil || plannedQuery.Query != query {
		return Result{Err: fmt.Errorf("execute_bydbql requires the current compiled query plan statement")}
	}
	schemaSnapshot, schemaErr := toolBridge.executor.DiscoverSchema(ctx, tools.SchemaRequest{
		Type:   plannedQuery.ResourceType,
		Name:   plannedQuery.Name,
		Groups: plannedQuery.Groups,
	})
	if schemaErr != nil {
		return Result{Err: fmt.Errorf("failed to refresh planned query schema: %w", schemaErr)}
	}
	setSessionSchema(querySession, schemaSnapshot)
	validation, validationErr := toolBridge.validator.Validate(ctx, query, &schemaSnapshot)
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
	executionCancelled := executionCtx.Err() != nil
	cancelQuery()
	toolBridge.clearExecutionCancel()
	querySession.ExecutionResult = executionResult
	if executeErr != nil {
		if executionCancelled {
			return Result{Err: fmt.Errorf("BYDBQL execution failed")}
		}
		return jsonResult(map[string]any{
			"rows":    executionResult.Rows,
			"summary": "BYDBQL execution failed",
			"error":   "BYDBQL execution failed",
		})
	}
	nextPlannedQuery := querySession.CompletePlannedQuery(query)
	response := map[string]any{
		"rows":      executionResult.Rows,
		"summary":   executionResult.Summary,
		"error":     providerError(executionResult.Error),
		"columns":   executionResult.Columns,
		"preview":   executionResult.Preview,
		"truncated": executionResult.Truncated,
	}
	if nextPlannedQuery != nil {
		response["next_query"] = nextPlannedQuery.Query
		toolBridge.emit(agent.Event{
			ID:          uuid.NewString(),
			Kind:        agent.EventKindCandidate,
			ToolName:    ToolProposeQueryPlan,
			Candidate:   nextPlannedQuery.Query,
			Message:     "next planned query ready for individual approval",
			Status:      agent.EventStatusSucceeded,
			CompletedAt: toolBridge.now(),
		})
	}
	return jsonResult(response)
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
	event.Origin = agent.EventOriginToolBridge
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
