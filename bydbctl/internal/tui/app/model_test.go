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

package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

func TestUpdateSyncsSessionAndEventsBeforeError(t *testing.T) {
	model := NewModel(Config{})
	querySession := &session.QuerySession{}
	querySession.AddCandidate(session.BydbqlCandidate{
		Query:  "SELECT * FROM STREAM sw IN default WHERE",
		Source: session.CandidateSourceAgent,
		Validation: session.ValidationReport{
			Valid:   false,
			Message: "syntax error: expected expression",
		},
	})
	updatedModel, _ := model.Update(workflowMsg{
		querySession: querySession,
		events: []agent.Event{
			{
				Kind:    agent.EventKindMessageDelta,
				Message: "agent raw output",
			},
		},
		err: errors.New("agent candidate failed validation"),
	})
	typedModel, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("unexpected model type: %T", updatedModel)
	}
	if typedModel.query.Value() != "SELECT * FROM STREAM sw IN default WHERE" {
		t.Fatalf("unexpected query value: %s", typedModel.query.Value())
	}
	events := strings.Join(typedModel.events, "\n")
	for _, expected := range []string{"agent raw output", "syntax error", "invalid candidate", "agent candidate failed validation"} {
		if !strings.Contains(events, expected) {
			t.Fatalf("expected event %q in:\n%s", expected, events)
		}
	}
}
