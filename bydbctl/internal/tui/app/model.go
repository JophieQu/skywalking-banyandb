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
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/tools"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/workflow"
)

const (
	defaultWidth  = 120
	defaultHeight = 36
)

const (
	focusGoal = iota
	focusResourceName
	focusGroups
	focusStart
	focusEnd
	focusQuery
	focusCount
)

// Config configures the TUI model.
type Config struct {
	AgentGateway agent.Gateway
	Executor     tools.Executor
	Provider     string
	Goal         string
	ResourceType string
	ResourceName string
	Groups       string
	Start        string
	End          string
	MaxRetries   int
}

// Model is the Bubble Tea state for the bydbctl agent TUI.
type Model struct {
	runner       *workflow.Runner
	querySession *session.QuerySession
	goal         textarea.Model
	query        textarea.Model
	resourceName textinput.Model
	groups       textinput.Model
	start        textinput.Model
	end          textinput.Model
	resourceType session.ResourceType
	provider     string
	status       string
	events       []string
	width        int
	height       int
	focus        int
	busy         bool
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
	goal := textarea.New()
	goal.Placeholder = "Describe the BanyanDB question to answer"
	goal.ShowLineNumbers = false
	goal.SetHeight(4)
	goal.SetValue(config.Goal)
	query := textarea.New()
	query.Placeholder = "BYDBQL candidate"
	query.ShowLineNumbers = false
	query.SetHeight(10)
	resourceName := newTextInput(config.ResourceName, "resource name")
	groups := newTextInput(config.Groups, "group, or group1,group2")
	start := newTextInput(config.Start, "time start, for example -30m")
	end := newTextInput(config.End, "optional time end")
	model := Model{
		runner: workflow.NewRunner(workflow.Config{
			AgentGateway: agentGateway,
			Executor:     config.Executor,
			MaxRetries:   config.MaxRetries,
		}),
		goal:         goal,
		query:        query,
		resourceName: resourceName,
		groups:       groups,
		start:        start,
		end:          end,
		resourceType: session.NormalizeResourceType(config.ResourceType),
		provider:     provider,
		status:       "ready",
		width:        defaultWidth,
		height:       defaultHeight,
	}
	model.resize(defaultWidth, defaultHeight)
	model.syncFocus()
	return model
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
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
	case workflowMsg:
		m.busy = false
		if typedMsg.querySession != nil {
			m.querySession = typedMsg.querySession
			if currentCandidate := m.querySession.CurrentCandidate(); currentCandidate != nil {
				m.query.SetValue(currentCandidate.Query)
			}
		}
		m.addAgentEvents(typedMsg.events)
		if m.querySession != nil && !m.querySession.Validation.Valid && strings.TrimSpace(m.querySession.Validation.Message) != "" {
			m.addEvent("validation: " + m.querySession.Validation.Message)
			if currentCandidate := m.querySession.CurrentCandidate(); currentCandidate != nil && strings.TrimSpace(currentCandidate.Query) != "" {
				m.addEvent("invalid candidate: " + singleLine(currentCandidate.Query))
			}
		}
		if typedMsg.err != nil {
			m.status = typedMsg.err.Error()
			m.addEvent("error: " + typedMsg.err.Error())
			return m, nil
		}
		m.status = typedMsg.status
		return m, nil
	}
	inputCmd := m.updateFocused(teaMsg)
	return m, inputCmd
}

// View implements tea.Model.
func (m Model) View() string {
	contentWidth := clamp(m.width-4, 80, 180)
	leftWidth := contentWidth * 43 / 100
	rightWidth := contentWidth - leftWidth - 2
	header := m.renderHeader(contentWidth)
	left := lipgloss.JoinVertical(lipgloss.Left,
		m.renderGoal(leftWidth),
		m.renderSlots(leftWidth),
		m.renderWorkflow(leftWidth),
		m.renderEvents(leftWidth),
	)
	right := lipgloss.JoinVertical(lipgloss.Left,
		m.renderQuery(rightWidth),
		m.renderValidation(rightWidth),
		m.renderExecution(rightWidth),
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, m.renderFooter(contentWidth))
}

type workflowMsg struct {
	querySession *session.QuerySession
	events       []agent.Event
	err          error
	status       string
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
	case "tab":
		m.focus = (m.focus + 1) % focusCount
		return m.syncFocus(), true
	case "shift+tab":
		m.focus = (m.focus + focusCount - 1) % focusCount
		return m.syncFocus(), true
	case "ctrl+r":
		m.resourceType = nextResourceType(m.resourceType)
		return nil, true
	case "ctrl+a":
		if m.busy {
			return nil, true
		}
		m.busy = true
		m.status = "asking agent"
		return m.agentCmd(), true
	case "ctrl+v":
		if m.busy {
			return nil, true
		}
		m.busy = true
		m.status = "validating query"
		return m.validateCmd(), true
	case "ctrl+e":
		if m.busy {
			return nil, true
		}
		m.busy = true
		m.status = "executing query"
		return m.executeCmd(), true
	case "ctrl+x":
		if m.busy {
			return nil, true
		}
		m.busy = true
		m.status = "accepting query"
		return m.acceptCmd(), true
	default:
		return nil, false
	}
}

func (m *Model) syncFocus() tea.Cmd {
	m.goal.Blur()
	m.resourceName.Blur()
	m.groups.Blur()
	m.start.Blur()
	m.end.Blur()
	m.query.Blur()
	switch m.focus {
	case focusGoal:
		return m.goal.Focus()
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
	default:
		return nil
	}
}

func (m *Model) updateFocused(teaMsg tea.Msg) tea.Cmd {
	var updateCmd tea.Cmd
	switch m.focus {
	case focusGoal:
		m.goal, updateCmd = m.goal.Update(teaMsg)
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
	}
	return updateCmd
}

func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height
	contentWidth := clamp(width-4, 80, 180)
	leftWidth := contentWidth * 43 / 100
	rightWidth := contentWidth - leftWidth - 2
	inputWidth := maxInt(18, leftWidth-18)
	m.goal.SetWidth(leftWidth - 4)
	m.query.SetWidth(rightWidth - 4)
	m.resourceName.Width = inputWidth
	m.groups.Width = inputWidth
	m.start.Width = inputWidth
	m.end.Width = inputWidth
	queryHeight := clamp(height-22, 8, 18)
	m.query.SetHeight(queryHeight)
}

func (m Model) agentCmd() tea.Cmd {
	runner := m.runner
	options := m.startOptions()
	query := m.query.Value()
	querySession := m.querySession
	return func() tea.Msg {
		updatedSession, ensureErr := ensureSession(context.Background(), runner, querySession, options, query)
		if ensureErr != nil {
			return workflowMsg{err: ensureErr}
		}
		events, reviseErr := runner.ReviseWithAgent(context.Background(), updatedSession)
		if reviseErr != nil {
			return workflowMsg{
				querySession: updatedSession,
				events:       events,
				err:          reviseErr,
			}
		}
		return workflowMsg{
			querySession: updatedSession,
			events:       events,
			status:       "agent revision complete",
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
		Goal:         m.goal.Value(),
		ResourceName: m.resourceName.Value(),
		Groups:       []string{m.groups.Value()},
	}
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
		if strings.TrimSpace(event.Message) != "" {
			m.addEvent(fmt.Sprintf("%s: %s", event.Kind, event.Message))
		}
		if strings.TrimSpace(event.Candidate) != "" {
			m.addEvent("candidate: " + singleLine(event.Candidate))
		}
	}
}

func (m *Model) addEvent(event string) {
	if strings.TrimSpace(event) == "" {
		return
	}
	m.events = append(m.events, event)
	if len(m.events) > 12 {
		m.events = m.events[len(m.events)-12:]
	}
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
