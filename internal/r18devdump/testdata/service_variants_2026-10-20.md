# Upstream service-variant fixture

Source: https://r18dotdev.s3.eu-west-1.wasabisys.com/dumps/r18dotdev_dump_2026-10-20.sql.gz
Official listing: https://r18.dev/dumps
Object size: 274,959,219 bytes; Last-Modified: 2026-10-20 03:04:19 UTC.

The fixture retains the original 20-column COPY header and 19 distinct source
rows. Only content_id, dvd_id, runtime_mins, release_date, site_id, and
service_code are retained; all unrelated payload columns are replaced with
PostgreSQL NULL. It is a representative sample, not the full dump.

Authoritative post-data DDL:

    ALTER TABLE ONLY public.derived_video
        ADD CONSTRAINT derived_video_pkey PRIMARY KEY (content_id, service_code);

Association primary keys use content_id and their entity ID, without service.
The dump contains legitimate video variants with different DVDs, runtimes and
release dates. There is no inferred preferred service. For ambiguous raw IDs,
local metadata lookups miss and existing caller fallback behavior is unchanged.

Evidence was obtained from the first 16 MiB plus final 256 KiB of the gzip
object. In 135,530 complete sampled video rows, 3,939 distinct content IDs had
multiple rows; no (content_id, service_code) pair repeated. No whole-dump counts
are inferred from this prefix.

SHA256 of original unredacted 19-row sample: ea660bf3e3faf8afd8dbeab7e8bcadff2f23d5facd8d623a67888b8f5fd2ae9a

SHA256 of redacted fixture: 04d9e625feaa7eb111faa0658e4deecf775c029809103fa6f8a739892a7a2860

The complete source gzip was subsequently acquired and passed gzip CRC checks.
Full gzip SHA256: 8230240cf9c655fc6c471b6f1ef0b21ed44bf83a61dfe52387ec022f3a525094
Official/recomputed multipart ETag: f37e191b5e00dd0e2811fef34f47cda1-33.

## Normalization fixture from the same dump

`unicode_normalization_2026-10-20.sql` retains exact content IDs, DVD IDs and
services for five video rows. Their DVD IDs contain a trailing U+3000
IDEOGRAPHIC SPACE. The importer already strips this through `normalizeDVDID`;
validation must use the same normalization rather than SQLite's ASCII-only
UPPER/REPLACE expression. No unrelated titles or comments are included.

Full-file verification with this fix stored all 1,914,173 video rows and passed
complete sidecar validation. There are 35,960 multi-service raw content IDs
and 44,587 additional service rows beyond one row per raw content ID.
