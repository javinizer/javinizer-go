# Shared offline evidence limits

`DefaultLimits` returns a fresh value with matching converter-output and store
artifact limits. Both CLI provisioning and runtime readers use this finite
contract: 32 MiB encoded artifacts, 100,000 records, 16 KiB record lines, 4 KiB
strings, 24 MiB total decoded strings, 32 aliases, 16 external IDs and 512 tokens
per record. Source input is bounded by 512 MiB, 1 MiB per line and 5 million COPY
rows. Store publication has a 1 GiB budget, 4096 entries and 4096-byte manifests.

These caps do not attest that an upstream dump is complete. Callers separately
bound concurrent conversions/reads and retained snapshots. The store does not
prune to fit its budget, and active durable-job artifact references must remain
on disk even after their manifests rotate out of current/previous. Windows can
read pre-provisioned artifacts; native publication remains fail-closed.
