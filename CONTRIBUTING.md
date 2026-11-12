# Contributing

Contributions to RFSPager are welcome. Open an issue to discuss substantial new behavior, and submit focused pull requests with the relevant tests and a brief explanation of the user-visible change.

By submitting a contribution, you license it under the repository's AGPL-3.0-only license and confirm you have the right to contribute it. Preserve existing copyright/license notices and identify third-party source or assets with their original license. We do not require copyright assignment. See LICENSE.md and NOTICE.

Follow the setup and verification commands in README.md. Use the disposable local PostGIS database for integration tests; they deliberately reset its schema. Never connect tests or development notification workers to production.

For security-sensitive changes, include tests that reject the attack and preserve the legitimate flow. Migrations run automatically on server start, must be idempotent, and must work both on an empty schema and on an existing production-shaped database; never edit a migration that has shipped, add a new one. Stop and report incompatible historical data instead of deleting it silently.

Do not commit receiver credentials, signing keys, service-account JSON, `.env` files, production database exports, real pager logs, personal data, or device push tokens. Use synthetic fixtures. Client provider configuration belongs to the contributor's own deployment. Report vulnerabilities privately rather than posting exploit details in a public issue.

Pull requests run tests with no deployment credentials. Releases and store submissions are maintainer actions. Do not add privileged pull-request workflows or make release secrets available to forked code.
