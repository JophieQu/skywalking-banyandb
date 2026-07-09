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

func (m Model) renderSchemaTab(width, height int) string {
	catalogWidth := clamp(width*38/100, 32, 56)
	detailWidth := width - catalogWidth - 2
	catalogPanel := m.renderCatalogList(catalogWidth, height)
	detailPanel := m.renderSchemaDetailPanel(detailWidth, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, catalogPanel, " ", detailPanel)
}

func (m Model) renderCatalogList(width, height int) string {
	lines := []string{titleStyle.Render("Catalog")}
	typeLabel := "ALL"
	if m.catalog.typeFilter != "" {
		typeLabel = m.catalog.typeFilter.String()
	}
	lines = append(lines, mutedStyle.Render(fmt.Sprintf("Type %s · / cycle · ctrl+l refresh", typeLabel)))
	if m.focus == focusCatalogFilter {
		lines = append(lines, titleStyle.Render("Filter")+" "+m.catalogFilter.View())
	} else {
		filterValue := strings.TrimSpace(m.catalogFilter.Value())
		if filterValue == "" {
			lines = append(lines, mutedStyle.Render("Filter: search group or resource name"))
		} else {
			lines = append(lines, mutedStyle.Render("Filter: "+filterValue))
		}
	}
	if m.catalog.loading {
		lines = append(lines, warnStyle.Render("Loading schema catalog..."))
		return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}
	if m.catalog.loadError != "" {
		lines = append(lines, badStyle.Render("Catalog: "+m.catalog.loadError))
		return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}
	if len(m.catalog.rows) == 0 {
		lines = append(lines, mutedStyle.Render("No resources match filter"))
		return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}
	lines = append(lines, mutedStyle.Render(fmt.Sprintf("%d groups · %d resources", m.catalog.groupCount(), m.catalog.resourceCount())))
	listHeight := clamp(height-len(lines)-2, 8, 28)
	viewportHeight := maxInt(listHeight, 6)
	endIdx := minInt(m.catalog.scrollOffset+viewportHeight, len(m.catalog.rows))
	for rowIdx := m.catalog.scrollOffset; rowIdx < endIdx; rowIdx++ {
		row := m.catalog.rows[rowIdx]
		line := renderCatalogRow(row, rowIdx == m.catalog.cursor && m.focus == focusCatalog, width-6)
		lines = append(lines, line)
	}
	lines = append(lines, mutedStyle.Render("↑↓ browse · enter select slot"))
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m Model) renderSchemaDetailPanel(width, height int) string {
	lines := []string{titleStyle.Render("Schema Detail")}
	detailLines := schemaDetailLines(m.selectedSchema)
	if len(detailLines) == 0 {
		lines = append(lines, mutedStyle.Render("Select a resource to inspect tags, fields, and indexed columns"))
		return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}
	viewportHeight := clamp(height-4, 10, 32)
	maxScroll := maxInt(len(detailLines)-viewportHeight, 0)
	scrollOffset := m.detailScroll
	if scrollOffset > maxScroll {
		scrollOffset = maxScroll
	}
	endIdx := minInt(scrollOffset+viewportHeight, len(detailLines))
	for lineIdx := scrollOffset; lineIdx < endIdx; lineIdx++ {
		line := detailLines[lineIdx]
		if strings.HasPrefix(line, "  ") {
			lines = append(lines, mutedStyle.Render(truncate(line, width-4)))
			continue
		}
		lines = append(lines, truncate(line, width-4))
	}
	lines = append(lines, mutedStyle.Render("↑↓ scroll fields"))
	return panelStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func schemaDetailLines(snapshot session.SchemaSnapshot) []string {
	if strings.TrimSpace(snapshot.Name) == "" {
		return nil
	}
	lines := []string{
		fmt.Sprintf("%s %s", snapshot.Type, snapshot.Name),
		"Group: " + strings.Join(snapshot.Groups, ","),
	}
	if !snapshot.Loaded {
		lines = append(lines, warnStyle.Render("Schema detail not loaded from BanyanDB API"))
		lines = append(lines, mutedStyle.Render("Check --addr and press enter again on the resource"))
		return lines
	}
	if len(snapshot.EntityTags) > 0 {
		lines = append(lines, titleStyle.Render("Entity (series key)"))
		for _, entityTag := range snapshot.EntityTags {
			lines = append(lines, "  · "+entityTag)
		}
	}
	if len(snapshot.Tags) > 0 {
		lines = append(lines, titleStyle.Render("Tags"))
		for _, tag := range snapshot.Tags {
			lines = append(lines, "  · "+tag)
		}
	}
	if len(snapshot.Fields) > 0 {
		lines = append(lines, titleStyle.Render("Fields"))
		for _, field := range snapshot.Fields {
			lines = append(lines, "  · "+field)
		}
	}
	if len(snapshot.IndexedFields) > 0 {
		lines = append(lines, titleStyle.Render("Indexed tags (ORDER BY)"))
		for _, indexedField := range snapshot.IndexedFields {
			lines = append(lines, "  · "+indexedField)
		}
	}
	if len(snapshot.Tags) == 0 && len(snapshot.Fields) == 0 && len(snapshot.EntityTags) == 0 {
		lines = append(lines, mutedStyle.Render("No tags or fields declared on this resource"))
	}
	return lines
}

func renderCatalogRow(row catalogRow, selected bool, maxWidth int) string {
	if row.kind == catalogRowKindGroup {
		label := "▸ " + row.group
		if selected {
			return activeChipStyle.Render(truncate(label, maxWidth))
		}
		return titleStyle.Render(truncate(label, maxWidth))
	}
	label := fmt.Sprintf("  %s  %s", shortTypeLabel(row.entry.Type), row.entry.Name)
	if selected {
		return activeChipStyle.Render(truncate(label, maxWidth))
	}
	return mutedStyle.Render(truncate(label, maxWidth))
}

func shortTypeLabel(resourceType session.ResourceType) string {
	switch resourceType {
	case session.ResourceTypeMeasure:
		return "MEASURE"
	case session.ResourceTypeStream:
		return "STREAM"
	case session.ResourceTypeTrace:
		return "TRACE"
	case session.ResourceTypeProperty:
		return "PROPERTY"
	case session.ResourceTypeTopN:
		return "TOPN"
	default:
		return "?"
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
