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
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/workflow"
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

func (m Model) currentPhaseLabel() string {
	if m.querySession == nil {
		return "phase intent"
	}
	return "phase " + m.querySession.Phase.String()
}

func (m Model) renderQueryTab(width, height int) string {
	if width < 100 {
		return m.renderNarrowQueryTab(width, height)
	}
	leftWidth, rightWidth := queryTabWidths(width)
	chatHeight := m.chatPanelHeight(height)
	left := lipgloss.JoinVertical(lipgloss.Left,
		m.renderChat(leftWidth, chatHeight),
		m.renderMessage(leftWidth),
		m.renderStatusLine(leftWidth),
	)
	right := lipgloss.JoinVertical(lipgloss.Left,
		m.renderQuery(rightWidth),
		m.renderCandidateHistory(rightWidth),
		m.renderApproval(rightWidth),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

func queryTabWidths(width int) (int, int) {
	if width < 100 {
		return width, width
	}
	leftWidth := clamp(width*58/100, 52, 108)
	return leftWidth, width - leftWidth - 2
}

func (m Model) renderNarrowQueryTab(width, height int) string {
	chatHeight := m.chatPanelHeight(height)
	left := lipgloss.JoinVertical(lipgloss.Left,
		m.renderChat(width, chatHeight),
		m.renderMessage(width),
		m.renderStatusLine(width),
	)
	right := lipgloss.JoinVertical(lipgloss.Left,
		m.renderQuery(width),
		m.renderCandidateHistory(width),
		m.renderApproval(width),
	)
	return lipgloss.JoinVertical(lipgloss.Left, left, right)
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
	reasoning := "off"
	if m.showReasoning {
		reasoning = "on"
	}
	statusLine := mutedStyle.Render(fmt.Sprintf(
		"Status: %s · Validation: %s · Policy: %s (Ctrl+P) · Reasoning: %s (Ctrl+R)",
		status,
		validation,
		m.executionPolicy.Label(),
		reasoning,
	))
	startLabelStyle := mutedStyle
	if m.focus == focusStart {
		startLabelStyle = titleStyle
	}
	endLabelStyle := mutedStyle
	if m.focus == focusEnd {
		endLabelStyle = titleStyle
	}
	timeRangeLine := lipgloss.JoinHorizontal(lipgloss.Top,
		mutedStyle.Render("Time: "),
		startLabelStyle.Render("start "),
		m.start.View(),
		mutedStyle.Render("  →  "),
		endLabelStyle.Render("end "),
		m.end.View(),
	)
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, statusLine, timeRangeLine))
}

func (m Model) renderMessage(width int) string {
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("Message · Ctrl+A to send"),
		m.message.View(),
		mutedStyle.Render("Ask a follow-up, refine the QL, or request a query."),
	))
}

func (m Model) renderChat(width, panelHeight int) string {
	rows := []string{
		titleStyle.Render("Conversation"),
		mutedStyle.Render("↑↓ messages · pgup/pgdn detail · Tab focus message"),
	}
	entries := chatEntries(m.querySession, m.showReasoning, m.liveResponse, m.queuedMessage)
	if len(entries) == 0 {
		rows = append(rows, mutedStyle.Render("Start a conversation. Your sent message appears here immediately."))
	} else {
		detailViewportHeight := 0
		detailLines := []string(nil)
		if m.chatCursor >= 0 && m.chatCursor < len(entries) {
			selected := entries[m.chatCursor]
			if strings.TrimSpace(selected.detail) != "" {
				detailViewportHeight = chatDetailViewportHeight(panelHeight)
				detailLines = formatChatDetailLines(selected.detail, width-4)
			}
		}
		listViewportHeight := maxInt(panelHeight-8-detailViewportHeight-2, 4)
		endIdx := minInt(m.chatScroll+listViewportHeight, len(entries))
		for entryIdx := m.chatScroll; entryIdx < endIdx; entryIdx++ {
			entry := entries[entryIdx]
			lineStyle := mutedStyle
			prefix := " "
			if entryIdx == m.chatCursor {
				prefix = ">"
				lineStyle = activeChipStyle
			}
			switch entry.role {
			case session.ChatRoleUser:
				if entryIdx != m.chatCursor {
					lineStyle = titleStyle
				}
			case session.ChatRoleTool:
				if entryIdx != m.chatCursor {
					lineStyle = warnStyle
				}
			case session.ChatRoleAssistant:
				if entryIdx != m.chatCursor {
					lineStyle = okStyle
				}
			}
			rows = append(rows, lineStyle.Render(prefix+truncate(entry.headline, width-12)))
		}
		if len(detailLines) > 0 {
			rows = append(rows, titleStyle.Render("Detail · pgup/pgdn scroll"))
			detailEnd := minInt(m.chatDetailScroll+detailViewportHeight, len(detailLines))
			for lineIdx := m.chatDetailScroll; lineIdx < detailEnd; lineIdx++ {
				line := renderChatDetailLine(detailLines[lineIdx])
				if lipgloss.Width(line) > width-4 {
					line = truncateANSI(line, width-4)
				}
				rows = append(rows, line)
			}
			if len(detailLines) > detailViewportHeight {
				rows = append(rows, mutedStyle.Render(fmt.Sprintf(
					"detail %d-%d/%d lines",
					m.chatDetailScroll+1,
					detailEnd,
					len(detailLines),
				)))
			}
		}
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("%d/%d messages", endIdx, len(entries))))
	}
	if m.busy {
		rows = append(rows, warnStyle.Render("agent turn in progress…"))
	}
	return panelStyle.Width(width).Height(panelHeight).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

type chatEntryView struct {
	role     session.ChatRole
	headline string
	detail   string
}

func chatEntryCount(querySession *session.QuerySession, showReasoning bool, liveResponse, queuedMessage string) int {
	return len(chatEntries(querySession, showReasoning, liveResponse, queuedMessage))
}

func chatEntries(querySession *session.QuerySession, showReasoning bool, liveResponse, queuedMessage string) []chatEntryView {
	chatMessageCount := 0
	if querySession != nil {
		chatMessageCount = len(querySession.ChatMessages)
	}
	entries := make([]chatEntryView, 0, chatMessageCount+2)
	if querySession != nil {
		for _, message := range querySession.ChatMessages {
			entries = append(entries, chatEntryFromMessage(message))
		}
	}
	if queued := strings.TrimSpace(queuedMessage); queued != "" {
		entries = append(entries, chatEntryView{
			role:     session.ChatRoleUser,
			headline: "You › " + queued,
		})
	}
	if showReasoning && strings.TrimSpace(liveResponse) != "" {
		entries = append(entries, chatEntryView{
			role:     session.ChatRoleAssistant,
			headline: "reasoning: " + truncateRunes(singleLine(liveResponse), 96),
			detail:   workflow.NormalizeAgentDisplayText(liveResponse),
		})
	}
	return entries
}

func chatEntryFromMessage(message session.ChatMessage) chatEntryView {
	content := workflow.NormalizeAgentDisplayText(strings.TrimSpace(message.Content))
	headline := chatRoleLabel(message.Role) + singleLine(content)
	detail := content
	if structuredDetail := strings.TrimSpace(message.Detail); structuredDetail != "" {
		detail = workflow.NormalizeAgentDisplayText(structuredDetail)
	}
	if message.ToolName != "" {
		headline = chatRoleLabel(message.Role) + message.ToolName + ": " + singleLine(content)
	}
	if strings.TrimSpace(message.Candidate) != "" {
		status := "unchecked"
		if message.Validation != nil {
			status = message.Validation.Status()
		}
		candidate := strings.TrimSpace(message.Candidate)
		candidateLine := chatRoleLabel(message.Role) + "candidate [" + status + "]: " + singleLine(candidate)
		compactDetail := strings.ReplaceAll(strings.ReplaceAll(detail, " ", ""), "\n", "")
		compactCandidate := strings.ReplaceAll(candidate, " ", "")
		if detail == "" {
			headline = candidateLine
			detail = candidate
		} else if !strings.Contains(compactDetail, compactCandidate) {
			detail = appendCandidateDetail(detail, candidate, status)
		}
		if message.Role == session.ChatRoleAssistant && strings.TrimSpace(content) == "" {
			headline = candidateLine
		}
	}
	if headline == chatRoleLabel(message.Role) {
		headline = chatRoleLabel(message.Role) + "(empty)"
		detail = ""
	}
	return chatEntryView{role: message.Role, headline: headline, detail: detail}
}

func chatLines(querySession *session.QuerySession, showReasoning bool, liveResponse string) []string {
	entries := chatEntries(querySession, showReasoning, liveResponse, "")
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.headline)
	}
	return lines
}

func chatRoleLabel(role session.ChatRole) string {
	switch role {
	case session.ChatRoleUser:
		return "You › "
	case session.ChatRoleTool:
		return "  ↳ "
	case session.ChatRoleSystem:
		return "System › "
	default:
		return "Agent › "
	}
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

func (m Model) renderCandidateHistory(width int) string {
	report := session.ValidationReport{Message: "not checked"}
	candidateCount := 0
	selectedCandidate := -1
	diff := "no previous version"
	probeSummary := "not probed"
	candidateSuperseded := false
	if m.querySession != nil {
		report = m.querySession.Validation
		candidateCount = len(m.querySession.Candidates)
		selectedCandidate = m.querySession.SelectedCandidateIndex()
		candidateSuperseded = m.querySession.CandidateSuperseded
		diff = candidateDiff(m.querySession)
		if currentCandidate := m.querySession.CurrentCandidate(); currentCandidate != nil && currentCandidate.Probe != nil {
			probe := currentCandidate.Probe
			if probe.Error != "" {
				probeSummary = probe.Error
			} else {
				probeSummary = fmt.Sprintf("%d rows, %d columns", probe.Rows, len(probe.Columns))
			}
		}
	}
	previewSharing := "automatic (up to 50 rows)"
	status := badStyle.Render(report.Status())
	if report.Valid {
		status = okStyle.Render(report.Status())
	} else if candidateSuperseded && candidateCount == 0 {
		status = mutedStyle.Render("not checked")
	}
	rows := []string{
		titleStyle.Render("Versions / validation"),
		fmt.Sprintf("Validation: %s", status),
		fmt.Sprintf("Query type: %s", fallback(report.QueryType, "-")),
		fmt.Sprintf("Version: %d/%d (Ctrl+←/→)", selectedCandidate+1, candidateCount),
	}
	if candidateSuperseded {
		rows = append(rows, warnStyle.Render("Candidate superseded by a new topic — waiting for agent draft"))
	}
	rows = append(rows,
		"Message: "+truncate(fallback(report.Message, "-"), width-12),
		"Probe: "+truncate(probeSummary, width-9),
		"Diff: "+truncate(diff, width-9),
		"Preview sharing: "+previewSharing,
	)
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m Model) renderApproval(width int) string {
	if m.pendingApproval == nil {
		return panelStyle.Width(width).Render(mutedStyle.Render("Read-only BYDBQL runs automatically. Mutating statements still require approval."))
	}
	request := *m.pendingApproval
	rows := []string{
		titleStyle.Render("Execution approval required"),
		"Exact statement:",
		wrapText(request.Query, width-4),
		"Resource: " + fallback(request.Resource, "-"),
		"Groups: " + fallback(strings.Join(request.Groups, ", "), "-"),
		"Time range: " + fallback(request.TimeRange, "-"),
		"Limit: " + fallback(request.Limit, "-"),
		"Timeout: " + request.Timeout.String(),
		fmt.Sprintf("Preview rows: %d", request.PreviewRows),
		"Source: " + string(request.Source),
		warnStyle.Render("y execute once · n reject · e copy to editor and revise"),
	}
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func candidateDiff(querySession *session.QuerySession) string {
	if querySession == nil {
		return "no version"
	}
	selectedIndex := querySession.SelectedCandidateIndex()
	if selectedIndex <= 0 || selectedIndex >= len(querySession.Candidates) {
		return "no previous version"
	}
	previousQuery := querySession.Candidates[selectedIndex-1].Query
	currentQuery := querySession.Candidates[selectedIndex].Query
	if previousQuery == currentQuery {
		return "unchanged"
	}
	return fmt.Sprintf("- %s + %s", singleLine(previousQuery), singleLine(currentQuery))
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
	runes := []rune(stripANSI(value))
	for len(runes) > 0 && lipgloss.Width(string(runes)) > maxWidth-3 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}

func truncateANSI(value string, maxWidth int) string {
	return truncate(value, maxWidth)
}
