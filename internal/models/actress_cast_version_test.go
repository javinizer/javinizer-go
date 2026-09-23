package models

import "testing"

func TestActressCastVersionCanonicalizesOrderAndFields(t *testing.T) {
	actresses := []Actress{
		{ID: 2, DMMID: 20, FirstName: "Beta", LastName: "Actress", NameKey: "beta-actress"},
		{ID: 1, DMMID: 10, FirstName: "Alpha", LastName: "Actress", NameKey: "alpha-actress"},
		{ID: 1, DMMID: 11, FirstName: "Alpha", LastName: "Other", NameKey: "alpha-other"},
	}
	reordered := []Actress{actresses[2], actresses[0], actresses[1]}
	version := ActressCastVersion(actresses)
	if got := ActressCastVersion(reordered); got != version {
		t.Fatalf("reordered actresses changed cast version: got %q want %q", got, version)
	}
	if actresses[0].ID != 2 {
		t.Fatalf("ActressCastVersion mutated input order")
	}
	changed := append([]Actress(nil), actresses...)
	changed[0].ThumbURL = "https://example.invalid/changed.jpg"
	if got := ActressCastVersion(changed); got == version {
		t.Fatalf("cast field change did not change version: %q", got)
	}
}
