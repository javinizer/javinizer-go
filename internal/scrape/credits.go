package scrape

import (
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// BuildCreditsFromScrape implements the credit identity lifecycle contract.
func BuildCreditsFromScrape(movie *models.Movie, actressSources map[string]string, results []*models.ScraperResult) {
	if movie == nil {
		return
	}
	if len(movie.Actresses) == 0 {
		movie.Credits = []models.MovieCredit{}
		return
	}
	resultsBySource := make(map[string]*models.ScraperResult, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		source := strings.TrimSpace(result.Source)
		if source != "" {
			resultsBySource[source] = result
		}
	}
	components := compactActressComponents(movie.Actresses)
	credits := make([]models.MovieCredit, 0, len(components))
	actresses := make([]models.Actress, 0, len(components))
	for _, component := range components {
		actress := mergeActressComponent(movie.Actresses, component)
		source, sourceKey := "", ""
		for _, candidateKey := range actressStableKeys(actress) {
			if candidateSource := strings.TrimSpace(actressSources[candidateKey]); candidateSource != "" {
				source, sourceKey = candidateSource, candidateKey
				break
			}
		}
		creditedName, creditedJP, reportedThumb := actress.FullName(), actress.JapaneseName, actress.ThumbURL
		if result, ok := resultsBySource[source]; ok {
			for _, info := range result.Actresses {
				if actressInfoMatchesKey(info, sourceKey) {
					creditedName, creditedJP, reportedThumb = infoFullName(info), info.JapaneseName, info.ThumbURL
					break
				}
			}
		}
		actresses = append(actresses, actress)
		credits = append(credits, models.MovieCredit{CreditedName: creditedName, CreditedJapaneseName: creditedJP, ReportedThumbURL: reportedThumb, Source: source, Origin: string(models.CreditOriginScrape), OrderIndex: len(credits), Scraped: actress})
	}
	movie.Actresses, movie.Credits = actresses, credits
}

type actressComponent struct {
	indexes []int
	dmmID   int
	keys    map[string]struct{}
}

func actressStableKeys(actress models.Actress) []string {
	return actressSourceKeysFromInfo(models.ActressInfo{DMMID: actress.DMMID, FirstName: actress.FirstName, LastName: actress.LastName, JapaneseName: actress.JapaneseName})
}

func keySetsOverlap(a, b map[string]struct{}) bool {
	for key := range a {
		if _, ok := b[key]; ok {
			return true
		}
	}
	return false
}

func compactActressComponents(actresses []models.Actress) [][]int {
	components := make([]actressComponent, 0, len(actresses))
	for i := range actresses {
		component := newActressComponent(actresses, []int{i})
		if len(component.keys) > 0 {
			components = append(components, component)
		}
	}
	for {
		merged := mergeCompatibleActressComponents(actresses, components)
		if len(merged) != len(components) {
			components = merged
			continue
		}
		attached := attachUnambiguousActressComponents(actresses, components)
		if len(attached) == len(components) {
			components = attached
			break
		}
		components = attached
	}
	components = dropAmbiguousNoDMMComponents(components)
	sort.Slice(components, func(i, j int) bool { return components[i].indexes[0] < components[j].indexes[0] })
	out := make([][]int, len(components))
	for i := range components {
		out[i] = components[i].indexes
	}
	return out
}

func mergeCompatibleActressComponents(actresses []models.Actress, components []actressComponent) []actressComponent {
	parent := make([]int, len(components))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	union := func(a, b int) {
		a, b = find(a), find(b)
		if a != b {
			parent[b] = a
		}
	}
	for i := range components {
		for j := i + 1; j < len(components); j++ {
			if !keySetsOverlap(components[i].keys, components[j].keys) {
				continue
			}
			if components[i].dmmID == 0 && components[j].dmmID == 0 || components[i].dmmID != 0 && components[i].dmmID == components[j].dmmID {
				union(i, j)
			}
		}
	}
	return rebuildActressComponents(actresses, components, find)
}

func attachUnambiguousActressComponents(actresses []models.Actress, components []actressComponent) []actressComponent {
	parent := make([]int, len(components))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for i := range components {
		if components[i].dmmID != 0 {
			continue
		}
		matches := -1
		ambiguous := false
		for j := range components {
			if components[j].dmmID == 0 || !keySetsOverlap(components[i].keys, components[j].keys) {
				continue
			}
			if matches >= 0 && components[matches].dmmID != components[j].dmmID {
				ambiguous = true
				break
			}
			matches = j
		}
		if matches >= 0 && !ambiguous {
			parent[i] = matches
		}
	}
	return rebuildActressComponents(actresses, components, find)
}

func dropAmbiguousNoDMMComponents(components []actressComponent) []actressComponent {
	out := make([]actressComponent, 0, len(components))
	for i := range components {
		if components[i].dmmID != 0 {
			out = append(out, components[i])
			continue
		}
		matches := make(map[int]struct{}, 2)
		for j := range components {
			if components[j].dmmID != 0 && keySetsOverlap(components[i].keys, components[j].keys) {
				matches[components[j].dmmID] = struct{}{}
			}
		}
		if len(matches) <= 1 {
			out = append(out, components[i])
		}
	}
	return out
}

func rebuildActressComponents(actresses []models.Actress, components []actressComponent, find func(int) int) []actressComponent {
	indexesByRoot := make(map[int][]int, len(components))
	for i := range components {
		root := find(i)
		indexesByRoot[root] = append(indexesByRoot[root], components[i].indexes...)
	}
	out := make([]actressComponent, 0, len(indexesByRoot))
	for _, indexes := range indexesByRoot {
		sort.Ints(indexes)
		out = append(out, newActressComponent(actresses, indexes))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].indexes[0] < out[j].indexes[0] })
	return out
}

func newActressComponent(actresses []models.Actress, indexes []int) actressComponent {
	component := actressComponent{indexes: indexes, keys: make(map[string]struct{})}
	firstNames := make(map[string]string, len(indexes))
	lastNames := make(map[string]string, len(indexes))
	for _, index := range indexes {
		actress := actresses[index]
		if actress.DMMID != 0 {
			component.dmmID = actress.DMMID
		}
		for _, key := range actressStableKeys(actress) {
			component.keys[key] = struct{}{}
		}
		if value := strings.TrimSpace(actress.FirstName); value != "" {
			firstNames[models.NormalizeActressNameKey(value)] = value
		}
		if value := strings.TrimSpace(actress.LastName); value != "" {
			lastNames[models.NormalizeActressNameKey(value)] = value
		}
	}
	if len(firstNames) <= 1 && len(lastNames) <= 1 {
		firstName, lastName := "", ""
		for _, value := range firstNames {
			firstName = value
		}
		for _, value := range lastNames {
			lastName = value
		}
		for _, name := range []string{strings.TrimSpace(firstName + " " + lastName), strings.TrimSpace(lastName + " " + firstName)} {
			if normalized := models.NormalizeActressNameKey(name); normalized != "" {
				component.keys["name:"+normalized] = struct{}{}
			}
		}
	}
	return component
}

func mergeActressComponent(actresses []models.Actress, component []int) models.Actress {
	merged := actresses[component[0]]
	for _, index := range component[1:] {
		candidate := actresses[index]
		if merged.ID == 0 {
			merged.ID = candidate.ID
		}
		if merged.DMMID == 0 {
			merged.DMMID = candidate.DMMID
		}
		if strings.TrimSpace(merged.FirstName) == "" {
			merged.FirstName = candidate.FirstName
		}
		if strings.TrimSpace(merged.LastName) == "" {
			merged.LastName = candidate.LastName
		}
		if strings.TrimSpace(merged.JapaneseName) == "" {
			merged.JapaneseName = candidate.JapaneseName
		}
		if strings.TrimSpace(merged.ThumbURL) == "" {
			merged.ThumbURL = candidate.ThumbURL
		}
		if strings.TrimSpace(merged.Aliases) == "" {
			merged.Aliases = candidate.Aliases
		}
		if strings.TrimSpace(merged.Origin) == "" {
			merged.Origin = candidate.Origin
		}
		if strings.TrimSpace(merged.NameKey) == "" {
			merged.NameKey = candidate.NameKey
		}
	}
	return merged
}

// AttachCreditPolicy implements the credit identity lifecycle contract.
func AttachCreditPolicy(movie *models.Movie, cfg *Config) {
	if movie == nil || cfg == nil {
		return
	}
	movie.CreditPolicy = cfg.CollisionPolicy
	movie.TrustedCollisionSources = cfg.TrustedCollisionSources
}

func infoFullName(info models.ActressInfo) string {
	if strings.TrimSpace(info.JapaneseName) != "" && strings.TrimSpace(info.FirstName) == "" && strings.TrimSpace(info.LastName) == "" {
		return strings.TrimSpace(info.JapaneseName)
	}
	if info.LastName != "" && info.FirstName != "" {
		return strings.TrimSpace(info.LastName + " " + info.FirstName)
	}
	if info.FirstName != "" {
		return strings.TrimSpace(info.FirstName)
	}
	return strings.TrimSpace(info.LastName)
}

func actressInfoMatchesKey(info models.ActressInfo, key string) bool {
	for _, infoKey := range actressSourceKeysFromInfo(info) {
		if infoKey == key {
			return true
		}
	}
	return false
}
