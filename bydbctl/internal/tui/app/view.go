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

package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

var (
	borderColor = lipgloss.Color("#3B454B")
	tealColor   = lipgloss.Color("#3FD0BD")
	amberColor  = lipgloss.Color("#E9B85D")
	redColor    = lipgloss.Color("#F0766D")
	greenColor  = lipgloss.Color("#84CC72")
	mutedColor  = lipgloss.Color("#B4ADA0")
	panelStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1)
	titleStyle = lipgloss.NewStyle().
			Foreground(tealColor).
			Bold(true)
	mutedStyle = lipgloss.NewStyle().
			Foreground(mutedColor)
	chipStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1)
	activeChipStyle = chipStyle.Copy().
			BorderForeground(tealColor).
			Foreground(tealColor)
	warnStyle = lipgloss.NewStyle().
			Foreground(amberColor)
	okStyle = lipgloss.NewStyle().
		Foreground(greenColor)
	badStyle = lipgloss.NewStyle().
			Foreground(redColor)
)

func (m Model) renderHeader(width int) string {
	title := titleStyle.Render("bydbctl agent")
	subtitle := mutedStyle.Render("F1 schema · F2 query/agent · F3 run/debug")
	chips := lipgloss.JoinHorizontal(lipgloss.Top,
		activeChipStyle.Render("provider "+m.provider),
		" ",
		chipStyle.Render(m.currentPhaseLabel()),
	)
	line := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", chips)
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, line, subtitle))
}

func (m Model) currentPhaseLabel() string {
	if m.querySession == nil {
		return "phase intent"
	}
	return "phase " + m.querySession.Phase.String()
}

func (m Model) renderQueryTab(width, height int) string {
	leftWidth := clamp(width*48/100, 40, 80)
	rightWidth := width - leftWidth - 2
	queryHeight := clamp(height-14, 10, 24)
	m.query.SetHeight(queryHeight)
	left := lipgloss.JoinVertical(lipgloss.Left,
		m.renderGoal(leftWidth),
		m.renderTurnHint(leftWidth),
		m.renderSlots(leftWidth),
		m.renderStatusLine(leftWidth),
	)
	right := lipgloss.JoinVertical(lipgloss.Left,
		m.renderQuery(rightWidth),
		m.renderValidation(rightWidth),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

func (m Model) renderStatusLine(width int) string {
	status := m.status
	if m.busy {
		status = warnStyle.Render(status)
	}
	validation := "not checked"
	if m.querySession != nil && m.querySession.Validation.Message != "" {
		validation = m.querySession.Validation.Status()
	}
	return panelStyle.Width(width).Render(mutedStyle.Render(fmt.Sprintf("Status: %s · Validation: %s", status, validation)))
}

func (m Model) renderGoal(width int) string {
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render("Goal"), m.goal.View()))
}

func (m Model) renderTurnHint(width int) string {
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render("Turn hint (Ctrl+A)"), m.turnHint.View()))
}

func (m Model) renderSlots(width int) string {
	rows := []string{
		m.slotRow("Type", activeChipStyle.Render(m.resourceType.String())),
		m.slotRow("Name", m.resourceName.View()),
		m.slotRow("Groups", m.groups.View()),
		m.slotRow("Start", m.start.View()),
		m.slotRow("End", m.end.View()),
	}
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, append([]string{titleStyle.Render("Slots")}, rows...)...))
}

func (m Model) renderWorkflow(width int) string {
	return m.renderStatusLine(width)
}

func (m Model) renderEvents(width int) string {
	lines := []string{titleStyle.Render("Events")}
	if len(m.events) == 0 {
		lines = append(lines, mutedStyle.Render("No events yet"))
	} else {
		for _, event := range m.events {
			lines = append(lines, mutedStyle.Render(truncate(event, width-6)))
		}
	}
	if logHint := formatLogHint(m.logPathDisplay); logHint != "" {
		lines = append(lines, mutedStyle.Render(truncate(logHint, width-6)))
	}
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m Model) renderQuery(width int) string {
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render("BYDBQL Candidate"), m.query.View()))
}

func (m Model) renderValidation(width int) string {
	report := session.ValidationReport{Message: "not checked"}
	candidateCount := 0
	acceptedQuery := ""
	if m.querySession != nil {
		report = m.querySession.Validation
		candidateCount = len(m.querySession.Candidates)
		acceptedQuery = m.querySession.AcceptedQuery
	}
	status := badStyle.Render(report.Status())
	if report.Valid {
		status = okStyle.Render(report.Status())
	}
	rows := []string{
		titleStyle.Render("Validation / Approval"),
		fmt.Sprintf("Validation: %s", status),
		fmt.Sprintf("Query type: %s", fallback(report.QueryType, "-")),
		fmt.Sprintf("Candidates: %d", candidateCount),
		"Message: " + truncate(fallback(report.Message, "-"), width-12),
	}
	if acceptedQuery != "" {
		rows = append(rows, okStyle.Render("Accepted: "+truncate(singleLine(acceptedQuery), width-12)))
	}
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m Model) renderExecution(width int) string {
	rows := []string{titleStyle.Render("Execution Preview")}
	if m.querySession == nil || m.querySession.ExecutionResult.Summary == "" {
		rows = append(rows, mutedStyle.Render("not executed"))
		return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	}
	executionResult := m.querySession.ExecutionResult
	rows = append(rows,
		"Command: "+truncate(fallback(executionResult.Command, "-"), width-12),
		"Path: "+truncate(fallback(executionResult.Path, "-"), width-9),
		fmt.Sprintf("Rows: %d", executionResult.Rows),
		"Summary: "+truncate(executionResult.Summary, width-12),
	)
	if executionResult.Response != "" {
		rows = append(rows, "Response: "+truncate(singleLine(executionResult.Response), width-12))
	}
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m Model) renderFooter(width int) string {
	return m.footerForTab(width)
}

func (m Model) slotRow(label, value string) string {
	focused := map[int]string{
		focusCatalogFilter: "Filter",
		focusTurnHint:      "Turn hint (Ctrl+A)",
		focusResourceName: "Name",
		focusGroups:       "Groups",
		focusStart:        "Start",
		focusEnd:          "End",
	}
	labelStyle := mutedStyle
	if focused[m.focus] == label {
		labelStyle = titleStyle
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Width(10).Render(label), value)
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func truncate(value string, maxWidth int) string {
	if maxWidth <= 3 {
		return value
	}
	if lipgloss.Width(value) <= maxWidth {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > maxWidth-3 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}
