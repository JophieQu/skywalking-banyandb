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

package approval

import (
	"context"
	"testing"
	"time"
)

func TestControllerReturnsOnlyTheDecisionForTheExactPendingRequest(t *testing.T) {
	controller := NewController()
	decisionCh := make(chan Decision, 1)
	errCh := make(chan error, 1)
	go func() {
		decision, requestErr := controller.Request(context.Background(), Request{
			Query:     "CREATE MEASURE test_latency IN production",
			Resource:  "MEASURE/latency",
			Groups:    []string{"production"},
			TimeRange: "TIME > '-30m'",
			Limit:     "10",
			Source:    SourceAgentTool,
		})
		decisionCh <- decision
		errCh <- requestErr
	}()

	request := receiveRequest(t, controller.Requests())
	if request.ID == "" {
		t.Fatal("expected a request ID")
	}
	if request.Query != "CREATE MEASURE test_latency IN production" {
		t.Fatalf("unexpected query: %s", request.Query)
	}
	if resolveErr := controller.Resolve(request.ID, Decision{Approved: true}); resolveErr != nil {
		t.Fatalf("failed to resolve approval: %v", resolveErr)
	}
	if requestErr := <-errCh; requestErr != nil {
		t.Fatalf("Request returned error: %v", requestErr)
	}
	if decision := <-decisionCh; !decision.Approved {
		t.Fatal("expected approved decision")
	}
	if resolveErr := controller.Resolve(request.ID, Decision{Approved: true}); resolveErr == nil {
		t.Fatal("expected consumed request to reject a second decision")
	}
}

func TestControllerCancelsPendingRequestWhenContextEnds(t *testing.T) {
	controller := NewController()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, requestErr := controller.Request(ctx, Request{Query: "CREATE MEASURE test_property IN default", Source: SourceManual})
		errCh <- requestErr
	}()

	request := receiveRequest(t, controller.Requests())
	cancel()
	if requestErr := <-errCh; requestErr == nil {
		t.Fatal("expected cancelled request to return an error")
	}
	if resolveErr := controller.Resolve(request.ID, Decision{Approved: true}); resolveErr == nil {
		t.Fatal("expected cancelled request to be removed")
	}
}

func TestNewRequestSummarizesApprovalConstraintsFromTheExactQuery(t *testing.T) {
	request := NewRequest(
		"SHOW TOP 25 FROM MEASURE latency IN production TIME BETWEEN '-30m' AND 'now' AGGREGATE BY SUM ORDER BY DESC",
		"MEASURE/latency",
		[]string{"production"},
		SourceManual,
	)
	if request.TimeRange != "TIME BETWEEN '-30m' AND 'now'" {
		t.Fatalf("unexpected time range: %s", request.TimeRange)
	}
	if request.Limit != "25" {
		t.Fatalf("unexpected TOPN limit: %s", request.Limit)
	}
}

func receiveRequest(t *testing.T, requests <-chan Request) Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval request")
		return Request{}
	}
}
