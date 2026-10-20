package service

import (
	"context"
	"strings"
	"testing"
)

func TestValidKey(t *testing.T) {
	for _, key := range []string{"abcdefgh", "a_B-123456", strings.Repeat("x", 80)} {
		if !ValidKey(key) {
			t.Errorf("reject %q", key)
		}
	}
	for _, key := range []string{"", "short", "contains space", strings.Repeat("x", 81), "кириллица"} {
		if ValidKey(key) {
			t.Errorf("accept %q", key)
		}
	}
}
func TestInvalidReserveDoesNotCallStore(t *testing.T) {
	s := &Service{}
	for _, pair := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}} {
		if _, _, err := s.Reserve(context.Background(), pair[0], pair[1], "valid-key"); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Reserve(context.Background(), 1, 1, "x"); err != ErrInvalid {
		t.Fatal(err)
	}
}
