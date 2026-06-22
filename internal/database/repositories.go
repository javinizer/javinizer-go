package database

// ContentRepos groups repositories for the core content domain:
// movies, actresses, and their associated metadata (aliases, content IDs, tags).
// Callers that scrape or organize content accept this sub-struct
// rather than the full Repositories bag.
type ContentRepos struct {
	MovieRepo            MovieRepositoryInterface
	ActressRepo          ActressRepositoryInterface
	ActressAliasRepo     ActressAliasRepositoryInterface
	ContentIDMappingRepo ContentIDMappingRepositoryInterface
	MovieTagRepo         MovieTagRepositoryInterface
}

// HistoryRepos groups repositories for job and file-operation tracking.
// Callers that manage batch job lifecycle accept this sub-struct.
type HistoryRepos struct {
	HistoryRepo     HistoryRepositoryInterface
	BatchFileOpRepo BatchFileOperationRepositoryInterface
	JobRepo         JobRepositoryInterface
}

// SystemRepos groups repositories for cross-cutting system concerns:
// event logging and API token management.
type SystemRepos struct {
	EventRepo    EventRepositoryInterface
	ApiTokenRepo ApiTokenRepositoryInterface
}

// TranslationRepos groups repositories for translation lookups.
type TranslationRepos struct {
	GenreTranslationRepo   GenreTranslationRepositoryInterface
	ActressTranslationRepo ActressTranslationRepositoryInterface
}

// ReplacementRepos groups repositories for genre/word replacement rules.
// The genre handlers and aggregator both use these together.
type ReplacementRepos struct {
	GenreRepo            GenreRepositoryInterface
	GenreReplacementRepo GenreReplacementRepositoryInterface
	WordReplacementRepo  WordReplacementRepositoryInterface
}

// Repositories is the top-level repository bag. It embeds the five
// domain-oriented sub-structs so that existing code reading
// repos.MovieRepo continues to work unchanged.
//
// New callers that only need a subset should accept the relevant
// sub-struct (ContentRepos, HistoryRepos, etc.) instead.
type Repositories struct {
	ContentRepos
	HistoryRepos
	SystemRepos
	TranslationRepos
	ReplacementRepos
}

func (db *DB) Repositories() Repositories {
	return Repositories{
		ContentRepos: ContentRepos{
			MovieRepo:            NewMovieRepository(db),
			ActressRepo:          NewActressRepository(db),
			ActressAliasRepo:     NewActressAliasRepository(db),
			ContentIDMappingRepo: NewContentIDMappingRepository(db),
			MovieTagRepo:         NewMovieTagRepository(db),
		},
		HistoryRepos: HistoryRepos{
			HistoryRepo:     NewHistoryRepository(db),
			BatchFileOpRepo: NewBatchFileOperationRepository(db),
			JobRepo:         NewJobRepository(db),
		},
		SystemRepos: SystemRepos{
			EventRepo:    NewEventRepository(db),
			ApiTokenRepo: NewApiTokenRepository(db),
		},
		TranslationRepos: TranslationRepos{
			GenreTranslationRepo:   newGenreTranslationRepository(db),
			ActressTranslationRepo: newActressTranslationRepository(db),
		},
		ReplacementRepos: ReplacementRepos{
			GenreRepo:            newGenreRepository(db),
			GenreReplacementRepo: NewGenreReplacementRepository(db),
			WordReplacementRepo:  NewWordReplacementRepository(db),
		},
	}
}
