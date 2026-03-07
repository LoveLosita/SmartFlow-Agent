package security

import "testing"

func TestValidateReadOnlySQL(t *testing.T) {
	validator := NewSQLValidator("smartflow", true, []string{"smartflow"}, []string{"smartflow.users", "smartflow.tasks"})

	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{name: "allow select", sql: "SELECT id, name FROM users WHERE id = 1", wantErr: false},
		{name: "allow explain", sql: "EXPLAIN SELECT * FROM tasks", wantErr: false},
		{name: "reject insert", sql: "INSERT INTO users(name) VALUES('x')", wantErr: true},
		{name: "reject multi statement", sql: "SELECT * FROM users; SELECT * FROM tasks", wantErr: true},
		{name: "reject comment", sql: "SELECT * FROM users -- bypass", wantErr: true},
		{name: "reject not whitelisted table", sql: "SELECT * FROM orders", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.ValidateReadOnlySQL(tc.sql)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	masked := RedactSQL("SELECT * FROM users WHERE token='abc123' AND id=42")
	if masked == "" || masked == "SELECT * FROM users WHERE token='abc123' AND id=42" {
		t.Fatalf("redaction not applied: %s", masked)
	}

	key := RedactKey("very-sensitive-key")
	if key == "very-sensitive-key" {
		t.Fatalf("key not redacted")
	}
}
