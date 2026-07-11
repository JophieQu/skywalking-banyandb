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

func proposeQueryPlanInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"oneOf": []map[string]any{
			{"required": []string{"plan"}},
			{"required": []string{"workflow"}},
		},
		"properties": map[string]any{
			"plan":     queryPlanObjectSchema(),
			"workflow": workflowPlanObjectSchema(),
		},
	}
}

func workflowPlanObjectSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"steps"},
		"properties": map[string]any{
			"steps": map[string]any{
				"type":  "array",
				"items": queryPlanObjectSchema(),
			},
		},
	}
}

func queryPlanObjectSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"resource"},
		"properties": map[string]any{
			"resource": map[string]any{
				"type":     "object",
				"required": []string{"type", "name", "groups"},
				"properties": map[string]any{
					"type":   map[string]any{"type": "string", "enum": []string{"MEASURE", "STREAM", "TRACE", "PROPERTY", "TOPN"}},
					"name":   map[string]string{"type": "string"},
					"groups": map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
				},
			},
			"projection": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"column": map[string]string{"type": "string"},
						"aggregate": map[string]any{
							"type":     "object",
							"required": []string{"function", "column"},
							"properties": map[string]any{
								"function": map[string]any{"type": "string", "enum": []string{"MEAN", "COUNT", "MAX", "MIN", "SUM"}},
								"column":   map[string]string{"type": "string"},
							},
						},
					},
				},
			},
			"filter": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"column":   map[string]string{"type": "string"},
					"operator": map[string]string{"type": "string"},
					"value":    map[string]string{"type": "string"},
					"children": map[string]any{"type": "array"},
				},
			},
			"aggregate": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"function": map[string]any{"type": "string", "enum": []string{"MEAN", "COUNT", "MAX", "MIN", "SUM"}},
					"column":   map[string]string{"type": "string"},
				},
			},
			"order_by": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"column":    map[string]string{"type": "string"},
					"direction": map[string]any{"type": "string", "enum": []string{"ASC", "DESC"}},
				},
			},
			"time_range": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"start": map[string]string{"type": "string"},
					"end":   map[string]string{"type": "string"},
				},
			},
			"group_by": map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
			"limit":    map[string]string{"type": "integer"},
			"top_n":    map[string]string{"type": "integer"},
			"id":       map[string]string{"type": "string"},
		},
	}
}

func queryPlanSchemaHint() string {
	return "Submit {\"plan\":{...}} or {\"workflow\":{\"steps\":[...]}}. " +
		"SHOW TOP goals use resource.type TOPN, top_n as an integer, aggregate.function, order_by.direction only, and time_range.start. " +
		"SELECT goals use resource.type MEASURE|STREAM|TRACE|PROPERTY with typed projection/filter/order_by/limit. " +
		"Do not put name, type, or groups at the plan root; nest them under resource."
}
