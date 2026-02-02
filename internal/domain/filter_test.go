package domain

import (
	"encoding/json"
	"testing"
)

func TestFilterRule_Evaluate_Equal(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		rule     FilterRule
		expected bool
	}{
		{
			name:    "string equal match",
			payload: `{"status": "active"}`,
			rule:    FilterRule{Path: "$.status", Operator: FilterOpEqual, Value: "active"},
			expected: true,
		},
		{
			name:    "string equal no match",
			payload: `{"status": "inactive"}`,
			rule:    FilterRule{Path: "$.status", Operator: FilterOpEqual, Value: "active"},
			expected: false,
		},
		{
			name:    "number equal match",
			payload: `{"amount": 100}`,
			rule:    FilterRule{Path: "$.amount", Operator: FilterOpEqual, Value: float64(100)},
			expected: true,
		},
		{
			name:    "boolean equal match",
			payload: `{"enabled": true}`,
			rule:    FilterRule{Path: "$.enabled", Operator: FilterOpEqual, Value: true},
			expected: true,
		},
		{
			name:    "nested path equal match",
			payload: `{"data": {"user": {"id": "123"}}}`,
			rule:    FilterRule{Path: "$.data.user.id", Operator: FilterOpEqual, Value: "123"},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter := Filter{Rule: &tc.rule}
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilterRule_Evaluate_NotEqual(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		rule     FilterRule
		expected bool
	}{
		{
			name:    "not equal match",
			payload: `{"status": "inactive"}`,
			rule:    FilterRule{Path: "$.status", Operator: FilterOpNotEqual, Value: "active"},
			expected: true,
		},
		{
			name:    "not equal no match",
			payload: `{"status": "active"}`,
			rule:    FilterRule{Path: "$.status", Operator: FilterOpNotEqual, Value: "active"},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter := Filter{Rule: &tc.rule}
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilterRule_Evaluate_Numeric(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		rule     FilterRule
		expected bool
	}{
		{
			name:    "greater than match",
			payload: `{"amount": 150}`,
			rule:    FilterRule{Path: "$.amount", Operator: FilterOpGreater, Value: float64(100)},
			expected: true,
		},
		{
			name:    "greater than no match",
			payload: `{"amount": 50}`,
			rule:    FilterRule{Path: "$.amount", Operator: FilterOpGreater, Value: float64(100)},
			expected: false,
		},
		{
			name:    "greater than equal match",
			payload: `{"amount": 100}`,
			rule:    FilterRule{Path: "$.amount", Operator: FilterOpGreaterEq, Value: float64(100)},
			expected: true,
		},
		{
			name:    "less than match",
			payload: `{"amount": 50}`,
			rule:    FilterRule{Path: "$.amount", Operator: FilterOpLess, Value: float64(100)},
			expected: true,
		},
		{
			name:    "less than equal match",
			payload: `{"amount": 100}`,
			rule:    FilterRule{Path: "$.amount", Operator: FilterOpLessEq, Value: float64(100)},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter := Filter{Rule: &tc.rule}
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilterRule_Evaluate_String(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		rule     FilterRule
		expected bool
	}{
		{
			name:    "contains match",
			payload: `{"message": "hello world"}`,
			rule:    FilterRule{Path: "$.message", Operator: FilterOpContains, Value: "world"},
			expected: true,
		},
		{
			name:    "contains no match",
			payload: `{"message": "hello"}`,
			rule:    FilterRule{Path: "$.message", Operator: FilterOpContains, Value: "world"},
			expected: false,
		},
		{
			name:    "starts with match",
			payload: `{"message": "hello world"}`,
			rule:    FilterRule{Path: "$.message", Operator: FilterOpStartsWith, Value: "hello"},
			expected: true,
		},
		{
			name:    "ends with match",
			payload: `{"message": "hello world"}`,
			rule:    FilterRule{Path: "$.message", Operator: FilterOpEndsWith, Value: "world"},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter := Filter{Rule: &tc.rule}
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilterRule_Evaluate_Regex(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		rule     FilterRule
		expected bool
	}{
		{
			name:    "regex match",
			payload: `{"email": "test@example.com"}`,
			rule:    FilterRule{Path: "$.email", Operator: FilterOpRegex, Value: `^[a-z]+@[a-z]+\.[a-z]+$`},
			expected: true,
		},
		{
			name:    "regex no match",
			payload: `{"email": "invalid"}`,
			rule:    FilterRule{Path: "$.email", Operator: FilterOpRegex, Value: `^[a-z]+@[a-z]+\.[a-z]+$`},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter := Filter{Rule: &tc.rule}
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilterRule_Evaluate_Exists(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		rule     FilterRule
		expected bool
	}{
		{
			name:    "exists true - field present",
			payload: `{"name": "John"}`,
			rule:    FilterRule{Path: "$.name", Operator: FilterOpExists, Value: true},
			expected: true,
		},
		{
			name:    "exists true - field absent",
			payload: `{"other": "value"}`,
			rule:    FilterRule{Path: "$.name", Operator: FilterOpExists, Value: true},
			expected: false,
		},
		{
			name:    "exists false - field absent",
			payload: `{"other": "value"}`,
			rule:    FilterRule{Path: "$.name", Operator: FilterOpExists, Value: false},
			expected: true,
		},
		{
			name:    "exists false - field present",
			payload: `{"name": "John"}`,
			rule:    FilterRule{Path: "$.name", Operator: FilterOpExists, Value: false},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter := Filter{Rule: &tc.rule}
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilter_Evaluate_And(t *testing.T) {
	filter := Filter{
		And: []Filter{
			{Rule: &FilterRule{Path: "$.status", Operator: FilterOpEqual, Value: "active"}},
			{Rule: &FilterRule{Path: "$.amount", Operator: FilterOpGreater, Value: float64(100)}},
		},
	}

	tests := []struct {
		name     string
		payload  string
		expected bool
	}{
		{
			name:     "both conditions match",
			payload:  `{"status": "active", "amount": 150}`,
			expected: true,
		},
		{
			name:     "first condition fails",
			payload:  `{"status": "inactive", "amount": 150}`,
			expected: false,
		},
		{
			name:     "second condition fails",
			payload:  `{"status": "active", "amount": 50}`,
			expected: false,
		},
		{
			name:     "both conditions fail",
			payload:  `{"status": "inactive", "amount": 50}`,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilter_Evaluate_Or(t *testing.T) {
	filter := Filter{
		Or: []Filter{
			{Rule: &FilterRule{Path: "$.country", Operator: FilterOpEqual, Value: "US"}},
			{Rule: &FilterRule{Path: "$.country", Operator: FilterOpEqual, Value: "CA"}},
		},
	}

	tests := []struct {
		name     string
		payload  string
		expected bool
	}{
		{
			name:     "first condition matches",
			payload:  `{"country": "US"}`,
			expected: true,
		},
		{
			name:     "second condition matches",
			payload:  `{"country": "CA"}`,
			expected: true,
		},
		{
			name:     "no condition matches",
			payload:  `{"country": "UK"}`,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilter_Evaluate_Not(t *testing.T) {
	filter := Filter{
		Not: &Filter{
			Rule: &FilterRule{Path: "$.status", Operator: FilterOpEqual, Value: "deleted"},
		},
	}

	tests := []struct {
		name     string
		payload  string
		expected bool
	}{
		{
			name:     "condition is false - not makes it true",
			payload:  `{"status": "active"}`,
			expected: true,
		},
		{
			name:     "condition is true - not makes it false",
			payload:  `{"status": "deleted"}`,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilter_Evaluate_Complex(t *testing.T) {
	// Complex filter: (status == "active" AND amount > 100) OR country IN ["US", "CA"]
	filter := Filter{
		Or: []Filter{
			{
				And: []Filter{
					{Rule: &FilterRule{Path: "$.status", Operator: FilterOpEqual, Value: "active"}},
					{Rule: &FilterRule{Path: "$.amount", Operator: FilterOpGreater, Value: float64(100)}},
				},
			},
			{
				Or: []Filter{
					{Rule: &FilterRule{Path: "$.country", Operator: FilterOpEqual, Value: "US"}},
					{Rule: &FilterRule{Path: "$.country", Operator: FilterOpEqual, Value: "CA"}},
				},
			},
		},
	}

	tests := []struct {
		name     string
		payload  string
		expected bool
	}{
		{
			name:     "status and amount match",
			payload:  `{"status": "active", "amount": 150, "country": "UK"}`,
			expected: true,
		},
		{
			name:     "country US matches",
			payload:  `{"status": "inactive", "amount": 50, "country": "US"}`,
			expected: true,
		},
		{
			name:     "country CA matches",
			payload:  `{"status": "inactive", "amount": 50, "country": "CA"}`,
			expected: true,
		},
		{
			name:     "nothing matches",
			payload:  `{"status": "inactive", "amount": 50, "country": "UK"}`,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := filter.Evaluate([]byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestFilter_Evaluate_EmptyFilter(t *testing.T) {
	filter := Filter{}

	result, err := filter.Evaluate([]byte(`{"any": "data"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("empty filter should match everything")
	}
}

func TestFilter_Evaluate_PathNotFound(t *testing.T) {
	filter := Filter{
		Rule: &FilterRule{Path: "$.nonexistent", Operator: FilterOpEqual, Value: "value"},
	}

	result, err := filter.Evaluate([]byte(`{"other": "data"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("filter with non-existent path should not match")
	}
}

func TestFilter_Evaluate_InvalidJSON(t *testing.T) {
	filter := Filter{
		Rule: &FilterRule{Path: "$.status", Operator: FilterOpEqual, Value: "active"},
	}

	_, err := filter.Evaluate([]byte(`invalid json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestValidateFilter(t *testing.T) {
	tests := []struct {
		name        string
		filter      Filter
		expectError bool
	}{
		{
			name:        "valid simple rule",
			filter:      Filter{Rule: &FilterRule{Path: "$.status", Operator: FilterOpEqual, Value: "active"}},
			expectError: false,
		},
		{
			name:        "valid and filter",
			filter:      Filter{And: []Filter{{Rule: &FilterRule{Path: "$.a", Operator: FilterOpEqual, Value: "b"}}}},
			expectError: false,
		},
		{
			name:        "missing path",
			filter:      Filter{Rule: &FilterRule{Path: "", Operator: FilterOpEqual, Value: "active"}},
			expectError: true,
		},
		{
			name:        "missing operator",
			filter:      Filter{Rule: &FilterRule{Path: "$.status", Operator: "", Value: "active"}},
			expectError: true,
		},
		{
			name:        "invalid path - no dollar sign",
			filter:      Filter{Rule: &FilterRule{Path: "status", Operator: FilterOpEqual, Value: "active"}},
			expectError: true,
		},
		{
			name:        "invalid operator",
			filter:      Filter{Rule: &FilterRule{Path: "$.status", Operator: "invalid", Value: "active"}},
			expectError: true,
		},
		{
			name:        "invalid regex pattern",
			filter:      Filter{Rule: &FilterRule{Path: "$.status", Operator: FilterOpRegex, Value: "[invalid"}},
			expectError: true,
		},
		{
			name:        "exists with non-boolean value",
			filter:      Filter{Rule: &FilterRule{Path: "$.status", Operator: FilterOpExists, Value: "true"}},
			expectError: true,
		},
		{
			name: "multiple fields set",
			filter: Filter{
				Rule: &FilterRule{Path: "$.a", Operator: FilterOpEqual, Value: "b"},
				And:  []Filter{{Rule: &FilterRule{Path: "$.c", Operator: FilterOpEqual, Value: "d"}}},
			},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFilter(tc.filter)
			if tc.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "valid filter JSON",
			input:       `{"rule": {"path": "$.status", "operator": "eq", "value": "active"}}`,
			expectError: false,
		},
		{
			name:        "empty input",
			input:       "",
			expectError: false,
		},
		{
			name:        "invalid JSON",
			input:       `{invalid}`,
			expectError: true,
		},
		{
			name:        "valid complex filter",
			input:       `{"and": [{"rule": {"path": "$.a", "operator": "eq", "value": "b"}}, {"rule": {"path": "$.c", "operator": "gt", "value": 100}}]}`,
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFilter([]byte(tc.input))
			if tc.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestFilter_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		filter   Filter
		expected bool
	}{
		{
			name:     "empty filter",
			filter:   Filter{},
			expected: true,
		},
		{
			name:     "filter with rule",
			filter:   Filter{Rule: &FilterRule{Path: "$.a", Operator: FilterOpEqual, Value: "b"}},
			expected: false,
		},
		{
			name:     "filter with and",
			filter:   Filter{And: []Filter{}},
			expected: true,
		},
		{
			name:     "filter with non-empty and",
			filter:   Filter{And: []Filter{{Rule: &FilterRule{Path: "$.a", Operator: FilterOpEqual, Value: "b"}}}},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.filter.IsEmpty()
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestEndpoint_MatchesFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   []byte
		payload  []byte
		expected bool
	}{
		{
			name:     "no filter matches everything",
			filter:   nil,
			payload:  []byte(`{"any": "data"}`),
			expected: true,
		},
		{
			name:     "empty filter matches everything",
			filter:   []byte(`{}`),
			payload:  []byte(`{"any": "data"}`),
			expected: true,
		},
		{
			name:     "filter matches",
			filter:   []byte(`{"rule": {"path": "$.status", "operator": "eq", "value": "active"}}`),
			payload:  []byte(`{"status": "active"}`),
			expected: true,
		},
		{
			name:     "filter does not match",
			filter:   []byte(`{"rule": {"path": "$.status", "operator": "eq", "value": "active"}}`),
			payload:  []byte(`{"status": "inactive"}`),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := Endpoint{Filter: tc.filter}
			result, err := endpoint.MatchesFilter(tc.payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestMarshalFilter(t *testing.T) {
	filter := Filter{
		Rule: &FilterRule{
			Path:     "$.status",
			Operator: FilterOpEqual,
			Value:    "active",
		},
	}

	data, err := MarshalFilter(filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed Filter
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.Rule == nil {
		t.Fatal("expected rule to be set")
	}
	if parsed.Rule.Path != "$.status" {
		t.Errorf("expected path $.status, got %s", parsed.Rule.Path)
	}
}

func TestMarshalFilter_Empty(t *testing.T) {
	filter := Filter{}

	data, err := MarshalFilter(filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil for empty filter, got %s", string(data))
	}
}
