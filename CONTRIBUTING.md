# Contributing

Thanks for your interest in improving `go-zalo-bot-api`. This document covers
how to set up the project, the work/test flow, and what a good pull request
looks like.

## Development setup

Requirements:

- Go 1.25 or newer (`go version`)
- [`golangci-lint`](https://golangci-lint.run/) v2 (only for `make lint` / `make ci`)
- Optional: a Zalo bot token, for the live integration suite

```bash
git clone https://github.com/lucanhost/go-zalo-bot-api
cd go-zalo-bot-api
go mod download
make test        # hermetic suite, no network or token required
```

## Work and test flow

The library is tested hermetically against `net/http/httptest` fakes — no bot
token, no network. Keep it that way for unit tests.

```bash
make help        # list every target
make fmt         # go fmt ./...
make vet         # go vet ./...
make test        # go test ./...
make test-race   # go test -race ./...   (also runs the goleak check)
make cover       # coverage profile + total
make lint        # golangci-lint run
make ci          # fmt-check + vet + test-race + lint (what CI runs)
```

### Live integration suite

The live tests are opt-in and skipped unless `ZALO_BOT_TOKEN` is set:

```bash
ZALO_BOT_TOKEN=... make integration
# or
ZALO_BOT_TOKEN=... go test -tags integration -run TestLive -v ./...
```

They make real API calls and therefore run **without** the package-wide
`goleak` check (see `testmain_integration_test.go`). Never commit a token.

## Making a change

1. Open an issue (or comment on an existing one) describing the problem or
   proposal before large changes, so design can be agreed first.
2. Branch from `main`: `git switch -c fix/short-description`.
3. Write the change and its tests. For behavior changes, add a test that
   fails before the fix and passes after.
4. Run `make ci` locally. It must be green.
5. Commit using [Conventional Commits](https://www.conventionalcommits.org/)
   (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`), then open a PR
   against `main` and fill in the template.

### Coding guidelines

- Prefer the standard library. New third-party dependencies are a hard sell
  for a library; test-only dependencies are considered case by case.
- Keep the public API small and idiomatic: `context.Context` first, `error`
  last, options as nil-able structs or functional options.
- Never log or expose the bot token. `TransportError` must keep the token
  redacted.
- Every exported identifier needs a doc comment.
- Add or update tests; keep test output pristine (no stray logs or warnings).

## Reporting bugs and security issues

- Bugs and feature requests: use the GitHub issue templates.
- Security vulnerabilities: do **not** open a public issue — follow
  [SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
