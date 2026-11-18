package domain

import (
	"errors"
	"testing"
)

func TestIsValidUUID(t *testing.T) {
	t.Parallel()

	valid := []string{
		"123e4567-e89b-12d3-a456-426614174000",
		"123E4567-E89B-12D3-A456-426614174000",
	}
	invalid := []string{
		"",
		"not-a-uuid",
		"123e4567e89b12d3a456426614174000",
		"123e4567-e89b-12d3-a456-42661417400g",
		"123e4567-e89b-12d3-a456_426614174000",
		"123e4567-e89b-12d3-a456-4266141740000",
		"'; DROP TABLE chatbot.messages; --",
	}

	for _, s := range valid {
		if !IsValidUUID(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range invalid {
		if IsValidUUID(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestRoleValid(t *testing.T) {
	t.Parallel()

	if !RoleUser.Valid() || !RoleAssistant.Valid() {
		t.Fatal("expected user and assistant roles to be valid")
	}
	for _, r := range []Role{"", "system", "User", "admin"} {
		if r.Valid() {
			t.Errorf("expected role %q to be invalid", r)
		}
	}
}

func TestPageValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		page    Page
		wantErr bool
	}{
		{"min limit", Page{Limit: 1, Offset: 0}, false},
		{"max limit", Page{Limit: MaxPageLimit, Offset: 500}, false},
		{"zero limit", Page{Limit: 0}, true},
		{"negative limit", Page{Limit: -1}, true},
		{"limit above max", Page{Limit: MaxPageLimit + 1}, true},
		{"negative offset", Page{Limit: 10, Offset: -1}, true},
	}

	for _, tc := range tests {
		err := tc.page.Validate()
		if tc.wantErr && !errors.Is(err, ErrInvalidPagination) {
			t.Errorf("%s: expected ErrInvalidPagination, got %v", tc.name, err)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: expected no error, got %v", tc.name, err)
		}
	}
}
