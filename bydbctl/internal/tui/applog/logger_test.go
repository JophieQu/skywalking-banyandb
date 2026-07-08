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

package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
)

func TestNewWritesSessionLog(t *testing.T) {
	tempDir := t.TempDir()
	sessionLog, createErr := New(tempDir)
	if createErr != nil {
		t.Fatalf("failed to create session log: %v", createErr)
	}
	defer func() {
		_ = sessionLog.Close()
	}()
	sessionLog.WriteAgentTurn([]agent.Event{
		{Kind: agent.EventKindMessageDelta, Message: "agent raw output"},
		{Kind: agent.EventKindFinalResponse, Message: "agent raw output"},
	})
	sessionLog.WriteError("workflow", os.ErrInvalid)
	logBytes, readErr := os.ReadFile(sessionLog.Path())
	if readErr != nil {
		t.Fatalf("failed to read log file: %v", readErr)
	}
	logContent := string(logBytes)
	for _, expected := range []string{"agent raw output", "workflow", os.ErrInvalid.Error()} {
		if !strings.Contains(logContent, expected) {
			t.Fatalf("expected log to contain %q:\n%s", expected, logContent)
		}
	}
	if !strings.HasPrefix(sessionLog.Path(), filepath.Join(tempDir, "agent-")) {
		t.Fatalf("unexpected log path: %s", sessionLog.Path())
	}
}
