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

// Package workflow owns the deterministic BYDBQL assistant state machine.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	tuibysql "github.com/apache/skywalking-banyandb/bydbctl/internal/tui/bydbql"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/tools"
)

const (
	defaultGroupName    = "default"
	defaultResourceName = "service_endpoint_latency"
	defaultTimeStart    = "-30m"
	defaultLimit        = 10
	defaultTopN         = 10
	defaultMaxRetries   = 2
	maxDiagnosticLength = 360
)

var fragmentedTimeRangePattern = regexp.MustCompile(`'-\s*(\d+)\s*m\s*'`)

var fragmentedTokenReplacements = []struct {
	old string
	new string
}{
	{old: "by db ql", new: "bydbql"},
	{old: "b yd b ql", new: "bydbql"},
	{old: "SH OW", new: "SHOW"},
	{old: "A GG REG ATE", new: "AGGREGATE"},
	{old: "AGGREGATE BY AV G", new: "AGGREGATE BY AVG"},
	{old: "AGGREGATE BY MA X", new: "AGGREGATE BY MAX"},
	{old: "AGGREGATE BY MI N", new: "AGGREGATE BY MIN"},
	{old: "AV G", new: "AVG"},
	{old: "MA X", new: "MAX"},
	{old: "MI N", new: "MIN"},
	{old: "TOP text 10", new: "TOP 10"},
	{old: "TOP text ", new: "TOP "},
	{old: "ME ASURE", new: "MEASURE"},
	{old: "ME AS URE", new: "MEASURE"},
	{old: "ST REAM", new: "STREAM"},
	{old: "TR ACE", new: "TRACE"},
	{old: "PROP ERTY", new: "PROPERTY"},
	{old: "SER VICE", new: "SERVICE"},
	{old: "LI MIT", new: "LIMIT"},
	{old: "GRO UP", new: "GROUP"},
	{old: "OR DER", new: "ORDER"},
	{old: "WHE RE", new: "WHERE"},
	{old: "service _", new: "service_"},
	{old: "service_end point_l atency", new: "service_endpoint_latency"},
	{old: "endpoint _", new: "endpoint_"},
	{old: "_ ", new: "_"},
	{old: " - ", new: "-"},
	{old: "text 10 text", new: "10"},
	{old: "text 100 text", new: "100"},
	{old: "text SELECT", new: "SELECT"},
}

// Validator validates a BYDBQL candidate.
type Validator interface {
	Validate(ctx context.Context, query string, schema *session.SchemaSnapshot) (session.ValidationReport, error)
}

// Runner coordinates deterministic workflow phases and agent turns.
type Runner struct {
	agentGateway agent.Gateway
	validator    Validator
	executor     tools.Executor
	now          func() time.Time
	maxRetries   int
}

// Config configures a Runner.
type Config struct {
	AgentGateway agent.Gateway
	Validator    Validator
	Executor     tools.Executor
	MaxRetries   int
}

// NewRunner creates a WorkflowRunner.
func NewRunner(config Config) *Runner {
	validator := config.Validator
	if validator == nil {
		validator = tuibysql.NewSemanticValidator()
	}
	executor := config.Executor
	if executor == nil {
		executor = tools.NewReadOnlyExecutor()
	}
	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	return &Runner{
		agentGateway: config.AgentGateway,
		validator:    validator,
		executor:     executor,
		now:          time.Now,
		maxRetries:   maxRetries,
	}
}

// StartOptions contains user-provided session slots.
type StartOptions struct {
	ResourceType   session.ResourceType
	TimeRange      session.TimeRange
	Goal           string
	ResourceName   string
	Groups         []string
	NameProvided   bool
	GroupsProvided bool
	TypeProvided   bool
}

// StartSession creates a session and discovers a schema summary.
func (runner *Runner) StartSession(ctx context.Context, options StartOptions) (*session.QuerySession, error) {
	catalog, catalogErr := runner.executor.DiscoverCatalog(ctx)
	if catalogErr != nil {
		return nil, fmt.Errorf("failed to discover schema catalog: %w", catalogErr)
	}
	resolved := ResolveSessionSlots(options, catalog)
	schemaSnapshot, schemaErr := runner.executor.DiscoverSchema(ctx, tools.SchemaRequest{
		Type:   resolved.ResourceType,
		Name:   resolved.ResourceName,
		Groups: resolved.Groups,
	})
	if schemaErr != nil {
		return nil, fmt.Errorf("failed to discover schema: %w", schemaErr)
	}
	schemaSnapshot.AvailableGroups = append([]string(nil), catalog.Groups...)
	schemaSnapshot.Catalog = append([]session.CatalogEntry(nil), catalog.Entries...)
	querySession := &session.QuerySession{
		ID:             uuid.NewString(),
		Phase:          session.PhaseIntent,
		UserGoal:       resolved.Goal,
		ResourceType:   resolved.ResourceType,
		ResourceName:   resolved.ResourceName,
		Groups:         append([]string(nil), resolved.Groups...),
		TimeRange:      resolved.TimeRange,
		SchemaSnapshot: schemaSnapshot,
		SlotsPinned:    resolved.SlotsPinned,
		AutoMatched:    resolved.AutoMatched,
	}
	querySession.AddTranscript("workflow", "created BYDBQL agent session", runner.now())
	if resolved.AutoMatched {
		querySession.AddTranscript(
			"workflow",
			fmt.Sprintf("auto-matched resource %s %s in %s from goal", resolved.ResourceType, resolved.ResourceName, strings.Join(resolved.Groups, ",")),
			runner.now(),
		)
	}
	return querySession, nil
}

// SyncSession updates session slots and refreshes schema when the TUI inputs change.
func (runner *Runner) SyncSession(ctx context.Context, querySession *session.QuerySession, options StartOptions) (*session.QuerySession, error) {
	if querySession == nil {
		return runner.StartSession(ctx, options)
	}
	if !slotsChanged(querySession, options) {
		return querySession, nil
	}
	catalog, catalogErr := runner.executor.DiscoverCatalog(ctx)
	if catalogErr != nil {
		return nil, fmt.Errorf("failed to discover schema catalog: %w", catalogErr)
	}
	resolved := ResolveSessionSlots(options, catalog)
	schemaSnapshot, schemaErr := runner.executor.DiscoverSchema(ctx, tools.SchemaRequest{
		Type:   resolved.ResourceType,
		Name:   resolved.ResourceName,
		Groups: resolved.Groups,
	})
	if schemaErr != nil {
		return nil, fmt.Errorf("failed to refresh schema: %w", schemaErr)
	}
	schemaSnapshot.AvailableGroups = append([]string(nil), catalog.Groups...)
	schemaSnapshot.Catalog = append([]session.CatalogEntry(nil), catalog.Entries...)
	querySession.UserGoal = resolved.Goal
	querySession.ResourceType = resolved.ResourceType
	querySession.ResourceName = resolved.ResourceName
	querySession.Groups = append([]string(nil), resolved.Groups...)
	querySession.TimeRange = resolved.TimeRange
	querySession.SchemaSnapshot = schemaSnapshot
	querySession.SlotsPinned = resolved.SlotsPinned
	querySession.AutoMatched = resolved.AutoMatched
	querySession.AddTranscript("workflow", "refreshed schema after slot change", runner.now())
	if resolved.AutoMatched {
		querySession.AddTranscript(
			"workflow",
			fmt.Sprintf("auto-matched resource %s %s in %s from goal", resolved.ResourceType, resolved.ResourceName, strings.Join(resolved.Groups, ",")),
			runner.now(),
		)
	}
	return querySession, nil
}

func slotsChanged(querySession *session.QuerySession, options StartOptions) bool {
	if querySession.UserGoal != strings.TrimSpace(options.Goal) {
		return true
	}
	if options.TypeProvided && querySession.ResourceType != options.ResourceType {
		return true
	}
	if options.NameProvided && querySession.ResourceName != strings.TrimSpace(options.ResourceName) {
		return true
	}
	if options.GroupsProvided && !sameGroups(querySession.Groups, normalizeGroupsIfProvided(options.Groups)) {
		return true
	}
	if querySession.TimeRange.Start != strings.TrimSpace(options.TimeRange.Start) || querySession.TimeRange.End != strings.TrimSpace(options.TimeRange.End) {
		return true
	}
	return false
}

func sameGroups(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

// ReviseWithAgent asks the configured agent to revise the current BYDBQL candidate.
func (runner *Runner) ReviseWithAgent(ctx context.Context, querySession *session.QuerySession) ([]agent.Event, error) {
	return runner.RunAgentTurn(ctx, querySession, "")
}

// RunAgentTurn runs one user-facing agent turn with an optional per-round hint.
func (runner *Runner) RunAgentTurn(ctx context.Context, querySession *session.QuerySession, turnHint string) ([]agent.Event, error) {
	if querySession == nil {
		return nil, errors.New("query session is required")
	}
	if runner.agentGateway == nil {
		return nil, errors.New("agent gateway is not configured")
	}
	querySession.Phase = session.PhaseAgentDraft
	agentSessionID := strings.TrimSpace(querySession.AgentSessionID)
	if agentSessionID == "" {
		agentSession, startErr := runner.agentGateway.Start(ctx, agent.StartRequest{
			Provider: "bydbctl-agent",
			Metadata: map[string]string{
				"query_session_id": querySession.ID,
			},
		})
		if startErr != nil {
			querySession.Phase = session.PhaseError
			return nil, fmt.Errorf("failed to start agent session: %w", startErr)
		}
		agentSessionID = agentSession.ID
		querySession.AgentSessionID = agentSessionID
	}
	trimmedTurnHint := strings.TrimSpace(turnHint)
	if trimmedTurnHint != "" {
		querySession.AddTranscript("user", trimmedTurnHint, runner.now())
	}
	templateHint := BuildTemplateQuery(
		querySession.ResourceType,
		querySession.ResourceName,
		querySession.Groups,
		querySession.TimeRange,
	)
	hints := ClassifyIntent(querySession)
	payload := agent.BuildAgentTurnRequest(querySession, hints, templateHint, trimmedTurnHint)
	turnEvents, turnErr := runner.runAgentTurn(ctx, agentSessionID, payload)
	if turnErr != nil {
		querySession.Phase = session.PhaseError
		return turnEvents, turnErr
	}
	candidate := RepairFragmentedQuery(finalCandidate(turnEvents))
	if strings.TrimSpace(candidate) == "" {
		querySession.Phase = session.PhaseValidate
		outputSummary := truncateDiagnostic(agentOutputSummary(turnEvents))
		if outputSummary == "" {
			return turnEvents, errors.New("agent returned no BYDBQL candidate and no readable output")
		}
		return turnEvents, fmt.Errorf("agent returned no BYDBQL candidate; agent output: %s", outputSummary)
	}
	validation, validationErr := runner.validator.Validate(ctx, candidate, &querySession.SchemaSnapshot)
	if validationErr != nil {
		querySession.Phase = session.PhaseError
		return turnEvents, fmt.Errorf("failed to validate agent candidate: %w", validationErr)
	}
	explanation := finalExplanation(turnEvents)
	querySession.AddCandidate(session.BydbqlCandidate{
		ID:          fmt.Sprintf("candidate-%d", len(querySession.Candidates)+1),
		Query:       candidate,
		Explanation: explanation,
		Source:      session.CandidateSourceAgent,
		CreatedAt:   runner.now(),
		Validation:  validation,
	})
	querySession.AddConversationTurn(session.ConversationTurn{
		Hint:      trimmedTurnHint,
		Response:  explanation,
		Candidate: candidate,
		CreatedAt: runner.now(),
	})
	querySession.AddTranscript("agent", explanation, runner.now())
	if validation.Valid {
		querySession.Phase = session.PhaseReady
		return turnEvents, nil
	}
	querySession.Phase = session.PhaseValidate
	return turnEvents, nil
}

// ValidateManualQuery validates an edited BYDBQL query and records it as a manual candidate.
func (runner *Runner) ValidateManualQuery(ctx context.Context, querySession *session.QuerySession, query string) error {
	if querySession == nil {
		return errors.New("query session is required")
	}
	validation, validationErr := runner.validator.Validate(ctx, query, &querySession.SchemaSnapshot)
	if validationErr != nil {
		querySession.Phase = session.PhaseError
		return fmt.Errorf("failed to validate manual query: %w", validationErr)
	}
	querySession.AddCandidate(session.BydbqlCandidate{
		ID:          fmt.Sprintf("candidate-%d", len(querySession.Candidates)+1),
		Query:       strings.TrimSpace(query),
		Explanation: "manual edit from TUI",
		Source:      session.CandidateSourceManual,
		CreatedAt:   runner.now(),
		Validation:  validation,
	})
	querySession.AddTranscript("workflow", "validated manual BYDBQL edit", runner.now())
	if validation.Valid {
		querySession.Phase = session.PhaseReady
		return nil
	}
	querySession.Phase = session.PhaseValidate
	return nil
}

// ExecuteCurrent runs the current BYDBQL candidate through the workflow-owned tool executor.
func (runner *Runner) ExecuteCurrent(ctx context.Context, querySession *session.QuerySession) error {
	if querySession == nil {
		return errors.New("query session is required")
	}
	currentCandidate := querySession.CurrentCandidate()
	if currentCandidate == nil {
		return errors.New("query candidate is required")
	}
	if !currentCandidate.Validation.Valid {
		querySession.Phase = session.PhaseValidate
		return errors.New("only a valid BYDBQL candidate can be executed")
	}
	executionResult, executeErr := runner.executor.Execute(ctx, querySession, currentCandidate.Query)
	if executeErr != nil {
		querySession.Phase = session.PhaseError
		executionResult.Error = executeErr.Error()
		if executionResult.Summary == "" {
			executionResult.Summary = executeErr.Error()
		}
		querySession.ExecutionResult = executionResult
		return fmt.Errorf("failed to execute query: %w", executeErr)
	}
	querySession.ExecutionResult = executionResult
	querySession.Phase = session.PhaseExecuted
	if executionResult.Hint != "" {
		querySession.AddTranscript("workflow", executionResult.Hint, runner.now())
	}
	querySession.AddTranscript("workflow", executionResult.Summary, runner.now())
	return nil
}

// AcceptCurrent accepts the newest valid BYDBQL candidate.
func (runner *Runner) AcceptCurrent(querySession *session.QuerySession) error {
	if querySession == nil {
		return errors.New("query session is required")
	}
	currentCandidate := querySession.CurrentCandidate()
	if currentCandidate == nil {
		return errors.New("query candidate is required")
	}
	if !currentCandidate.Validation.Valid {
		return errors.New("only a valid BYDBQL candidate can be accepted")
	}
	if strings.TrimSpace(querySession.ExecutionResult.Query) == "" {
		return errors.New("execute the BYDBQL candidate before accepting")
	}
	if strings.TrimSpace(querySession.ExecutionResult.Query) != strings.TrimSpace(currentCandidate.Query) {
		return errors.New("execute the current BYDBQL candidate before accepting")
	}
	querySession.AcceptedQuery = currentCandidate.Query
	querySession.Phase = session.PhaseAccepted
	querySession.AddTranscript("workflow", "accepted BYDBQL candidate", runner.now())
	return nil
}

// BuildTemplateQuery creates the deterministic starter query for a resource.
func BuildTemplateQuery(resourceType session.ResourceType, resourceName string, groups []string, timeRange session.TimeRange) string {
	groupExpr := strings.Join(normalizeGroups(groups), ", ")
	timeExpr := buildTimeClause(timeRange)
	switch resourceType {
	case session.ResourceTypeStream:
		return fmt.Sprintf("SELECT * FROM STREAM %s IN %s %s LIMIT %d", resourceName, groupExpr, timeExpr, defaultLimit)
	case session.ResourceTypeTrace:
		return fmt.Sprintf("SELECT * FROM TRACE %s IN %s %s LIMIT %d", resourceName, groupExpr, timeExpr, defaultLimit)
	case session.ResourceTypeProperty:
		return fmt.Sprintf("SELECT * FROM PROPERTY %s IN %s LIMIT %d", resourceName, groupExpr, defaultLimit)
	case session.ResourceTypeTopN:
		return fmt.Sprintf("SHOW TOP %d FROM MEASURE %s IN %s %s AGGREGATE BY SUM ORDER BY DESC", defaultTopN, resourceName, groupExpr, timeExpr)
	default:
		return fmt.Sprintf("SELECT * FROM MEASURE %s IN %s %s LIMIT %d", resourceName, groupExpr, timeExpr, defaultLimit)
	}
}

func (runner *Runner) runAgentTurn(ctx context.Context, agentSessionID string, payload agent.RequestPayload) ([]agent.Event, error) {
	taskPrompt := "Generate one BYDBQL query from the goal, slots, and schema in the context JSON."
	if strings.TrimSpace(payload.Candidate) != "" {
		taskPrompt = "Revise the BYDBQL candidate using validation or execution feedback in the context JSON."
	}
	events, sendErr := runner.agentGateway.Send(ctx, agentSessionID, agent.TurnRequest{
		Task:    payload.Task,
		Prompt:  taskPrompt,
		Payload: payload,
	})
	if sendErr != nil {
		return nil, fmt.Errorf("failed to send agent turn: %w", sendErr)
	}
	var collectedEvents []agent.Event
	for event := range events {
		collectedEvents = append(collectedEvents, event)
		if event.Kind == agent.EventKindError {
			if event.Err != nil {
				return collectedEvents, fmt.Errorf("agent error: %w", event.Err)
			}
			return collectedEvents, fmt.Errorf("agent error: %s", event.Message)
		}
	}
	return collectedEvents, nil
}

func inferResourceType(goal string) session.ResourceType {
	normalizedGoal := strings.ToLower(goal)
	switch {
	case strings.Contains(normalizedGoal, "trace"):
		return session.ResourceTypeTrace
	case strings.Contains(normalizedGoal, "property"):
		return session.ResourceTypeProperty
	case strings.Contains(normalizedGoal, "stream") || strings.Contains(normalizedGoal, "log"):
		return session.ResourceTypeStream
	case strings.Contains(normalizedGoal, "top"):
		return session.ResourceTypeTopN
	default:
		return session.ResourceTypeMeasure
	}
}

func normalizeGroups(groups []string) []string {
	var normalizedGroups []string
	for _, group := range groups {
		parts := strings.Split(group, ",")
		for _, part := range parts {
			trimmedPart := strings.TrimSpace(part)
			if trimmedPart != "" {
				normalizedGroups = append(normalizedGroups, trimmedPart)
			}
		}
	}
	if len(normalizedGroups) == 0 {
		return []string{defaultGroupName}
	}
	return normalizedGroups
}

func buildTimeClause(timeRange session.TimeRange) string {
	start := strings.TrimSpace(timeRange.Start)
	end := strings.TrimSpace(timeRange.End)
	if start == "" {
		start = defaultTimeStart
	}
	if end != "" {
		return fmt.Sprintf("TIME BETWEEN '%s' AND '%s'", start, end)
	}
	return fmt.Sprintf("TIME > '%s'", start)
}

func finalCandidate(events []agent.Event) string {
	for eventIdx := len(events) - 1; eventIdx >= 0; eventIdx-- {
		if candidate := cleanBydbqlCandidate(events[eventIdx].Candidate); candidate != "" {
			return candidate
		}
		if candidate := extractCandidateFromText(events[eventIdx].Message); candidate != "" {
			return candidate
		}
	}
	var messages []string
	for _, event := range events {
		if event.Kind == agent.EventKindPlanUpdate {
			continue
		}
		if strings.TrimSpace(event.Message) != "" {
			messages = append(messages, event.Message)
		}
	}
	if candidate := extractCandidateFromText(strings.Join(messages, "\n")); candidate != "" {
		return candidate
	}
	if candidate := extractCandidateFromFragmentedText(agentOutputText(events)); candidate != "" {
		return candidate
	}
	return ""
}

func agentOutputText(events []agent.Event) string {
	for eventIdx := len(events) - 1; eventIdx >= 0; eventIdx-- {
		event := events[eventIdx]
		if event.Kind == agent.EventKindFinalResponse && strings.TrimSpace(event.Message) != "" {
			return event.Message
		}
	}
	var messages []string
	for _, event := range events {
		if event.Kind == agent.EventKindPlanUpdate {
			continue
		}
		if strings.TrimSpace(event.Message) != "" {
			messages = append(messages, event.Message)
		}
	}
	return strings.Join(messages, "\n")
}

func finalExplanation(events []agent.Event) string {
	for eventIdx := len(events) - 1; eventIdx >= 0; eventIdx-- {
		event := events[eventIdx]
		if strings.TrimSpace(event.Explanation) != "" {
			return strings.TrimSpace(event.Explanation)
		}
		if strings.TrimSpace(event.Message) != "" {
			return strings.TrimSpace(event.Message)
		}
	}
	return "agent returned a BYDBQL candidate"
}

func agentOutputSummary(events []agent.Event) string {
	var outputParts []string
	for _, event := range events {
		if strings.TrimSpace(event.Candidate) != "" {
			outputParts = append(outputParts, "candidate="+singleLine(event.Candidate))
		}
		if strings.TrimSpace(event.Message) != "" {
			outputParts = append(outputParts, singleLine(event.Message))
		}
		if strings.TrimSpace(event.Explanation) != "" {
			outputParts = append(outputParts, singleLine(event.Explanation))
		}
	}
	return strings.TrimSpace(strings.Join(outputParts, " | "))
}

func truncateDiagnostic(value string) string {
	trimmedValue := strings.TrimSpace(value)
	if len(trimmedValue) <= maxDiagnosticLength {
		return trimmedValue
	}
	return trimmedValue[:maxDiagnosticLength] + "..."
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func extractCandidateFromText(text string) string {
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return ""
	}
	if jsonCandidate := extractJSONCandidate(trimmedText); jsonCandidate != "" {
		return jsonCandidate
	}
	if fencedCandidate := extractFencedCandidate(trimmedText); fencedCandidate != "" {
		return fencedCandidate
	}
	return firstBydbqlStatement(trimmedText)
}

func extractCandidateFromFragmentedText(text string) string {
	normalizedText := normalizeFragmentedAgentText(text)
	if normalizedText == strings.TrimSpace(text) {
		return ""
	}
	return extractCandidateFromText(normalizedText)
}

// RepairFragmentedQuery normalizes ACP fragmented BYDBQL text into a single statement.
func RepairFragmentedQuery(query string) string {
	normalizedQuery := normalizeFragmentedAgentText(query)
	if normalizedQuery == "" {
		return strings.TrimSpace(query)
	}
	return normalizedQuery
}

func normalizeFragmentedAgentText(text string) string {
	normalizedText := singleLine(text)
	normalizedText = strings.ReplaceAll(normalizedText, "` ` `", "```")
	normalizedText = strings.ReplaceAll(normalizedText, "`` `", "```")
	normalizedText = strings.ReplaceAll(normalizedText, "` ``", "```")
	normalizedText = collapseIdentifierFragments(normalizedText)
	for _, replacement := range fragmentedTokenReplacements {
		normalizedText = strings.ReplaceAll(normalizedText, replacement.old, replacement.new)
	}
	normalizedText = strings.ReplaceAll(normalizedText, " ,", ",")
	normalizedText = strings.ReplaceAll(normalizedText, " .", ".")
	normalizedText = strings.ReplaceAll(normalizedText, "( ", "(")
	normalizedText = strings.ReplaceAll(normalizedText, " )", ")")
	normalizedText = strings.ReplaceAll(normalizedText, " text ", " ")
	normalizedText = fragmentedTimeRangePattern.ReplaceAllString(normalizedText, "'-${1}m'")
	normalizedText = strings.ReplaceAll(normalizedText, ">'", "> '")
	normalizedText = strings.ReplaceAll(normalizedText, "<'", "< '")
	return strings.TrimSpace(normalizedText)
}

func collapseIdentifierFragments(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	collapsedFields := make([]string, 0, len(fields))
	for fieldIdx := 0; fieldIdx < len(fields); fieldIdx++ {
		currentField := fields[fieldIdx]
		if fieldIdx+1 < len(fields) && shouldJoinIdentifierFragment(currentField, fields[fieldIdx+1]) {
			collapsedFields = append(collapsedFields, currentField+fields[fieldIdx+1])
			fieldIdx++
			continue
		}
		collapsedFields = append(collapsedFields, currentField)
	}
	return strings.Join(collapsedFields, " ")
}

func shouldJoinIdentifierFragment(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	if strings.HasSuffix(left, "_") || strings.HasPrefix(right, "_") {
		return true
	}
	if len(right) == 1 && isUpperAlpha(right) {
		switch left + right {
		case "AVG", "MAX", "MIN":
			return true
		}
	}
	return isLowerAlpha(left) && isLowerAlpha(right) && len(left) <= 4 && len(right) <= 12
}

func isUpperAlpha(value string) bool {
	for _, valueRune := range value {
		if valueRune < 'A' || valueRune > 'Z' {
			return false
		}
	}
	return value != ""
}

func isLowerAlpha(value string) bool {
	for _, valueRune := range value {
		if valueRune < 'a' || valueRune > 'z' {
			return false
		}
	}
	return true
}

func extractJSONCandidate(text string) string {
	var rawValue any
	if unmarshalErr := json.Unmarshal([]byte(text), &rawValue); unmarshalErr != nil {
		return ""
	}
	return candidateFromJSON(rawValue)
}

func candidateFromJSON(value any) string {
	switch typedValue := value.(type) {
	case map[string]any:
		for _, key := range []string{"candidate", "bydbql", "BydbQL", "query", "final"} {
			if candidateText, ok := typedValue[key].(string); ok {
				if candidate := cleanBydbqlCandidate(candidateText); candidate != "" {
					return candidate
				}
			}
		}
		for _, key := range []string{"result", "data", "params"} {
			if candidate := candidateFromJSON(typedValue[key]); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, item := range typedValue {
			if candidate := candidateFromJSON(item); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func extractFencedCandidate(text string) string {
	parts := strings.Split(text, "```")
	for partIdx := 1; partIdx+1 < len(parts); partIdx += 2 {
		part := strings.TrimSpace(parts[partIdx])
		lines := strings.Split(part, "\n")
		if len(lines) > 1 && !looksLikeBydbql(lines[0]) {
			part = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
		if candidate := cleanBydbqlCandidate(part); candidate != "" {
			return candidate
		}
	}
	return ""
}

func firstBydbqlStatement(text string) string {
	lines := strings.Split(text, "\n")
	for lineIdx, line := range lines {
		trimmedLine := trimToBydbqlStart(line)
		if !looksLikeBydbql(trimmedLine) {
			continue
		}
		statementLines := []string{trimmedLine}
		for nextLineIdx := lineIdx + 1; nextLineIdx < len(lines); nextLineIdx++ {
			nextLine := strings.TrimSpace(lines[nextLineIdx])
			if nextLine == "" {
				break
			}
			if !isLikelyBydbqlContinuation(nextLine) {
				break
			}
			statementLines = append(statementLines, nextLine)
		}
		return cleanBydbqlCandidate(strings.Join(statementLines, "\n"))
	}
	return ""
}

func trimToBydbqlStart(line string) string {
	trimmedLine := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
	upperLine := strings.ToUpper(trimmedLine)
	selectIdx := strings.Index(upperLine, "SELECT ")
	topNIdx := strings.Index(upperLine, "SHOW TOP ")
	switch {
	case selectIdx >= 0 && topNIdx >= 0 && selectIdx < topNIdx:
		return strings.TrimSpace(trimmedLine[selectIdx:])
	case selectIdx >= 0 && topNIdx >= 0:
		return strings.TrimSpace(trimmedLine[topNIdx:])
	case selectIdx >= 0:
		return strings.TrimSpace(trimmedLine[selectIdx:])
	case topNIdx >= 0:
		return strings.TrimSpace(trimmedLine[topNIdx:])
	default:
		return trimmedLine
	}
}

func isLikelyBydbqlContinuation(line string) bool {
	trimmedLine := strings.TrimSpace(line)
	if strings.HasPrefix(trimmedLine, "```") {
		return false
	}
	lowerLine := strings.ToLower(trimmedLine)
	for _, prefix := range []string{"explanation:", "note:", "because ", "this query ", "the query "} {
		if strings.HasPrefix(lowerLine, prefix) {
			return false
		}
	}
	return true
}

func cleanBydbqlCandidate(text string) string {
	candidate := strings.TrimSpace(text)
	if candidate == "" {
		return ""
	}
	if semicolonIdx := strings.Index(candidate, ";"); semicolonIdx >= 0 {
		candidate = candidate[:semicolonIdx]
	}
	candidate = strings.TrimSpace(strings.TrimSuffix(candidate, "```"))
	if !looksLikeBydbql(candidate) {
		return ""
	}
	upperCandidate := strings.ToUpper(candidate)
	if strings.HasPrefix(upperCandidate, "SELECT ") && !strings.Contains(upperCandidate, " FROM ") && !strings.Contains(upperCandidate, "\nFROM ") {
		return ""
	}
	return candidate
}

func looksLikeBydbql(text string) bool {
	upperText := strings.ToUpper(strings.TrimSpace(text))
	return strings.HasPrefix(upperText, "SELECT ") || strings.HasPrefix(upperText, "SHOW TOP ")
}
