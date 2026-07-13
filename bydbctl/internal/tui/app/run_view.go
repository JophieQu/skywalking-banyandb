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

func (m Model) renderRunTab(width, height int) string {
	executionHeight := minInt(maxInt(height/2, 10), 22)
	executionPanel := m.renderExecutionDetail(width, executionHeight)
	activityHeight := clamp(height-lipgloss.Height(executionPanel)-4, 6, 30)
	activityPanel := m.renderActivityLog(width, activityHeight)
	return lipgloss.JoinVertical(lipgloss.Left, executionPanel, activityPanel)
}

func (m Model) renderExecutionDetail(width, viewportHeight int) string {
	title := "Execution"
	if m.focus == focusExecution {
		title = "Execution · focused"
	}
	rows := []string{titleStyle.Render(title)}
	if m.querySession == nil || m.querySession.ExecutionResult.Summary == "" {
		rows = append(rows, mutedStyle.Render("Press Ctrl+E on the Query tab to execute the current BYDBQL candidate."))
		return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	}
	phase := session.PhaseIntent
	if m.querySession != nil {
		phase = m.querySession.Phase
	}
	rows = append(rows, fmt.Sprintf("Phase: %s", phase))
	if m.executionExportPath != "" {
		rows = append(rows, mutedStyle.Render("Exported: "+m.executionExportPath))
	}
	bodyLines := m.executionBodyLines(width - 4)
	visibleHeight := maxInt(viewportHeight-lipgloss.Height(strings.Join(rows, "\n"))-2, 6)
	if len(bodyLines) > visibleHeight {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf(
			"Detail · pgup/pgdn scroll · %d/%d lines",
			minInt(m.executionDetailScroll+visibleHeight, len(bodyLines)),
			len(bodyLines),
		)))
	}
	startIdx := m.executionDetailScroll
	endIdx := minInt(startIdx+visibleHeight, len(bodyLines))
	for lineIdx := startIdx; lineIdx < endIdx; lineIdx++ {
		rows = append(rows, renderExecutionLine(bodyLines[lineIdx]))
	}
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func renderExecutionLine(line string) string {
	switch {
	case strings.HasPrefix(line, "Error: "):
		return badStyle.Render(line)
	case strings.HasPrefix(line, "Hint: "):
		return warnStyle.Render(line)
	case strings.HasPrefix(line, "Full JSON hidden"), strings.HasPrefix(line, "Full JSON response omitted"), strings.HasPrefix(line, "…"):
		return mutedStyle.Render(line)
	case strings.HasPrefix(line, "Table preview"):
		return titleStyle.Render(line)
	case strings.HasPrefix(line, "Row detail"):
		return titleStyle.Render(line)
	case line == "Response preview":
		return titleStyle.Render(line)
	default:
		return line
	}
}

func (m Model) renderActivityLog(width, height int) string {
	rows := []string{
		titleStyle.Render("Activity"),
		mutedStyle.Render("Agent plans, tool calls, validation, and execution details"),
	}
	if logHint := formatLogHint(m.logPathDisplay); logHint != "" {
		rows = append(rows, mutedStyle.Render(logHint))
	}
	if len(m.activityLog) == 0 {
		rows = append(rows, mutedStyle.Render("No activity yet — run agent or execute a query"))
		return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	}
	viewportHeight := maxInt(height-4, 6)
	endIdx := minInt(m.activityScroll+viewportHeight, len(m.activityLog))
	for entryIdx := m.activityScroll; entryIdx < endIdx; entryIdx++ {
		entry := m.activityLog[entryIdx]
		lineStyle := mutedStyle
		prefix := " "
		if entryIdx == m.activityCursor {
			prefix = ">"
			lineStyle = activeChipStyle
		}
		switch entry.category {
		case "tool":
			if entryIdx != m.activityCursor {
				lineStyle = warnStyle
			}
		case "error", "execution":
			if entryIdx != m.activityCursor {
				lineStyle = badStyle
			}
		case "agent":
			if entryIdx != m.activityCursor {
				lineStyle = okStyle
			}
		case "user":
			if entryIdx != m.activityCursor {
				lineStyle = titleStyle
			}
		}
		rows = append(rows, lineStyle.Render(prefix+fmt.Sprintf("[%s] %s", entry.category, truncate(entry.title, width-12))))
	}
	if m.activityCursor >= 0 && m.activityCursor < len(m.activityLog) {
		selected := m.activityLog[m.activityCursor]
		if selected.detail != "" {
			detailLines := formatActivityDetailText(selected.detail, width-4)
			detailViewport := maxInt(height-lipgloss.Height(strings.Join(rows, "\n"))-2, 4)
			if len(detailLines) > detailViewport {
				rows = append(rows, titleStyle.Render("Detail · pgup/pgdn scroll"))
			} else {
				rows = append(rows, titleStyle.Render("Detail"))
			}
			detailEnd := minInt(m.activityDetailScroll+detailViewport, len(detailLines))
			for lineIdx := m.activityDetailScroll; lineIdx < detailEnd; lineIdx++ {
				rows = append(rows, mutedStyle.Render(detailLines[lineIdx]))
			}
		}
	}
	rows = append(rows, mutedStyle.Render(fmt.Sprintf("%d/%d entries", endIdx, len(m.activityLog))))
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func wrapText(value string, width int) string {
	if width <= 0 {
		return value
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return value
	}
	var lines []string
	var current strings.Builder
	for _, word := range words {
		if current.Len() == 0 {
			current.WriteString(word)
			continue
		}
		if current.Len()+1+len(word) > width {
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(word)
			continue
		}
		current.WriteString(" ")
		current.WriteString(word)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return strings.Join(lines, "\n")
}
