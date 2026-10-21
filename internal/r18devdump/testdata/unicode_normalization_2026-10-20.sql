-- Exact projected fields from official r18dotdev_dump_2026-10-20.sql.gz
-- The dvd_id fields each contain one trailing UTF-8 U+3000 IDEOGRAPHIC SPACE.
COPY public.derived_video (content_id, dvd_id, service_code) FROM stdin;
49fa108	FA-108　	digital
49hs105	HS-105　	digital
49mvd84	MVD-84　	digital
49mvd85	MVD-85　	digital
49vf49	VF-49　	digital
\.
