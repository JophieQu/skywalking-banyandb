// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache License, Version 2.0 (the "License"); you may
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

package session

import "testing"

func TestCandidateSelectionPreservesManualVersionAsTheCurrentBasis(t *testing.T) {
	querySession := &QuerySession{}
	querySession.AddCandidate(BydbqlCandidate{
		ID:     "agent",
		Query:  "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10",
		Source: CandidateSourceAgent,
		Validation: ValidationReport{
			Valid: true,
		},
	})
	querySession.AddCandidate(BydbqlCandidate{
		ID:     "manual",
		Query:  "SELECT endpoint FROM MEASURE latency IN production TIME > '-30m' LIMIT 10",
		Source: CandidateSourceManual,
		Validation: ValidationReport{
			Valid: true,
		},
	})
	if currentCandidate := querySession.CurrentCandidate(); currentCandidate == nil || currentCandidate.ID != "manual" {
		t.Fatalf("expected manual version to be selected, got %+v", currentCandidate)
	}
	if !querySession.SelectCandidate(0) {
		t.Fatal("expected agent version to be selectable")
	}
	if currentCandidate := querySession.CurrentCandidate(); currentCandidate == nil || currentCandidate.ID != "agent" {
		t.Fatalf("expected selected agent version, got %+v", currentCandidate)
	}
	if querySession.SelectCandidate(2) {
		t.Fatal("expected nonexistent version selection to fail")
	}
}

func TestPlannedQueriesAdvanceOnlyAfterTheExactCurrentStatement(t *testing.T) {
	querySession := &QuerySession{}
	querySession.SetPlannedQueries([]PlannedQuery{
		{ID: "first", Query: "SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10"},
		{ID: "second", Query: "SELECT * FROM STREAM logs IN production TIME > '-30m' LIMIT 10"},
	})
	if querySession.CompletePlannedQuery("SELECT * FROM STREAM logs IN production TIME > '-30m' LIMIT 10") != nil {
		t.Fatal("unexpectedly advanced a plan with the wrong query")
	}
	nextQuery := querySession.CompletePlannedQuery("SELECT * FROM MEASURE latency IN production TIME > '-30m' LIMIT 10")
	if nextQuery == nil || nextQuery.ID != "second" {
		t.Fatalf("expected second query after completing first, got %+v", nextQuery)
	}
	if !querySession.PlannedQueries[0].Completed {
		t.Fatal("expected first planned query to be complete")
	}
}
