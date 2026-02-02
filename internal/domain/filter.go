package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/PaesslerAG/jsonpath"
)

// FilterOperator represents the comparison operator for a filter condition.
type FilterOperator string

const (
	FilterOpEqual      FilterOperator = "eq"
	FilterOpNotEqual   FilterOperator = "ne"
	FilterOpGreater    FilterOperator = "gt"
	FilterOpGreaterEq  FilterOperator = "gte"
	FilterOpLess       FilterOperator = "lt"
	FilterOpLessEq     FilterOperator = "lte"
	FilterOpContains   FilterOperator = "contains"
	FilterOpStartsWith FilterOperator = "startsWith"
	FilterOpEndsWith   FilterOperator = "endsWith"
	FilterOpExists     FilterOperator = "exists"
	FilterOpRegex      FilterOperator = "regex"
)

// FilterRule represents a single filter condition.
// It evaluates a JSONPath expression against a payload and compares using an operator.
type FilterRule struct {
	Path     string         `json:"path"`     // JSONPath expression (e.g., "$.data.amount")
	Operator FilterOperator `json:"operator"` // Comparison operator
	Value    any            `json:"value"`    // Value to compare against
}

// Filter represents a complete filter expression with logical combinators.
// A filter can be:
// - A single rule (Rule is set)
// - A logical AND of multiple filters (And is set)
// - A logical OR of multiple filters (Or is set)
// - A logical NOT of a single filter (Not is set)
type Filter struct {
	Rule *FilterRule `json:"rule,omitempty"`
	And  []Filter    `json:"and,omitempty"`
	Or   []Filter    `json:"or,omitempty"`
	Not  *Filter     `json:"not,omitempty"`
}

// IsEmpty returns true if the filter has no conditions.
func (f Filter) IsEmpty() bool {
	return f.Rule == nil && len(f.And) == 0 && len(f.Or) == 0 && f.Not == nil
}

// Evaluate evaluates the filter against a JSON payload.
// Returns true if the payload matches the filter conditions.
func (f Filter) Evaluate(payload []byte) (bool, error) {
	if f.IsEmpty() {
		return true, nil // Empty filter matches everything
	}

	var data any
	if err := json.Unmarshal(payload, &data); err != nil {
		return false, fmt.Errorf("invalid JSON payload: %w", err)
	}

	return f.evaluateValue(data)
}

func (f Filter) evaluateValue(data any) (bool, error) {
	// Handle single rule
	if f.Rule != nil {
		return f.Rule.evaluate(data)
	}

	// Handle AND
	if len(f.And) > 0 {
		for _, subFilter := range f.And {
			result, err := subFilter.evaluateValue(data)
			if err != nil {
				return false, err
			}
			if !result {
				return false, nil // Short-circuit: one false means all false
			}
		}
		return true, nil
	}

	// Handle OR
	if len(f.Or) > 0 {
		for _, subFilter := range f.Or {
			result, err := subFilter.evaluateValue(data)
			if err != nil {
				return false, err
			}
			if result {
				return true, nil // Short-circuit: one true means all true
			}
		}
		return false, nil
	}

	// Handle NOT
	if f.Not != nil {
		result, err := f.Not.evaluateValue(data)
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	return true, nil // Empty filter matches everything
}

func (r *FilterRule) evaluate(data any) (bool, error) {
	// Get value at path
	value, err := jsonpath.Get(r.Path, data)
	if err != nil {
		// Path not found - handle based on operator
		if r.Operator == FilterOpExists {
			return r.Value == false, nil // exists: false matches when path doesn't exist
		}
		return false, nil // Path not found means no match for other operators
	}

	// Handle exists operator
	if r.Operator == FilterOpExists {
		expectedExists, ok := r.Value.(bool)
		if !ok {
			return false, fmt.Errorf("exists operator requires boolean value")
		}
		return expectedExists, nil // Path exists, so return expected value
	}

	return r.compare(value)
}

func (r *FilterRule) compare(actual any) (bool, error) {
	switch r.Operator {
	case FilterOpEqual:
		return compareEqual(actual, r.Value), nil
	case FilterOpNotEqual:
		return !compareEqual(actual, r.Value), nil
	case FilterOpGreater:
		return compareNumeric(actual, r.Value, func(a, b float64) bool { return a > b })
	case FilterOpGreaterEq:
		return compareNumeric(actual, r.Value, func(a, b float64) bool { return a >= b })
	case FilterOpLess:
		return compareNumeric(actual, r.Value, func(a, b float64) bool { return a < b })
	case FilterOpLessEq:
		return compareNumeric(actual, r.Value, func(a, b float64) bool { return a <= b })
	case FilterOpContains:
		return compareString(actual, r.Value, strings.Contains)
	case FilterOpStartsWith:
		return compareString(actual, r.Value, strings.HasPrefix)
	case FilterOpEndsWith:
		return compareString(actual, r.Value, strings.HasSuffix)
	case FilterOpRegex:
		return compareRegex(actual, r.Value)
	default:
		return false, fmt.Errorf("unknown operator: %s", r.Operator)
	}
}

func compareEqual(actual, expected any) bool {
	// Handle numeric comparison (JSON numbers are float64)
	if actualNum, ok := toFloat64(actual); ok {
		if expectedNum, ok := toFloat64(expected); ok {
			return actualNum == expectedNum
		}
	}

	// Handle string comparison
	if actualStr, ok := actual.(string); ok {
		if expectedStr, ok := expected.(string); ok {
			return actualStr == expectedStr
		}
	}

	// Handle bool comparison
	if actualBool, ok := actual.(bool); ok {
		if expectedBool, ok := expected.(bool); ok {
			return actualBool == expectedBool
		}
	}

	// Handle nil comparison
	if actual == nil && expected == nil {
		return true
	}

	// Fallback to string comparison
	return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", expected)
}

func compareNumeric(actual, expected any, cmp func(a, b float64) bool) (bool, error) {
	actualNum, ok := toFloat64(actual)
	if !ok {
		return false, nil // Not a number, no match
	}

	expectedNum, ok := toFloat64(expected)
	if !ok {
		return false, fmt.Errorf("expected numeric value for comparison")
	}

	return cmp(actualNum, expectedNum), nil
}

func compareString(actual, expected any, cmp func(s, substr string) bool) (bool, error) {
	actualStr, ok := actual.(string)
	if !ok {
		actualStr = fmt.Sprintf("%v", actual)
	}

	expectedStr, ok := expected.(string)
	if !ok {
		return false, fmt.Errorf("expected string value for comparison")
	}

	return cmp(actualStr, expectedStr), nil
}

func compareRegex(actual, expected any) (bool, error) {
	actualStr, ok := actual.(string)
	if !ok {
		actualStr = fmt.Sprintf("%v", actual)
	}

	pattern, ok := expected.(string)
	if !ok {
		return false, fmt.Errorf("expected string pattern for regex comparison")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("invalid regex pattern: %w", err)
	}

	return re.MatchString(actualStr), nil
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// ValidateFilter validates that a filter is well-formed.
func ValidateFilter(filter Filter) error {
	return validateFilterRecursive(filter, 0)
}

func validateFilterRecursive(filter Filter, depth int) error {
	const maxDepth = 10
	if depth > maxDepth {
		return fmt.Errorf("filter nesting too deep (max %d levels)", maxDepth)
	}

	// Count how many fields are set
	setCount := 0
	if filter.Rule != nil {
		setCount++
	}
	if len(filter.And) > 0 {
		setCount++
	}
	if len(filter.Or) > 0 {
		setCount++
	}
	if filter.Not != nil {
		setCount++
	}

	if setCount > 1 {
		return fmt.Errorf("filter can only have one of: rule, and, or, not")
	}

	// Validate rule
	if filter.Rule != nil {
		if err := validateFilterRule(filter.Rule); err != nil {
			return err
		}
	}

	// Validate nested filters
	for i, subFilter := range filter.And {
		if err := validateFilterRecursive(subFilter, depth+1); err != nil {
			return fmt.Errorf("and[%d]: %w", i, err)
		}
	}

	for i, subFilter := range filter.Or {
		if err := validateFilterRecursive(subFilter, depth+1); err != nil {
			return fmt.Errorf("or[%d]: %w", i, err)
		}
	}

	if filter.Not != nil {
		if err := validateFilterRecursive(*filter.Not, depth+1); err != nil {
			return fmt.Errorf("not: %w", err)
		}
	}

	return nil
}

func validateFilterRule(rule *FilterRule) error {
	if rule.Path == "" {
		return fmt.Errorf("filter rule path is required")
	}

	// Validate path starts with $
	if !strings.HasPrefix(rule.Path, "$") {
		return fmt.Errorf("filter path must start with $")
	}

	// Validate operator
	switch rule.Operator {
	case FilterOpEqual, FilterOpNotEqual, FilterOpGreater, FilterOpGreaterEq,
		FilterOpLess, FilterOpLessEq, FilterOpContains, FilterOpStartsWith,
		FilterOpEndsWith, FilterOpExists, FilterOpRegex:
		// Valid operator
	case "":
		return fmt.Errorf("filter rule operator is required")
	default:
		return fmt.Errorf("unknown filter operator: %s", rule.Operator)
	}

	// Validate regex pattern if regex operator
	if rule.Operator == FilterOpRegex {
		pattern, ok := rule.Value.(string)
		if !ok {
			return fmt.Errorf("regex operator requires string pattern")
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	// Validate exists operator value
	if rule.Operator == FilterOpExists {
		if _, ok := rule.Value.(bool); !ok {
			return fmt.Errorf("exists operator requires boolean value")
		}
	}

	return nil
}

// ParseFilter parses a JSON filter into a Filter struct.
func ParseFilter(data []byte) (Filter, error) {
	if len(data) == 0 {
		return Filter{}, nil
	}

	var filter Filter
	if err := json.Unmarshal(data, &filter); err != nil {
		return Filter{}, fmt.Errorf("invalid filter JSON: %w", err)
	}

	if err := ValidateFilter(filter); err != nil {
		return Filter{}, err
	}

	return filter, nil
}

// MarshalFilter marshals a Filter to JSON.
func MarshalFilter(filter Filter) ([]byte, error) {
	if filter.IsEmpty() {
		return nil, nil
	}
	return json.Marshal(filter)
}

// FilterContext provides context for filter evaluation.
type FilterContext struct {
	context.Context
	EventType string
	ClientID  string
}
