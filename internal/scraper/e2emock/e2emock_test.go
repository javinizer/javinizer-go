package e2emock

import (
	"context"
	"strings"
	"testing"
)

func TestScraper_Search(t *testing.T) {
	s := &Scraper{}
	ctx := context.Background()

	testCases := []struct {
		name       string
		id         string
		wantErr    bool
		errSubstr  string
		wantID     string
		wantTitle  string
		wantActrJP string
	}{
		{"good prefix success", "GOOD-001", false, "", "GOOD-001", "E2E Movie GOOD-001", ""},
		{"good prefix lowercased", "good-002", false, "", "good-002", "E2E Movie good-002", ""},
		{"multi prefix success", "MULTI-001", false, "", "MULTI-001", "E2E Movie MULTI-001", ""},
		{"fail prefix returns verbose error", "FAIL-002", true, "e2emock:", "", "", ""},
		{"fail error mentions id", "FAIL-003", true, "FAIL-003", "", "", ""},
		{"alias prefix success with seeded japanese name", "ALIAS-001", false, "", "ALIAS-001", "E2E Alias Movie ALIAS-001", "朝日芹奈"},
		{"alias prefix lowercased", "alias-009", false, "", "alias-009", "E2E Alias Movie alias-009", "朝日芹奈"},
		{"unrecognized id errors loud", "UNKNOWN-001", true, "unrecognized ID", "", "", ""},
		{"whitespace trimmed", "  GOOD-001  ", false, "", "GOOD-001", "E2E Movie GOOD-001", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.Search(ctx, tc.id)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for id %q, got nil", tc.id)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("error %q must contain %q", err.Error(), tc.errSubstr)
				}
				if res != nil {
					t.Errorf("expected nil result on error, got %+v", res)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for id %q: %v", tc.id, err)
			}
			if res == nil {
				t.Fatalf("expected result for id %q, got nil", tc.id)
			}
			if res.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", res.ID, tc.wantID)
			}
			if res.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", res.Title, tc.wantTitle)
			}
			if res.Source != Name {
				t.Errorf("Source = %q, want %q", res.Source, Name)
			}
			if len(res.Actresses) != 1 {
				t.Fatalf("expected 1 actress, got %d", len(res.Actresses))
			}
			if res.Actresses[0].JapaneseName != tc.wantActrJP {
				t.Errorf("actress JapaneseName = %q, want %q", res.Actresses[0].JapaneseName, tc.wantActrJP)
			}
		})
	}
}

func TestScraper_aliasResult_SeededAliasGroup(t *testing.T) {
	res := aliasResult("ALIAS-001")
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	a := res.Actresses[0]
	if a.JapaneseName != "朝日芹奈" {
		t.Errorf("JapaneseName = %q, want 朝日芹奈 (a seeded alias → canonical 新セリナ)", a.JapaneseName)
	}
	if a.FirstName != "Asahi" || a.LastName != "Serina" {
		t.Errorf("romanized name = %q %q, want Asahi Serina", a.FirstName, a.LastName)
	}
	if a.DMMID == 0 {
		t.Error("DMMID should be non-zero so the actress is distinct from the GOOD-* default (DMMID 1)")
	}
	if !strings.HasPrefix(res.PosterURL, "https://e2e.invalid/") {
		t.Errorf("PosterURL = %q, want e2e.invalid placeholder", res.PosterURL)
	}
}

func TestScraper_GetURL(t *testing.T) {
	s := &Scraper{}
	got, err := s.GetURL(context.Background(), "GOOD-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "GOOD-001") {
		t.Errorf("GetURL = %q, must contain the id", got)
	}
}

func TestScraper_Search_AllSuccessPrefixesShareShape(t *testing.T) {
	s := &Scraper{}
	for _, id := range []string{"GOOD-001", "MULTI-001", "ALIAS-001"} {
		res, err := s.Search(context.Background(), id)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", id, err)
		}
		if res.Maker == "" || res.CoverURL == "" || res.PosterURL == "" {
			t.Errorf("%s: success result must be fully populated (maker/cover/poster), got %+v", id, res)
		}
		if len(res.Genres) == 0 {
			t.Errorf("%s: success result must carry genres", id)
		}
	}
}

func TestScraper_Name_IsEnabled_Close(t *testing.T) {
	s := &Scraper{}
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if !s.IsEnabled() {
		t.Error("IsEnabled() must be true — the e2e binary registers only this scraper")
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
	if cfg := s.Config(); cfg == nil || !cfg.Enabled {
		t.Error("Config() must return enabled settings")
	}
}

func TestScraper_Search_FailErrorNotCollapsedToNoResult(t *testing.T) {
	s := &Scraper{}
	_, err := s.Search(context.Background(), "FAIL-999")
	if err == nil {
		t.Fatal("expected error for FAIL-* id")
	}
	if err.Error() == "no result" {
		t.Error("verbose per-scraper error must not collapse to hardcoded 'no result' (commit 42d89e65)")
	}
	// The error carries the per-scraper "e2emock:" substring so the
	// aggregator surfaces it verbatim instead of a generic fallback.
	if !strings.Contains(err.Error(), "e2emock:") {
		t.Errorf("error %q must carry the e2emock: substring", err.Error())
	}
}
