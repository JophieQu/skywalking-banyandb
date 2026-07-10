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

package acp

import (
	"strings"
	"testing"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
)

func TestNormalizeEventMapsFinalCandidate(t *testing.T) {
	event := NormalizeEvent([]byte(`{"jsonrpc":"2.0","method":"session/final","params":{"message":"done","candidate":"SELECT * FROM STREAM sw IN default"}}`))
	if event.Kind != agent.EventKindFinalResponse {
		t.Fatalf("unexpected event kind: %s", event.Kind)
	}
	if event.Candidate != "SELECT * FROM STREAM sw IN default" {
		t.Fatalf("unexpected candidate: %s", event.Candidate)
	}
}

func TestNormalizeEventMapsError(t *testing.T) {
	event := NormalizeEvent([]byte(`{"jsonrpc":"2.0","error":{"code":-1,"message":"denied"}}`))
	if event.Kind != agent.EventKindError {
		t.Fatalf("unexpected event kind: %s", event.Kind)
	}
	if event.Err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeEventMapsAgentMessageChunk(t *testing.T) {
	line := []byte(`{
		"jsonrpc": "2.0",
		"method": "session/update",
		"params": {
			"sessionId": "s1",
			"update": {
				"sessionUpdate": "agent_message_chunk",
				"content": {
					"type": "text",
					"text": "SELECT * FROM MEASURE m IN g TIME > '-30m' LIMIT 10"
				}
			}
		}
	}`)
	event := NormalizeEvent(line)
	if event.Kind != agent.EventKindMessageDelta {
		t.Fatalf("unexpected event kind: %s", event.Kind)
	}
	if event.Message != "SELECT * FROM MEASURE m IN g TIME > '-30m' LIMIT 10" {
		t.Fatalf("unexpected message: %s", event.Message)
	}
}

func TestNormalizeEventMapsClarification(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","method":"session/update",` +
		`"params":{"update":{"sessionUpdate":"clarification","content":{"text":"Which group should I use?"}}}}`)
	event := NormalizeEvent(line)
	if event.Kind != agent.EventKindClarification || event.Message != "Which group should I use?" {
		t.Fatalf("unexpected clarification event: %+v", event)
	}
}

func TestNormalizeEventMapsPermissionRequest(t *testing.T) {
	line := []byte(`{
		"jsonrpc": "2.0",
		"id": "1",
		"method": "session/request_permission",
		"params": {
			"toolCall": {
				"title": "run shell"
			}
		}
	}`)
	event := NormalizeEvent(line)
	if event.Kind != agent.EventKindPermissionRequest {
		t.Fatalf("unexpected event kind: %s", event.Kind)
	}
	if event.Message == "" {
		t.Fatal("expected permission message")
	}
}

func TestPermissionDecisionAllowsOnlyControlledTools(t *testing.T) {
	decision := permissionDecision(map[string]any{
		"toolCall": map[string]any{"name": "validate_bydbql"},
		"options": []any{
			map[string]any{"optionId": "allow", "kind": "allow_once"},
			map[string]any{"optionId": "reject", "kind": "reject_once"},
		},
	})
	outcome := mapValue(decision, "outcome")
	if stringValue(outcome, "optionId") != "allow" {
		t.Fatalf("expected controlled tool permission to be approved: %+v", decision)
	}
	decision = permissionDecision(map[string]any{
		"toolCall": map[string]any{"name": "shell"},
		"options": []any{
			map[string]any{"optionId": "allow", "kind": "allow_once"},
			map[string]any{"optionId": "reject", "kind": "reject_once"},
		},
	})
	outcome = mapValue(decision, "outcome")
	if stringValue(outcome, "optionId") != "reject" {
		t.Fatalf("expected external tool permission to be rejected: %+v", decision)
	}
	decision = permissionDecision(map[string]any{
		"toolCall": map[string]any{"name": "evil_validate_bydbql"},
		"options": []any{
			map[string]any{"optionId": "allow", "kind": "allow_once"},
			map[string]any{"optionId": "reject", "kind": "reject_once"},
		},
	})
	outcome = mapValue(decision, "outcome")
	if stringValue(outcome, "optionId") != "reject" {
		t.Fatalf("expected lookalike tool permission to be rejected: %+v", decision)
	}
}

func TestTurnStateRecordsOnlyAssistantText(t *testing.T) {
	turn := &turnState{}
	turn.record(agent.Event{Kind: agent.EventKindPlanUpdate, Message: "available commands updated"})
	turn.record(agent.Event{Kind: agent.EventKindMessageDelta, Message: "SELECT * FROM MEASURE m IN g TIME > '-30m' LIMIT 10"})
	message := turn.message()
	if strings.Contains(message, "available commands") {
		t.Fatalf("unexpected plan update in final message: %s", message)
	}
	if message != "SELECT * FROM MEASURE m IN g TIME > '-30m' LIMIT 10" {
		t.Fatalf("unexpected message: %s", message)
	}
}
