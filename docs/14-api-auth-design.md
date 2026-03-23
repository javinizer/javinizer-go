# API Authentication Design (Deferred)

Status: Deferred  
Date: 2026-03-23

## Goal
Define a concrete implementation path for API authentication/authorization without enabling it yet in runtime code.

## Threat Model
- Unauthenticated access to state-changing API endpoints from untrusted networks.
- Credential leakage via verbose error messages or logs.
- Replay/abuse of static credentials without rate limiting or auditability.
- Cross-origin browser access to protected endpoints when CORS is misconfigured.

## Candidate Options
1. Static API key middleware
- Operationally simple, low implementation risk.
- Good fit for single-user/self-host deployments.
- Weak identity granularity (shared secret model).

2. JWT bearer authentication
- Stronger identity model and future role support.
- Higher implementation and key-management complexity.
- Requires token issuance/rotation strategy.

## Recommended First Implementation
Static API key middleware as phase 1, with a migration path to JWT if multi-user or role-based access is required.

## Migration Path
1. Add optional auth config block to `api.security` (disabled by default).
2. Implement middleware that protects `/api/v1/**` and `/ws/progress`, while keeping `/health`, `/docs`, and `/swagger/*any` public.
3. Add constant-time API key comparison and sanitized auth error responses.
4. Add audit log events for auth failures without logging secrets.
5. Extend design to JWT (phase 2) behind a separate auth mode switch.

## Acceptance Criteria For Implementation
- Auth disabled by default preserves current behavior.
- With auth enabled:
  - Missing/invalid credentials return `401`.
  - Valid credentials allow protected routes.
  - Public routes remain accessible.
  - WebSocket auth behavior matches HTTP auth policy.
- No secret values in error payloads or logs.
- Unit and integration tests cover allow/deny paths and race-safe middleware behavior.
