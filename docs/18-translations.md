# Translations & Localization

Javinizer ships with two independent user interface surfaces — a SvelteKit
**Web UI** and a Bubble Tea **TUI** — and each has its own message catalog and
runtime. This guide tells a community translator exactly where catalogs live,
how they are formatted, how to draft and review a new locale, and what a locale
pull request must satisfy before it can be advertised as supported.

The base (source) locale is **English (`en`)**. English catalogs are the
complete canonical source of truth; every other locale is a translation of
them. A locale only becomes "supported" — and only then appears in the language
selector — once it has been reviewed by a fluent human and exercised on
representative screens.

> This guide is for **interface chrome** (menus, buttons, labels, dialogs,
> toasts, progress text, accessibility labels). It is *not* about translating
> scraped movie titles, actress names, or genres — that is the separate
> `metadata.translation` pipeline. See [Scope](#what-is-and-isnt-translatable)
> below.

---

## Contents

1. [What is and isn't translatable](#what-is-and-isnt-translatable)
2. [The two catalogs are separate](#the-two-catalogs-are-separate)
3. [BCP 47 locale naming](#bcp-47-locale-naming)
4. [Setup](#setup)
5. [Frontend catalog format (Paraglide)](#frontend-catalog-format-paraglide)
6. [TUI catalog format (go-i18n)](#tui-catalog-format-go-i18n)
7. [Key rules](#key-rules)
8. [Placeholder rules](#placeholder-rules)
9. [Plural rules](#plural-rules)
10. [AI draft + human review policy](#ai-draft--human-review-policy)
11. [Review checklist](#review-checklist)
12. [Locale PR checklist](#locale-pr-checklist)
13. [Coverage reporting](#coverage-reporting)
14. [Locale maintainer expectations](#locale-maintainer-expectations)
15. [Glossary](#glossary)

---

## What is and isn't translatable

Only **interface chrome** is in scope for these catalogs. The application deals
with a lot of text-like data that must never be routed through the UI message
catalogs.

### Translatable

- Headings, tab labels, menu items, buttons, links.
- Form labels, field descriptions, placeholders, hints, tooltips.
- Dialog titles and bodies, confirmation prompts, toasts/notifications.
- Empty states, loading text, badges, status labels.
- Accessibility attributes: `aria-label`, `title`, `alt`, visually-hidden
  descriptions.
- Progress and result summaries shown to the user.
- Locally-rendered mappings of **stable API error/progress codes** (the API
  itself stays language-neutral; the Web UI and TUI translate known codes via
  `web/frontend/src/lib/i18n/api-messages.ts` and the TUI localizer).

### NOT translatable (keep as-is, usually as variables)

- **Movie metadata**: titles, descriptions, actress names, genres, studios,
  labels, release dates, ratings. These flow through the
  `metadata.translation` pipeline and the database, not the UI catalogs.
- **Scraper / source language**: which language a scraper fetches from a source
  site is a scraper option, independent of the interface locale.
- **NFO and output naming**: NFO file content and folder/file naming language
  are controlled by `output.*` and `nfo.*` settings, not by `ui.language`.
- **File paths, JAV IDs, content IDs, URLs** — render these as placeholders,
  never translate them.
- **Enum values** (job status, operation mode) and **JSON property names** —
  these are programmatic identifiers.
- **API error/progress codes** (e.g. `JOB_NOT_FOUND`, `SCRAPE_SUCCEEDED`) —
  clients translate the *code*, the code string itself is a stable contract.
- **Configuration keys** and **template tags** (e.g. `{title}`, `{id}`).
- **Diagnostic logs and raw provider/system error detail** — intentionally
  English for diagnostics.
- **Scraper names and provider names** (e.g. a scraper's display identity) and
  product names covered in the [glossary](#glossary).

When in doubt: if the string is *data the user brought in* (a path, an ID, a
movie title) or *a programmatic identifier*, it is not a catalog message.

---

## The two catalogs are separate

The Web UI and TUI run on different toolchains and use **different message
schemas**. They are kept in separate files and must never be loaded into each
other's runtime. Do not attempt to share one catalog between them.

| Surface | Runtime | Catalog files | Generated code |
|---|---|---|---|
| Web UI | `@inlang/paraglide-js` v2 (SvelteKit/Vite) | `web/frontend/messages/{locale}.json` | `web/frontend/src/lib/paraglide/` (generated) |
| TUI | `github.com/nicksnyder/go-i18n/v2` (Go, embedded) | `internal/tui/localization/locales/active.{locale}.json` | none (embedded at compile time) |

Other supporting files a translator should know about:

- `web/frontend/project.inlang/settings.json` — Inlang project config: base
  locale, the list of locales, pinned Inlang modules, and the message
  path pattern.
- `web/frontend/src/lib/i18n/locale.ts` — frontend locale bootstrap/reconcile
  logic and the `SUPPORTED_LOCALES` list (the Web UI selector source).
- `web/frontend/src/lib/i18n/api-messages.ts` — maps stable API error/progress
  codes to Paraglide message functions, with English fallback for unknown
  codes.
- `web/frontend/src/lib/i18n/format.ts` — active-locale date/number/relative-
  time/duration formatters (wraps `Intl`).
- `internal/tui/localization/localizer.go` — the TUI localizer wrapper
  (`New`, `Localize`, `Plural`).
- `internal/tui/localization/detect*.go` — cross-platform OS-locale detection.
- `internal/config/config.go` — `UIConfig.Language` (the `ui.language` setting).

The HTTP API does **not** have a translation catalog by design. It returns
stable codes plus English compatibility text; the Web UI and TUI translate
known codes locally. See [AI draft + human review policy](#ai-draft--human-review-policy)
for why auth/destructive text is held to a stricter standard.

---

## BCP 47 locale naming

Locale identifiers are **BCP 47 language tags**. The configuration value
`ui.language` is either `auto` (the default — resolve per environment) or one
of these tags.

Rules:

- **Primary language subtag** is required and lowercase: `en`, `ja`, `pt`, `zh`.
- **Script subtag** (title case, four letters) is used when needed to
  disambiguate, especially for CJK: `zh-Hans` (Simplified Chinese),
  `zh-Hant` (Traditional Chinese). Do not drop the script for Chinese unless
  the regional variant already implies it.
- **Region subtag** is uppercase two letters: `pt-BR`, `en-GB`. Use a regional
  variant only when the locale genuinely differs from the base language; do not
  add `-US` to `en` (the base `en` catalog is canonical).
- Use the **hyphen** `-` as the subtag separator, never an underscore. `ja_JP`
  is a POSIX value that the TUI detector normalizes to `ja-JP` before matching;
  catalog filenames must already be in canonical BCP 47 form.
- **Canonical form**: subtags in the order language–script–region, each in its
  conventional case, e.g. `zh-Hans-CN` (not `zh-cn-hans` or `ZH_hans_cn`).
- Keep tags minimal: prefer `ja` over `ja-JP`, `pt-BR` over `pt-BR-x-foo`.

Locale matching at runtime resolves, in order:

1. an exact supported tag (e.g. `pt-BR`);
2. a supported base language (e.g. `pt`);
3. English (`en`).

So a valid but unsupported tag is never an error — it renders English.

### Filenames

- Frontend: `web/frontend/messages/<locale>.json` (e.g. `ja.json`,
  `zh-Hans.json`, `pt-BR.json`).
- TUI: `internal/tui/localization/locales/active.<locale>.json`
  (e.g. `active.ja.json`, `active.zh-Hans.json`).

The `en-XA` pseudo-locale (`web/frontend/messages/en-XA.json`) is a **test
fixture only** — it decorates English to surface missed literals and narrow
containers. Never list it in the production selector.

---

## Setup

### Prerequisites

- **Node.js** (matches `web/frontend/package.json`'s engine; a current LTS).
- **Go** (matches `go.mod`; required only if you also exercise the TUI).
- The repository checked out on the `feat/i18n-support` branch (or later
  `main`, once merged).

### Frontend (Paraglide)

The frontend catalogs are hand-edited JSON at
`web/frontend/messages/*.json`. Paraglide compiles them into typed message
functions under `web/frontend/src/lib/paraglide/`. **Generated files must never
be hand-edited** — regenerate them.

Regenerate after editing catalogs:

```bash
cd web/frontend
npx @inlang/paraglide-js compile --project ./project.inlang --outdir ./src/lib/paraglide
```

The Vite plugin (`web/frontend/vite.config.ts`) is configured with:

```ts
paraglideVitePlugin({
  project: './project.inlang',
  outdir: './src/lib/paraglide',
  strategy: ['localStorage', 'preferredLanguage', 'baseLocale'],
  localStorageKey: 'javinizer-locale'
});
```

so a normal `vite build` / `vite dev` also regenerates on the fly.

Run the type checker, unit tests, and dev server:

```bash
cd web/frontend
npm run check        # svelte-check + regenerate types
npm run test         # Vitest
npm run dev          # dev server on :5174 (proxies /api and /ws to :8765)
```

To exercise a locale end-to-end, add it to
`web/frontend/project.inlang/settings.json` `locales` and to
`SUPPORTED_LOCALES` in `web/frontend/src/lib/i18n/locale.ts`, regenerate, and
select it from the Settings → Web UI language selector.

### TUI (go-i18n)

TUI catalogs are embedded into the binary at compile time:

```go
//go:embed locales/active.*.json
var catalogs embed.FS
```

So adding `internal/tui/localization/locales/active.<locale>.json` automatically
makes it available — there is no `SUPPORTED_LOCALES` to edit for the TUI. The
localizer (`internal/tui/localization/localizer.go`) loads every embedded
`active.*.json` and falls back to English for any missing message.

Build and run the TUI against a configured locale:

```bash
make build                              # full build (requires web-build first)
go build -o bin/javinizer ./cmd/javinizer
JAVINIZER_CONFIG=configs/config.yaml go run ./cmd/javinizer tui ~/Videos
```

Set the locale explicitly in `config.yaml`:

```yaml
ui:
  language: ja          # or "auto" to follow the OS locale
```

The TUI's `go-i18n` extract/merge tooling can update translation work files:

```bash
goi18n extract -outdir internal/tui/localization/locales   # gather IDs from source
goi18n merge -outdir internal/tui/localization/locales     # merge new/changed IDs
```

Run TUI localization tests (the `internal/tui/` package is race-detector
required):

```bash
go test -v ./internal/tui/localization/...
go test -race ./internal/tui/...
```

---

## Frontend catalog format (Paraglide)

Frontend catalogs are Inlang Message Format JSON, one file per locale at
`web/frontend/messages/{locale}.json`. The schema is
`https://inlang.com/schema/inlang-message-format`.

A catalog is a flat JSON object mapping **semantic message IDs** to either a
string or a complex-message array.

### Simple message

```json
{
  "$schema": "https://inlang.com/schema/inlang-message-format",
  "settings_save_changes": "Save Changes",
  "nav_scrape": "Scrape"
}
```

### Message with a placeholder

Placeholders are `{name}`:

```json
{
  "actresses_actress_label_full": "#{id} - {name}",
  "error_path_not_found": "Path not found: {path}"
}
```

### Plural message

Plurals use the complex-message array form with `declarations`, `selectors`,
and `match`. Declare a plural variable from an `input`, list it as a
selector, and provide a match arm per CLDR plural category. The catch-all
default arm uses `*`:

```json
{
  "jobs_file_count": [
    {
      "declarations": [
        "input count",
        "local countPlural = count: plural"
      ],
      "selectors": ["countPlural"],
      "match": {
        "countPlural=one": "{count} file",
        "countPlural=*": "{count} files"
      }
    }
  ]
}
```

- `declarations` first declares the `input` (the argument passed to the
  generated function) and then a `local` plural variable derived from it via
  `: plural`.
- `selectors` lists the variable(s) the match branches on.
- `match` keys are `<selector>=<category>`. English CLDR categories are `one`
  and `other`; the `*` arm is the default and is also accepted in place of
  `other`. Other languages may use `few`, `many`, `two`, etc. — provide every
  category that language's CLDR rules require.
- The placeholder `{count}` refers back to the declared input.

> Do **not** use the legacy split-key approach (`"key_one"` / `"key_other"`).
  Always use the complex-message array form above for count-dependent
  messages.

### Using a message in Svelte

```svelte
<script lang="ts">
  import * as m from '$lib/paraglide/messages.js';
</script>

<a href="/browse">{m.nav_scrape()}</a>
<p>{m.jobs_file_count({ count: selected.length })}</p>
```

Generated functions are type-checked at compile time, so a wrong argument name
or count is a build error.

### Sorting

`project.inlang/settings.json` sets `"sort": "asc"`, so keys are kept in
ascending alphabetical order. Preserve that ordering when editing by hand
(re-running the compiler will re-sort).

---

## TUI catalog format (go-i18n)

TUI catalogs are go-i18n v2 JSON, one file per locale at
`internal/tui/localization/locales/active.{locale}.json`. The English file is
the canonical source and contains a `description` for every entry.

A catalog is a JSON object mapping **message IDs** to an object with a
`description`, an `other` form, and (for plurals) a `one` form.

### Simple message

```json
{
  "TUISettingsTitle": {
    "description": "Heading of the terminal settings view",
    "other": "Settings"
  }
}
```

### Message with placeholders

Placeholders use Go-template syntax `{{.Name}}`:

```json
{
  "TUILogPathChanged": {
    "description": "Log message shown when the scan path is changed",
    "other": "Path changed to: {{.Path}}"
  }
}
```

### Plural message

Plurals use `one` / `other` (and other CLDR categories where the language
requires). go-i18n selects the form from the plural count passed to
`Localizer.Plural`. A `{{.PluralCount}}` placeholder (and a convenience
`{{.Count}}` alias) is auto-populated:

```json
{
  "TUIFilesProcessed": {
    "description": "Number of files processed in the completion banner",
    "one": "Processed {{.Count}} file in {{.Elapsed}}",
    "other": "Processed {{.Count}} files in {{.Elapsed}}"
  }
}
```

Call site:

```go
text := l.Plural("TUIFilesProcessed", count, map[string]any{"Elapsed": elapsed})
```

### Rules specific to the TUI

- **Every** entry must carry a `description`. English is the source of truth;
  the description is what an AI drafter and a human reviewer use to translate
  ambiguous short labels.
- Message IDs are stable strings. The TUI uses a `TUI…` prefix convention
  (e.g. `TUISettingsTitle`, `TUIFilesProcessed`, `TUIActressMergeTitle`); do
  not rename IDs.
- The localizer never panics on a missing message — it falls back to the
  English `other` form, then to the raw ID. Incomplete translations render
  mixed English/text rather than blank, but [coverage reporting](#coverage-reporting)
  makes the gap visible in CI.
- Keep genuinely diagnostic console/log messages (provider errors, raw system
  detail) in English where they are outside interface scope.

---

## Key rules

These rules apply to **both** catalogs.

- **Stable semantic IDs, never English source text.** Use `settings_save_changes`,
  not `"Save Changes"`. IDs are stable forever; renaming one breaks every
  translation.
- **Namespace by feature.** Frontend IDs are lower-snake-case with a feature
  prefix: `auth_*`, `nav_*`, `settings_*`, `jobs_*`, `browse_*`, `actresses_*`,
  `genres_*`, `history_*`, `logs_*`, `home_*`, `progress_*`, `error_*`,
  `field_*`, `common_*`. TUI IDs use a `TUI…` PascalCase prefix grouped by
  surface (`TUISetting*`, `TUIHelp*`, `TUIBrowser*`, `TUIDash*`, `TUITab*`,
  `TUIActressMerge*`, `TUILog*`).
- **Translate complete sentences, including punctuation.** A period, colon,
  ellipsis, or trailing space is part of the message, not added later.
- **Never concatenate fragments** at runtime (e.g. `label + ": " + value`).
  Each full string is its own message with its own placeholders.
- **Add a `description`** wherever a short English word is ambiguous. `Apply`,
  `Move`, `Label`, `Source`, `Skip` all mean different things in different
  parts of Javinizer. The English catalog's `description` field is the contract
  for what the label means in context.
- **Never use English text as a stable test selector** or as a programmatic
  identifier. Tests should query by role/label or a dedicated test ID.
- When adding new user-facing text, update the **English** catalog in the same
  pull request. Do not leave English-only strings in components.

---

## Placeholder rules

- **Preserve placeholder names exactly across locales.** If English is
  `Path not found: {path}`, the Japanese entry must also use `{path}` — not
  `{パス}` or `{p}`. In the TUI, `{{.Path}}` stays `{{.Path}}`.
- **Never translate** the contents of a placeholder: paths, JAV/content IDs,
  URLs, config keys, enum values, code literals, template tags. They are data.
- If a locale's grammar reorders where a value appears, move the placeholder
  within the message — do not split the message into fragments to "fix" word
  order.
- Do not introduce new placeholders, and do not drop one that English
  declares. The generated frontend functions type-check argument names, and
  the TUI's `templateData` keys must line up; a mismatch is a CI failure (see
  [Coverage reporting](#coverage-reporting)).
- Keep markup out of translations. Do not inject raw HTML; the Web UI renders
  text, not markup, from catalog messages.

---

## Plural rules

- **Always use the plural mechanism** for count-dependent messages. Never
  hand-roll `count !== 1 ? 's' : ''` (frontend) or string-concatenate a suffix
  (TUI). Many languages have more than two plural forms.
- Declare the count as an `input` and derive a `local … : plural` selector
  (frontend), or pass the count to `Localizer.Plural` (TUI).
- Provide **every CLDR plural category the target language requires**. English
  needs `one` and `other`/`*`. Arabic needs `zero`, `one`, `two`, `few`,
  `many`, `other`. Russian needs `one`, `few`, `many`, `other`. Omitting a
  required category falls back to English for that count, which is a coverage
  gap.
- Test plural boundaries explicitly: counts 0, 1, and 2 at minimum, plus any
  language-specific boundary (e.g. the few/many split).

---

## AI draft + human review policy

Machine translation is welcome as a **draft** starting point, but no AI text
becomes a supported locale without a fluent human reviewer checking every
entry in context. This mirrors the i18n design doc §6.2.

The workflow:

1. **Propose the locale.** Open an issue or PR stating the BCP 47 tag, the
   script/region scope, and ideally a volunteer fluent reviewer.
2. **Generate a draft with AI** using the English catalog's `description`
   fields and this guide's [glossary](#glossary) as context. The descriptions
   exist precisely so an AI knows what an ambiguous label means.
3. **Mark the PR clearly as AI-drafted** (e.g. a `ai-drafted` label and a note
   in the description). This is mandatory.
4. **A fluent reviewer checks every entry in context** — terminology, tone,
   register, grammar, plural rules, placeholders, punctuation, and the
   destructive-action guarantees below. Reviewing a diff in GitHub is not
   enough; the reviewer must see the strings on real screens.
5. **Exercise representative Web and TUI screens** for meaning and clipping
   (long translations, narrow terminals, CJK width, RTL layout for RTL
   locales).
6. **Add the locale to the supported list only after approval** — frontend
   `SUPPORTED_LOCALES` and `project.inlang/settings.json` `locales`; the TUI
   needs no list edit because it auto-detects embedded catalogs.

### Hard rule: security-sensitive and destructive text

**Never merge unreviewed AI text** for messages in any of these areas. These
must be reviewed entry-by-entry by a fluent human even when the rest of the
locale is AI-drafted:

- **Authentication** — login, logout, credentials, session, "remember me".
- **Delete** — any delete confirmation, title, or result toast.
- **Overwrite / move** — file move/copy/hardlink/softlink confirmations and
  "force update / replace existing" wording.
- **Proxy credentials** — any text near proxy username/password fields.
- **Token display** — auth token reveal/copy surfaces.
- **Revert** — revert confirmations, batch-revert labels, revert result
  summaries.
- **Destructive confirmations** — clear-all, clean-history, batch clear, and
  any "this cannot be undone" wording.

For these, an AI draft is acceptable *only* as a proposal for the reviewer to
accept or reject line by line; it must not be merged on the AI's output alone.

### Self-names in the selector

The Web UI language selector and the TUI settings choice list use language
**self-names** (`English`, `日本語`, `Deutsch`, `简体中文`) rather than the
current locale's name for each language. This keeps the selector usable when
the active locale is wrong or unreadable to the user. When you add a locale,
provide its self-name in `SUPPORTED_LOCALES` (`web/frontend/src/lib/i18n/locale.ts`)
and in the TUI's language choice list.

---

## Review checklist

A reviewer must verify, for **every** message in the PR:

- [ ] **Terminology** matches the [glossary](#glossary). Domain terms that
  should stay in English (JAV ID, NFO, scraper, FlareSolverr, etc.) are kept
  as-is, not translated.
- [ ] **Tone and register** fit a desktop media-organizer tool. Consistent
  with the rest of the locale (formal vs. informal "you", etc.).
- [ ] **Grammar** is correct and natural, not literal.
- [ ] **Plurals** are correct for the language's CLDR rules, including all
  required categories, at counts 0/1/2 and any language-specific boundary.
- [ ] **Placeholders** match English exactly by name; none added, dropped, or
  translated. Paths/IDs/URLs inside them render as data.
- [ ] **Punctuation** — terminal punctuation, colons, ellipses, quotes — is
  locale-correct and complete (not added/removed by code).
- [ ] **Complete sentences** — no runtime fragment concatenation was
  introduced to "fix" word order.
- [ ] **No markup** injected into translations.
- [ ] **Clipping** — long translations fit their container on representative
  Web pages; in the TUI they fit narrow terminals without corrupting layout
  (especially CJK width and long Germanic compounds).
- [ ] **Destructive / security surfaces** — every auth, delete, overwrite,
  move, proxy-credential, token, revert, and destructive-confirmation message
  has been read in context and is unambiguous. (See the hard rule above.)
- [ ] **`html lang` and `html dir`** are correct after selecting the locale
  (Web UI), and the TUI renders the locale without global-state bleed between
  independent model instances.
- [ ] **Unknown API codes fall back to English**, not blank. Switching to the
  locale and hitting an unmapped error should show English fallback text.

---

## Locale PR checklist

A locale contribution PR must include:

- [ ] **Frontend catalog**: `web/frontend/messages/<locale>.json` present,
  valid JSON, UTF-8, sorted ascending (matches `project.inlang/settings.json`
  `"sort": "asc"`).
- [ ] **TUI catalog**: `internal/tui/localization/locales/active.<locale>.json`
  present, valid go-i18n JSON, with a `description` retained on every entry
  (copy the English `description`).
- [ ] **Keys match English**: no missing keys, no extra/stale keys, in either
  catalog. (CI enforces this — see [Coverage reporting](#coverage-reporting).)
- [ ] **Placeholders match** English exactly: same names, same count, in both
  catalogs. `{var}` in frontend, `{{.Var}}` in TUI.
- [ ] **Plurals correct**: every plural message provides all CLDR categories
  the target language requires; complex-message arrays compile.
- [ ] **`project.inlang/settings.json`** `locales` array updated to include
  the new tag.
- [ ] **`SUPPORTED_LOCALES`** in `web/frontend/src/lib/i18n/locale.ts` updated
  with the tag, its self-name, and `dir` (`'ltr'` or `'rtl'`). *(Frontend
  only — the TUI auto-detects from embedded files and needs no list edit.)*
- [ ] **Paraglide regenerated**: `src/lib/paraglide/` rebuilt via
  `npx @inlang/paraglide-js compile` (or the build did it). Do not hand-edit
  generated files.
- [ ] **Coverage documented**: state the Web UI and TUI coverage percentages
  (see [Coverage reporting](#coverage-reporting)). Security-sensitive and
  destructive workflows must be 100%.
- [ ] **PR labeled `ai-drafted`** if any AI-generated text is included, with
  the reviewer sign-off recorded for the destructive/security surfaces.
- [ ] **Representative screens exercised**: note which Web pages and TUI views
  were checked, including at least one plural-bearing screen and one
  destructive confirmation.

A locale that does not yet meet 100% coverage may still be merged as a
**draft** (not added to the supported list); runtime English fallback keeps the
interface working, but fallback is never counted as translated coverage.

---

## Coverage reporting

Coverage is tracked **separately for the Web UI and TUI**, because they are
separate catalogs. A locale can be complete on one surface and partial on the
other.

### The `i18n-check` tool (CI)

A dedicated `i18n-check` command is being added (tracked separately) and is
intended to be invoked by CI and locally. It verifies:

- English catalogs parse with unique, valid semantic IDs.
- Every declared supported locale has the required surface files (frontend
  `messages/<locale>.json` and/or TUI `active.<locale>.json`).
- Target keys exist in English; missing and stale/extra keys are reported.
- Required placeholders and selector names match English exactly.
- Complex plural/select messages compile for each locale.
- JSON is deterministic (sorted) and UTF-8 without comments.
- Locale filenames are canonical valid BCP 47 tags.
- Coverage is calculated per surface (Web UI and TUI), with English fallback
  **not** counted as translated.
- Generated Paraglide output is current under the project's generated-code
  policy.

Run it locally (once available) before opening a locale PR. Until it lands,
use the manual process below.

### Manual coverage check

Until `i18n-check` ships, compute coverage by hand against the English source:

1. Count the top-level keys in `web/frontend/messages/en.json` (excluding the
   `$schema` entry) — that is the denominator for the Web UI.
2. Count the top-level keys present in your `messages/<locale>.json`
   (excluding `$schema`) — the numerator. Keys that exist but whose value is
   still the English string count as **missing** for coverage purposes.
3. Repeat for the TUI: denominator = keys in
   `internal/tui/localization/locales/active.en.json`; numerator = keys in
   your `active.<locale>.json` whose `other`/`one` are actually translated
   (not copied English).
4. Report both percentages in the PR. For example: *"Web UI 98%, TUI 100%"*.

A locale must be **100% on all security-sensitive and destructive workflows**
(auth, delete, overwrite/move, proxy credentials, token display, revert,
destructive confirmations) before it is added to the supported list, even if
the overall percentage is lower.

### Frontend type-checking as a free validator

Because Paraglide generates typed functions, `npm run check` will fail to
compile if a placeholder name or argument is wrong — this catches many
placeholder/plural mistakes even before `i18n-check` runs.

---

## Locale maintainer expectations

Ownership of a locale is **optional** and lightweight. A locale maintainer is
a volunteer who:

- is a fluent speaker of the locale and is willing to review future AI drafts
  and contributor PRs for it;
- keeps the locale's coverage above the support threshold as new English
  strings are added (the English catalog is the source of truth; new English
  messages land in the same PR that introduces them);
- is named (GitHub handle) in the locale's PR description so reviewers know
  whom to ping.

There is no formal CODEOWNERS requirement for a locale to ship. A locale
without a maintainer can still be accepted if a fluent reviewer signs off on
the original PR; it simply has no guaranteed steward for follow-up strings.
When a maintainer steps down, the locale remains supported — English fallback
prevents breakage — but new strings will render in English until someone
re-translates them.

Maintainers should re-run the [review checklist](#review-checklist) whenever
the English catalog changes messages in their locale's scope, and watch the
coverage report for newly-added English keys that have no translation yet.

---

## Glossary

These are the domain terms a translator must handle consistently. For each:
what it means **in this project**, and whether to translate it or keep it
as-is. When a term is kept as-is, render it as a placeholder or literal inside
the message rather than translating it.

| Term | Meaning in Javinizer | Translate? |
|---|---|---|
| **Javinizer** | The application/product name. | **Keep as-is.** Never translate or transliterate in catalogs. |
| **JAV ID** | The identifier of a Japanese adult video title (e.g. `IPX-123`), parsed from a filename by the matcher. The primary key scrapers use to look up metadata. | **Keep as-is.** It is a programmatic/data term. Render as a placeholder where it appears in prose. |
| **Content ID** | An alternative identifier some sources expose (distinct from the JAV ID). A metadata field. | Translate the *label* ("Content ID" → localized equivalent) only if the locale has an established term; the ID value itself is never translated. Prefer keeping the English label if unclear. |
| **Scraper** | A source-site adapter that fetches metadata for a JAV ID (e.g. an R18, DMM, or javlibrary adapter). Aggregated by priority. | **Keep as-is** as a domain term. A short clarifying gloss in the message `description` is acceptable; do not coin a new translation like "fetcher". |
| **NFO** | An `.nfo` XML sidecar file written for media centers (Kodi/Jellyfin/Emby), holding movie metadata. Javinizer merges scraped data with existing NFO data. | **Keep as-is.** "NFO" is a recognized media-center term across locales. |
| **Metadata** | The structured movie data (title, actresses, genres, studio, etc.) scraped and stored. Distinct from "interface chrome". | Translate the word where it is user-facing prose, but keep it consistent with the locale's media-library conventions. |
| **Organize / Reorganize** | Moving/copying scraped files into a structured folder tree with artwork and NFO. "Reorganize in place" renames the folder and file without moving the file's location. | Translate the *verb* with a consistent term across the locale (e.g. "整理" / "再整理" in Japanese). Use the same root for both so the relationship is visible. |
| **Actress alias** | A performer may have multiple known names; the editor lets the user pick which name to write to the NFO `<name>` tag. | Translate "alias" per locale convention, but keep the underlying names as data. |
| **Proxy** | An HTTP/SOCKS proxy configured for scraper requests (and for FlareSolverr). Involves credential fields — security-sensitive. | Translate the label per locale; keep the surrounding credential text reviewed by a human. |
| **FlareSolverr** | A third-party service that solves Cloudflare challenges, used as a scraper proxy. A proper noun. | **Keep as-is.** It is a product name. |
| **Poster** | The front-cover-style image written to `poster.jpg` in the organized folder. | Translate the label per locale media-library convention; the filename stays `poster.ext`. |
| **Cover** | The primary front-cover artwork image. | Translate the label per locale; the value/URL is data. |
| **Fanart** | Background/extra artwork image(s) for media centers. | Translate per locale media-library convention where one exists; otherwise keep "fanart" — it is a recognized term in Kodi/Jellyfin ecosystems. |
| **Extrafanart** | A subfolder (`extrafanart/`) of additional artwork images. | **Keep as-is** as a folder/feature name; translate the descriptive label ("Download Extrafanart") consistently with how "fanart" was handled. |
| **Dry run** | A preview mode that reports what *would* happen without making file changes. Surfaced as a TUI badge `[DRY RUN]` and a setting. | Translate the setting label and the concept per locale; keep the badge short and recognisable. |
| **Revert** | Undo a previous organize/scrape operation, restoring files to their prior location and metadata state. Destructive-adjacent — confirmation text is security-sensitive. | Translate the verb consistently across the locale; ensure revert confirmations are human-reviewed. |
| **Force refresh** | Clear the DB cache and re-scrape metadata for an item, ignoring cached results. | Translate the label per locale; keep "refresh" consistent with the locale's refresh terminology elsewhere. |
| **Force update** | Replace existing files (images, NFO) during organize. | Translate the label per locale; disambiguate from "force refresh" in the `description`. |
| **Template tag** | A `{name}`-style token in folder/file name templates (e.g. `{title}`, `{id}`). | **Never translate** the tag itself; it is a literal. The *label* "Template Tags" can be translated. |
| **Allowed directory** | A path whitelisted under Settings → Security for scanning/organizing. | Translate the label per locale; the path value is data. |

When a term is not in this table, default to: translate user-facing prose
labels using the locale's established media-library conventions, and keep
proper nouns, product names, file extensions, folder names, IDs, and paths
as-is.
