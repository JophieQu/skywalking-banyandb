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

// Package app implements the Bubble Tea user interface for bydbctl agent.
package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent/fake"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/applog"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/tools"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/workflow"
)

const (
	defaultWidth  = 120
	defaultHeight = 36
)

const (
	focusCatalog = iota
	focusCatalogFilter
	focusGoal
	focusTurnHint
	focusResourceName
	focusGroups
	focusStart
	focusEnd
	focusQuery
	focusActivity
	focusCount
)

// Config configures the TUI model.
type Config struct {
	AgentGateway agent.Gateway
	Executor     tools.Executor
	SessionLog   *applog.Logger
	LogDir       string
	Provider     string
	Goal         string
	ResourceType string
	ResourceName string
	Groups       string
	Start           string
	End             string
	MaxRetries      int
	NameProvided    bool
	GroupsProvided  bool
	TypeProvided    bool
}

// Model is the Bubble Tea state for the bydbctl agent TUI.
type Model struct {
	runner         *workflow.Runner
	executor       tools.Executor
	querySession   *session.QuerySession
	catalog        catalogBrowser
	selectedSchema session.SchemaSnapshot
	catalogFilter  textinput.Model
	goal           textarea.Model
	turnHint       textarea.Model
	query          textarea.Model
	resourceName   textinput.Model
	groups         textinput.Model
	start          textinput.Model
	end            textinput.Model
	resourceType   session.ResourceType
	provider         string
	status           string
	events           []string
	sessionLog       *applog.Logger
	logPathDisplay   string
	width            int
	height           int
	catalogHeight    int
	activeTab        appTab
	activityLog      []activityEntry
	activityScroll   int
	activityCursor   int
	detailScroll     int
	focus            int
	busy             bool
	typePinned       bool
	namePinned       bool
	groupsPinned     bool
}

// NewModel creates a TUI model with the configured agent gateway.
func NewModel(config Config) Model {
	agentGateway := config.AgentGateway
	if agentGateway == nil {
		agentGateway = fake.NewGateway()
	}
	provider := strings.TrimSpace(config.Provider)
	if provider == "" {
		provider = "fake"
	}
	catalogFilter := newTextInput("", "filter groups/resources")
	catalogFilter.Width = 24
	goal := textarea.New()
	goal.Placeholder = "Overall question (set once, e.g. top 10 slow endpoints in last 30m)"
	goal.ShowLineNumbers = false
	goal.SetHeight(3)
	goal.SetValue(config.Goal)
	turnHint := textarea.New()
	turnHint.Placeholder = "This round: extra hint for agent (Ctrl+A). e.g. use sw_metrics, aggregate by AVG"
	turnHint.ShowLineNumbers = false
	turnHint.SetHeight(3)
	query := textarea.New()
	query.Placeholder = "BYDBQL candidate"
	query.ShowLineNumbers = false
	query.SetHeight(10)
	resourceName := newTextInput(config.ResourceName, "resource name")
	groups := newTextInput(config.Groups, "group, or group1,group2")
	start := newTextInput(config.Start, "time start, for example -30m")
	end := newTextInput(config.End, "optional time end")
	sessionLog := config.SessionLog
	if sessionLog == nil {
		createdLog, createErr := applog.New(config.LogDir)
		if createErr == nil {
			sessionLog = createdLog
		}
	}
	model := Model{
		runner: workflow.NewRunner(workflow.Config{
			AgentGateway: agentGateway,
			Executor:     config.Executor,
			MaxRetries:   config.MaxRetries,
		}),
		executor:     config.Executor,
		catalog:      newCatalogBrowser(),
		catalogFilter: catalogFilter,
		goal:         goal,
		turnHint:     turnHint,
		query:        query,
		resourceName: resourceName,
		groups:       groups,
		start:        start,
		end:          end,
		resourceType: session.NormalizeResourceType(config.ResourceType),
		provider:       provider,
		status:         "ready",
		sessionLog:     sessionLog,
		logPathDisplay: applog.DisplayPath(sessionLogPath(sessionLog)),
		width:          defaultWidth,
		height:         defaultHeight,
		typePinned:     config.TypeProvided,
		namePinned:     config.NameProvided,
		groupsPinned:   config.GroupsProvided,
		activeTab:      tabQuery,
	}
	if sessionLog != nil {
		sessionLog.Write("session", fmt.Sprintf("provider=%s addr=workflow", provider))
	}
	model.addEvent("ready: browse Schema Browser, set Goal, Ctrl+A")
	model.resize(defaultWidth, defaultHeight)
	model.syncFocus()
	return model
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadCatalogCmd())
}

// Update implements tea.Model.
func (m Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch typedMsg := teaMsg.(type) {
	case tea.WindowSizeMsg:
		m.resize(typedMsg.Width, typedMsg.Height)
		return m, nil
	case tea.KeyMsg:
		command, handled := m.handleKey(typedMsg)
		if handled {
			return m, command
		}
	case catalogMsg:
		m.busy = false
		if typedMsg.loadErr != nil {
			m.catalog.setLoadError(typedMsg.loadErr.Error())
			m.status = "catalog load failed"
			m.addUIEvent("catalog: " + typedMsg.loadErr.Error())
			m.logWriteError("catalog", typedMsg.loadErr)
			return m, nil
		}
		m.catalog.setCatalog(typedMsg.catalog)
		m.status = fmt.Sprintf("catalog loaded: %d resources in %d groups", len(typedMsg.catalog.Entries), len(typedMsg.catalog.Groups))
		m.addUIEvent(m.status)
		m.logWrite("catalog", m.status)
		return m, nil
	case schemaDetailMsg:
		if typedMsg.loadErr != nil {
			m.addUIEvent("schema detail: " + typedMsg.loadErr.Error())
			m.logWriteError("schema", typedMsg.loadErr)
			return m, nil
		}
		m.selectedSchema = typedMsg.snapshot
		if typedMsg.snapshot.Loaded {
			m.detailScroll = 0
		}
		return m, nil
	case workflowMsg:
		m.busy = false
		if typedMsg.querySession != nil {
			m.querySession = typedMsg.querySession
			if currentCandidate := m.querySession.CurrentCandidate(); currentCandidate != nil {
				m.query.SetValue(currentCandidate.Query)
			}
			if m.querySession.AutoMatched {
				m.resourceName.SetValue(m.querySession.ResourceName)
				m.groups.SetValue(strings.Join(m.querySession.Groups, ","))
				m.resourceType = m.querySession.ResourceType
			}
			if strings.TrimSpace(m.querySession.SchemaSnapshot.Name) != "" {
				m.selectedSchema = m.querySession.SchemaSnapshot
			}
		}
		m.addAgentEvents(typedMsg.events)
		m.recordAgentActivities(typedMsg.events)
		if typedMsg.clearTurnHint {
			m.turnHint.SetValue("")
		}
		if m.querySession != nil {
			if validationHint := formatValidationHint(m.querySession.Validation.Message); validationHint != "" {
				m.addUIEvent(validationHint)
				m.logWrite("validation", m.querySession.Validation.Message)
			}
			if currentCandidate := m.querySession.CurrentCandidate(); currentCandidate != nil && strings.TrimSpace(currentCandidate.Query) != "" {
				if !m.querySession.Validation.Valid {
					if invalidHint := formatInvalidCandidateHint(currentCandidate.Query); invalidHint != "" {
						m.addUIEvent(invalidHint)
					}
					m.logWrite("candidate", currentCandidate.Query)
				}
			}
			m.logQuerySession(m.querySession)
			if typedMsg.status == "execution complete" {
				m.recordExecutionActivity(m.querySession)
				m.switchTab(tabRun)
			}
		}
		if typedMsg.err != nil {
			m.status = typedMsg.err.Error()
			m.addUIEvent(summarizeError("error", typedMsg.err.Error()))
			m.logWriteError("workflow", typedMsg.err)
			return m, nil
		}
		if typedMsg.status != "" {
			m.addUIEvent(summarizeStatusEvent(typedMsg.status))
			m.logWrite("workflow", typedMsg.status)
			m.status = typedMsg.status
		} else if m.querySession != nil && !m.querySession.Validation.Valid && m.querySession.CurrentCandidate() != nil {
			m.status = "invalid candidate — add Turn hint and press Ctrl+A"
			m.addUIEvent("validation: add Turn hint and press Ctrl+A to refine")
		}
		if typedMsg.switchRunTab {
			m.switchTab(tabRun)
		}
		return m, nil
	}
	inputCmd := m.updateFocused(teaMsg)
	return m, inputCmd
}

// View implements tea.Model.
func (m Model) View() string {
	contentWidth := clamp(m.width-4, 100, 200)
	bodyHeight := clamp(m.height-8, 18, 40)
	tabBar := m.renderTabBar(contentWidth)
	var body string
	switch m.activeTab {
	case tabSchema:
		body = m.renderSchemaTab(contentWidth, bodyHeight)
	case tabRun:
		body = m.renderRunTab(contentWidth, bodyHeight)
	default:
		body = m.renderQueryTab(contentWidth, bodyHeight)
	}
	return lipgloss.JoinVertical(lipgloss.Left, tabBar, body, m.renderFooter(contentWidth))
}

type catalogMsg struct {
	catalog session.SchemaCatalog
	loadErr error
}

type schemaDetailMsg struct {
	snapshot session.SchemaSnapshot
	loadErr  error
}

type workflowMsg struct {
	querySession  *session.QuerySession
	events        []agent.Event
	err           error
	status        string
	clearTurnHint bool
	switchRunTab  bool
}

func newTextInput(value, placeholder string) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.SetValue(value)
	input.Prompt = ""
	input.Width = 24
	return input
}

func (m *Model) handleKey(keyMsg tea.KeyMsg) (tea.Cmd, bool) {
	switch keyMsg.String() {
	case "ctrl+c", "esc":
		return tea.Quit, true
	case "f1":
		m.switchTab(tabSchema)
		return m.syncFocus(), true
	case "f2":
		m.switchTab(tabQuery)
		return m.syncFocus(), true
	case "f3":
		m.switchTab(tabRun)
		return m.syncFocus(), true
	case "tab":
		m.cycleFocus(1)
		return m.syncFocus(), true
	case "shift+tab":
		m.cycleFocus(-1)
		return m.syncFocus(), true
	case "ctrl+r":
		m.resourceType = nextResourceType(m.resourceType)
		m.typePinned = true
		return nil, true
	case "ctrl+l":
		if m.busy {
			return nil, true
		}
		m.busy = true
		m.catalog.setLoading()
		m.status = "refreshing catalog"
		return m.loadCatalogCmd(), true
	case "/":
		if m.activeTab == tabSchema && (m.focus == focusCatalog || m.focus == focusCatalogFilter) {
			m.catalog.cycleTypeFilter()
			return nil, true
		}
		return nil, false
	case "[", "]":
		if m.isTypingFocus() {
			return nil, false
		}
		delta := -1
		if keyMsg.String() == "]" {
			delta = 1
		}
		m.cycleTab(delta)
		return m.syncFocus(), true
	case "up", "down":
		if m.activeTab == tabSchema {
			if m.focus == focusCatalog {
				delta := 1
				if keyMsg.String() == "up" {
					delta = -1
				}
				m.catalog.moveCursor(delta, m.catalogListHeight())
				return m.loadSchemaDetailCmdForCursor(), true
			}
			delta := 1
			if keyMsg.String() == "up" {
				delta = -1
			}
			m.scrollSchemaDetail(delta, m.schemaDetailHeight())
			return nil, true
		}
		if m.activeTab == tabRun && m.focus == focusActivity {
			delta := 1
			if keyMsg.String() == "up" {
				delta = -1
			}
			m.moveActivityCursor(delta, 8)
			return nil, true
		}
		return nil, false
	case "pgup", "pgdown":
		if m.activeTab == tabRun && m.focus == focusActivity {
			delta := 8
			if keyMsg.String() == "pgup" {
				delta = -8
			}
			m.moveActivityCursor(delta, 8)
			return nil, true
		}
		return nil, false
	case "enter":
		if m.focus == focusCatalog {
			return m.selectCatalogEntry(), true
		}
		return nil, false
	case "ctrl+a":
		if m.busy {
			return nil, true
		}
		if strings.TrimSpace(m.goal.Value()) == "" {
			m.addUIEvent("goal required before asking agent")
			return nil, true
		}
		m.busy = true
		m.status = "asking agent"
		turnHintValue := strings.TrimSpace(m.turnHint.Value())
		m.logWrite("action", fmt.Sprintf("ctrl+a agent turn hint=%q", turnHintValue))
		return m.agentCmd(turnHintValue), true
	case "ctrl+v":
		if m.busy {
			return nil, true
		}
		m.busy = true
		m.status = "validating query"
		m.logWrite("action", "ctrl+v validate query")
		return m.validateCmd(), true
	case "ctrl+e":
		if m.busy {
			return nil, true
		}
		m.busy = true
		m.status = "executing query"
		m.logWrite("action", "ctrl+e execute query")
		return m.executeCmd(), true
	case "ctrl+x":
		if m.busy {
			return nil, true
		}
		m.busy = true
		m.status = "accepting query"
		m.logWrite("action", "ctrl+x accept query")
		return m.acceptCmd(), true
	default:
		return nil, false
	}
}

func (m *Model) syncFocus() tea.Cmd {
	m.goal.Blur()
	m.turnHint.Blur()
	m.catalogFilter.Blur()
	m.resourceName.Blur()
	m.groups.Blur()
	m.start.Blur()
	m.end.Blur()
	m.query.Blur()
	switch m.focus {
	case focusCatalog:
		return nil
	case focusCatalogFilter:
		return m.catalogFilter.Focus()
	case focusGoal:
		return m.goal.Focus()
	case focusTurnHint:
		return m.turnHint.Focus()
	case focusResourceName:
		return m.resourceName.Focus()
	case focusGroups:
		return m.groups.Focus()
	case focusStart:
		return m.start.Focus()
	case focusEnd:
		return m.end.Focus()
	case focusQuery:
		return m.query.Focus()
	case focusActivity:
		return nil
	default:
		return nil
	}
}

func (m *Model) updateFocused(teaMsg tea.Msg) tea.Cmd {
	var updateCmd tea.Cmd
	switch m.focus {
	case focusCatalog:
		return nil
	case focusCatalogFilter:
		m.catalogFilter, updateCmd = m.catalogFilter.Update(teaMsg)
		if strings.TrimSpace(m.catalogFilter.Value()) != m.catalog.filter {
			m.catalog.setFilter(m.catalogFilter.Value())
		}
	case focusGoal:
		m.goal, updateCmd = m.goal.Update(teaMsg)
	case focusTurnHint:
		m.turnHint, updateCmd = m.turnHint.Update(teaMsg)
	case focusResourceName:
		m.resourceName, updateCmd = m.resourceName.Update(teaMsg)
	case focusGroups:
		m.groups, updateCmd = m.groups.Update(teaMsg)
	case focusStart:
		m.start, updateCmd = m.start.Update(teaMsg)
	case focusEnd:
		m.end, updateCmd = m.end.Update(teaMsg)
	case focusQuery:
		m.query, updateCmd = m.query.Update(teaMsg)
	case focusActivity:
		return nil
	}
	return updateCmd
}

func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height
	contentWidth := clamp(width-4, 100, 200)
	catalogWidth := clamp(contentWidth*28/100, 28, 44)
	rightWidth := contentWidth - catalogWidth - 4
	middleWidth := rightWidth * 46 / 100
	queryWidth := rightWidth - middleWidth - 2
	inputWidth := maxInt(18, middleWidth-18)
	m.catalogHeight = clamp(height-6, 20, 40)
	m.catalogFilter.Width = maxInt(12, catalogWidth-8)
	m.goal.SetWidth(middleWidth - 4)
	m.turnHint.SetWidth(middleWidth - 4)
	m.query.SetWidth(queryWidth - 4)
	m.resourceName.Width = inputWidth
	m.groups.Width = inputWidth
	m.start.Width = inputWidth
	m.end.Width = inputWidth
	queryHeight := clamp(height-18, 10, 22)
	m.query.SetHeight(queryHeight)
	m.goal.SetHeight(3)
	m.turnHint.SetHeight(2)
}

func (m Model) agentCmd(turnHintValue string) tea.Cmd {
	runner := m.runner
	options := m.startOptions()
	query := m.query.Value()
	querySession := m.querySession
	return func() tea.Msg {
		updatedSession, ensureErr := ensureSession(context.Background(), runner, querySession, options, query)
		if ensureErr != nil {
			return workflowMsg{err: ensureErr}
		}
		events, turnErr := runner.RunAgentTurn(context.Background(), updatedSession, turnHintValue)
		if turnErr != nil {
			return workflowMsg{
				querySession: updatedSession,
				events:       events,
				err:          turnErr,
			}
		}
		status := "agent turn complete"
		if currentCandidate := updatedSession.CurrentCandidate(); currentCandidate != nil && currentCandidate.Validation.Valid {
			status = "valid candidate ready — Ctrl+V/E/X"
		}
		return workflowMsg{
			querySession:  updatedSession,
			events:        events,
			status:        status,
			clearTurnHint: true,
			switchRunTab:  true,
		}
	}
}

func (m Model) validateCmd() tea.Cmd {
	runner := m.runner
	options := m.startOptions()
	query := m.query.Value()
	querySession := m.querySession
	return func() tea.Msg {
		updatedSession, ensureErr := ensureSession(context.Background(), runner, querySession, options, query)
		if ensureErr != nil {
			return workflowMsg{err: ensureErr}
		}
		if strings.TrimSpace(query) == "" {
			if currentCandidate := updatedSession.CurrentCandidate(); currentCandidate != nil {
				query = currentCandidate.Query
			}
		}
		if validateErr := runner.ValidateManualQuery(context.Background(), updatedSession, query); validateErr != nil {
			return workflowMsg{
				querySession: updatedSession,
				err:          validateErr,
			}
		}
		return workflowMsg{
			querySession: updatedSession,
			status:       "validation complete",
		}
	}
}

func (m Model) executeCmd() tea.Cmd {
	runner := m.runner
	options := m.startOptions()
	query := m.query.Value()
	querySession := m.querySession
	return func() tea.Msg {
		updatedSession, ensureErr := ensureSession(context.Background(), runner, querySession, options, query)
		if ensureErr != nil {
			return workflowMsg{err: ensureErr}
		}
		if executeErr := runner.ExecuteCurrent(context.Background(), updatedSession); executeErr != nil {
			return workflowMsg{
				querySession: updatedSession,
				err:          executeErr,
			}
		}
		return workflowMsg{
			querySession: updatedSession,
			status:       "execution complete",
		}
	}
}

func (m Model) acceptCmd() tea.Cmd {
	runner := m.runner
	options := m.startOptions()
	query := m.query.Value()
	querySession := m.querySession
	return func() tea.Msg {
		updatedSession, ensureErr := ensureSession(context.Background(), runner, querySession, options, query)
		if ensureErr != nil {
			return workflowMsg{err: ensureErr}
		}
		if acceptErr := runner.AcceptCurrent(updatedSession); acceptErr != nil {
			return workflowMsg{
				querySession: updatedSession,
				err:          acceptErr,
			}
		}
		return workflowMsg{
			querySession: updatedSession,
			status:       "query accepted",
		}
	}
}

func (m Model) startOptions() workflow.StartOptions {
	return workflow.StartOptions{
		ResourceType: m.resourceType,
		TimeRange: session.TimeRange{
			Start: m.start.Value(),
			End:   m.end.Value(),
		},
		Goal:           m.goal.Value(),
		ResourceName:   m.resourceName.Value(),
		Groups:         []string{m.groups.Value()},
		NameProvided:   m.namePinned || strings.TrimSpace(m.resourceName.Value()) != "",
		GroupsProvided: m.groupsPinned || strings.TrimSpace(m.groups.Value()) != "",
		TypeProvided:   m.typePinned,
	}
}

func (m Model) loadCatalogCmd() tea.Cmd {
	executor := m.executor
	return func() tea.Msg {
		if executor == nil {
			return catalogMsg{loadErr: fmt.Errorf("schema executor is not configured")}
		}
		catalog, catalogErr := executor.DiscoverCatalog(context.Background())
		if catalogErr != nil {
			return catalogMsg{loadErr: catalogErr}
		}
		return catalogMsg{catalog: catalog}
	}
}

func (m *Model) selectCatalogEntry() tea.Cmd {
	entry, ok := m.catalog.selectedEntry()
	if !ok {
		return nil
	}
	m.applyCatalogEntry(entry)
	m.detailScroll = 0
	m.addUIEvent(fmt.Sprintf("selected %s %s in %s", entry.Type, entry.Name, entry.Group))
	m.logWrite("catalog", fmt.Sprintf("selected %s/%s group=%s", entry.Type, entry.Name, entry.Group))
	return m.loadSchemaDetailCmd(entry)
}

func (m *Model) applyCatalogEntry(entry session.CatalogEntry) {
	m.resourceType = entry.Type
	m.resourceName.SetValue(entry.Name)
	m.groups.SetValue(entry.Group)
	m.typePinned = true
	m.namePinned = true
	m.groupsPinned = true
}

func (m Model) loadSchemaDetailCmd(entry session.CatalogEntry) tea.Cmd {
	executor := m.executor
	return func() tea.Msg {
		if executor == nil {
			return schemaDetailMsg{loadErr: fmt.Errorf("schema executor is not configured")}
		}
		snapshot, schemaErr := executor.DiscoverSchema(context.Background(), tools.SchemaRequest{
			Type:   entry.Type,
			Name:   entry.Name,
			Groups: []string{entry.Group},
		})
		if schemaErr != nil {
			return schemaDetailMsg{loadErr: schemaErr}
		}
		return schemaDetailMsg{snapshot: snapshot}
	}
}

func (m Model) catalogListHeight() int {
	return clamp(m.catalogHeight-16, 8, 22)
}

func (m Model) schemaDetailHeight() int {
	return clamp(m.height-14, 10, 28)
}

func (m *Model) moveActivityCursor(delta, viewportHeight int) {
	if len(m.activityLog) == 0 {
		m.activityCursor = 0
		m.activityScroll = 0
		return
	}
	m.activityCursor += delta
	if m.activityCursor < 0 {
		m.activityCursor = 0
	}
	if m.activityCursor >= len(m.activityLog) {
		m.activityCursor = len(m.activityLog) - 1
	}
	if m.activityCursor < m.activityScroll {
		m.activityScroll = m.activityCursor
	}
	if m.activityCursor >= m.activityScroll+viewportHeight {
		m.activityScroll = m.activityCursor - viewportHeight + 1
	}
}

func (m Model) loadSchemaDetailCmdForCursor() tea.Cmd {
	entry, ok := m.catalog.selectedEntry()
	if !ok {
		return nil
	}
	return m.loadSchemaDetailCmd(entry)
}

func ensureSession(
	ctx context.Context,
	runner *workflow.Runner,
	querySession *session.QuerySession,
	options workflow.StartOptions,
	query string,
) (*session.QuerySession, error) {
	updatedSession := querySession
	if updatedSession == nil {
		var startErr error
		updatedSession, startErr = runner.StartSession(ctx, options)
		if startErr != nil {
			return nil, startErr
		}
	} else {
		var syncErr error
		updatedSession, syncErr = runner.SyncSession(ctx, updatedSession, options)
		if syncErr != nil {
			return nil, syncErr
		}
	}
	currentCandidate := updatedSession.CurrentCandidate()
	if strings.TrimSpace(query) != "" && (currentCandidate == nil || strings.TrimSpace(currentCandidate.Query) != strings.TrimSpace(query)) {
		if validateErr := runner.ValidateManualQuery(ctx, updatedSession, query); validateErr != nil {
			return nil, validateErr
		}
	}
	return updatedSession, nil
}

func nextResourceType(resourceType session.ResourceType) session.ResourceType {
	switch resourceType {
	case session.ResourceTypeMeasure:
		return session.ResourceTypeStream
	case session.ResourceTypeStream:
		return session.ResourceTypeTrace
	case session.ResourceTypeTrace:
		return session.ResourceTypeProperty
	case session.ResourceTypeProperty:
		return session.ResourceTypeTopN
	default:
		return session.ResourceTypeMeasure
	}
}

func (m *Model) addAgentEvents(events []agent.Event) {
	for _, event := range events {
		if uiEvent := summarizeAgentEvent(event); shouldShowAgentEvent(event) && uiEvent != "" {
			m.addUIEvent(uiEvent)
		}
	}
	m.logAgentTurn(events)
}

func shouldShowAgentEvent(event agent.Event) bool {
	switch event.Kind {
	case agent.EventKindMessageDelta:
		return false
	default:
		return true
	}
}

func (m *Model) addEvent(event string) {
	m.addUIEvent(summarizeStatusEvent(event))
	m.logWrite("event", event)
}

func (m *Model) addUIEvent(event string) {
	if strings.TrimSpace(event) == "" {
		return
	}
	m.events = append(m.events, event)
	m.recordActivity("workflow", event, "")
	if len(m.events) > maxVisibleEvents {
		m.events = m.events[len(m.events)-maxVisibleEvents:]
	}
}

func (m *Model) logWrite(category, message string) {
	if m.sessionLog == nil {
		return
	}
	m.sessionLog.Write(category, message)
}

func (m *Model) logWriteError(category string, err error) {
	if m.sessionLog == nil {
		return
	}
	m.sessionLog.WriteError(category, err)
}

func (m *Model) logAgentTurn(events []agent.Event) {
	if m.sessionLog == nil {
		return
	}
	m.sessionLog.WriteAgentTurn(events)
}

func (m *Model) logQuerySession(querySession *session.QuerySession) {
	if m.sessionLog == nil {
		return
	}
	m.sessionLog.WriteQuerySession(querySession)
}

func sessionLogPath(sessionLog *applog.Logger) string {
	if sessionLog == nil {
		return ""
	}
	return sessionLog.Path()
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
