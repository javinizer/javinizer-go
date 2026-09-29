import type { Movie, MovieCredit } from '$lib/api/types';

/**
 * Change signature for the live display-title preview in MovieEditor.
 * A change in the signature refires the (debounced) display-title-preview POST.
 *
 * The signature keys ONLY on inputs the title template actually renders:
 *
 * - Volatile actress/credit fields (thumb_url, origin, name_key, aliases,
 *   reported_thumb_url, dmm_id, timestamps) are excluded: media snapshots and
 *   identity resolution churn them between job polls while the rendered title
 *   is unchanged (v1.6.0 flap between "Preview unavailable" and "Rendering
 *   preview…").
 * - Raw `verified` is excluded: since v1.6.1 the render gate is
 *   `verified || !ambiguity_quarantined`, so a bare verified flip does not
 *   change the rendered title. Render visibility instead comes from the
 *   server-computed `render_visible` credit flag (the exact template gate).
 * - Credits drive the render whenever any exist (mirrors
 *   NewContextFromMovieWithOptions), so the raw movie.actresses array — whose
 *   authoritative set membership oscillates mid-batch as quarantine is
 *   recomputed across sibling movies — is only hashed wholesale when no
 *   credits render; with credits, only the canonical names of actresses
 *   referenced by visible credits are hashed (identity renames change the
 *   rendered canonical name and must refire). A visible credit whose user
 *   override determines the rendered name (user_override with a non-blank
 *   override_name) contributes NO canonical names — RenderNameWithSelection
 *   renders the override regardless of the identity row, and the override
 *   text itself stays hashed in the credit render token.
 * - A credit that cannot render (render_visible=false, or locally suppressed,
 *   matching the nil-actress/quarantine gate) hashes to a fixed,
 *   identity-independent marker: its name/order churn AND identity
 *   reconciliation re-pointing its actress_id are invisible in the title and
 *   must not refire. The marker still pads the array slot, so visible credits
 *   reordering past a hidden one keep invalidating the signature positionally.
 * - poster_url is dropped: the title Context has no poster field (cover_url
 *   and trailer_url remain — they are template inputs).
 */
export function buildDisplayTitlePreviewSignature(movie: Movie): string {
	const credits = movie.credits ?? [];
	const creditTokens = credits.map((c) => creditRenderToken(c));
	const includeActresses = credits.length === 0;
	return JSON.stringify({
		id: movie.id,
		code: movie.code,
		title: movie.title,
		original_title: movie.original_title,
		description: movie.description,
		actresses: includeActresses
			? (movie.actresses ?? []).map((a) => [
					a.id ?? 0,
					a.first_name ?? '',
					a.last_name ?? '',
					a.japanese_name ?? '',
				])
			: null,
		referencedActresses: includeActresses ? null : referencedActressTokens(movie, credits),
		credits: creditTokens,
		genres: movie.genres,
		runtime: movie.runtime,
		release_year: movie.release_year,
		release_date: movie.release_date,
		director: movie.director,
		maker: movie.maker,
		label: movie.label,
		series: movie.series,
		rating_score: movie.rating_score,
		cover_url: movie.cover_url,
		trailer_url: movie.trailer_url,
		original_filename: movie.original_filename,
	});
}

// canonical-name tokens for actresses referenced by VISIBLE credits, in credit
// order. Canonical-name rendering (the default) draws the rendered name from
// the actress identity row, so an identity rename must refire the preview even
// though credit-level fields are unchanged. Unreferenced/extra actresses in the
// poll payload cannot affect the render and stay out of the hash. A credit
// whose user override determines the rendered name (user_override set with a
// non-blank override_name — RenderNameWithSelection's first-precedence arm,
// ahead of display_force_canonical) contributes only its actress_id: canonical
// name churn on the referenced identity row cannot reach the render and must
// not refire; the override text itself remains hashed via creditRenderToken.
function referencedActressTokens(movie: Movie, credits: MovieCredit[]): unknown[] {
	const actresses = movie.actresses ?? [];
	return credits
		.filter((c) => (c.render_visible ?? true) && !(c.suppressed ?? false))
		.map((c) => {
			if (c.user_override && (c.override_name ?? '').trim() !== '') {
				return [c.actress_id ?? 0];
			}
			const a = actresses.find((x) => (x.id ?? 0) === (c.actress_id ?? 0));
			return [c.actress_id ?? 0, a?.first_name ?? '', a?.last_name ?? '', a?.japanese_name ?? ''];
		});
}

function creditRenderToken(c: MovieCredit): unknown[] {
	const visible = (c.render_visible ?? true) && !(c.suppressed ?? false);
	if (!visible) {
		return ['hidden'];
	}
	return [
		c.actress_id ?? 0,
		c.credited_name ?? '',
		c.credited_japanese_name ?? '',
		c.override_name ?? '',
		c.order_index ?? 0,
		c.order_pinned ?? false,
		c.display_force_canonical ?? false,
		c.user_override ?? false,
	];
}
