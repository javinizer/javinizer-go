package translation

import "context"

type TranslatorProvider interface {
	Name() string
	Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error)
}
