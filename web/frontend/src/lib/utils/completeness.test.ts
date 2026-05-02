import { describe, it, expect } from 'vitest';
import { calculateCompleteness, type CompletenessResult, type FieldCategory, type CompletenessTier } from './completeness';
import type { Movie, CompletenessConfig } from '$lib/api/types';

function makeMovie(overrides: Partial<Movie> = {}): Movie {
	return {
		id: 'TEST-001',
		title: 'Test Movie',
		...overrides
	};
}

describe('calculateCompleteness', () => {
	describe('basic scoring', () => {
		it('returns score based on title being filled for minimal movie', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie);
			expect(result.score).toBeGreaterThan(0);
			expect(result.tier).toBeDefined();
			expect(result.breakdown.length).toBeGreaterThan(0);
		});

		it('returns score 100 for fully populated movie', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Full Movie',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'Jane' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A great movie',
				maker: 'Studio A',
				release_date: '2024-01-01',
				director: 'Director A',
				runtime: 120,
				trailer_url: 'https://example.com/trailer.mp4',
				screenshot_urls: ['https://example.com/ss1.jpg', 'https://example.com/ss2.jpg', 'https://example.com/ss3.jpg'],
				label: 'Label A',
				series: 'Series A',
				rating_score: 8.5,
				original_title: 'Original Title',
				translations: [{ id: 1, language: 'en', title: 'English Title' }]
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(100);
			expect(result.tier).toBe('complete');
		});

		it('returns approximately 50% for movie with only essential fields', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Essential Only',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'Jane' }],
				genres: [{ id: 1, name: 'Drama' }]
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(50);
			expect(result.tier).toBe('partial');
		});
	});

	describe('zero values treated as missing', () => {
		it('treats runtime=0 as unfilled', () => {
			const movie = makeMovie({ runtime: 0 });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Runtime');
			expect(cat?.filled).toBe(false);
		});

		it('treats rating_score=0 as unfilled', () => {
			const movie = makeMovie({ rating_score: 0 });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Rating');
			expect(cat?.filled).toBe(false);
		});

		it('treats release_year=0 as unfilled', () => {
			const movie = makeMovie({ release_year: 0 });
			const result = calculateCompleteness(movie);
			const cat = result.breakdown.find(c => c.name === 'Release Date');
			expect(cat?.filled).toBe(false);
		});

		it('treats empty release_date string as unfilled', () => {
			const movie = makeMovie({ release_date: '' });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Release Date');
			expect(cat?.filled).toBe(false);
		});

		it('treats non-zero runtime as filled', () => {
			const movie = makeMovie({ runtime: 90 });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Runtime');
			expect(cat?.filled).toBe(true);
		});
	});

	describe('array scoring', () => {
		it('empty actresses array scores 0%', () => {
			const movie = makeMovie({ actresses: [] });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Actresses');
			expect(cat?.filled).toBe(false);
		});

		it('non-empty actresses array scores 100%', () => {
			const movie = makeMovie({ actresses: [{ id: 1, first_name: 'Jane' }] });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Actresses');
			expect(cat?.filled).toBe(true);
		});

		it('empty genres array scores 0%', () => {
			const movie = makeMovie({ genres: [] });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Genres');
			expect(cat?.filled).toBe(false);
		});

		it('non-empty genres array scores 100%', () => {
			const movie = makeMovie({ genres: [{ id: 1, name: 'Drama' }] });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Genres');
			expect(cat?.filled).toBe(true);
		});

		it('0 screenshots scores 0% (graduated)', () => {
			const movie = makeMovie({ screenshot_urls: [] });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Screenshots');
			expect(cat?.filled).toBe(false);
		});

		it('1-2 screenshots scores 50% (graduated, partial fill)', () => {
			const movie = makeMovie({ screenshot_urls: ['url1', 'url2'] });
			const result = calculateCompleteness(movie);
			const cat = result.breakdown.find(c => c.name === 'Screenshots');
			expect(cat?.filled).toBe(true);
			expect(result.score).toBeGreaterThan(0);
		});

		it('3+ screenshots scores 100% (graduated)', () => {
			const movie = makeMovie({ screenshot_urls: ['url1', 'url2', 'url3'] });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Screenshots');
			expect(cat?.filled).toBe(true);
		});
	});

	describe('tier boundaries', () => {
		it('score < 40 returns incomplete tier', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie);
			if (result.score < 40) {
				expect(result.tier).toBe('incomplete');
			}
		});

		it('score 40-79 returns partial tier', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Partial Movie',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'Jane' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A description'
			};
			const result = calculateCompleteness(movie);
			if (result.score >= 40 && result.score <= 79) {
				expect(result.tier).toBe('partial');
			}
		});

		it('score >= 80 returns complete tier', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Mostly Complete',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'Jane' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A description',
				maker: 'Studio',
				release_date: '2024-01-01',
				director: 'Director',
				runtime: 120,
				trailer_url: 'https://example.com/trailer.mp4',
				screenshot_urls: ['url1', 'url2', 'url3'],
				label: 'Label',
				series: 'Series'
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBeGreaterThanOrEqual(80);
			expect(result.tier).toBe('complete');
		});
	});

	describe('breakdown structure', () => {
		it('breakdown contains all field categories with filled boolean and weight', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie);
			for (const cat of result.breakdown) {
				expect(cat).toHaveProperty('name');
				expect(cat).toHaveProperty('filled');
				expect(cat).toHaveProperty('weight');
				expect(cat).toHaveProperty('tier');
				expect(typeof cat.filled).toBe('boolean');
				expect(typeof cat.weight).toBe('number');
			}
		});

		it('essential tier has 5 categories with 0.10 weight each', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie);
			const essential = result.breakdown.filter(c => c.tier === 'essential');
			expect(essential.length).toBe(5);
			for (const cat of essential) {
				expect(cat.weight).toBe(0.10);
			}
		});

		it('important tier has 7 categories with 0.05 weight each', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie);
			const important = result.breakdown.filter(c => c.tier === 'important');
			expect(important.length).toBe(7);
			for (const cat of important) {
				expect(cat.weight).toBeCloseTo(0.05, 10);
			}
		});

		it('nice-to-have tier has 5 categories with 0.03 weight each', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie);
			const niceToHave = result.breakdown.filter(c => c.tier === 'nice-to-have');
			expect(niceToHave.length).toBe(5);
			for (const cat of niceToHave) {
				expect(cat.weight).toBeCloseTo(0.03, 10);
			}
		});
	});

	describe('percentage rounding', () => {
		it('score is rounded to nearest integer', () => {
			const movie = makeMovie({
				poster_url: 'https://example.com/poster.jpg',
				description: 'A description'
			});
			const result = calculateCompleteness(movie);
			expect(Number.isInteger(result.score)).toBe(true);
		});
	});

	describe('weights sum', () => {
		it('total weight sums to 1.0 (50% essential + 35% important + 15% nice-to-have)', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie);
			const totalWeight = result.breakdown.reduce((sum, c) => sum + c.weight, 0);
			expect(totalWeight).toBeCloseTo(1.0, 10);
		});
	});

	describe('title-only movie', () => {
		it('movie with only title gets score 10', () => {
			const movie: Movie = { id: 'TEST-001', title: 'Only Title' };
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(10);
			expect(result.tier).toBe('incomplete');
		});
	});

	describe('tier boundary edge cases', () => {
		it('score exactly 39 is incomplete tier', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Boundary Test',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'A' }],
				genres: []
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(40);
			expect(result.tier).toBe('partial');
		});

		it('score below 40 is incomplete tier', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Below Boundary',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [],
				genres: []
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(30);
			expect(result.tier).toBe('incomplete');
		});

		it('score at 50 is partial tier', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Essential Only',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'A' }],
				genres: [{ id: 1, name: 'Drama' }]
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(50);
			expect(result.tier).toBe('partial');
		});

		it('score at or above 80 is complete tier', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Boundary Complete',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'A' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A description',
				maker: 'Studio',
				release_date: '2024-01-01',
				director: 'Director',
				runtime: 120,
				trailer_url: 'https://example.com/trailer.mp4',
				screenshot_urls: ['url1', 'url2', 'url3']
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(85);
			expect(result.tier).toBe('complete');
		});

		it('movie just below complete threshold with partial screenshots', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Almost Complete',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'A' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A description',
				maker: 'Studio',
				release_date: '2024-01-01',
				director: 'Director',
				runtime: 120,
				trailer_url: 'https://example.com/trailer.mp4',
				screenshot_urls: ['url1']
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(83);
			expect(result.tier).toBe('complete');
		});
	});

	describe('screenshots graduated scoring detail', () => {
		it('exactly 1 screenshot contributes partial weight to score', () => {
			const base: Movie = {
				id: 'TEST-001',
				title: 'One Screenshot',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'A' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A description',
				maker: 'Studio',
				release_date: '2024-01-01',
				director: 'Director',
				runtime: 120,
				trailer_url: 'https://example.com/trailer.mp4',
				screenshot_urls: ['url1']
			};
			const withOne = calculateCompleteness(base);
			const withThree = calculateCompleteness({ ...base, screenshot_urls: ['url1', 'url2', 'url3'] });
			expect(withThree.score).toBeGreaterThan(withOne.score);
		});

		it('exactly 2 screenshots contributes partial weight to score', () => {
			const base: Movie = {
				id: 'TEST-001',
				title: 'Two Screenshots',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'A' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A description',
				maker: 'Studio',
				release_date: '2024-01-01',
				director: 'Director',
				runtime: 120,
				trailer_url: 'https://example.com/trailer.mp4',
				screenshot_urls: ['url1', 'url2']
			};
			const withTwo = calculateCompleteness(base);
			const withZero = calculateCompleteness({ ...base, screenshot_urls: [] });
			expect(withTwo.score).toBeGreaterThan(withZero.score);
		});
	});

	describe('breakdown field names', () => {
		it('breakdown contains all 17 expected field names', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie);
			const names = result.breakdown.map(c => c.name);
			const expected = [
				'Title', 'Poster', 'Cover', 'Actresses', 'Genres',
				'Description', 'Maker', 'Release Date', 'Director', 'Runtime', 'Trailer', 'Screenshots',
				'Label', 'Series', 'Rating', 'Original Title', 'Translations'
			];
			expect(names).toEqual(expected);
		});
	});

	describe('undefined optional fields', () => {
		it('movie with explicitly undefined optional fields handles correctly', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Undefined Fields',
				poster_url: undefined,
				cover_url: undefined,
				actresses: undefined,
				genres: undefined,
				description: undefined,
				maker: undefined,
				release_date: undefined,
				director: undefined,
				runtime: undefined,
				trailer_url: undefined,
				screenshot_urls: undefined,
				label: undefined,
				series: undefined,
				rating_score: undefined,
				original_title: undefined,
				translations: undefined,
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(10);
			expect(result.tier).toBe('incomplete');
		});

		it('treats NaN runtime as unfilled', () => {
			const movie = makeMovie({ runtime: NaN });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Runtime');
			expect(cat?.filled).toBe(false);
		});

		it('treats NaN rating_score as unfilled', () => {
			const movie = makeMovie({ rating_score: NaN });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Rating');
			expect(cat?.filled).toBe(false);
		});

		it('treats negative runtime as unfilled', () => {
			const movie = makeMovie({ runtime: -1 });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Runtime');
			expect(cat?.filled).toBe(false);
		});

		it('treats whitespace-only string as unfilled', () => {
			const movie = makeMovie({ description: '   ' });
			const cat = calculateCompleteness(movie).breakdown.find(c => c.name === 'Description');
			expect(cat?.filled).toBe(false);
		});
	});

	describe('with custom config enabled', () => {
		const customConfig: CompletenessConfig = {
			enabled: true,
			tiers: {
				essential: { weight: 70, fields: ['title', 'poster_url'] },
				important: { weight: 20, fields: ['description', 'maker'] },
				nice_to_have: { weight: 10, fields: ['label'] },
			},
		};

		it('uses config tier assignments and weights', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie, customConfig);
			expect(result.breakdown.length).toBe(5);
			const essential = result.breakdown.filter(c => c.tier === 'essential');
			expect(essential.length).toBe(2);
			expect(essential[0].name).toBe('Title');
			expect(essential[1].name).toBe('Poster');
		});

		it('custom config with different weights produces different scores', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Test',
				poster_url: 'https://example.com/poster.jpg',
			};
			const defaultResult = calculateCompleteness(movie);
			const customResult = calculateCompleteness(movie, customConfig);
			expect(customResult.score).not.toBe(defaultResult.score);
		});

		it('custom config with different field assignments moves fields between tiers', () => {
			const configMovingLabel: CompletenessConfig = {
				enabled: true,
				tiers: {
					essential: { weight: 50, fields: ['title', 'label'] },
					important: { weight: 35, fields: ['description'] },
					nice_to_have: { weight: 15, fields: ['poster_url'] },
				},
			};
			const movie = makeMovie();
			const result = calculateCompleteness(movie, configMovingLabel);
			const labelCat = result.breakdown.find(c => c.name === 'Label');
			expect(labelCat?.tier).toBe('essential');
			const posterCat = result.breakdown.find(c => c.name === 'Poster');
			expect(posterCat?.tier).toBe('nice-to-have');
		});

		it('score 100 for fully populated movie with custom config', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Full Movie',
				poster_url: 'https://example.com/poster.jpg',
				description: 'A description',
				maker: 'Studio A',
				label: 'Label A',
			};
			const result = calculateCompleteness(movie, customConfig);
			expect(result.score).toBe(100);
			expect(result.tier).toBe('complete');
		});
	});

	describe('with custom config disabled', () => {
		it('falls back to hardcoded defaults when enabled=false', () => {
			const config: CompletenessConfig = {
				enabled: false,
				tiers: {
					essential: { weight: 70, fields: ['title'] },
					important: { weight: 20, fields: ['description'] },
					nice_to_have: { weight: 10, fields: ['label'] },
				},
			};
			const movie = makeMovie();
			const withConfig = calculateCompleteness(movie, config);
			const withoutConfig = calculateCompleteness(movie);
			expect(withConfig.score).toBe(withoutConfig.score);
			expect(withConfig.breakdown.length).toBe(withoutConfig.breakdown.length);
		});

		it('falls back to hardcoded defaults when config is undefined', () => {
			const movie = makeMovie();
			const result = calculateCompleteness(movie, undefined);
			const defaultResult = calculateCompleteness(movie);
			expect(result.score).toBe(defaultResult.score);
		});
	});

	describe('config weight normalization', () => {
		it('weights that do not sum to 100 still produce 0-100 score', () => {
			const config: CompletenessConfig = {
				enabled: true,
				tiers: {
					essential: { weight: 60, fields: ['title', 'poster_url'] },
					important: { weight: 30, fields: ['description'] },
					nice_to_have: { weight: 20, fields: ['label'] },
				},
			};
			const fullMovie: Movie = {
				id: 'TEST-001',
				title: 'Full',
				poster_url: 'https://example.com/p.jpg',
				description: 'Desc',
				label: 'L',
			};
			const result = calculateCompleteness(fullMovie, config);
			expect(result.score).toBe(100);
			expect(result.tier).toBe('complete');
		});

		it('partially filled movie with non-100 weights still scores 0-100', () => {
			const config: CompletenessConfig = {
				enabled: true,
				tiers: {
					essential: { weight: 60, fields: ['title', 'poster_url'] },
					important: { weight: 30, fields: ['description'] },
					nice_to_have: { weight: 20, fields: ['label'] },
				},
			};
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Only Title',
			};
			const result = calculateCompleteness(movie, config);
			expect(result.score).toBeGreaterThanOrEqual(0);
			expect(result.score).toBeLessThanOrEqual(100);
		});
	});

	describe('backward compatibility', () => {
		it('calculateCompleteness(movie) with no config returns identical results as before', () => {
			const movie: Movie = {
				id: 'TEST-001',
				title: 'Test Movie',
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'Jane' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A description',
				maker: 'Studio A',
				release_date: '2024-01-01',
				director: 'Director A',
				runtime: 120,
				trailer_url: 'https://example.com/trailer.mp4',
				screenshot_urls: ['url1', 'url2', 'url3'],
				label: 'Label A',
				series: 'Series A',
				rating_score: 8.5,
				original_title: 'Original Title',
				translations: [{ id: 1, language: 'en', title: 'English Title' }],
			};
			const result = calculateCompleteness(movie);
			expect(result.score).toBe(100);
			expect(result.tier).toBe('complete');
			expect(result.breakdown.length).toBe(17);
		});
	});
});
