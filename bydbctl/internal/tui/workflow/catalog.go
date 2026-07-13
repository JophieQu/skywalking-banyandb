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
	"sort"
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
		return finalizeResolvedSlots(resolved, catalog)
	}
	if len(catalog.Entries) == 0 {
		return finalizeResolvedSlots(resolved, catalog)
	}
	match := matchResourceFromGoal(goal, catalog, resourceType, resourceName, groups)
	if !match.Matched {
		return finalizeResolvedSlots(resolved, catalog)
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
	return finalizeResolvedSlots(resolved, catalog)
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
		score := scoreCatalogEntry(goal, goalTokens, entry, preferredTypeHint, len(catalog.Groups))
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

func scoreCatalogEntry(goal string, goalTokens []string, entry session.CatalogEntry, preferredType session.ResourceType, catalogGroupCount int) int {
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
	if entry.Group == defaultGroupName && catalogGroupCount > 1 {
		score -= 8
	}
	if strings.Contains(entryGroup, "metric") && (strings.Contains(normalizedGoal, "metric") || strings.Contains(normalizedGoal, "endpoint") || strings.Contains(normalizedGoal, "latency")) {
		score += 6
	}
	score += typeKeywordScore(normalizedGoal, entry.Type)
	if strings.Contains(entryName, "latency") && strings.Contains(normalizedGoal, "slow") {
		score += 8
	}
	if strings.Contains(entryName, "endpoint") && (strings.Contains(normalizedGoal, "endpoint") || strings.Contains(normalizedGoal, "payment")) {
		score += 8
	}
	if strings.Contains(entryName, "cpu") && strings.Contains(normalizedGoal, "cpu") {
		score += 12
	}
	if strings.Contains(entryName, "cpu") && strings.Contains(normalizedGoal, "处理器") {
		score += 12
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

const maxPromptCatalogCandidates = 10

// CatalogRankingGoal returns the text used to rank catalog entries for the current turn.
func CatalogRankingGoal(userGoal, turnHint string) string {
	turnHint = strings.TrimSpace(turnHint)
	userGoal = strings.TrimSpace(userGoal)
	if turnHint == "" {
		return userGoal
	}
	if userGoal == "" {
		return turnHint
	}
	return turnHint + " " + userGoal
}

var resourceMentionPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9_]{5,}`)

// FindExplicitResourceMention matches a catalog entry named directly in the user text.
func FindExplicitResourceMention(goal string, entries []session.CatalogEntry) *session.CatalogEntry {
	repairedGoal := strings.ToLower(RepairFragmentedQuery(goal))
	compactGoal := strings.ReplaceAll(repairedGoal, " ", "")
	mentions := resourceMentionPattern.FindAllString(repairedGoal, -1)
	mentions = append(mentions, resourceMentionPattern.FindAllString(compactGoal, -1)...)
	var bestEntry *session.CatalogEntry
	bestScore := 0
	for _, mention := range mentions {
		mention = strings.ToLower(strings.TrimSpace(mention))
		if len(mention) < 8 || isGenericResourceMention(mention) {
			continue
		}
		mentionCompact := strings.ReplaceAll(mention, "_", "")
		for entryIdx := range entries {
			entry := &entries[entryIdx]
			entryName := strings.ToLower(entry.Name)
			entryCompact := strings.ReplaceAll(entryName, "_", "")
			score := scoreExplicitResourceMention(mention, mentionCompact, entryName, entryCompact)
			if score > bestScore {
				bestScore = score
				bestEntry = entry
			}
		}
	}
	if bestScore < 20 {
		return nil
	}
	return bestEntry
}

func isGenericResourceMention(mention string) bool {
	switch mention {
	case "minute", "minutes", "metrics", "metric", "schema", "schemas", "groups", "group", "query", "queries":
		return true
	default:
		return false
	}
}

func scoreExplicitResourceMention(mention, mentionCompact, entryName, entryCompact string) int {
	mentionCore := normalizeResourceGranularity(mention)
	entryCore := normalizeResourceGranularity(entryName)
	if mention == entryName {
		return 120 + len(entryName)
	}
	if mentionCore == entryCore {
		return 110 + len(entryName)
	}
	if strings.Contains(mention, entryName) {
		return 100 + len(entryName)
	}
	if strings.Contains(entryCompact, entryCompact) || strings.Contains(entryCompact, mentionCompact) {
		return 80 + len(entryName)
	}
	prefixLen := commonNamePrefixLen(mentionCompact, entryCompact)
	if prefixLen >= 18 {
		return 40 + prefixLen
	}
	return 0
}

func normalizeResourceGranularity(name string) string {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	for _, suffix := range []string{"_minute", "_hour", "_day", "_second"} {
		if strings.HasSuffix(normalizedName, suffix) {
			return strings.TrimSuffix(normalizedName, suffix)
		}
	}
	return normalizedName
}

func commonNamePrefixLen(left, right string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for idx := 0; idx < limit; idx++ {
		if left[idx] != right[idx] {
			return idx
		}
	}
	return limit
}

// RankCatalogCandidates returns the highest-scoring catalog entries for a goal.
func RankCatalogCandidates(goal string, entries []session.CatalogEntry, limit int) []session.CatalogEntry {
	if limit <= 0 {
		limit = maxPromptCatalogCandidates
	}
	goalTokens := catalogTokens(goal)
	preferredType := inferResourceType(goal)
	type rankedEntry struct {
		entry session.CatalogEntry
		score int
	}
	ranked := make([]rankedEntry, 0, len(entries))
	for _, entry := range entries {
		if shouldSkipCatalogEntry(entry) {
			continue
		}
		score := scoreCatalogEntry(goal, goalTokens, entry, preferredType, len(entries))
		ranked = append(ranked, rankedEntry{entry: entry, score: score})
	}
	sort.SliceStable(ranked, func(leftIndex, rightIndex int) bool {
		if ranked[leftIndex].score != ranked[rightIndex].score {
			return ranked[leftIndex].score > ranked[rightIndex].score
		}
		if ranked[leftIndex].entry.Group != ranked[rightIndex].entry.Group {
			return ranked[leftIndex].entry.Group < ranked[rightIndex].entry.Group
		}
		if ranked[leftIndex].entry.Type != ranked[rightIndex].entry.Type {
			return ranked[leftIndex].entry.Type < ranked[rightIndex].entry.Type
		}
		return ranked[leftIndex].entry.Name < ranked[rightIndex].entry.Name
	})
	candidates := make([]session.CatalogEntry, 0, limit)
	for _, rankedItem := range ranked {
		if rankedItem.score <= 0 && len(candidates) > 0 {
			break
		}
		candidates = append(candidates, rankedItem.entry)
		if len(candidates) >= limit {
			break
		}
	}
	return candidates
}

// EnsureCatalogEntry includes entry in candidates when missing, keeping the shortest list possible.
func EnsureCatalogEntry(candidates []session.CatalogEntry, entry session.CatalogEntry, limit int) []session.CatalogEntry {
	if limit <= 0 {
		limit = maxPromptCatalogCandidates
	}
	for _, candidate := range candidates {
		if candidate.Type == entry.Type && strings.EqualFold(candidate.Name, entry.Name) && strings.EqualFold(candidate.Group, entry.Group) {
			return candidates
		}
	}
	updated := append([]session.CatalogEntry{entry}, candidates...)
	if len(updated) > limit {
		updated = updated[:limit]
	}
	return updated
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

func finalizeResolvedSlots(resolved ResolvedSlots, catalog session.SchemaCatalog) ResolvedSlots {
	if shouldPreferCatalogMatch(resolved) && len(catalog.Entries) > 0 {
		match := matchResourceFromGoal(resolved.Goal, catalog, resolved.ResourceType, "", nil)
		if match.Matched {
			resolved.ResourceName = match.Name
			resolved.Groups = []string{match.Group}
			resolved.ResourceType = match.Type
			resolved.AutoMatched = true
		}
	}
	if strings.TrimSpace(resolved.ResourceName) == "" {
		resolved.ResourceName = defaultResourceName
	}
	if len(resolved.Groups) == 0 {
		resolved.Groups = []string{defaultGroupName}
	}
	return resolved
}

func shouldPreferCatalogMatch(resolved ResolvedSlots) bool {
	if resolved.SlotsPinned {
		return false
	}
	if strings.TrimSpace(resolved.ResourceName) == "" || resolved.ResourceName == defaultResourceName {
		return true
	}
	if len(resolved.Groups) == 0 || (len(resolved.Groups) == 1 && resolved.Groups[0] == defaultGroupName) {
		return true
	}
	return false
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
