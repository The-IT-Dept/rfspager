# Pre-publication hardening and Go cutover readiness

## Agreed outcome

Prepare this repository for the Laravel-to-Go deployment and later public release. Laravel is still the production system. Do not deploy, connect to production, rotate live credentials, push, publish, or rewrite existing Git history during this work. The owner will switch receivers and rotate existing credentials at deployment. The unauthenticated legacy ingress path must not exist in the new server. Preserve existing work first (commit 4e0c18b).

The audit and the user's direction to fix its findings establish the implementation scope. Work on the existing application, retaining its mobile/web/API architecture. The user selected AGPLv3: preserve attribution and share covered distributed/networked modifications; compliant commercial use is permitted. The current project license is AGPL-3.0-only.

## Tasks and acceptance checks

- [x] Preserve existing pager diagnostics and capcode notes in a separate commit.
- [x] Authentication: reproduce then reject cross-use of admin/mobile tokens, invalid/missing claims, inactive users/admins, forged social links, and invalid provider audiences. Require admin authority on every admin stream and appropriate roles on administrator management. Implement real password recovery or remove misleading success; invalidate sessions after password changes/reset/logout. Tests must cover rejected attacks and successful legitimate flows.
- [x] Database: supply a sanitized base schema for empty PostgreSQL/PostGIS installations based on legacy migrations; preserve upgrades of existing Laravel databases. Make migrations transactional and serialized, remove embedded receiver credentials, and exercise empty schema plus representative legacy upgrade without real data. Supply a migration-only command and cutover instructions that separate schema changes from notification workers.
- [x] Ingress/configuration: remove legacy UUID/Sanctum ingress and receiver legacy mode; retain approved receiver key authentication on supported ingress and WebSocket. Require explicit secure JWT configuration and intentional admin bootstrap, reject old example defaults and partial webhook credentials, authenticate webhooks, bound request sizes, and prevent query credentials from being logged. Add negative and positive regression tests.
- [x] Billing/push follow-ups: ensure purchases cannot be reused across accounts and installation identity cannot be reassigned by an unrelated user. Validate configured products and preserve legitimate restore/login flows. Add focused tests and upgrade constraints where needed.
- [x] Build/dependencies: include the verified public Apple certificate, align web lockfile/CI/Docker, update vulnerable Go/JS dependencies and toolchains, verify Go test/vet/build plus web/mobile checks, and add repeatable secret/dependency checks.
- [x] Publication: remove production config and sensitive operational notes from the current tree, provide synthetic examples, add contribution/security guidance and asset notices, replace FSL per the licensing decision, and document the still-required credential rotation/history/publication actions.
- [x] Integration: clean-checkout build, disposable PostgreSQL migration tests, full relevant suites, current-tree redacted secret scan, vulnerability audits with explicit residual limitations, and independent review of the final changes.

## Design decisions and interfaces

Authentication tokens need a fixed issuer, distinct audience/type, exact signing method, expiry, valid numeric subject and revocation version checked against the appropriate table. Password/social changes must not silently attach a new identity by email. The server must use the same admin middleware on normal and streaming routes.

Migration files 000 establish missing base tables only; existing migrations remain ordered and are upgraded safely. Reserve 022 for session/recovery schema and 023 for purchase ownership if needed. Operational secrets never belong in migrations. Avoid destructive cleanup of existing production rows; report incompatible data during preflight instead.

HTTP and WebSocket ingress remain usable by provisioned receivers with unique runtime-generated keys and approval. Legacy UUID URLs and Sanctum compatibility endpoints are removed before deployment. No production host, credential or database is required to run local tests.

## Verification focus

1. Valid sessions keep working while cross-audience, expired, malformed and revoked sessions fail.
2. Fresh schema and existing legacy schema both migrate; concurrent boot does not apply a migration twice; failure does not leave partial DDL.
3. Unapproved/disabled/deleted receivers fail every supported ingress; removed routes return not found.
4. Purchase and push ownership hold across two distinct users, retries, and restore/login flows.
5. Clean builds succeed without ignored local files or real provider secrets; dependency upgrades preserve core UI/API behavior.

## Progress and rulings

- Initial audit evidence and throwaway reproductions: /tmp/rfspager-security-review-2026-09-22.md and /tmp/rfspager-audit-snapshot.
- Ruling: retain original private history for now; user requested existing changes be committed and will rotate credentials at deployment. Public release remains gated on rotation plus a deliberate sanitized-history/fresh-import decision.
- Ruling: work on a separate local branch in the shared checkout so ignored legacy source remains available for schema comparison. No deployment or external state changes are authorized.

- License decision: user selected AGPLv3 after confirming it allows compliant commercial use; new code is AGPL-3.0-only, with preserved attribution and source links.
- Independent review found/fixed nonzero-version login, destructive duplicate migration, JSON-key SQL injection, social-provider error-log leakage, recovery/session revocation gaps, and client logout wiring. Tests were added for the affected boundaries.
- Ruling: use a reproducible CJS backport of the upstream fixed URI decoder beneath Expo Router 57, because the registry-patched package is ESM-only and breaks its CJS caller. A direct regression tests the actual transitive dependency; registry audit may continue to flag the old version string.
- Ruling: remove unconsumed Lottie assets without documented redistribution rights. Preserve verified Lato/Poppins OFL notices. Historical originals remain private until the publication-history decision.
- Migration integration suites share a destructive fixture database; run packages sequentially with -p 1.


## Final verification record

- Clean export (no ignored files): Go test, vet and build passed.
- Disposable PostGIS: fresh/legacy migrations, rollback, serialized migrations,
  duplicate preflight preservation, receiver approval/rotation, purchase ownership,
  historical/cross-platform billing, and concurrent push registration passed.
- Local Go server with synthetic accounts: 33 HTTP checks passed (including
  admin/mobile isolation, legacy404s, receiver approval, push ownership,
  logout/relogin, password change and account deletion).
- Web typecheck and production build passed. Mobile strict TypeScript and both
  decoder/account-switch race regressions passed. Current-tree Gitleaks scan:0.
- Production deployment, receiver credential rotation, store/provider delivery,
  and native store submission are intentionally not performed. See cutover.md.
- Prior private history still contains retired-to-be keys and operational notes;
  current-tree remediation is not a history rewrite or proof of live rotation.

- Final govulncheck rebuilt with Go1.27.1: no called/imported vulnerabilities;
  one advisory in an unused required-module package remains reported.
- Native receiver compiled using the documented librtlsdr pkg-config flags.
- Fresh iOS Metro export passed. After restarting the pre-update Metro process
  with --clear, the connected development client bundled successfully and
  reported AdMob ads loading successfully. Native store builds remain unrun.
