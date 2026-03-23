package cmd

import (
	"io"
	"strings"
	"testing"
)

func TestReadPasswordFromPipe(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name:  "reads password from pipe",
			input: "secret123\n",
			want:  "secret123",
		},
		{
			name:  "trims trailing newline",
			input: "mypassword\n",
			want:  "mypassword",
		},
		{
			name:  "trims trailing CRLF",
			input: "mypassword\r\n",
			want:  "mypassword",
		},
		{
			name:  "handles no trailing newline",
			input: "mypassword",
			want:  "mypassword",
		},
		{
			name:    "rejects empty input",
			input:   "\n",
			wantErr: "password is required",
		},
		{
			name:    "rejects whitespace-only input",
			input:   "  \n",
			wantErr: "password is required",
		},
		{
			name:    "rejects EOF with no data",
			input:   "",
			wantErr: "password is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			got, err := readPasswordFromPipe(r)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadPasswordFromPipeLargeInput(t *testing.T) {
	// Only first line should be used as the password.
	input := "firstline\nsecondline\n"
	r := strings.NewReader(input)
	got, err := readPasswordFromPipe(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "firstline" {
		t.Errorf("got %q, want %q", got, "firstline")
	}
}

// Verify the function signature accepts io.Reader.
var _ func(io.Reader) (string, error) = readPasswordFromPipe
