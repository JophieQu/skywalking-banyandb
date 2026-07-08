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
	prompt.WriteString("- Return exactly one BYDBQL statement in one fenced code block marked bydbql.\n")
	prompt.WriteString("- The final answer must end with the fenced code block and must not include prose after it.\n")
	prompt.WriteString("- The statement must start with SELECT or SHOW TOP.\n")
	prompt.WriteString("- Do not include a trailing semicolon.\n")
	prompt.WriteString("- Use only the schema summary, slots, query_hints, and template_hint from the context JSON.\n")
	prompt.WriteString("- Do not call external tools, shell commands, or MCP servers.\n")
	prompt.WriteString("- Keep the query read-only. Never generate create, update, delete, drop, or apply operations.\n")
	prompt.WriteString("- Use FROM <resource_type> <resource_name> IN <groups> from the context when slots are provided.\n")
	prompt.WriteString("- For MEASURE, STREAM, TRACE, and SHOW TOP, include a TIME clause from time_range in the context.\n")
	if initial {
		prompt.WriteString("- For exploratory SELECT queries, include LIMIT 10 unless query_hints specify otherwise.\n")
	} else {
		prompt.WriteString("- Fix validation_error or execution_summary.error when present; preserve correct parts of the candidate.\n")
	}
	prompt.WriteString("- ORDER BY may only use fields listed in schema.indexed_fields; omit ORDER BY when no indexed field matches.\n\n")
}

func writeNLRules(prompt *bytes.Buffer) {
	prompt.WriteString("Natural language rules:\n")
	prompt.WriteString("- Slots (type, name, groups, time_range) override names inferred from the goal.\n")
	prompt.WriteString("- query_hints.prefer_show_top=true means use SHOW TOP, not SELECT with LIMIT.\n")
	prompt.WriteString("- Distinguish time ranges (TIME clause) from data-point limits (LIMIT clause).\n")
	prompt.WriteString("- template_hint shows a valid baseline query for the current slots; adapt it to the goal.\n")
	prompt.WriteString("- schema.available_resources lists resource names in the current group when the name slot may be wrong.\n\n")
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
	prompt.WriteString("```bydbql\n")
	prompt.WriteString("<one BYDBQL statement only>\n")
	prompt.WriteString("```\n")
	prompt.WriteString(fmt.Sprintf("Alternative accepted format: {\"bydbql\":\"<statement>\"} or {\"BydbQL\":\"<statement>\"}\n\n"))
}
