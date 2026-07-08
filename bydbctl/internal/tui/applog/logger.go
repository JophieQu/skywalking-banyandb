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

// Package applog writes detailed bydbctl agent TUI diagnostics to a session log file.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

const defaultLogDirName = ".bydbctl/logs"

// Logger appends structured session diagnostics to a log file.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// New creates a timestamped agent session log under dir or $HOME/.bydbctl/logs.
func New(dir string) (*Logger, error) {
	logDir := strings.TrimSpace(dir)
	if logDir == "" {
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			logDir = filepath.Join(os.TempDir(), "bydbctl", "logs")
		} else {
			logDir = filepath.Join(homeDir, defaultLogDirName)
		}
	}
	if mkdirErr := os.MkdirAll(logDir, 0o755); mkdirErr != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", mkdirErr)
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("agent-%s.log", time.Now().Format("20060102-150405")))
	logFile, openErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if openErr != nil {
		return nil, fmt.Errorf("failed to open log file: %w", openErr)
	}
	sessionLogger := &Logger{
		file: logFile,
		path: logPath,
	}
	sessionLogger.Write("session", "bydbctl agent TUI session log created")
	return sessionLogger, nil
}

// Path returns the absolute log file path.
func (sessionLogger *Logger) Path() string {
	if sessionLogger == nil {
		return ""
	}
	return sessionLogger.path
}

// Close closes the underlying log file.
func (sessionLogger *Logger) Close() error {
	if sessionLogger == nil || sessionLogger.file == nil {
		return nil
	}
	sessionLogger.mu.Lock()
	defer sessionLogger.mu.Unlock()
	closeErr := sessionLogger.file.Close()
	sessionLogger.file = nil
	return closeErr
}

// Write appends one categorized log line.
func (sessionLogger *Logger) Write(category, message string) {
	if sessionLogger == nil || sessionLogger.file == nil {
		return
	}
	trimmedMessage := strings.TrimSpace(message)
	if trimmedMessage == "" {
		return
	}
	sessionLogger.mu.Lock()
	defer sessionLogger.mu.Unlock()
	_, _ = fmt.Fprintf(sessionLogger.file, "[%s] %s: %s\n", time.Now().Format(time.RFC3339), category, trimmedMessage)
}

// WriteError appends an error with optional wrapped context.
func (sessionLogger *Logger) WriteError(category string, err error) {
	if err == nil {
		return
	}
	sessionLogger.Write(category, err.Error())
}

// WriteAgentEvent appends a full agent event record.
func (sessionLogger *Logger) WriteAgentEvent(event agent.Event) {
	if sessionLogger == nil {
		return
	}
	parts := []string{fmt.Sprintf("kind=%s", event.Kind)}
	if strings.TrimSpace(event.Message) != "" {
		parts = append(parts, "message="+event.Message)
	}
	if strings.TrimSpace(event.Candidate) != "" {
		parts = append(parts, "candidate="+event.Candidate)
	}
	if strings.TrimSpace(event.Explanation) != "" {
		parts = append(parts, "explanation="+event.Explanation)
	}
	if strings.TrimSpace(event.Permission) != "" {
		parts = append(parts, "permission="+event.Permission)
	}
	if event.Err != nil {
		parts = append(parts, "error="+event.Err.Error())
	}
	sessionLogger.Write("agent", strings.Join(parts, " | "))
}

// WriteQuerySession appends the current workflow snapshot.
func (sessionLogger *Logger) WriteQuerySession(querySession *session.QuerySession) {
	if sessionLogger == nil || querySession == nil {
		return
	}
	sessionLogger.Write("session", fmt.Sprintf(
		"phase=%s goal=%q resource=%s/%s groups=%v validation=%s",
		querySession.Phase,
		querySession.UserGoal,
		querySession.ResourceType,
		querySession.ResourceName,
		querySession.Groups,
		querySession.Validation.Message,
	))
	if currentCandidate := querySession.CurrentCandidate(); currentCandidate != nil {
		sessionLogger.Write("candidate", currentCandidate.Query)
	}
	if querySession.ExecutionResult.Summary != "" || querySession.ExecutionResult.Error != "" || querySession.ExecutionResult.Hint != "" {
		executionResult := querySession.ExecutionResult
		sessionLogger.Write("execution", fmt.Sprintf(
			"rows=%d summary=%q error=%q hint=%q response=%s",
			executionResult.Rows,
			executionResult.Summary,
			executionResult.Error,
			executionResult.Hint,
			executionResult.Response,
		))
	}
}

// DisplayPath shortens the log path for the TUI, preferring ~/ when possible.
func DisplayPath(path string) string {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return ""
	}
	homeDir, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return trimmedPath
	}
	if strings.HasPrefix(trimmedPath, homeDir) {
		return "~" + strings.TrimPrefix(trimmedPath, homeDir)
	}
	return trimmedPath
}
