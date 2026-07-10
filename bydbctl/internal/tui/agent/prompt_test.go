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

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
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
		"Use validate_bydbql with the complete candidate",
		"Do not rely on Markdown, JSON, or prose",
		"Context JSON:",
		"top slow endpoints",
		"Use only the four provided bydbctl tools",
		"time_range",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt does not contain %q:\n%s", expected, prompt)
		}
	}
}

func TestBuildAgentTurnRequestSharesPreviewOnlyAfterExplicitOptIn(t *testing.T) {
	querySession := &session.QuerySession{
		ExecutionResult: session.ExecutionResult{
			Query:        "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10",
			Rows:         2,
			ResourceType: "measure",
			Columns:      []string{"service", "latency"},
			Preview:      [][]string{{"payment", "20"}, {"checkout", "50"}},
		},
	}
	payload := BuildAgentTurnRequest(querySession, QueryHints{}, "", "")
	if payload.ExecutionSummary == nil {
		t.Fatal("expected execution summary")
	}
	if len(payload.ExecutionSummary.Preview) != 0 {
		t.Fatalf("preview must not be shared by default: %+v", payload.ExecutionSummary.Preview)
	}
	querySession.IncludePreview = true
	payload = BuildAgentTurnRequest(querySession, QueryHints{}, "", "")
	if len(payload.ExecutionSummary.Preview) != 2 {
		t.Fatalf("expected opted-in preview to be shared: %+v", payload.ExecutionSummary.Preview)
	}
}

func TestBuildAgentTurnRequestRedactsTransportErrors(t *testing.T) {
	querySession := &session.QuerySession{
		ExecutionResult: session.ExecutionResult{
			Query: "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10",
			Error: "Post https://banyandb.internal:17913/api/v1/bydbql/query: dial tcp 10.0.0.1: connect: refused",
		},
	}
	payload := BuildAgentTurnRequest(querySession, QueryHints{}, "", "")
	if payload.ExecutionSummary == nil || payload.ExecutionSummary.Error != "BYDBQL execution failed" {
		t.Fatalf("unexpected execution error summary: %+v", payload.ExecutionSummary)
	}
	if strings.Contains(payload.ExecutionSummary.Error, "banyandb.internal") {
		t.Fatalf("transport details leaked to provider: %+v", payload.ExecutionSummary)
	}
}
