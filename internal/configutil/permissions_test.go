package configutil

import (
	"os"
	"testing"
)

func TestApplyUmask_DefaultZero(t *testing.T) {
	result := ApplyUmask(FilePerm)
	if result != FilePerm {
		t.Errorf("ApplyUmask with default cached umask 0: got %o, want %o", result, FilePerm)
	}
}

func TestStoreUmask_AndApplyUmask(t *testing.T) {
	original := cachedUmask.Load()
	defer cachedUmask.Store(original)

	StoreUmask(0o002)
	result := ApplyUmask(FilePerm)
	expected := os.FileMode(FilePerm &^ 0o002)
	if result != expected {
		t.Errorf("ApplyUmask(FilePerm) with umask 002: got %o, want %o", result, expected)
	}

	StoreUmask(0o022)
	result = ApplyUmask(DirPerm)
	expected = os.FileMode(DirPerm &^ 0o022)
	if result != expected {
		t.Errorf("ApplyUmask(DirPerm) with umask 022: got %o, want %o", result, expected)
	}

	StoreUmask(0o077)
	result = ApplyUmask(FilePerm)
	expected = os.FileMode(FilePerm &^ 0o077)
	if result != expected {
		t.Errorf("ApplyUmask(FilePerm) with umask 077: got %o, want %o", result, expected)
	}
}

func TestApplyUmask_ZeroUmask(t *testing.T) {
	original := cachedUmask.Load()
	defer cachedUmask.Store(original)

	StoreUmask(0)
	result := ApplyUmask(FilePerm)
	if result != FilePerm {
		t.Errorf("ApplyUmask with umask 0: got %o, want %o", result, FilePerm)
	}
}

func TestApplyUmask_RestrictiveUmask(t *testing.T) {
	original := cachedUmask.Load()
	defer cachedUmask.Store(original)

	StoreUmask(0o777)
	result := ApplyUmask(FilePerm)
	if result != 0 {
		t.Errorf("ApplyUmask with umask 777: got %o, want 0", result)
	}
}

func TestApplyUmask_DirPerm(t *testing.T) {
	original := cachedUmask.Load()
	defer cachedUmask.Store(original)

	StoreUmask(0o002)
	result := ApplyUmask(DirPerm)
	expected := os.FileMode(0o775)
	if result != expected {
		t.Errorf("ApplyUmask(DirPerm) with umask 002: got %o, want %o", result, expected)
	}
}
