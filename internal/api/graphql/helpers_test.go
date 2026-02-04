package graphql

import (
	"testing"
)

func TestValidateJSONPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		// Valid paths
		{"empty path", "", false},
		{"root only", "$", false},
		{"simple field", "$.field", false},
		{"nested field", "$.field.subfield", false},
		{"deep nesting", "$.a.b.c.d", false},
		{"underscore field", "$.my_field", false},
		{"hyphen field", "$.my-field", false},
		{"numeric field", "$.field1", false},
		{"bracket notation string", "$['field']", false},
		{"bracket notation double quote", "$[\"field\"]", false},
		{"bracket notation array index", "$[0]", false},
		{"mixed notation", "$.field['subfield']", false},
		{"array then field", "$[0].field", false},

		// Invalid paths
		{"no dollar sign", "field", true},
		{"invalid start", "field.subfield", true},
		{"trailing dot", "$.field.", true},
		{"empty field after dot", "$..", true},
		{"unclosed bracket", "$[", true},
		{"unclosed bracket with content", "$['field", true},
		{"unclosed string in bracket", "$['field]", true},
		{"empty bracket", "$[]", true},
		{"empty string key", "$['']", true},
		{"invalid char after dollar", "$field", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSONPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateJSONPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFIFOConfig(t *testing.T) {
	tests := []struct {
		name         string
		fifo         bool
		partitionKey string
		wantErr      bool
	}{
		// Valid configurations
		{"fifo disabled, no partition key", false, "", false},
		{"fifo enabled, no partition key", true, "", false},
		{"fifo enabled, valid partition key", true, "$.customer_id", false},
		{"fifo enabled, nested partition key", true, "$.data.order_id", false},

		// Invalid configurations
		{"fifo disabled with partition key", false, "$.field", true},
		{"fifo enabled, invalid partition key", true, "invalid", true},
		{"fifo enabled, empty field path", true, "$.", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFIFOConfig(tt.fifo, tt.partitionKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFIFOConfig(%v, %q) error = %v, wantErr %v", tt.fifo, tt.partitionKey, err, tt.wantErr)
			}
		})
	}
}
