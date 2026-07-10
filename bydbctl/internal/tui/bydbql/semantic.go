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

package bydbql

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	corebydbql "github.com/apache/skywalking-banyandb/pkg/bydbql"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/session"
)

var (
	timeClausePattern    = regexp.MustCompile(`(?i)\bTIME\b`)
	limitClausePattern   = regexp.MustCompile(`(?i)\bLIMIT\s+\d+\b`)
	orderByFieldPattern  = regexp.MustCompile(`(?i)\bORDER\s+BY\s+([A-Za-z_][\w.]*)`)
	topNOrderOnlyPattern = regexp.MustCompile(`(?i)\bORDER\s+BY\s+(ASC|DESC)\s*$`)
)

// SemanticValidator validates BYDBQL syntax and lightweight semantic rules.
type SemanticValidator struct {
	parser *ParserValidator
}

// NewSemanticValidator creates a validator with semantic checks.
func NewSemanticValidator() *SemanticValidator {
	return &SemanticValidator{
		parser: NewParserValidator(),
	}
}

// Validate parses a query and applies semantic checks when schema context is available.
func (validator *SemanticValidator) Validate(_ context.Context, query string, schema *session.SchemaSnapshot) (session.ValidationReport, error) {
	report, validateErr := validator.parser.Validate(context.Background(), query, nil)
	if validateErr != nil || !report.Valid {
		return report, validateErr
	}
	if schema == nil {
		return report, nil
	}
	if semanticMessage := validator.semanticMessage(query, schema); semanticMessage != "" {
		report.Valid = false
		report.Message = semanticMessage
	}
	return report, nil
}

func (validator *SemanticValidator) semanticMessage(query string, schema *session.SchemaSnapshot) string {
	if requiresTimeClause(query) && !timeClausePattern.MatchString(query) {
		return "TIME clause is required for MEASURE, STREAM, TRACE, and SHOW TOP queries"
	}
	if requiresLimitClause(query) && !limitClausePattern.MatchString(query) {
		return "LIMIT clause is required for SELECT queries"
	}
	if orderField := extractOrderByField(query); orderField != "" && len(schema.IndexedFields) > 0 {
		if !containsIndexedField(schema.IndexedFields, orderField) {
			if suggestion := suggestIndexedField(schema.IndexedFields, orderField); suggestion != "" {
				return fmt.Sprintf("ORDER BY field %q is not indexed; use %q or omit ORDER BY", orderField, suggestion)
			}
			return fmt.Sprintf("ORDER BY field %q is not indexed; omit ORDER BY or choose one of: %s", orderField, strings.Join(schema.IndexedFields, ", "))
		}
	}
	if identifierMessage := validateSchemaIdentifiers(query, schema); identifierMessage != "" {
		return identifierMessage
	}
	return ""
}

func requiresLimitClause(query string) bool {
	grammar, parseErr := corebydbql.ParseQuery(query)
	return parseErr == nil && grammar != nil && grammar.Select != nil
}

func requiresTimeClause(query string) bool {
	grammar, parseErr := corebydbql.ParseQuery(query)
	if parseErr != nil || grammar == nil {
		return false
	}
	if grammar.TopN != nil {
		return true
	}
	if grammar.Select == nil {
		return false
	}
	resourceType := strings.ToUpper(strings.TrimSpace(grammar.Select.From.ResourceType))
	return resourceType != session.ResourceTypeProperty.String()
}

func extractOrderByField(query string) string {
	if topNOrderOnlyPattern.MatchString(strings.TrimSpace(query)) {
		return ""
	}
	matches := orderByFieldPattern.FindStringSubmatch(query)
	if len(matches) < 2 {
		return ""
	}
	fieldName := strings.TrimSpace(matches[1])
	if strings.EqualFold(fieldName, "TIME") {
		return ""
	}
	return fieldName
}

func containsIndexedField(indexedFields []string, fieldName string) bool {
	for _, indexedField := range indexedFields {
		if strings.EqualFold(indexedField, fieldName) {
			return true
		}
	}
	return false
}

func suggestIndexedField(indexedFields []string, fieldName string) string {
	lowerField := strings.ToLower(fieldName)
	for _, indexedField := range indexedFields {
		lowerIndexed := strings.ToLower(indexedField)
		if strings.Contains(lowerIndexed, lowerField) || strings.Contains(lowerField, lowerIndexed) {
			return indexedField
		}
	}
	return ""
}
