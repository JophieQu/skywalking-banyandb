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

package workflow

import (
	"regexp"
	"strings"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

var catalogTokenPattern = regexp.MustCompile(`[a-z0-9]+`)

// ResolvedSlots contains workflow-owned slot values after catalog resolution.
type ResolvedSlots struct {
	ResourceType session.ResourceType
	ResourceName string
	Groups       []string
	Goal         string
	TimeRange    session.TimeRange
	SlotsPinned  bool
	AutoMatched  bool
}

// ResolveSessionSlots applies user slots, catalog matching, and safe defaults.
func ResolveSessionSlots(options StartOptions, catalog session.SchemaCatalog) ResolvedSlots {
	goal := strings.TrimSpace(options.Goal)
	resourceType := options.ResourceType
	if resourceType == "" {
		resourceType = inferResourceType(goal)
	}
	resourceName := strings.TrimSpace(options.ResourceName)
	groups := normalizeGroupsIfProvided(options.Groups)
	slotsPinned := options.NameProvided && options.GroupsProvided

	resolved := ResolvedSlots{
		ResourceType: resourceType,
		ResourceName: resourceName,
		Groups:       groups,
		Goal:         goal,
		TimeRange:    applyTimeDefaults(options.TimeRange),
		SlotsPinned:  slotsPinned,
	}
	if slotsPinned {
		return finalizeResolvedSlots(resolved)
	}
	if len(catalog.Entries) == 0 {
		return finalizeResolvedSlots(resolved)
	}
	match := matchResourceFromGoal(goal, catalog, resourceType, resourceName, groups)
	if !match.Matched {
		return finalizeResolvedSlots(resolved)
	}
	if resourceName == "" {
		resolved.ResourceName = match.Name
		resolved.AutoMatched = true
	}
	if len(resolved.Groups) == 0 {
		resolved.Groups = []string{match.Group}
		resolved.AutoMatched = true
	}
	if !options.TypeProvided {
		resolved.ResourceType = match.Type
		resolved.AutoMatched = true
	}
	return finalizeResolvedSlots(resolved)
}

type catalogMatch struct {
	Matched bool
	Group   string
	Name    string
	Type    session.ResourceType
	Score   int
}

func matchResourceFromGoal(
	goal string,
	catalog session.SchemaCatalog,
	preferredType session.ResourceType,
	preferredName string,
	preferredGroups []string,
) catalogMatch {
	goalTokens := catalogTokens(goal)
	preferredTypeHint := inferResourceType(goal)
	var bestMatch catalogMatch
	for _, entry := range catalog.Entries {
		if shouldSkipCatalogEntry(entry) {
			continue
		}
		if preferredName != "" && entry.Name != preferredName {
			continue
		}
		if len(preferredGroups) > 0 && !containsString(preferredGroups, entry.Group) {
			continue
		}
		if preferredType != "" && preferredType != preferredTypeHint && entry.Type != preferredType {
			continue
		}
		score := scoreCatalogEntry(goal, goalTokens, entry, preferredTypeHint)
		if score > bestMatch.Score {
			bestMatch = catalogMatch{
				Matched: score > 0,
				Group:   entry.Group,
				Name:    entry.Name,
				Type:    entry.Type,
				Score:   score,
			}
		}
	}
	return bestMatch
}

func scoreCatalogEntry(goal string, goalTokens []string, entry session.CatalogEntry, preferredType session.ResourceType) int {
	score := 0
	entryName := strings.ToLower(entry.Name)
	entryGroup := strings.ToLower(entry.Group)
	normalizedGoal := strings.ToLower(goal)
	nameTokens := catalogTokens(entryName)
	groupTokens := catalogTokens(entryGroup)
	for _, goalToken := range goalTokens {
		for _, nameToken := range nameTokens {
			if goalToken == nameToken {
				score += 12
			}
		}
		for _, groupToken := range groupTokens {
			if goalToken == groupToken {
				score += 6
			}
		}
	}
	if strings.Contains(normalizedGoal, entryName) {
		score += 20
	}
	if strings.Contains(normalizedGoal, entryGroup) {
		score += 10
	}
	if entry.Type == preferredType {
		score += 8
	}
	score += typeKeywordScore(normalizedGoal, entry.Type)
	if strings.Contains(entryName, "latency") && strings.Contains(normalizedGoal, "slow") {
		score += 8
	}
	if strings.Contains(entryName, "endpoint") && strings.Contains(normalizedGoal, "endpoint") {
		score += 8
	}
	return score
}

func typeKeywordScore(goal string, resourceType session.ResourceType) int {
	switch resourceType {
	case session.ResourceTypeStream:
		if strings.Contains(goal, "log") || strings.Contains(goal, "stream") {
			return 6
		}
	case session.ResourceTypeTrace:
		if strings.Contains(goal, "trace") || strings.Contains(goal, "span") {
			return 6
		}
	case session.ResourceTypeProperty:
		if strings.Contains(goal, "property") {
			return 6
		}
	case session.ResourceTypeTopN:
		if strings.Contains(goal, "top") {
			return 4
		}
	default:
		if strings.Contains(goal, "measure") || strings.Contains(goal, "metric") || strings.Contains(goal, "latency") {
			return 4
		}
	}
	return 0
}

func shouldSkipCatalogEntry(entry session.CatalogEntry) bool {
	if strings.HasPrefix(entry.Group, "_") {
		return true
	}
	return strings.TrimSpace(entry.Name) == ""
}

func catalogTokens(value string) []string {
	normalizedValue := strings.ToLower(strings.ReplaceAll(value, "_", " "))
	matches := catalogTokenPattern.FindAllString(normalizedValue, -1)
	return compactStrings(matches)
}

func normalizeGroupsIfProvided(groups []string) []string {
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
	return normalizedGroups
}

func applyTimeDefaults(timeRange session.TimeRange) session.TimeRange {
	if strings.TrimSpace(timeRange.Start) != "" {
		return timeRange
	}
	return session.TimeRange{
		Start: defaultTimeStart,
		End:   strings.TrimSpace(timeRange.End),
	}
}

func finalizeResolvedSlots(resolved ResolvedSlots) ResolvedSlots {
	if strings.TrimSpace(resolved.ResourceName) == "" {
		resolved.ResourceName = defaultResourceName
	}
	if len(resolved.Groups) == 0 {
		resolved.Groups = []string{defaultGroupName}
	}
	return resolved
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func compactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	compactedValues := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		compactedValues = append(compactedValues, value)
	}
	return compactedValues
}
