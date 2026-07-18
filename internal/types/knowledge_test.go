package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKnowledge_GetFolderID(t *testing.T) {
	tests := []struct {
		name string
		k    *Knowledge
		want string
	}{
		{
			name: "nil pointer returns empty",
			k:    nil,
			want: "",
		},
		{
			name: "nil FolderID (root) returns empty",
			k:    &Knowledge{FolderID: nil},
			want: "",
		},
		{
			name: "set FolderID returns the value",
			k:    &Knowledge{FolderID: strPtr("folder-123")},
			want: "folder-123",
		},
		{
			name: "empty string FolderID returns empty",
			k:    &Knowledge{FolderID: strPtr("")},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.k.GetFolderID())
		})
	}
}

func TestFolderIDPtrToString(t *testing.T) {
	tests := []struct {
		name string
		s    *string
		want string
	}{
		{name: "nil returns empty", s: nil, want: ""},
		{name: "set pointer returns value", s: strPtr("abc"), want: "abc"},
		{name: "empty string pointer returns empty", s: strPtr(""), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FolderIDPtrToString(tt.s))
		})
	}
}
