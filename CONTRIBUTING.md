# Contributing to What Iff

Thanks for contributing. Small, focused pull requests with tests are easiest to review.

## Local development

1. Set an LLM provider key in `.env` (copy `.env.example` if you want overrides).
2. Run `docker compose up --build`.
3. Run Go checks from the repository root:

   ```sh
   go fmt ./...
   go mod tidy
   go test ./...
   ```

Run the frontend's lint, test, and build commands when changing `web/`.

## Architecture and documentation

Read [`docs/ARCHITECTURE_SUMMARY.md`](docs/ARCHITECTURE_SUMMARY.md) before changing system boundaries. Update it and the affected `internal/<package>/_PACKAGE_SUMMARY.md` in the same pull request whenever behavior, dependencies, or tests change.

## Bugs and feature requests

Open an issue with the [bug report](https://github.com/theimaginaryfoundation/what-iff/issues/new?template=bug_report.yml) or [feature request](https://github.com/theimaginaryfoundation/what-iff/issues/new?template=feature_request.yml) form. Search existing issues first. Security problems go through the [private advisory flow](SECURITY.md), never a public issue. Agents filing issues follow `.agents/skills/gh-issue/SKILL.md`.

## Pull requests

- Explain the problem and how you tested the change.
- Include tests for new behavior.
- Do not commit credentials, personal exports, generated local state, or production configuration.
- Keep public defaults self-hostable: local JWT auth and unlimited usage must work without hosted services.

PRs use `.github/PULL_REQUEST_TEMPLATE.md`. Agents opening PRs follow `.agents/skills/gh-pr/SKILL.md`.
