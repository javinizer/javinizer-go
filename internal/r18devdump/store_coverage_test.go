package r18devdump

import (
	"testing"
)

func TestListActressesNotOpen(t *testing.T) {
	store := &Store{}
	_, err := store.ListActresses(nil)
	if err == nil {
		t.Fatal("expected error for unopened store")
	}
}
