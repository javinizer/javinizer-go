#!/usr/bin/env bash
# i18n-check: validate i18n catalogs (frontend Paraglide + TUI go-i18n).
# Implements design doc §10.1 catalog validation. Exits 0 when all error-level
# checks pass; warnings are reported but do not fail. Exits 1 on any error.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

FE_DIR="$REPO_ROOT/web/frontend"
FE_MSG_DIR="$FE_DIR/messages"
FE_SETTINGS="$FE_DIR/project.inlang/settings.json"
FE_PARAGLIDE_DIR="$FE_DIR/src/lib/paraglide"
TUI_DIR="$REPO_ROOT/internal/tui/localization/locales"

errors=0
warnings=0

err() { echo "ERROR: $*" >&2; errors=$((errors + 1)); }
warn() { echo "WARN:  $*" >&2; warnings=$((warnings + 1)); }
info() { echo "       $*"; }

require_jq() {
	if ! command -v jq >/dev/null 2>&1; then
		echo "ERROR: jq is required but not installed (install jq to run i18n-check)" >&2
		exit 2
	fi
}

# Canonical BCP 47: primary subtag 2-3 alpha, optional subtags 2-8 alphanum,
# hyphen-separated. Underscores (e.g. ja_JP) are non-canonical.
is_canonical_bcp47() {
	local tag="$1"
	[[ "$tag" =~ ^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$ ]] && [[ "$tag" != *_* ]]
}

fe_keys() {
	# English/source keys excluding the $schema metadata key.
	jq -r 'del(."$schema") | keys[]' "$1" | LC_ALL=C sort
}

fe_keys_unsorted() {
	jq -r 'del(."$schema") | keys[]' "$1"
}

fe_placeholder_map() {
	# key<TAB>comma-sep sorted unique {placeholder} tokens found in all string
	# leaves of the message (covers plain strings and plural match values).
	jq -r '
		del(."$schema")
		| to_entries[]
		| .key as $k
		| [(.value | .. | strings)] | join("\u0000")
		| [scan("[{][a-zA-Z_][a-zA-Z0-9_]*[}]")] | unique
		| "\($k)\t\(join(","))"
	' "$1" | LC_ALL=C sort
}

fe_selector_map() {
	# key<TAB>comma-sep selectors for native Paraglide plural messages (arrays).
	# Only emits keys whose value is an array, so flat-string targets are not
	# compared (a target may legitimately collapse a plural to its other-form).
	jq -r '
		del(."$schema")
		| to_entries[]
		| select(.value | type == "array")
		| .key as $k
		| (.value | [.[].selectors // [] | .[]] | unique)
		| "\($k)\t\(join(","))"
	' "$1" | LC_ALL=C sort
}

tui_keys() {
	jq -r 'keys[]' "$1" | LC_ALL=C sort
}

tui_placeholder_map() {
	jq -r '
		to_entries[]
		| .key as $k
		| [(.value | .. | strings)] | join("\u0000")
		| [scan("[{][{][.][A-Za-z_][A-Za-z0-9_]*[}]")] | unique
		| "\($k)\t\(join(","))"
	' "$1" | LC_ALL=C sort
}

validate_frontend() {
	echo "== Frontend catalog (Paraglide) =="

	if [ ! -f "$FE_SETTINGS" ]; then
		err "missing Inlang settings: $FE_SETTINGS"
		return
	fi

	# Locales declared in project.inlang/settings.json.
	local fe_locales
	fe_locales="$(jq -r '.locales[]' "$FE_SETTINGS")"
	local base_locale
	base_locale="$(jq -r '.baseLocale' "$FE_SETTINGS")"

	if [ -z "$fe_locales" ]; then
		err "no locales declared in $FE_SETTINGS"
		return
	fi

	local locale
	for locale in $fe_locales; do
		local file="$FE_MSG_DIR/$locale.json"
		if [ ! -f "$file" ]; then
			err "frontend: locale '$locale' declared in settings but missing $file"
			continue
		fi

		if ! is_canonical_bcp47 "$locale"; then
			err "frontend: locale filename '$locale' is not canonical BCP 47 (use hyphens, e.g. ja-JP not ja_JP)"
		fi

		if ! jq empty "$file" 2>/dev/null; then
			err "frontend: $file is not valid JSON (or has duplicate keys)"
			continue
		fi

		# Sorted ascending (C byte order, matching Paraglide sort:asc).
		if ! diff -q <(fe_keys_unsorted "$file") <(fe_keys "$file") >/dev/null; then
			warn "frontend: $locale.json keys are not sorted asc (run the Inlang formatter / Paraglide compile to re-sort)"
		fi

		# Native Paraglide plurals must be arrays [{declarations,selectors,match}].
		local bad_plural
		bad_plural="$(jq -r '
			del(."$schema")
			| to_entries[]
			| select((.value | type) == "object")
			| .key
		' "$file")"
		if [ -n "$bad_plural" ]; then
			err "frontend: $locale.json has object-form messages (expected string or plural array): $bad_plural"
		fi
	done

	# Base locale must exist.
	local en_file="$FE_MSG_DIR/$base_locale.json"
	if [ ! -f "$en_file" ]; then
		err "frontend: base locale catalog missing: $en_file"
		return
	fi

	# Per-target locale: key coverage, missing/stale keys, placeholder + selector parity.
	local en_keys_file
	en_keys_file="$(mktemp)"
	fe_keys "$en_file" >"$en_keys_file"
	local en_count
	en_count="$(wc -l <"$en_keys_file" | tr -d ' ')"
	info "frontend base '$base_locale': $en_count message keys"

	local en_ph_file en_sel_file
	en_ph_file="$(mktemp)"
	en_sel_file="$(mktemp)"
	fe_placeholder_map "$en_file" >"$en_ph_file"
	fe_selector_map "$en_file" >"$en_sel_file"

	for locale in $fe_locales; do
		[ "$locale" = "$base_locale" ] && continue
		local t_file="$FE_MSG_DIR/$locale.json"
		[ -f "$t_file" ] || continue

		local t_keys_file
		t_keys_file="$(mktemp)"
		fe_keys "$t_file" >"$t_keys_file"
		local t_count
		t_count="$(wc -l <"$t_keys_file" | tr -d ' ')"

		# Missing keys: target keys absent from English (error).
		local missing
		missing="$(comm -23 "$t_keys_file" "$en_keys_file")"
		if [ -n "$missing" ]; then
			local n
			n="$(printf '%s\n' "$missing" | grep -c . || true)"
			err "frontend: $locale.json has $n key(s) missing from English base (e.g. $(printf '%s\n' "$missing" | head -1))"
		fi

		# Stale/extra keys: English keys absent from target (warning).
		local stale
		stale="$(comm -13 "$t_keys_file" "$en_keys_file")"
		if [ -n "$stale" ]; then
			local n
			n="$(printf '%s\n' "$stale" | grep -c . || true)"
			warn "frontend: $locale.json is missing $n key(s) present in English (incomplete translation)"
		fi

		# Split-key _one/_other remnants: any key ending _one/_other without a base key.
		local remnants
		remnants="$(jq -r 'del(."$schema") | keys[]' "$t_file" | grep -E '_(one|other)$' || true)"
		if [ -n "$remnants" ]; then
			local r
			for r in $remnants; do
				local base="${r%_one}"
				base="${base%_other}"
				if ! grep -qx "$base" "$en_keys_file"; then
					warn "frontend: $locale.json has split-key remnant '$r' with no base key '$base'"
				fi
			done
		fi

		# Placeholder parity (error).
		local t_ph_file
		t_ph_file="$(mktemp)"
		fe_placeholder_map "$t_file" >"$t_ph_file"
		local ph_mismatch
		ph_mismatch="$(join -t$'\t' "$en_ph_file" "$t_ph_file" | awk -F'\t' '$2 != $3 {print $1}')"
		if [ -n "$ph_mismatch" ]; then
			err "frontend: $locale.json placeholder mismatch vs English for: $(printf '%s, ' $ph_mismatch)"
		fi
		rm -f "$t_ph_file"

		# Selector parity for plural messages (error).
		local t_sel_file
		t_sel_file="$(mktemp)"
		fe_selector_map "$t_file" >"$t_sel_file"
		local sel_mismatch
		sel_mismatch="$(join -t$'\t' "$en_sel_file" "$t_sel_file" | awk -F'\t' '$2 != $3 {print $1}')"
		if [ -n "$sel_mismatch" ]; then
			err "frontend: $locale.json plural selector mismatch vs English for: $(printf '%s, ' $sel_mismatch)"
		fi
		rm -f "$t_sel_file"

		# Coverage.
		if [ "$en_count" -gt 0 ]; then
			local pct
			pct=$((t_count * 100 / en_count))
			local status="complete"
			[ "$t_count" -eq "$en_count" ] || status="incomplete"
			info "frontend coverage [$locale]: $t_count/$en_count = ${pct}% ($status)"
		fi
		rm -f "$t_keys_file"
	done

	rm -f "$en_keys_file" "$en_ph_file" "$en_sel_file"

	# Generated Paraglide output freshness.
	validate_paraglide_freshness
}

validate_paraglide_freshness() {
	echo "== Paraglide generated output =="
	if [ ! -d "$FE_DIR/node_modules/@inlang/paraglide-js" ]; then
		warn "skipping Paraglide freshness check: @inlang/paraglide-js not installed (run 'npm ci --prefix web/frontend')"
		return
	fi

	# The generated dir is gitignored (auto-generated by the vite plugin at
	# build/dev time). The meaningful CI check is that compile SUCCEEDS, which
	# validates that complex plural/select messages compile for every locale.
	# A changed hash is only a local-staleness signal (self-correcting on the
	# next build), so it is a warning, not an error.

	local existed=0
	[ -d "$FE_PARAGLIDE_DIR" ] && existed=1

	local before_hash=""
	if [ "$existed" -eq 1 ]; then
		before_hash="$(find "$FE_PARAGLIDE_DIR" -type f -print0 2>/dev/null | LC_ALL=C sort -z | xargs -0 shasum 2>/dev/null | shasum | awk '{print $1}')"
	fi

	if ! (cd "$FE_DIR" && npx --no-install @inlang/paraglide-js compile --project ./project.inlang --outdir ./src/lib/paraglide >/dev/null 2>&1); then
		err "Paraglide compile failed (complex plural/select messages may not compile) — run 'cd web/frontend && npx @inlang/paraglide-js compile' to see errors"
		return
	fi

	if [ "$existed" -eq 1 ]; then
		local after_hash
		after_hash="$(find "$FE_PARAGLIDE_DIR" -type f -print0 2>/dev/null | LC_ALL=C sort -z | xargs -0 shasum 2>/dev/null | shasum | awk '{print $1}')"
		if [ "$before_hash" != "$after_hash" ]; then
			warn "Paraglide generated output was stale and has been regenerated (output is gitignored; 'make web-build' / vite dev also regenerate it). If compile keeps reporting changes, run 'cd web/frontend && npx @inlang/paraglide-js compile'."
		else
			info "Paraglide generated output is up to date"
		fi
	else
		info "Paraglide generated output created (fresh checkout; nothing to diff)"
	fi
}

validate_tui() {
	echo "== TUI catalog (go-i18n) =="

	local en_file="$TUI_DIR/active.en.json"
	if [ ! -f "$en_file" ]; then
		err "missing TUI base catalog: $en_file"
		return
	fi

	if ! jq empty "$en_file" 2>/dev/null; then
		err "TUI: active.en.json is not valid JSON (or has duplicate keys)"
		return
	fi

	# Validate every catalog file present (en + future locales).
	local file
	for file in "$TUI_DIR"/active.*.json; do
		[ -f "$file" ] || continue
		local locale
		locale="$(basename "$file" | sed -E 's/^active\.(.*)\.json$/\1/')"
		if ! is_canonical_bcp47 "$locale"; then
			err "TUI: locale filename '$locale' is not canonical BCP 47 (use hyphens, e.g. ja-JP not ja_JP)"
		fi
		if ! jq empty "$file" 2>/dev/null; then
			err "TUI: $file is not valid JSON (or has duplicate keys)"
			continue
		fi

		# Every key must have description + other.
		local missing_desc missing_other
		missing_desc="$(jq -r 'to_entries[] | select((.value|has("description"))|not) | .key' "$file")"
		missing_other="$(jq -r 'to_entries[] | select((.value|has("other"))|not) | .key' "$file")"
		if [ -n "$missing_desc" ]; then
			err "TUI: $locale missing 'description' for: $(printf '%s, ' $missing_desc)"
		fi
		if [ -n "$missing_other" ]; then
			err "TUI: $locale missing 'other' form for: $(printf '%s, ' $missing_other)"
		fi

		# Plurals: any key with 'one' must also have 'other'.
		local one_no_other
		one_no_other="$(jq -r 'to_entries[] | select(.value|has("one")) | select((.value|has("other"))|not) | .key' "$file")"
		if [ -n "$one_no_other" ]; then
			err "TUI: $locale has 'one' without 'other' for: $(printf '%s, ' $one_no_other)"
		fi
	done

	# Coverage + parity per non-en locale.
	local en_keys_file
	en_keys_file="$(mktemp)"
	tui_keys "$en_file" >"$en_keys_file"
	local en_count
	en_count="$(wc -l <"$en_keys_file" | tr -d ' ')"
	info "TUI base 'en': $en_count message keys"

	local en_ph_file
	en_ph_file="$(mktemp)"
	tui_placeholder_map "$en_file" >"$en_ph_file"

	for file in "$TUI_DIR"/active.*.json; do
		[ -f "$file" ] || continue
		local locale
		locale="$(basename "$file" | sed -E 's/^active\.(.*)\.json$/\1/')"
		[ "$locale" = "en" ] && continue

		local t_keys_file
		t_keys_file="$(mktemp)"
		tui_keys "$file" >"$t_keys_file"
		local t_count
		t_count="$(wc -l <"$t_keys_file" | tr -d ' ')"

		local missing
		missing="$(comm -23 "$t_keys_file" "$en_keys_file")"
		if [ -n "$missing" ]; then
			local n
			n="$(printf '%s\n' "$missing" | grep -c . || true)"
			err "TUI: $locale has $n key(s) missing from English base (e.g. $(printf '%s\n' "$missing" | head -1))"
		fi

		local t_ph_file
		t_ph_file="$(mktemp)"
		tui_placeholder_map "$file" >"$t_ph_file"
		local ph_mismatch
		ph_mismatch="$(join -t$'\t' "$en_ph_file" "$t_ph_file" | awk -F'\t' '$2 != $3 {print $1}')"
		if [ -n "$ph_mismatch" ]; then
			err "TUI: $locale placeholder mismatch vs English for: $(printf '%s, ' $ph_mismatch)"
		fi
		rm -f "$t_ph_file"

		if [ "$en_count" -gt 0 ]; then
			local pct
			pct=$((t_count * 100 / en_count))
			local status="complete"
			[ "$t_count" -eq "$en_count" ] || status="incomplete"
			info "TUI coverage [$locale]: $t_count/$en_count = ${pct}% ($status)"
		fi
		rm -f "$t_keys_file"
	done

	rm -f "$en_keys_file" "$en_ph_file"
}

main() {
	require_jq
	echo "i18n-check: validating catalogs in $REPO_ROOT"
	validate_frontend
	echo
	validate_tui
	echo
	echo "== Summary =="
	echo "       errors:   $errors"
	echo "       warnings: $warnings"
	if [ "$errors" -gt 0 ]; then
		echo "i18n-check: FAILED (errors found)" >&2
		exit 1
	fi
	echo "i18n-check: OK (warnings allowed)"
	exit 0
}

main "$@"
