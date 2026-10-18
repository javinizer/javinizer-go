package nfo

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// mergeCreditPresentation keeps the durable credit ledger separate from NFO
// presentation. The legacy actress merge remains available to review/JSON
// consumers, but cannot overwrite explicit credit choices during rendering.
func mergeCreditPresentation(merged, scraped, existing *models.Movie, nfoActors []models.Actress, strategy MergeStrategy, scraperOnly, useCreditedName bool) error {
	merged.Credits = (&models.Movie{Credits: scraped.Credits}).Clone().Credits
	if len(nfoActors) == 0 || scraperOnly {
		return nil
	}
	links, parsed := existing.NFOActorLinks()
	if len(merged.Credits) == 0 {
		return bindPresentationLinks(scraped, nfoActors, links, nfoCastComponents(nfoActors, nil))
	}
	projection, err := buildMergedCreditPresentation(scraped, nfoActors, strategy, links, parsed, useCreditedName)
	if err != nil {
		return err
	}
	merged.SetRenderCredits(projection)
	return nil
}

// CastAmbiguityError blocks unsafe legacy cast imports and invalid presentation
// references. Callers must not turn it into a successful scraper-only fallback.
type CastAmbiguityError struct {
	Actors []string
	Reason string
}

func (e *CastAmbiguityError) Error() string {
	return fmt.Sprintf("NFO cast requires review (%s): %s. Remove stale NFO actor entries or add genuinely new actors in the movie editor, then retry; ForceOverwrite deliberately replaces existing NFO metadata", e.Reason, strings.Join(e.Actors, ", "))
}

func castAmbiguity(actor models.Actress, reason string) error {
	return &CastAmbiguityError{Actors: []string{models.FormatActressName(actor, models.FormatActressNameOptions{FirstNameOrder: true})}, Reason: reason}
}

func hasOwnedCastSelection(credits []models.MovieCredit, useCreditedName bool) bool {
	for _, c := range credits {
		if c.Origin == string(models.CreditOriginUser) || c.Suppressed || c.UserOverride || c.OverrideName != "" || c.DisplayForceCanonical || (useCreditedName && (c.CreditedName != "" || c.CreditedJapaneseName != "")) {
			return true
		}
	}
	return false
}

func buildMergedCreditPresentation(source *models.Movie, nfoActors []models.Actress, strategy MergeStrategy, links [][]models.ActorPresentationLink, parsed, useCreditedName bool) ([]models.MovieCredit, error) {
	credits := (&models.Movie{Credits: source.Credits}).Clone().Credits
	projection := make([]models.MovieCredit, 0, len(credits)+len(nfoActors))
	projectionIndex := make([]int, len(credits))
	keys := make(map[string]map[int]struct{})
	for i, credit := range credits {
		projectionIndex[i] = -1
		addCastKeys(keys, creditPresentationKeys(credit), i)
		if credit.RenderVisible() {
			projectionIndex[i] = len(projection)
			projection = append(projection, credit)
		}
	}
	matched := make(map[int]bool)
	preferNFO := strategy == PreferNFO || strategy == PreserveExisting || strategy == FillMissingOnly
	components := nfoCastComponents(nfoActors, keys)
	if err := bindPresentationLinks(source, nfoActors, links, components); err != nil {
		return nil, err
	}
	owned := parsed && hasOwnedCastSelection(credits, useCreditedName)
	seenComponents := make(map[*nfoCastComponent]bool)
	for row, nfoActor := range nfoActors {
		component := components[row]
		if len(actressPresentationKeys(nfoActor)) == 0 || seenComponents[component] {
			continue
		}
		seenComponents[component] = true
		candidates := component.owners
		if len(candidates) > 1 {
			if component.linked {
				return nil, castAmbiguity(nfoActor, "presentation link disagrees with actor names or another link")
			}
			logging.Warnf("NFO actor %q matches multiple existing credits; keeping movie credit selections. Correct or remove the ambiguous NFO actor name before merging it.", models.FormatActressName(nfoActor, models.FormatActressNameOptions{FirstNameOrder: true}))
			continue
		}
		if len(candidates) == 1 {
			for index := range candidates {
				// Hidden credits still own their known names. An NFO row cannot
				// revive suppression/quarantine or resolve a missing identity.
				if projectionIndex[index] < 0 || matched[index] {
					continue
				}
				matched[index] = true
				credit := &projection[projectionIndex[index]]
				if credit.DisplayForceCanonical || component.selectedName {
					if credit.Actress.ThumbURL == "" {
						credit.Actress.ThumbURL = nfoActor.ThumbURL
					}
				} else {
					merged := applyActressMergeStrategy([]actressMatch{{actress: *credit.Actress, fromScraper: true, partner: &nfoActor}}, preferNFO)
					credit.Actress = &merged[0]
				}
			}
			continue
		}
		if owned {
			return nil, castAmbiguity(nfoActor, "unlinked actor may be a previous movie-only name")
		}
		// Unmatched NFO actors are presentation only. Never import their IDs,
		// verification, aliases, ownership, or other identity metadata.
		actor := models.Actress{FirstName: nfoActor.FirstName, LastName: nfoActor.LastName, JapaneseName: nfoActor.JapaneseName, ThumbURL: nfoActor.ThumbURL}
		projection = append(projection, models.MovieCredit{Actress: &actor, Source: "nfo", OrderIndex: len(projection)})
	}
	return projection, nil
}

func addCastKeys(index map[string]map[int]struct{}, keys []string, actor int) {
	for _, key := range keys {
		if index[key] == nil {
			index[key] = make(map[int]struct{})
		}
		index[key][actor] = struct{}{}
	}
}

func creditPresentationKeys(credit models.MovieCredit) []string {
	keys := normalizedCastKeys(credit.CreditedName, credit.CreditedJapaneseName, credit.OverrideName)
	if credit.Actress != nil {
		keys = append(keys, actressPresentationKeys(*credit.Actress)...)
	}
	return keys
}

func actressPresentationKeys(actor models.Actress) []string {
	return normalizedCastKeys(actor.JapaneseName, strings.TrimSpace(actor.FirstName+" "+actor.LastName), strings.TrimSpace(actor.LastName+" "+actor.FirstName))
}

func normalizedCastKeys(names ...string) []string {
	keys := make([]string, 0, len(names))
	for _, name := range names {
		if key := models.NormalizeActressNameKey(name); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

// Components are computed before rendering any row: a later row may connect
// an otherwise unknown name to a suppressed credit or to multiple owners.
type nfoCastComponent struct {
	owners       map[int]struct{}
	linked       bool
	selectedName bool
}

func nfoCastComponents(actors []models.Actress, sourceKeys map[string]map[int]struct{}) []*nfoCastComponent {
	parents := make([]int, len(actors))
	for i := range parents {
		parents[i] = i
	}
	root := func(i int) int {
		for parents[i] != i {
			i = parents[i]
		}
		return i
	}
	first := make(map[string]int)
	for i, actor := range actors {
		for _, key := range actressPresentationKeys(actor) {
			if other, exists := first[key]; exists {
				parents[root(i)] = root(other)
			} else {
				first[key] = i
			}
		}
	}
	components := make([]*nfoCastComponent, len(actors))
	byRoot := make(map[int]*nfoCastComponent)
	for i, actor := range actors {
		r := root(i)
		component := byRoot[r]
		if component == nil {
			component = &nfoCastComponent{owners: make(map[int]struct{})}
			byRoot[r] = component
		}
		components[i] = component
		for _, key := range actressPresentationKeys(actor) {
			for owner := range sourceKeys[key] {
				component.owners[owner] = struct{}{}
			}
		}
	}
	return components
}

// References only select from already supplied same-movie credits. They never
// create or resolve an identity, including when the linked credit is hidden.
func bindPresentationLinks(source *models.Movie, actors []models.Actress, links [][]models.ActorPresentationLink, components []*nfoCastComponent) error {
	for row, rowLinks := range links {
		if len(rowLinks) == 0 {
			continue
		}
		if row >= len(actors) {
			return &CastAmbiguityError{Reason: "presentation links do not match actor rows"}
		}
		if len(rowLinks) != 1 {
			return castAmbiguity(actors[row], "duplicate presentation links")
		}
		link := rowLinks[0]
		id, err := strconv.ParseUint(link.CreditID, 10, 64)
		if err != nil || id == 0 || link.Version != "1" || source.ContentID == "" || link.MovieContentID != source.ContentID {
			return castAmbiguity(actors[row], "invalid or cross-movie presentation link")
		}
		owner := -1
		for i, credit := range source.Credits {
			if uint64(credit.ID) == id && credit.MovieContentID == source.ContentID {
				owner = i
				break
			}
		}
		if owner < 0 {
			return castAmbiguity(actors[row], "presentation link references a missing credit")
		}
		component := components[row]
		if link.Selection != "canonical" && link.Selection != "override" && link.Selection != "credited" {
			return castAmbiguity(actors[row], "unknown presentation selection")
		}
		component.selectedName = component.selectedName || link.Selection == "override" || link.Selection == "credited"
		component.linked = true
		component.owners[owner] = struct{}{}
	}
	return nil
}

// A generated row can intentionally have the same display name as another
// source credit. Do not publish a single-owner link for a multi-owner name
// component: re-reading our unchanged output must retain source choices rather
// than reject its own deduplicated presentation as a conflicting claim.
func stripAmbiguousGeneratedLinks(actors []actor, authority []models.MovieCredit, contentID string) {
	keys := make(map[string]map[int]struct{})
	for i, credit := range authority {
		addCastKeys(keys, creditPresentationKeys(credit), i)
	}
	rows := make([]models.Actress, len(actors))
	for i, a := range actors {
		rows[i] = parseActorToActress(a)
	}
	components := nfoCastComponents(rows, keys)
	for i, a := range actors {
		if len(a.PresentationLinks) == 0 {
			continue
		}
		link := a.PresentationLinks[0]
		owner := -1
		for j, c := range authority {
			if c.ID != 0 && c.MovieContentID == contentID && strconv.FormatUint(uint64(c.ID), 10) == link.CreditID {
				owner = j
				break
			}
		}
		if owner < 0 {
			actors[i].PresentationLinks = nil
			continue
		}
		components[i].owners[owner] = struct{}{}
	}
	for i := range actors {
		if len(components[i].owners) > 1 {
			actors[i].PresentationLinks = nil
		}
	}
}
