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

package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	measurev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/measure/v1"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/pkg/auth"
)

func TestHTTPExecutorDiscoverSchema(t *testing.T) {
	var gotPaths []string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPaths = append(gotPaths, request.URL.Path)
		gotAuth = request.Header.Get("Authorization")
		if strings.HasPrefix(request.URL.Path, "/api/v1/index-rule/schema/lists/") {
			listResponse := &databasev1.IndexRuleRegistryServiceListResponse{
				IndexRule: []*databasev1.IndexRule{
					{
						Metadata: &commonv1.Metadata{Name: "endpoint", Group: "production"},
					},
				},
			}
			body, marshalErr := protojson.Marshal(listResponse)
			if marshalErr != nil {
				t.Fatalf("failed to marshal index rules: %v", marshalErr)
			}
			_, _ = writer.Write(body)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/v1/measure/schema/lists/") {
			listResponse := &databasev1.MeasureRegistryServiceListResponse{
				Measure: []*databasev1.Measure{
					{Metadata: &commonv1.Metadata{Name: "service_latency", Group: "production"}},
					{Metadata: &commonv1.Metadata{Name: "service_cpm", Group: "production"}},
				},
			}
			body, marshalErr := protojson.Marshal(listResponse)
			if marshalErr != nil {
				t.Fatalf("failed to marshal measure list: %v", marshalErr)
			}
			_, _ = writer.Write(body)
			return
		}
		measure := &databasev1.Measure{
			Metadata: &commonv1.Metadata{
				Group: "production",
				Name:  "service_latency",
			},
			TagFamilies: []*databasev1.TagFamilySpec{
				{
					Name: "default",
					Tags: []*databasev1.TagSpec{
						{Name: "service"},
						{Name: "endpoint"},
					},
				},
			},
			Fields: []*databasev1.FieldSpec{
				{Name: "latency"},
				{Name: "cpm"},
			},
		}
		body, marshalErr := protojson.Marshal(measure)
		if marshalErr != nil {
			t.Fatalf("failed to marshal measure: %v", marshalErr)
		}
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	executor := NewHTTPExecutor(HTTPConfig{
		Addr:     server.URL,
		Username: "user",
		Password: "pass",
	})
	snapshot, discoverErr := executor.DiscoverSchema(context.Background(), SchemaRequest{
		Type:   session.ResourceTypeMeasure,
		Name:   "service_latency",
		Groups: []string{"production"},
	})
	if discoverErr != nil {
		t.Fatalf("DiscoverSchema returned error: %v", discoverErr)
	}
	if !containsPath(gotPaths, "/api/v1/measure/schema/production/service_latency") {
		t.Fatalf("unexpected paths: %v", gotPaths)
	}
	if !containsPath(gotPaths, "/api/v1/index-rule/schema/lists/production") {
		t.Fatalf("unexpected paths: %v", gotPaths)
	}
	if gotAuth != auth.GenerateBasicAuthHeader("user", "pass") {
		t.Fatalf("unexpected auth header: %s", gotAuth)
	}
	if !reflect.DeepEqual(snapshot.Tags, []string{"service", "endpoint"}) {
		t.Fatalf("unexpected tags: %v", snapshot.Tags)
	}
	if !reflect.DeepEqual(snapshot.Fields, []string{"latency", "cpm"}) {
		t.Fatalf("unexpected fields: %v", snapshot.Fields)
	}
	if !containsPath(gotPaths, "/api/v1/measure/schema/lists/production") {
		t.Fatalf("unexpected paths: %v", gotPaths)
	}
	if !reflect.DeepEqual(snapshot.IndexedFields, []string{"endpoint"}) {
		t.Fatalf("unexpected indexed fields: %v", snapshot.IndexedFields)
	}
	if !reflect.DeepEqual(snapshot.ResourceNames, []string{"service_latency", "service_cpm"}) {
		t.Fatalf("unexpected resource names: %v", snapshot.ResourceNames)
	}
}

func TestHTTPExecutorFallsBackWhenSchemaUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	executor := NewHTTPExecutor(HTTPConfig{Addr: server.URL})
	snapshot, discoverErr := executor.DiscoverSchema(context.Background(), SchemaRequest{
		Type:   session.ResourceTypeStream,
		Name:   "sw",
		Groups: []string{"default"},
	})
	if discoverErr != nil {
		t.Fatalf("DiscoverSchema returned error: %v", discoverErr)
	}
	if snapshot.Name != "sw" || snapshot.Type != session.ResourceTypeStream {
		t.Fatalf("unexpected fallback snapshot: %+v", snapshot)
	}
	if len(snapshot.Tags) != 0 || len(snapshot.Fields) != 0 {
		t.Fatalf("expected empty fallback schema summary: %+v", snapshot)
	}
}

func TestHTTPExecutorExecuteBydbQL(t *testing.T) {
	var gotPath string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		queryRequest := new(bydbqlv1.QueryRequest)
		if decodeErr := protojson.Unmarshal(readRequestBody(t, request), queryRequest); decodeErr != nil {
			t.Fatalf("failed to decode request: %v", decodeErr)
		}
		gotQuery = queryRequest.GetQuery()
		queryResponse := &bydbqlv1.QueryResponse{
			Result: &bydbqlv1.QueryResponse_MeasureResult{
				MeasureResult: &measurev1.QueryResponse{
					DataPoints: []*measurev1.DataPoint{
						{},
						{},
					},
				},
			},
		}
		body, marshalErr := protojson.Marshal(queryResponse)
		if marshalErr != nil {
			t.Fatalf("failed to marshal query response: %v", marshalErr)
		}
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	executor := NewHTTPExecutor(HTTPConfig{Addr: server.URL})
	executionResult, executeErr := executor.Execute(context.Background(), nil, "SELECT * FROM MEASURE service_latency IN production TIME > '-30m' LIMIT 10")
	if executeErr != nil {
		t.Fatalf("Execute returned error: %v", executeErr)
	}
	if gotPath != "/api/v1/bydbql/query" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotQuery != "SELECT * FROM MEASURE service_latency IN production TIME > '-30m' LIMIT 10" {
		t.Fatalf("unexpected query: %s", gotQuery)
	}
	if executionResult.Rows != 2 {
		t.Fatalf("unexpected rows: %d", executionResult.Rows)
	}
	if executionResult.Command != "POST /api/v1/bydbql/query" || executionResult.Path != "/api/v1/bydbql/query" {
		t.Fatalf("unexpected command summary: %+v", executionResult)
	}
	if executionResult.Response == "" {
		t.Fatal("expected raw response preview")
	}
}

func readRequestBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	body, readErr := io.ReadAll(request.Body)
	if readErr != nil {
		t.Fatalf("failed to read request body: %v", readErr)
	}
	return body
}

func containsPath(paths []string, expected string) bool {
	for _, path := range paths {
		if path == expected {
			return true
		}
	}
	return false
}
