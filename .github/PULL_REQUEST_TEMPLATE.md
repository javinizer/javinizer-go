<!--
Thanks for the PR! Keep the description focused — reviewers should understand
the *what* and *why* in under 30 seconds.

Issue linking: if this PR resolves an existing issue, use a closing keyword
below (`Closes #NN` / `Fixes #NN` / `Resolves #NN`). A bare `(#NN)` reference
does NOT auto-close the issue. This matters for both PR merges and direct
commits to main — GitHub only auto-closes on the keywords.
-->

## Summary

<!-- 1–3 sentences. What changed and why. -->

## Related Issue

<!-- e.g. Closes #79  — delete if there's no related issue -->

## Checklist

- [ ] Tests added/updated for the change
- [ ] `make test-short` passes locally
- [ ] `make lint` passes (golangci-lint)
- [ ] Commit messages follow `type(scope): summary` (≤72 chars)
- [ ] Linked the issue with `Closes #NN` (if a related issue exists)
- [ ] If adding a `*_timeout_seconds` or `*_timeout` config field, confirmed it is read by a runtime code path outside the config package (the grep lint guard only catches hard-coded timeout literals, not unread config fields)
