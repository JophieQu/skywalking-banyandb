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

package agent

import (
	"strings"
	"testing"
)

func TestBuildBydbqlPromptIncludesOutputContract(t *testing.T) {
	prompt, promptErr := BuildBydbqlPrompt(TurnRequest{
		Prompt: "Generate a query.",
		Payload: RequestPayload{
			Task:      "revise_bydbql",
			Goal:      "top slow endpoints",
			Candidate: "",
			Schema: SchemaSummary{
				Type:          "MEASURE",
				Name:          "service_latency",
				Groups:        []string{"production"},
				Tags:          []string{"endpoint"},
				Fields:        []string{"latency"},
				IndexedFields: []string{"endpoint"},
			},
			TimeRange: TimeRangePayload{Start: "-30m"},
		},
	})
	if promptErr != nil {
		t.Fatalf("BuildBydbqlPrompt returned error: %v", promptErr)
	}
	for _, expected := range []string{
		"BYDBQL generation specialist",
		"Return exactly one BYDBQL statement in one fenced code block marked bydbql",
		"```bydbql",
		"Context JSON:",
		"top slow endpoints",
		"Do not call external tools",
		"time_range",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt does not contain %q:\n%s", expected, prompt)
		}
	}
}
