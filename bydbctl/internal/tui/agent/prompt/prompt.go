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
	"fmt"
	"strings"
)

//go:embed references/safety.md
var safetyReference string

//go:embed references/syntax.md
var syntaxReference string

//go:embed references/examples.md
var examplesReference string

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
		taskPrompt = "Generate one BYDBQL query from the goal, slots, and schema in the context JSON."
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
		taskPrompt = "Revise the BYDBQL candidate using validation or execution feedback in the context JSON."
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
	prompt.WriteString("You are a BanyanDB BYDBQL generation specialist.\n")
	prompt.WriteString("Your only job is to return one syntactically valid BYDBQL candidate for the bydbctl editor.\n\n")
}

func writeHardRules(prompt *bytes.Buffer, initial bool) {
	prompt.WriteString("Hard rules:\n")
	prompt.WriteString("- Use validate_bydbql with the complete candidate before presenting it to the user. This is the only way to publish a candidate.\n")
	prompt.WriteString("- Do not rely on Markdown, JSON, or prose to communicate a candidate; bydbctl ignores candidates embedded in text.\n")
	prompt.WriteString("- The statement must start with SELECT or SHOW TOP.\n")
	prompt.WriteString("- Do not include a trailing semicolon.\n")
	prompt.WriteString("- Use only the schema summary, slots, query_hints, and template_hint from the context JSON.\n")
	prompt.WriteString("- Use only the four provided bydbctl tools. Do not use shell commands, external MCP servers, downloads, or runtime tool registration.\n")
	prompt.WriteString("- You may use schema and validation tools without asking. Call execute_bydbql only after the user explicitly asks to run the exact statement; ")
	prompt.WriteString("it always requires a fresh TUI approval.\n")
	prompt.WriteString("- Keep the query read-only. Never generate create, update, delete, drop, or apply operations.\n")
	prompt.WriteString("- When query_hints.slots_pinned=true, use schema.type, schema.name, and schema.groups exactly.\n")
	prompt.WriteString("- When query_hints.slots_pinned=false and schema.catalog is present, choose the best matching catalog entry for the goal.\n")
	prompt.WriteString("- For MEASURE, STREAM, TRACE, and SHOW TOP, include a TIME clause from time_range in the context.\n")
	prompt.WriteString("- turn_hint is the user's instruction for the current round; apply it on top of goal and prior conversation.\n")
	if initial {
		prompt.WriteString("- For exploratory SELECT queries, include LIMIT 10 unless query_hints specify otherwise.\n")
	} else {
		prompt.WriteString("- Fix validation_error or execution_summary.error when present; preserve correct parts of the candidate.\n")
		prompt.WriteString("- After validate_bydbql succeeds, give a short user-facing summary. Do not repeat the query in Markdown or JSON.\n")
	}
	prompt.WriteString("- ORDER BY may only use fields listed in schema.indexed_fields; omit ORDER BY when no indexed field matches.\n\n")
}

func writeNLRules(prompt *bytes.Buffer) {
	prompt.WriteString("Natural language rules:\n")
	prompt.WriteString("- When slots_pinned=true, schema slots override names inferred from the goal.\n")
	prompt.WriteString("- When slots_pinned=false, prefer schema.catalog and schema.available_groups to infer type, name, and group.\n")
	prompt.WriteString("- query_hints.prefer_show_top=true means use SHOW TOP, not SELECT with LIMIT.\n")
	prompt.WriteString("- Distinguish time ranges (TIME clause) from data-point limits (LIMIT clause).\n")
	prompt.WriteString("- template_hint shows a valid baseline query for the current slots; adapt it to the goal.\n")
	prompt.WriteString("- schema.available_resources lists resource names in the current group when the name slot may be wrong.\n")
	prompt.WriteString("- schema.catalog lists discoverable resources across groups when the user only provided a goal.\n")
	prompt.WriteString("- conversation lists prior user hints and agent candidates; continue from the latest state.\n\n")
}

func writeReferences(prompt *bytes.Buffer) {
	prompt.WriteString("Reference:\n")
	prompt.WriteString(strings.TrimSpace(safetyReference))
	prompt.WriteString("\n\n")
	prompt.WriteString(strings.TrimSpace(syntaxReference))
	prompt.WriteString("\n\n")
	prompt.WriteString(strings.TrimSpace(examplesReference))
	prompt.WriteString("\n\n")
}

func writeOutputContract(prompt *bytes.Buffer) {
	prompt.WriteString("Output contract:\n")
	prompt.WriteString("- Explain your plan and result briefly in user-facing language.\n")
	prompt.WriteString(fmt.Sprintf("- Publish the complete candidate through %s; do not embed it in free text.\n\n", "validate_bydbql"))
}
