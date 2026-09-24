# Task 3: Update and message model — report

Implemented `update.go` and the supplied `update_test.go` coverage for update/message models, JSON normalizers, timestamp helpers, tolerant update decoding, and envelope decoding.

## TDD evidence

- RED: `go test . -run 'TestUser|TestMessage|TestUpdate|TestUnsupported|TestDecodeUpdates|TestDecodeUpdateEnvelope' -v` failed as expected with undefined `User`, `Message`, `Update`, event constants, and decoder functions.
- GREEN: the same command passed after implementation (11 test executions/subtests passed).

## Verification

- `gofmt -w update.go update_test.go` — completed.
- `go vet .` — passed.
- `go test -race ./...` — passed (27 tests across 2 packages).
- `git diff --check` — passed.

## Self-review

The implementation follows the brief's requested public names, JSON field names, aliases/fallback normalization, raw input copying, tolerant handling of absent/null/empty results, and envelope/bare update handling. No unrelated changes were made.

## Review finding fix

Added `TestBotInfoNormalizesAccountName` to `update_test.go`. It verifies `BotInfo.AccountName` falls back to `name` when `account_name` is absent and prefers `account_name` when both fields are present. No changes were made to `update.go`.

- Covering test: `TestBotInfoNormalizesAccountName`
- Exact command: `cd /home/dat/dev/go-zalo-bot-api/.worktrees/impl && go test -race . -run TestBotInfo -v`
- Output: `Go test: 1 passed in 1 packages`

Commit: `828299b feat: typed update/message model with tolerant decoding`
