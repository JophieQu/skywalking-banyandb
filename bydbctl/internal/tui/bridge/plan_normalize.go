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
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bridge

import (
	"encoding/json"
	"strconv"
	"strings"
)

func normalizePlanArgument(value any) any {
	planMap, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if workflowSteps, ok := planMap["steps"].([]any); ok {
		normalized := copyPlanMap(planMap)
		normalizedSteps := make([]any, 0, len(workflowSteps))
		for _, step := range workflowSteps {
			normalizedSteps = append(normalizedSteps, normalizePlanArgument(step))
		}
		normalized["steps"] = normalizedSteps
		return normalized
	}
	normalized := copyPlanMap(planMap)
	normalized["resource"] = normalizeResourceMap(normalized)
	for _, key := range []string{"type", "name", "groups", "group"} {
		delete(normalized, key)
	}
	if _, hasTopN := normalized["top_n"]; hasTopN {
		normalized["top_n"] = coercePlanInt(normalized["top_n"])
	}
	if _, hasLimit := normalized["limit"]; hasLimit {
		normalized["limit"] = coercePlanInt(normalized["limit"])
	}
	if topN := planIntValue(normalized["top_n"]); topN > 0 {
		applyTopNShape(normalized, topN)
	}
	if normalized["aggregate"] != nil {
		normalized["aggregate"] = normalizeAggregate(normalized["aggregate"])
	}
	if normalized["order_by"] != nil {
		normalized["order_by"] = normalizeOrderBy(normalized["order_by"])
	}
	if normalized["time_range"] != nil {
		normalized["time_range"] = normalizeTimeRange(normalized["time_range"])
	}
	return normalized
}

func normalizeResourceMap(planMap map[string]any) map[string]any {
	resource, _ := planMap["resource"].(map[string]any)
	if resource == nil {
		resource = map[string]any{}
	} else {
		resource = copyPlanMap(resource)
	}
	if resourceType, ok := planMap["type"].(string); ok && strings.TrimSpace(resourceType) != "" {
		resource["type"] = strings.ToUpper(strings.TrimSpace(resourceType))
	}
	if resourceName, ok := planMap["name"].(string); ok && strings.TrimSpace(resourceName) != "" {
		resource["name"] = strings.TrimSpace(resourceName)
	}
	if groups, ok := planMap["groups"]; ok {
		resource["groups"] = normalizeGroups(groups)
	}
	if group, ok := planMap["group"].(string); ok && strings.TrimSpace(group) != "" {
		resource["groups"] = []any{strings.TrimSpace(group)}
	}
	if resourceGroups, ok := resource["groups"]; ok {
		resource["groups"] = normalizeGroups(resourceGroups)
	}
	if resourceType, ok := resource["type"].(string); ok {
		resource["type"] = strings.ToUpper(strings.TrimSpace(resourceType))
	}
	return resource
}

func applyTopNShape(planMap map[string]any, topN int) {
	resource, _ := planMap["resource"].(map[string]any)
	if resource == nil {
		resource = map[string]any{}
		planMap["resource"] = resource
	}
	resourceType, _ := resource["type"].(string)
	if strings.EqualFold(resourceType, "MEASURE") || strings.TrimSpace(resourceType) == "" {
		resource["type"] = "TOPN"
	}
	planMap["top_n"] = topN
	delete(planMap, "limit")
	delete(planMap, "projection")
	delete(planMap, "filter")
	delete(planMap, "group_by")
}

func normalizeAggregate(value any) any {
	switch typed := value.(type) {
	case string:
		function := strings.ToUpper(strings.TrimSpace(typed))
		if function == "" {
			return value
		}
		return map[string]any{"function": function}
	case map[string]any:
		normalized := copyPlanMap(typed)
		if function, ok := normalized["function"].(string); ok {
			normalized["function"] = strings.ToUpper(strings.TrimSpace(function))
		}
		if column, ok := normalized["column"].(string); ok && strings.TrimSpace(column) == "" {
			delete(normalized, "column")
		}
		return normalized
	default:
		return value
	}
}

func normalizeOrderBy(value any) any {
	switch typed := value.(type) {
	case string:
		direction := strings.ToUpper(strings.TrimSpace(typed))
		if direction != "ASC" && direction != "DESC" {
			return value
		}
		return map[string]any{"direction": direction}
	case map[string]any:
		normalized := copyPlanMap(typed)
		if direction, ok := normalized["direction"].(string); ok {
			normalized["direction"] = strings.ToUpper(strings.TrimSpace(direction))
		}
		if column, ok := normalized["column"].(string); ok && strings.TrimSpace(column) == "" {
			delete(normalized, "column")
		}
		return normalized
	default:
		return value
	}
}

func normalizeTimeRange(value any) any {
	timeRange, ok := value.(map[string]any)
	if !ok {
		return value
	}
	return copyPlanMap(timeRange)
}

func normalizeGroups(groups any) []any {
	switch typed := groups.(type) {
	case []any:
		return typed
	case []string:
		normalized := make([]any, 0, len(typed))
		for _, group := range typed {
			normalized = append(normalized, group)
		}
		return normalized
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []any{strings.TrimSpace(typed)}
	default:
		return nil
	}
}

func coercePlanInt(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if intValue, parseErr := typed.Int64(); parseErr == nil {
			return int(intValue)
		}
	case string:
		if intValue, parseErr := strconv.Atoi(strings.TrimSpace(typed)); parseErr == nil {
			return intValue
		}
	case map[string]any:
		for _, key := range []string{"value", "limit", "n", "top_n", "count", "number"} {
			if inner, ok := typed[key]; ok {
				return coercePlanInt(inner)
			}
		}
	}
	return value
}

func planIntValue(value any) int {
	coerced := coercePlanInt(value)
	switch typed := coerced.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func copyPlanMap(planMap map[string]any) map[string]any {
	copied := make(map[string]any, len(planMap))
	for key, value := range planMap {
		copied[key] = value
	}
	return copied
}
