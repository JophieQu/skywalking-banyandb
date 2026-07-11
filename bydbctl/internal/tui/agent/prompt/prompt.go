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

// Package prompt builds BYDBQL generation prompts for agent adapters.
// Reference content is derived from skills/bydbql/references/.
package prompt

import (
	"bytes"
	_ "embed"
	"strings"
)

//go:embed references/syntax.md
var syntaxReference string

// Input carries rendered prompt inputs for BYDBQL generation.
type Input struct {
	TaskPrompt  string
	PayloadJSON string
	Candidate   string
}

// Build renders the provider prompt for BYDBQL generation or revision.
func Build(input Input) string {
	if strings.TrimSpace(input.Candidate) == "" {
		return buildInitial(input)
	}
	return buildRevise(input)
}

func buildInitial(input Input) string {
	taskPrompt := strings.TrimSpace(input.TaskPrompt)
	if taskPrompt == "" {
		taskPrompt = "Discover the relevant BanyanDB schema and submit one structured query plan from the context JSON."
	}
	var promptBuffer bytes.Buffer
	writeRole(&promptBuffer)
	promptBuffer.WriteString("Task:\n")
	promptBuffer.WriteString(taskPrompt)
	promptBuffer.WriteString("\n\n")
	writeHardRules(&promptBuffer, true)
	writeNLRules(&promptBuffer)
	writeReferences(&promptBuffer)
	writeOutputContract(&promptBuffer)
	promptBuffer.WriteString("Context JSON:\n")
	promptBuffer.WriteString(input.PayloadJSON)
	return promptBuffer.String()
}

func buildRevise(input Input) string {
	taskPrompt := strings.TrimSpace(input.TaskPrompt)
	if taskPrompt == "" {
		taskPrompt = "Revise the structured query plan using validation or execution feedback in the context JSON."
	}
	var promptBuffer bytes.Buffer
	writeRole(&promptBuffer)
	promptBuffer.WriteString("Task:\n")
	promptBuffer.WriteString(taskPrompt)
	promptBuffer.WriteString("\n\n")
	writeHardRules(&promptBuffer, false)
	writeNLRules(&promptBuffer)
	writeReferences(&promptBuffer)
	writeOutputContract(&promptBuffer)
	promptBuffer.WriteString("Context JSON:\n")
	promptBuffer.WriteString(input.PayloadJSON)
	return promptBuffer.String()
}

func writeRole(prompt *bytes.Buffer) {
	prompt.WriteString("You are a BanyanDB query-planning specialist.\n")
	prompt.WriteString("Discover schemas and submit typed query plans; bydbctl, not you, renders the final BYDBQL.\n\n")
}

func writeHardRules(prompt *bytes.Buffer, initial bool) {
	prompt.WriteString("Hard rules:\n")
	prompt.WriteString("- Never write, validate, or publish a raw BYDBQL statement. Publish candidates only through propose_query_plan.\n")
	prompt.WriteString("- The session working directory is not a codebase. Do not read files, search source, or run shell commands.\n")
	prompt.WriteString("- On a new goal, your first action must be an MCP tool call: list_groups_schemas, or describe_schema when schema.ranked_candidates already names one resource.\n")
	prompt.WriteString("- Do not end the turn until propose_query_plan returns valid=true. Free-text BYDBQL is ignored by bydbctl.\n")
	prompt.WriteString("- On a new goal, rank at most five catalog candidates, and call describe_schema for at most three.\n")
	prompt.WriteString("- Use only the typed columns returned by describe_schema. Do not invent a resource, group, field, tag, type, or index.\n")
	prompt.WriteString("- Use only the five provided bydbctl tools. Do not use shell commands, external MCP servers, downloads, or runtime tool registration.\n")
	prompt.WriteString("- propose_query_plan accepts a plan or workflow. Its result is the only structured candidate that bydbctl will show.\n")
	prompt.WriteString("- You may use schema and plan tools without asking. Call execute_bydbql only after the user explicitly asks to run the exact statement; ")
	prompt.WriteString("it always requires a fresh TUI approval.\n")
	prompt.WriteString("- Keep every plan read-only. Never generate create, update, delete, drop, or apply operations.\n")
	prompt.WriteString("- The deterministic planner supports projections, typed comparison/IN filters, AND/OR trees, indexed ordering, ")
	prompt.WriteString("measure aggregation/grouping, and SHOW TOP.\n")
	prompt.WriteString("- Supported aggregation functions are MEAN, COUNT, MAX, MIN, and SUM. ORDER BY may use TIME or a typed indexed column.\n")
	prompt.WriteString("- Do not request MATCH, HAVING, OFFSET, STAGES, WITH QUERY_TRACE, joins, or unsupported expressions. Ask one clarification instead.\n")
	prompt.WriteString("- turn_hint is the user's instruction for the current round; apply it on top of goal and prior conversation.\n")
	if initial {
		prompt.WriteString("- Omitted time and row-count constraints are rendered by bydbctl as the safe defaults: last 30 minutes and LIMIT 10.\n")
	} else {
		prompt.WriteString("- Fix validation_error or execution_summary.error when present; preserve correct parts of the prior plan.\n")
		prompt.WriteString("- Execution previews contain at most 50 rows and may be used to plan the next independently approved step.\n")
	}
	prompt.WriteString("- When a catalog choice remains ambiguous after inspection, ask exactly one concise clarification question instead of guessing.\n\n")
}

func writeNLRules(prompt *bytes.Buffer) {
	prompt.WriteString("Natural language rules:\n")
	prompt.WriteString("- schema.ranked_candidates and schema.catalog are discovery hints, not user selections. Select a resource only after inspecting its actual typed schema.\n")
	prompt.WriteString("- query_hints.prefer_show_top means use a SHOW TOP plan, not SELECT with LIMIT.\n")
	prompt.WriteString("- Distinguish time ranges from data-point limits; use the user wording when it is explicit.\n")
	prompt.WriteString("- A goal spanning multiple resources requires a workflow with one independently approved plan step per resource.\n")
	prompt.WriteString("- conversation lists prior user hints and compiled candidates; continue from the latest state.\n\n")
}

func writeReferences(prompt *bytes.Buffer) {
	prompt.WriteString("Compiler vocabulary reference (understand it, but submit a plan rather than BYDBQL text):\n")
	prompt.WriteString(strings.TrimSpace(syntaxReference))
	prompt.WriteString("\n\n")
}

func writeOutputContract(prompt *bytes.Buffer) {
	prompt.WriteString("Output contract:\n")
	prompt.WriteString("- Explain the selected schema and result briefly in user-facing language.\n")
	prompt.WriteString("- Submit propose_query_plan with {\"plan\":{...}} or {\"workflow\":{\"steps\":[...]}} only.\n")
	prompt.WriteString("- Nest resource fields under resource: {\"type\",\"name\",\"groups\"}. Never put name/type/groups at the plan root.\n")
	prompt.WriteString("- SHOW TOP goals: resource.type TOPN, top_n as integer, aggregate.function, order_by.direction, time_range.start. No column in aggregate or order_by.\n")
	prompt.WriteString("- SELECT goals: resource.type MEASURE|STREAM|TRACE|PROPERTY with typed projection/filter/order_by/limit.\n")
	prompt.WriteString("- When plan_example is present in context JSON, copy its shape and fill columns from describe_schema.\n")
	prompt.WriteString("- Do not embed BYDBQL in free text; propose_query_plan is the only accepted candidate path.\n\n")
}
