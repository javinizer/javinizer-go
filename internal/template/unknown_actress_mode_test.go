package template

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestUnknownActressSkipMode_SuppressesAtUnknown(t *testing.T) {
	eng := NewEngine()

	ctx := &Context{
		GroupActress:            true,
		GroupUnknownActressName: "@Unknown",
		UnknownActressMode:      models.UnknownActressModeSkip,
	}

	got, err := eng.Execute("<ACTORS>", ctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got != "" {
		t.Fatalf("skip mode should suppress @Unknown, got %q", got)
	}
}

func TestUnknownActressFallbackMode_InsertsAtUnknown(t *testing.T) {
	eng := NewEngine()

	ctx := &Context{
		GroupActress:            true,
		GroupUnknownActressName: "@Unknown",
		UnknownActressMode:      models.UnknownActressModeFallback,
	}

	got, err := eng.Execute("<ACTORS>", ctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got != "@Unknown" {
		t.Fatalf("fallback mode should insert @Unknown, got %q", got)
	}
}

func TestUnknownActressSkipMode_LoneUnknownActressSuppressed(t *testing.T) {
	eng := NewEngine()

	ctx := &Context{
		GroupActress:            true,
		GroupUnknownActressName: "@Unknown",
		UnknownActressMode:      models.UnknownActressModeSkip,
		Actresses:               []string{"Unknown"},
		ActressDetails:          []ActressDetail{{FirstName: "Unknown"}},
	}

	got, err := eng.Execute("<ACTORS>", ctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got != "" {
		t.Fatalf("skip mode should suppress lone unknown actress, got %q", got)
	}
}

func TestUnknownActressUnsetMode_SuppressesAtUnknown(t *testing.T) {
	eng := NewEngine()

	ctx := &Context{
		GroupActress:            true,
		GroupUnknownActressName: "@Unknown",
	}

	got, err := eng.Execute("<ACTORS>", ctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got != "" {
		t.Fatalf("unset mode defaults to skip, should suppress @Unknown, got %q", got)
	}
}
