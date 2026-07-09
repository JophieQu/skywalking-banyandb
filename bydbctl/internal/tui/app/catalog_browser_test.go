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
	"testing"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

func TestCatalogBrowserFilterAndSelect(t *testing.T) {
	browser := newCatalogBrowser()
	browser.setCatalog(session.SchemaCatalog{
		Entries: []session.CatalogEntry{
			{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_endpoint_latency"},
			{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_cpm"},
			{Group: "default", Type: session.ResourceTypeStream, Name: "access_log"},
		},
	})
	browser.setFilter("endpoint")
	if browser.resourceCount() != 1 {
		t.Fatalf("expected one filtered resource, got %d", browser.resourceCount())
	}
	browser.moveCursor(1, 8)
	entry, ok := browser.selectedEntry()
	if !ok || entry.Name != "service_endpoint_latency" {
		t.Fatalf("unexpected selected entry: %+v ok=%v", entry, ok)
	}
}

func TestCatalogBrowserTypeFilterDelta(t *testing.T) {
	browser := newCatalogBrowser()
	browser.setCatalog(session.SchemaCatalog{
		Entries: []session.CatalogEntry{
			{Group: "g", Type: session.ResourceTypeMeasure, Name: "a"},
			{Group: "g", Type: session.ResourceTypeStream, Name: "b"},
		},
	})
	browser.cycleTypeFilterDelta(1)
	if browser.typeFilter != session.ResourceTypeMeasure {
		t.Fatalf("expected MEASURE, got %s", browser.typeFilter)
	}
	browser.cycleTypeFilterDelta(-1)
	if browser.typeFilter != "" {
		t.Fatalf("expected ALL, got %s", browser.typeFilter)
	}
}

func TestCatalogBrowserTypeFilter(t *testing.T) {
	browser := newCatalogBrowser()
	browser.setCatalog(session.SchemaCatalog{
		Entries: []session.CatalogEntry{
			{Group: "sw_metrics", Type: session.ResourceTypeMeasure, Name: "service_cpm"},
			{Group: "default", Type: session.ResourceTypeStream, Name: "access_log"},
		},
	})
	browser.typeFilter = session.ResourceTypeStream
	browser.rebuildRows()
	if browser.resourceCount() != 1 {
		t.Fatalf("expected one stream resource, got %d", browser.resourceCount())
	}
}
