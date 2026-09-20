package ids

import (
	"strings"
	"testing"
)

func TestNewIsUUIDv7Shape(t *testing.T) {
	id := New()
	if len(id) != 36 {
		t.Fatalf("expected 36 chars, got %d (%q)", len(id), id)
	}
	if id[14] != '7' {
		t.Fatalf("expected version 7 nibble, got %q", id[14])
	}
	if strings.ToLower(id) != id {
		t.Fatalf("expected lowercase id, got %q", id)
	}
}

func TestNewIsUniqueAndSortable(t *testing.T) {
	seen := map[string]bool{}
	prev := ""
	for i := 0; i < 500; i++ {
		id := New()
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
		if prev != "" && id < prev {
			// IDs generated in the same millisecond may tie; equal is allowed,
			// strictly decreasing is not.
			if id != prev {
				t.Fatalf("ids not sortable: %q < %q", id, prev)
			}
		}
		prev = id
	}
}
