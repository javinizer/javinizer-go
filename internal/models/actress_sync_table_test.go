package models

import "testing"

func TestActressSyncJobTableName(t *testing.T) {
	j := ActressSyncJob{}
	if got := j.TableName(); got != "actress_sync_jobs" {
		t.Fatalf("expected %q, got %q", "actress_sync_jobs", got)
	}
}

func TestActressSyncTaskTableName(t *testing.T) {
	tk := ActressSyncTask{}
	if got := tk.TableName(); got != "actress_sync_tasks" {
		t.Fatalf("expected %q, got %q", "actress_sync_tasks", got)
	}
}

func TestSplitFullName(t *testing.T) {
	first, last := SplitFullName("John Doe")
	if first != "John" || last != "Doe" {
		t.Fatalf("expected John/Doe, got %s/%s", first, last)
	}
	first, last = SplitFullName("")
	if first != "" || last != "" {
		t.Fatalf("expected empty, got %s/%s", first, last)
	}
	first, last = SplitFullName("Single")
	if first != "Single" || last != "" {
		t.Fatalf("expected Single/empty, got %s/%s", first, last)
	}
	first, last = SplitFullName("John Michael Doe")
	if first != "John" || last != "Michael Doe" {
		t.Fatalf("expected John/Michael Doe, got %s/%s", first, last)
	}
}

func TestNormalizeActressNameKey(t *testing.T) {
	if got := NormalizeActressNameKey("John Doe"); got != "john doe" {
		t.Fatalf("expected 'john doe', got %q", got)
	}
	if got := NormalizeActressNameKey(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := NormalizeActressNameKey("  Test   Name  "); got != "test name" {
		t.Fatalf("expected 'test name', got %q", got)
	}
}
