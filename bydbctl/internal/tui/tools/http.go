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
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"google.golang.org/protobuf/encoding/protojson"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
	"github.com/apache/skywalking-banyandb/pkg/auth"
)

const (
	defaultHTTPTimeout = 3 * time.Second
	measureSchemaPath  = "/api/v1/measure/schema/{group}/{name}"
	streamSchemaPath   = "/api/v1/stream/schema/{group}/{name}"
	traceSchemaPath    = "/api/v1/trace/schema/{group}/{name}"
	propertySchemaPath = "/api/v1/property/schema/{group}/{name}"
	topnSchemaPath     = "/api/v1/topn-agg/schema/{group}/{name}"
	measureListPath    = "/api/v1/measure/schema/lists/{group}"
	streamListPath     = "/api/v1/stream/schema/lists/{group}"
	traceListPath      = "/api/v1/trace/schema/lists/{group}"
	propertyListPath   = "/api/v1/property/schema/lists/{group}"
	topnListPath       = "/api/v1/topn-agg/schema/lists/{group}"
	indexRuleListPath  = "/api/v1/index-rule/schema/lists/{group}"
	bydbqlQueryPath    = "/api/v1/bydbql/query"
)

// HTTPConfig configures schema discovery through BanyanDB's HTTP API.
type HTTPConfig struct {
	Timeout  time.Duration
	Addr     string
	Username string
	Password string
}

// HTTPExecutor discovers schema through BanyanDB's read-only HTTP endpoints.
type HTTPExecutor struct {
	client   *resty.Client
	fallback *ReadOnlyExecutor
	config   HTTPConfig
	now      func() time.Time
}

// NewHTTPExecutor creates a read-only HTTP executor.
func NewHTTPExecutor(config HTTPConfig) *HTTPExecutor {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	client := resty.New().SetTimeout(timeout)
	return &HTTPExecutor{
		client:   client,
		fallback: NewReadOnlyExecutor(),
		config: HTTPConfig{
			Timeout:  timeout,
			Addr:     strings.TrimRight(config.Addr, "/"),
			Username: config.Username,
			Password: config.Password,
		},
		now: time.Now,
	}
}

// DiscoverSchema fetches and summarizes a resource schema, falling back to a local snapshot when unavailable.
func (executor *HTTPExecutor) DiscoverSchema(ctx context.Context, req SchemaRequest) (session.SchemaSnapshot, error) {
	fallbackSnapshot, fallbackErr := executor.fallback.DiscoverSchema(ctx, req)
	if fallbackErr != nil {
		return session.SchemaSnapshot{}, fallbackErr
	}
	snapshot := fallbackSnapshot
	if executor.config.Addr != "" && len(req.Groups) > 0 {
		if resourceNames, listErr := executor.listResources(ctx, req.Groups[0], req.Type); listErr == nil {
			snapshot.ResourceNames = resourceNames
		}
		if indexedFields, indexErr := executor.discoverIndexedFields(ctx, req.Groups[0]); indexErr == nil {
			snapshot.IndexedFields = indexedFields
		}
	}
	if executor.config.Addr == "" || req.Name == "" || len(req.Groups) == 0 {
		return snapshot, nil
	}
	path, pathErr := schemaPath(req.Type)
	if pathErr != nil {
		return snapshot, nil
	}
	request := executor.client.R().
		SetContext(ctx).
		SetPathParam("group", req.Groups[0]).
		SetPathParam("name", req.Name).
		SetHeader("Accept", "application/json")
	if authHeader := executor.authHeader(); authHeader != "" {
		request.SetHeader("Authorization", authHeader)
	}
	response, requestErr := request.Get(executor.config.Addr + path)
	if requestErr != nil || response.StatusCode() != http.StatusOK {
		return snapshot, nil
	}
	schemaSnapshot, summarizeErr := summarizeSchema(req, response.Body(), executor.now())
	if summarizeErr != nil {
		return snapshot, nil
	}
	if len(snapshot.ResourceNames) > 0 {
		schemaSnapshot.ResourceNames = snapshot.ResourceNames
	}
	if len(snapshot.IndexedFields) > 0 {
		schemaSnapshot.IndexedFields = snapshot.IndexedFields
	}
	return schemaSnapshot, nil
}

func (executor *HTTPExecutor) listResources(ctx context.Context, group string, resourceType session.ResourceType) ([]string, error) {
	listPath, listErr := resourceListPath(resourceType)
	if listErr != nil {
		return nil, listErr
	}
	request := executor.client.R().
		SetContext(ctx).
		SetPathParam("group", group).
		SetHeader("Accept", "application/json")
	if authHeader := executor.authHeader(); authHeader != "" {
		request.SetHeader("Authorization", authHeader)
	}
	response, requestErr := request.Get(executor.config.Addr + listPath)
	if requestErr != nil || response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("resource list unavailable")
	}
	return resourceNamesFromList(resourceType, response.Body())
}

func (executor *HTTPExecutor) discoverIndexedFields(ctx context.Context, group string) ([]string, error) {
	request := executor.client.R().
		SetContext(ctx).
		SetPathParam("group", group).
		SetHeader("Accept", "application/json")
	if authHeader := executor.authHeader(); authHeader != "" {
		request.SetHeader("Authorization", authHeader)
	}
	response, requestErr := request.Get(executor.config.Addr + indexRuleListPath)
	if requestErr != nil || response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("index rule list unavailable")
	}
	listResponse := new(databasev1.IndexRuleRegistryServiceListResponse)
	if unmarshalErr := protojson.Unmarshal(response.Body(), listResponse); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	var indexedFields []string
	for _, indexRule := range listResponse.GetIndexRule() {
		if indexRule.GetNoSort() {
			continue
		}
		if ruleName := strings.TrimSpace(indexRule.GetMetadata().GetName()); ruleName != "" {
			indexedFields = append(indexedFields, ruleName)
		}
	}
	return compactStrings(indexedFields), nil
}

// Execute runs a read-only BYDBQL query through the BanyanDB HTTP gateway.
func (executor *HTTPExecutor) Execute(ctx context.Context, querySession *session.QuerySession, query string) (session.ExecutionResult, error) {
	if executor.config.Addr == "" {
		return executor.fallback.Execute(ctx, querySession, query)
	}
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return session.ExecutionResult{}, fmt.Errorf("BYDBQL query is required")
	}
	requestBody, marshalErr := protojson.Marshal(&bydbqlv1.QueryRequest{Query: trimmedQuery})
	if marshalErr != nil {
		return session.ExecutionResult{}, fmt.Errorf("failed to marshal BYDBQL request: %w", marshalErr)
	}
	request := executor.client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		SetBody(requestBody)
	if authHeader := executor.authHeader(); authHeader != "" {
		request.SetHeader("Authorization", authHeader)
	}
	response, requestErr := request.Post(executor.config.Addr + bydbqlQueryPath)
	if requestErr != nil {
		executionResult := session.ExecutionResult{
			CheckedAt: executor.now(),
			Query:     trimmedQuery,
			Command:   "POST " + bydbqlQueryPath,
			Path:      bydbqlQueryPath,
			Error:     requestErr.Error(),
		}
		return executionResult, fmt.Errorf("failed to execute BYDBQL query: %w", requestErr)
	}
	rawResponse := strings.TrimSpace(string(response.Body()))
	executionResult := session.ExecutionResult{
		CheckedAt: executor.now(),
		Query:     trimmedQuery,
		Command:   "POST " + bydbqlQueryPath,
		Path:      bydbqlQueryPath,
		Response:  rawResponse,
	}
	if response.StatusCode() != http.StatusOK {
		executionResult.Error = truncateBody(rawResponse)
		return executionResult, fmt.Errorf("BYDBQL query returned HTTP %d: %s", response.StatusCode(), executionResult.Error)
	}
	queryResponse := new(bydbqlv1.QueryResponse)
	if unmarshalErr := protojson.Unmarshal(response.Body(), queryResponse); unmarshalErr != nil {
		executionResult.Error = unmarshalErr.Error()
		return executionResult, fmt.Errorf("failed to decode BYDBQL response: %w", unmarshalErr)
	}
	rows, resultType := responseRows(queryResponse)
	executionResult.Rows = rows
	executionResult.Summary = fmt.Sprintf("executed %s BYDBQL query through %s; rows=%d", resultType, bydbqlQueryPath, rows)
	if rows == 0 {
		executionResult.Hint = "query returned zero rows; consider widening the TIME range or verifying resource name, group, and filters"
	}
	return executionResult, nil
}

func (executor *HTTPExecutor) authHeader() string {
	if executor.config.Username == "" && executor.config.Password == "" {
		return ""
	}
	return auth.GenerateBasicAuthHeader(executor.config.Username, executor.config.Password)
}

func schemaPath(resourceType session.ResourceType) (string, error) {
	switch resourceType {
	case session.ResourceTypeMeasure:
		return measureSchemaPath, nil
	case session.ResourceTypeStream:
		return streamSchemaPath, nil
	case session.ResourceTypeTrace:
		return traceSchemaPath, nil
	case session.ResourceTypeProperty:
		return propertySchemaPath, nil
	case session.ResourceTypeTopN:
		return topnSchemaPath, nil
	default:
		return "", fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

func resourceListPath(resourceType session.ResourceType) (string, error) {
	switch resourceType {
	case session.ResourceTypeMeasure:
		return measureListPath, nil
	case session.ResourceTypeStream:
		return streamListPath, nil
	case session.ResourceTypeTrace:
		return traceListPath, nil
	case session.ResourceTypeProperty:
		return propertyListPath, nil
	case session.ResourceTypeTopN:
		return topnListPath, nil
	default:
		return "", fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

func resourceNamesFromList(resourceType session.ResourceType, body []byte) ([]string, error) {
	switch resourceType {
	case session.ResourceTypeMeasure:
		listResponse := new(databasev1.MeasureRegistryServiceListResponse)
		if unmarshalErr := protojson.Unmarshal(body, listResponse); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		return metadataNames(extractMeasureMetadata(listResponse.GetMeasure())), nil
	case session.ResourceTypeStream:
		listResponse := new(databasev1.StreamRegistryServiceListResponse)
		if unmarshalErr := protojson.Unmarshal(body, listResponse); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		return metadataNames(extractStreamMetadata(listResponse.GetStream())), nil
	case session.ResourceTypeTrace:
		listResponse := new(databasev1.TraceRegistryServiceListResponse)
		if unmarshalErr := protojson.Unmarshal(body, listResponse); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		return metadataNames(extractTraceMetadata(listResponse.GetTrace())), nil
	case session.ResourceTypeProperty:
		listResponse := new(databasev1.PropertyRegistryServiceListResponse)
		if unmarshalErr := protojson.Unmarshal(body, listResponse); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		return metadataNames(extractPropertyMetadata(listResponse.GetProperties())), nil
	case session.ResourceTypeTopN:
		listResponse := new(databasev1.TopNAggregationRegistryServiceListResponse)
		if unmarshalErr := protojson.Unmarshal(body, listResponse); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		return metadataNames(extractTopNMetadata(listResponse.GetTopNAggregation())), nil
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

func extractMeasureMetadata(measures []*databasev1.Measure) []*commonv1.Metadata {
	return extractMetadata(len(measures), func(idx int) *commonv1.Metadata {
		if measures[idx] == nil {
			return nil
		}
		return measures[idx].GetMetadata()
	})
}

func extractStreamMetadata(streams []*databasev1.Stream) []*commonv1.Metadata {
	return extractMetadata(len(streams), func(idx int) *commonv1.Metadata {
		if streams[idx] == nil {
			return nil
		}
		return streams[idx].GetMetadata()
	})
}

func extractTraceMetadata(traces []*databasev1.Trace) []*commonv1.Metadata {
	return extractMetadata(len(traces), func(idx int) *commonv1.Metadata {
		if traces[idx] == nil {
			return nil
		}
		return traces[idx].GetMetadata()
	})
}

func extractPropertyMetadata(properties []*databasev1.Property) []*commonv1.Metadata {
	return extractMetadata(len(properties), func(idx int) *commonv1.Metadata {
		if properties[idx] == nil {
			return nil
		}
		return properties[idx].GetMetadata()
	})
}

func extractTopNMetadata(topNItems []*databasev1.TopNAggregation) []*commonv1.Metadata {
	return extractMetadata(len(topNItems), func(idx int) *commonv1.Metadata {
		if topNItems[idx] == nil {
			return nil
		}
		return topNItems[idx].GetMetadata()
	})
}

func extractMetadata(count int, at func(int) *commonv1.Metadata) []*commonv1.Metadata {
	metadataItems := make([]*commonv1.Metadata, 0, count)
	for idx := 0; idx < count; idx++ {
		metadataItems = append(metadataItems, at(idx))
	}
	return metadataItems
}

func metadataNames(metadataItems []*commonv1.Metadata) []string {
	var names []string
	for _, metadata := range metadataItems {
		if metadata == nil {
			continue
		}
		if name := strings.TrimSpace(metadata.GetName()); name != "" {
			names = append(names, name)
		}
	}
	return compactStrings(names)
}

func summarizeSchema(req SchemaRequest, body []byte, updatedAt time.Time) (session.SchemaSnapshot, error) {
	switch req.Type {
	case session.ResourceTypeMeasure:
		measure := new(databasev1.Measure)
		if unmarshalErr := protojson.Unmarshal(body, measure); unmarshalErr != nil {
			return session.SchemaSnapshot{}, unmarshalErr
		}
		return session.SchemaSnapshot{
			UpdatedAt: updatedAt,
			Type:      req.Type,
			Name:      req.Name,
			Groups:    append([]string(nil), req.Groups...),
			Tags:      tagFamilies(measure.GetTagFamilies()),
			Fields:    fieldNames(measure.GetFields()),
		}, nil
	case session.ResourceTypeStream:
		stream := new(databasev1.Stream)
		if unmarshalErr := protojson.Unmarshal(body, stream); unmarshalErr != nil {
			return session.SchemaSnapshot{}, unmarshalErr
		}
		return session.SchemaSnapshot{
			UpdatedAt: updatedAt,
			Type:      req.Type,
			Name:      req.Name,
			Groups:    append([]string(nil), req.Groups...),
			Tags:      tagFamilies(stream.GetTagFamilies()),
		}, nil
	case session.ResourceTypeTrace:
		trace := new(databasev1.Trace)
		if unmarshalErr := protojson.Unmarshal(body, trace); unmarshalErr != nil {
			return session.SchemaSnapshot{}, unmarshalErr
		}
		return session.SchemaSnapshot{
			UpdatedAt: updatedAt,
			Type:      req.Type,
			Name:      req.Name,
			Groups:    append([]string(nil), req.Groups...),
			Tags:      traceTagNames(trace.GetTags()),
		}, nil
	case session.ResourceTypeProperty:
		property := new(databasev1.Property)
		if unmarshalErr := protojson.Unmarshal(body, property); unmarshalErr != nil {
			return session.SchemaSnapshot{}, unmarshalErr
		}
		return session.SchemaSnapshot{
			UpdatedAt: updatedAt,
			Type:      req.Type,
			Name:      req.Name,
			Groups:    append([]string(nil), req.Groups...),
			Tags:      tagNames(property.GetTags()),
		}, nil
	case session.ResourceTypeTopN:
		topN := new(databasev1.TopNAggregation)
		if unmarshalErr := protojson.Unmarshal(body, topN); unmarshalErr != nil {
			return session.SchemaSnapshot{}, unmarshalErr
		}
		return session.SchemaSnapshot{
			UpdatedAt: updatedAt,
			Type:      req.Type,
			Name:      req.Name,
			Groups:    append([]string(nil), req.Groups...),
			Tags:      append([]string(nil), topN.GetGroupByTagNames()...),
			Fields:    compactStrings([]string{topN.GetFieldName()}),
		}, nil
	default:
		return session.SchemaSnapshot{}, fmt.Errorf("unsupported resource type: %s", req.Type)
	}
}

func tagFamilies(families []*databasev1.TagFamilySpec) []string {
	var tags []string
	for _, family := range families {
		tags = append(tags, tagNames(family.GetTags())...)
	}
	return compactStrings(tags)
}

func tagNames(tags []*databasev1.TagSpec) []string {
	var names []string
	for _, tag := range tags {
		names = append(names, tag.GetName())
	}
	return compactStrings(names)
}

func traceTagNames(tags []*databasev1.TraceTagSpec) []string {
	var names []string
	for _, tag := range tags {
		names = append(names, tag.GetName())
	}
	return compactStrings(names)
}

func fieldNames(fields []*databasev1.FieldSpec) []string {
	var names []string
	for _, field := range fields {
		names = append(names, field.GetName())
	}
	return compactStrings(names)
}

func compactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	var compactedValues []string
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			continue
		}
		if _, ok := seen[trimmedValue]; ok {
			continue
		}
		seen[trimmedValue] = struct{}{}
		compactedValues = append(compactedValues, trimmedValue)
	}
	return compactedValues
}

func responseRows(response *bydbqlv1.QueryResponse) (int, string) {
	if measureResult := response.GetMeasureResult(); measureResult != nil {
		return len(measureResult.GetDataPoints()), "measure"
	}
	if streamResult := response.GetStreamResult(); streamResult != nil {
		return len(streamResult.GetElements()), "stream"
	}
	if propertyResult := response.GetPropertyResult(); propertyResult != nil {
		return len(propertyResult.GetProperties()), "property"
	}
	if traceResult := response.GetTraceResult(); traceResult != nil {
		if len(traceResult.GetTraces()) > 0 {
			return len(traceResult.GetTraces()), "trace"
		}
		if traceResult.GetTraceQueryResult() != nil {
			return 1, "trace"
		}
		return 0, "trace"
	}
	if topNResult := response.GetTopnResult(); topNResult != nil {
		rows := 0
		for _, topNList := range topNResult.GetLists() {
			rows += len(topNList.GetItems())
		}
		return rows, "topn"
	}
	return 0, "unknown"
}

func truncateBody(value string) string {
	const maxBodyLength = 300
	if len(value) <= maxBodyLength {
		return value
	}
	return value[:maxBodyLength] + "..."
}
