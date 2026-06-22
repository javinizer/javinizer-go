package organizer

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/javinizer/javinizer-go/internal/models"
)

// groupFileMatchInfosByID groups FileMatchInfo entries by their MovieID field.
func groupFileMatchInfosByID(matches []models.FileMatchInfo) map[string][]models.FileMatchInfo {
	grouped := make(map[string][]models.FileMatchInfo)
	for _, m := range matches {
		grouped[m.MovieID] = append(grouped[m.MovieID], m)
	}
	return grouped
}

// organizeBatchViaOrganize replaces the deleted OrganizeBatch/OrganizeBatchWithLinkMode
// convenience methods. It groups matches by ID, sorts parts, tracks in-place renames,
// and calls Organize(OrganizeCmd) per match.
func organizeBatchViaOrganize(
	o *Organizer,
	matches []models.FileMatchInfo,
	movies map[string]*models.Movie,
	destDir string,
	dryRun bool,
	forceUpdate bool,
	copyOnly bool,
	linkMode LinkMode,
) ([]OrganizeResult, error) {
	results := make([]OrganizeResult, 0, len(matches))

	// Group by ID to process multi-part sets together
	grouped := groupFileMatchInfosByID(matches)

	// Stable process: deterministic ID order
	ids := make([]string, 0, len(grouped))
	for id := range grouped {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		group := grouped[id]

		// Sort parts: 0 (single/no suffix) first, then 1..N
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].PartNumber < group[j].PartNumber
		})

		// Track directory renames for multi-part path updates
		var lastInPlaceRename *OrganizeResult

		for idx := range group {
			match := group[idx] // Use index to get mutable reference

			// If a previous part in this group triggered an in-place directory rename,
			// update this match's path to reflect the new directory
			if lastInPlaceRename != nil && lastInPlaceRename.InPlaceRenamed {
				oldDir := lastInPlaceRename.OldDirectoryPath
				newDir := lastInPlaceRename.NewDirectoryPath

				// Check if this match's path is in the old directory
				if filepath.Dir(match.Path) == oldDir {
					// Update path to new directory
					oldFileName := filepath.Base(match.Path)
					match.Path = filepath.Join(newDir, oldFileName)
					group[idx] = match // Update the slice
				}
			}

			movie, exists := movies[match.MovieID]
			if !exists {
				results = append(results, OrganizeResult{
					OriginalPath: match.Path,
					Error:        fmt.Errorf("no movie data found for ID: %s", match.MovieID),
				})
				continue
			}

			result, err := o.Organize(context.Background(), OrganizeCmd{
				Match:       match,
				Movie:       movie,
				DestDir:     destDir,
				DryRun:      dryRun,
				ForceUpdate: forceUpdate,
				MoveFiles:   !copyOnly,
				LinkMode:    linkMode,
			})
			if err != nil {
				result = &OrganizeResult{
					OriginalPath: match.Path,
					Error:        err,
				}
			}

			// Track in-place renames for subsequent parts
			if result.InPlaceRenamed {
				lastInPlaceRename = result
			}

			results = append(results, *result)
		}
	}

	return results, nil
}

// organizeBatchViaOrganizeSimple is a convenience wrapper matching the old
// OrganizeBatch signature (no link mode).
func organizeBatchViaOrganizeSimple(
	o *Organizer,
	matches []models.FileMatchInfo,
	movies map[string]*models.Movie,
	destDir string,
	dryRun bool,
	forceUpdate bool,
	copyOnly bool,
) ([]OrganizeResult, error) {
	return organizeBatchViaOrganize(o, matches, movies, destDir, dryRun, forceUpdate, copyOnly, LinkModeNone)
}
