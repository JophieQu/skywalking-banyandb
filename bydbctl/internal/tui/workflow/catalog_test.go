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

package workflow

import (
	"testing"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

func TestRankCatalogCandidatesPaymentEndpoints(t *testing.T) {
	catalog := []session.CatalogEntry{
		{Group: "default", Type: session.ResourceTypeMeasure, Name: "service_cpm"},
		{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_endpoint_latency"},
		{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_endpoint_cpm"},
	}
	ranked := RankCatalogCandidates("top 10 slow payment endpoints in last 30 minutes", catalog, 3)
	if len(ranked) == 0 {
		t.Fatal("expected ranked catalog candidates")
	}
	if ranked[0].Name != "service_endpoint_latency" {
		t.Fatalf("unexpected top candidate: %+v", ranked[0])
	}
}

func TestMatchResourceFromGoalEndpointLatency(t *testing.T) {
	catalog := session.SchemaCatalog{
		Entries: []session.CatalogEntry{
			{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_cpm"},
			{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_endpoint_latency"},
			{Group: "default", Type: session.ResourceTypeStream, Name: "access_log"},
		},
	}
	match := matchResourceFromGoal(
		"top 10 slow payment endpoints in last 30 minutes",
		catalog,
		session.ResourceTypeMeasure,
		"",
		nil,
	)
	if !match.Matched {
		t.Fatal("expected catalog match")
	}
	if match.Name != "service_endpoint_latency" {
		t.Fatalf("unexpected resource name: %s", match.Name)
	}
	if match.Group != "sw_metrics" {
		t.Fatalf("unexpected group: %s", match.Group)
	}
}

func TestResolveSessionSlotsGoalOnly(t *testing.T) {
	catalog := session.SchemaCatalog{
		Entries: []session.CatalogEntry{
			{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_endpoint_latency"},
		},
	}
	resolved := ResolveSessionSlots(StartOptions{
		Goal:         "top 10 slow payment endpoints in last 30 minutes",
		ResourceType: session.ResourceTypeMeasure,
	}, catalog)
	if !resolved.AutoMatched {
		t.Fatal("expected auto match")
	}
	if resolved.ResourceName != "service_endpoint_latency" {
		t.Fatalf("unexpected resource name: %s", resolved.ResourceName)
	}
	if resolved.Groups[0] != "sw_metrics" {
		t.Fatalf("unexpected group: %s", resolved.Groups[0])
	}
	if resolved.SlotsPinned {
		t.Fatal("expected unpinned slots")
	}
}

func TestResolveSessionSlotsPinnedOverridesCatalog(t *testing.T) {
	catalog := session.SchemaCatalog{
		Entries: []session.CatalogEntry{
			{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_endpoint_latency"},
		},
	}
	resolved := ResolveSessionSlots(StartOptions{
		Goal:           "slow endpoints",
		ResourceType:   session.ResourceTypeMeasure,
		ResourceName:   "service_cpm",
		Groups:         []string{"production"},
		NameProvided:   true,
		GroupsProvided: true,
		TypeProvided:   true,
	}, catalog)
	if resolved.AutoMatched {
		t.Fatal("expected pinned slots to skip auto match")
	}
	if resolved.ResourceName != "service_cpm" {
		t.Fatalf("unexpected resource name: %s", resolved.ResourceName)
	}
	if resolved.Groups[0] != "production" {
		t.Fatalf("unexpected group: %s", resolved.Groups[0])
	}
	if !resolved.SlotsPinned {
		t.Fatal("expected pinned slots")
	}
}
