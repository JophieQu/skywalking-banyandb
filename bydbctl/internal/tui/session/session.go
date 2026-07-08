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

// Package session defines the durable state shared by the bydbctl agent TUI workflow and agent adapters.
package session

import (
	"strings"
	"time"
)

// Phase is the deterministic workflow phase owned by bydbctl.
type Phase string

// Workflow phases.
const (
	PhaseIntent     Phase = "intent"
	PhaseAgentDraft Phase = "agent_draft"
	PhaseValidate   Phase = "validate"
	PhaseReady      Phase = "ready"
	PhaseExecuted   Phase = "executed"
	PhaseAccepted   Phase = "accepted"
	PhaseError      Phase = "error"
)

// String returns the phase name.
func (p Phase) String() string {
	return string(p)
}

// ResourceType identifies the BanyanDB resource targeted by a BYDBQL query.
type ResourceType string

// Supported resource types.
const (
	ResourceTypeMeasure  ResourceType = "MEASURE"
	ResourceTypeStream   ResourceType = "STREAM"
	ResourceTypeTrace    ResourceType = "TRACE"
	ResourceTypeProperty ResourceType = "PROPERTY"
	ResourceTypeTopN     ResourceType = "TOPN"
)

// NormalizeResourceType converts user input into a supported resource type.
func NormalizeResourceType(input string) ResourceType {
	switch strings.ToUpper(strings.TrimSpace(input)) {
	case string(ResourceTypeStream):
		return ResourceTypeStream
	case string(ResourceTypeTrace):
		return ResourceTypeTrace
	case string(ResourceTypeProperty):
		return ResourceTypeProperty
	case string(ResourceTypeTopN), "TOP-N", "TOP_N":
		return ResourceTypeTopN
	default:
		return ResourceTypeMeasure
	}
}

// String returns the resource type name.
func (rt ResourceType) String() string {
	return string(rt)
}

// TimeRange stores raw BYDBQL-compatible time bounds.
type TimeRange struct {
	Start string
	End   string
}

// SchemaSnapshot is the schema summary passed across the agent boundary.
type SchemaSnapshot struct {
	UpdatedAt     time.Time
	Type          ResourceType
	Name          string
	Groups        []string
	Tags          []string
	Fields        []string
	IndexedFields []string
	ResourceNames []string
}

// CandidateSource records where a BYDBQL candidate came from.
type CandidateSource string

// Candidate sources.
const (
	CandidateSourceAgent  CandidateSource = "agent"
	CandidateSourceManual CandidateSource = "manual"
)

// BydbqlCandidate is a versioned candidate query and its validation state.
type BydbqlCandidate struct {
	CreatedAt   time.Time
	Validation  ValidationReport
	ID          string
	Query       string
	Explanation string
	Source      CandidateSource
}

// ValidationReport stores local BYDBQL validation output.
type ValidationReport struct {
	CheckedAt time.Time
	Valid     bool
	Message   string
	QueryType string
}

// Status returns a compact validation status.
func (vr ValidationReport) Status() string {
	if vr.Valid {
		return "valid"
	}
	if vr.Message == "" {
		return "not checked"
	}
	return "invalid"
}

// ExecutionResult stores read-only BYDBQL execution output.
type ExecutionResult struct {
	CheckedAt time.Time
	Rows      int
	Summary   string
	Query     string
	Command   string
	Path      string
	Response  string
	Error     string
	Hint      string
}

// TranscriptEntry is one visible agent or workflow event.
type TranscriptEntry struct {
	CreatedAt time.Time
	Role      string
	Content   string
}

// QuerySession is the workflow contract between the TUI, agent gateway, validator, and tool executor.
type QuerySession struct {
	ID              string
	Phase           Phase
	UserGoal        string
	ResourceType    ResourceType
	ResourceName    string
	Groups          []string
	TimeRange       TimeRange
	SchemaSnapshot  SchemaSnapshot
	Candidates      []BydbqlCandidate
	Validation      ValidationReport
	ExecutionResult ExecutionResult
	AcceptedQuery   string
	Transcript      []TranscriptEntry
}

// CurrentCandidate returns the newest candidate query.
func (qs *QuerySession) CurrentCandidate() *BydbqlCandidate {
	if qs == nil || len(qs.Candidates) == 0 {
		return nil
	}
	return &qs.Candidates[len(qs.Candidates)-1]
}

// AddCandidate appends a candidate and updates the session validation summary.
func (qs *QuerySession) AddCandidate(candidate BydbqlCandidate) {
	qs.Candidates = append(qs.Candidates, candidate)
	qs.Validation = candidate.Validation
}

// AddTranscript appends a visible workflow or agent event.
func (qs *QuerySession) AddTranscript(role, content string, createdAt time.Time) {
	if strings.TrimSpace(content) == "" {
		return
	}
	qs.Transcript = append(qs.Transcript, TranscriptEntry{
		Role:      role,
		Content:   content,
		CreatedAt: createdAt,
	})
}
