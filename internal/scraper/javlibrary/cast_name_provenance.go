package javlibrary

import "github.com/javinizer/javinizer-go/internal/models"

// castNameProvenance adds an honest source-level interpretation without changing
// compatibility fields or asserting a canonical identity. No catalog inference
// promotes an unknown source order to user-corrected.
func castNameProvenance(actors []models.ActressInfo, evidence []models.CastNameEvidence) []models.CastNameProvenance {
	if len(actors) == 0 || len(actors) != len(evidence) {
		return nil
	}
	provenance := make([]models.CastNameProvenance, len(actors))
	for i, actor := range actors {
		item := evidence[i]
		state := models.CastNameUnknownOrder
		switch {
		case actor.JapaneseName != "":
			state = models.CastNameNativeUnsplit
		case item.ManualOverride:
			state = models.CastNameUserCorrected
		case actor.LastName == "":
			state = models.CastNameSingleName
		}
		provenance[i] = models.CastNameProvenance{Source: "javlibrary", SourceID: item.SourceID, DisplayName: item.DisplayName, State: state}
	}
	return provenance
}
