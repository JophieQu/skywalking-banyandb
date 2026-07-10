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
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type appTab int

const (
	tabSchema appTab = iota
	tabQuery
	tabRun
	tabCount
)

var tabLabels = []string{
	"F1 Schema",
	"F2 Query",
	"F3 Run",
}

func (m Model) renderTabBar(width int) string {
	var tabs []string
	for tabIdx := 0; tabIdx < int(tabCount); tabIdx++ {
		label := tabLabels[tabIdx]
		style := chipStyle
		if appTab(tabIdx) == m.activeTab {
			style = activeChipStyle
		}
		tabs = append(tabs, style.Render(label))
	}
	phase := m.currentPhaseLabel()
	line := lipgloss.JoinHorizontal(lipgloss.Top,
		titleStyle.Render("bydbctl"),
		"  ",
		lipgloss.JoinHorizontal(lipgloss.Top, tabs...),
		"  ",
		chipStyle.Render("provider "+m.provider),
		"  ",
		mutedStyle.Render(phase),
	)
	return lipgloss.NewStyle().Width(width).Render(line)
}

func (m Model) footerForTab(width int) string {
	var commands []string
	switch m.activeTab {
	case tabSchema:
		commands = []string{"f1-f3 or [ ] tabs", "↑↓ browse", "enter select", "/ type", "ctrl+l refresh", "tab focus", "esc quit"}
	case tabQuery:
		commands = []string{
			"f1-f3 or [ ] tabs", "ctrl+a agent", "ctrl+v validate", "ctrl+e request execution", "ctrl+p share preview",
			"ctrl+←/→ versions", "y/n/e approval", "tab focus", "esc stop/quit",
		}
	default:
		commands = []string{"f1-f3 or [ ] tabs", "↑↓ scroll activity", "pgup/pgdn", "tab focus", "esc quit"}
	}
	return lipgloss.NewStyle().Width(width).Foreground(mutedColor).Render(strings.Join(commands, "  "))
}

func (m *Model) switchTab(tab appTab) {
	m.activeTab = tab
	order := m.focusOrder()
	if len(order) == 0 {
		return
	}
	m.focus = order[0]
}

func (m *Model) cycleTab(delta int) {
	if delta == 0 {
		return
	}
	nextTab := int(m.activeTab) + delta
	for nextTab < 0 {
		nextTab += int(tabCount)
	}
	nextTab %= int(tabCount)
	m.switchTab(appTab(nextTab))
}

func (m Model) isTypingFocus() bool {
	switch m.focus {
	case focusGoal, focusTurnHint, focusQuery, focusCatalogFilter, focusResourceName, focusGroups, focusStart, focusEnd:
		return true
	default:
		return false
	}
}

func (m Model) focusOrder() []int {
	switch m.activeTab {
	case tabSchema:
		return []int{focusCatalog, focusCatalogFilter}
	case tabQuery:
		return []int{focusGoal, focusTurnHint, focusResourceName, focusGroups, focusStart, focusEnd, focusQuery}
	case tabRun:
		return []int{focusActivity}
	default:
		return []int{focusGoal}
	}
}

func (m *Model) cycleFocus(delta int) {
	order := m.focusOrder()
	if len(order) == 0 {
		return
	}
	currentIdx := 0
	for idx, focusValue := range order {
		if focusValue == m.focus {
			currentIdx = idx
			break
		}
	}
	nextIdx := (currentIdx + delta) % len(order)
	if nextIdx < 0 {
		nextIdx += len(order)
	}
	m.focus = order[nextIdx]
}
