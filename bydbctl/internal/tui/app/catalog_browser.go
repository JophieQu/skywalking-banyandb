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
	"sort"
	"strings"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

const catalogRowKindGroup = "group"

type catalogRow struct {
	kind    string
	group   string
	entry   session.CatalogEntry
}

type catalogBrowser struct {
	catalog      session.SchemaCatalog
	rows         []catalogRow
	filter       string
	typeFilter   session.ResourceType
	cursor       int
	scrollOffset int
	loading      bool
	loadError    string
}

func newCatalogBrowser() catalogBrowser {
	return catalogBrowser{}
}

func (browser *catalogBrowser) setCatalog(catalog session.SchemaCatalog) {
	browser.catalog = catalog
	browser.loading = false
	browser.loadError = ""
	browser.rebuildRows()
}

func (browser *catalogBrowser) setLoadError(loadError string) {
	browser.loading = false
	browser.loadError = loadError
	browser.catalog = session.SchemaCatalog{}
	browser.rows = nil
	browser.cursor = 0
	browser.scrollOffset = 0
}

func (browser *catalogBrowser) setLoading() {
	browser.loading = true
	browser.loadError = ""
}

func (browser *catalogBrowser) setFilter(filter string) {
	browser.filter = strings.ToLower(strings.TrimSpace(filter))
	browser.rebuildRows()
}

func (browser *catalogBrowser) cycleTypeFilter() {
	browser.cycleTypeFilterDelta(1)
}

func (browser *catalogBrowser) cycleTypeFilterDelta(delta int) {
	if delta == 0 {
		return
	}
	types := catalogTypeFilters()
	options := append([]session.ResourceType{""}, types...)
	currentIdx := 0
	for idx, resourceType := range options {
		if resourceType == browser.typeFilter {
			currentIdx = idx
			break
		}
	}
	nextIdx := currentIdx + delta
	for nextIdx < 0 {
		nextIdx += len(options)
	}
	nextIdx %= len(options)
	browser.typeFilter = options[nextIdx]
	browser.rebuildRows()
}

func catalogTypeFilters() []session.ResourceType {
	return []session.ResourceType{
		session.ResourceTypeMeasure,
		session.ResourceTypeStream,
		session.ResourceTypeTrace,
		session.ResourceTypeProperty,
		session.ResourceTypeTopN,
	}
}

func (browser *catalogBrowser) rebuildRows() {
	filterTokens := strings.Fields(browser.filter)
	groupedEntries := make(map[string][]session.CatalogEntry)
	groupOrder := make([]string, 0)
	for _, entry := range browser.catalog.Entries {
		if browser.typeFilter != "" && entry.Type != browser.typeFilter {
			continue
		}
		if !catalogEntryMatches(entry, filterTokens) {
			continue
		}
		if _, ok := groupedEntries[entry.Group]; !ok {
			groupOrder = append(groupOrder, entry.Group)
		}
		groupedEntries[entry.Group] = append(groupedEntries[entry.Group], entry)
	}
	sort.Strings(groupOrder)
	browser.rows = browser.rows[:0]
	for _, group := range groupOrder {
		browser.rows = append(browser.rows, catalogRow{kind: catalogRowKindGroup, group: group})
		entries := groupedEntries[group]
		sort.Slice(entries, func(leftIdx, rightIdx int) bool {
			leftEntry := entries[leftIdx]
			rightEntry := entries[rightIdx]
			if leftEntry.Type != rightEntry.Type {
				return leftEntry.Type < rightEntry.Type
			}
			return leftEntry.Name < rightEntry.Name
		})
		for _, entry := range entries {
			browser.rows = append(browser.rows, catalogRow{kind: "resource", group: group, entry: entry})
		}
	}
	if len(browser.rows) == 0 {
		browser.cursor = 0
		browser.scrollOffset = 0
		return
	}
	browser.cursor = 0
	for rowIdx, row := range browser.rows {
		if row.kind == "resource" {
			browser.cursor = rowIdx
			break
		}
	}
	browser.clampScroll(12)
}

func catalogEntryMatches(entry session.CatalogEntry, filterTokens []string) bool {
	if len(filterTokens) == 0 {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{entry.Group, entry.Name, string(entry.Type)}, " "))
	for _, token := range filterTokens {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func (browser *catalogBrowser) moveCursor(delta int, viewportHeight int) {
	if len(browser.rows) == 0 {
		browser.cursor = 0
		browser.scrollOffset = 0
		return
	}
	nextCursor := browser.cursor + delta
	if nextCursor < 0 {
		nextCursor = 0
	}
	if nextCursor >= len(browser.rows) {
		nextCursor = len(browser.rows) - 1
	}
	for nextCursor > browser.cursor && browser.rows[nextCursor].kind == catalogRowKindGroup {
		nextCursor++
		if nextCursor >= len(browser.rows) {
			nextCursor = len(browser.rows) - 1
			break
		}
	}
	for nextCursor < browser.cursor && browser.rows[nextCursor].kind == catalogRowKindGroup {
		nextCursor--
		if nextCursor < 0 {
			nextCursor = 0
			break
		}
	}
	browser.cursor = nextCursor
	browser.clampScroll(viewportHeight)
}

func (browser *catalogBrowser) clampScroll(viewportHeight int) {
	if viewportHeight <= 0 {
		viewportHeight = 8
	}
	if browser.cursor < browser.scrollOffset {
		browser.scrollOffset = browser.cursor
	}
	if browser.cursor >= browser.scrollOffset+viewportHeight {
		browser.scrollOffset = browser.cursor - viewportHeight + 1
	}
}

func (browser catalogBrowser) selectedEntry() (session.CatalogEntry, bool) {
	if browser.cursor < 0 || browser.cursor >= len(browser.rows) {
		return session.CatalogEntry{}, false
	}
	row := browser.rows[browser.cursor]
	if row.kind != "resource" {
		return session.CatalogEntry{}, false
	}
	return row.entry, true
}

func (browser catalogBrowser) resourceCount() int {
	count := 0
	for _, row := range browser.rows {
		if row.kind == "resource" {
			count++
		}
	}
	return count
}

func (browser catalogBrowser) groupCount() int {
	seen := make(map[string]struct{})
	for _, row := range browser.rows {
		if row.kind == catalogRowKindGroup {
			seen[row.group] = struct{}{}
		}
	}
	return len(seen)
}
