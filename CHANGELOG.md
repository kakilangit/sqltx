# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-09-02

### Added

- Managed transactions through `Run`, with automatic commit and rollback.
- Manual transactions through `Begin` with safe, idempotent `Close`, `Commit`, and `Rollback`.
- Transaction options, duration budgets, and setup hooks.
- PostgreSQL-local `transaction_timeout`, `statement_timeout`, and `lock_timeout` options.
- Query and statement delegation over `database/sql.Tx`.
- Lifecycle, error handling, race, timeout, and allocation tests.
- Lifecycle benchmarks comparing the library with raw `database/sql`.
- Linting, test, release automation, and unified Make targets.

[0.1.0]: https://github.com/kakilangit/sqltx/releases/tag/v0.1.0
