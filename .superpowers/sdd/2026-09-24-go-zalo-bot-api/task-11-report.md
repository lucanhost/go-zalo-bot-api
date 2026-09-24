# Task 11 report: Integration suite, examples, README

## Changes
- Added `integration_test.go` behind the `integration` build tag, with live `GetMe` and `GetUpdates` tests gated by `ZALO_BOT_TOKEN` and bounded contexts.
- Added runnable external-package examples for `Bot.OnText` and `StyleRange`; the UTF-16 style range output is `start=4 len=4` for the requested Vietnamese text.
- Added `README.md` covering installation, polling, webhook, and rich text. The polling imports include `time`.

## Verification
- `go test -race ./...` — passed (tool reported 76 tests across 2 packages).
- `go test -tags integration ./... -run TestLive -v` — executed; tool reported “No tests found” rather than showing the expected skip output. The integration test source skips with `ZALO_BOT_TOKEN not set` when selected and run by Go's test runner. This output could not confirm the skip in this environment.
- `gofmt -l .` — no output.
- `go vet ./...` — clean (no diagnostics).
- `git show --check HEAD` — clean.

## Self-review
Diff contains only the three requested files, no placeholders or unrelated changes. README polling imports are complete. The integration suite remains opt-in and does not run against live credentials unless explicitly configured.

## Commit
`88fc17c docs: examples, README, and opt-in live integration suite`

## Final whole-branch review fixes
- Finding 1: webhook re-check attempts now reset the empty-poll counter whether `getWebhookInfo` succeeds or fails, preserving the 30-empty-poll throttle and existing fatal stop for an active webhook. Added `TestPollingWebhookRecheckFailuresAreThrottled`, which confirms repeated errors remain bounded during rapid empty polls.
- Finding 2: documented immediate nil return for repeated/in-progress `Shutdown` and `Stop`; callers needing drain completion wait on `Done()`.
- Finding 3: empty-poll remainder sleeps now use a timer selectable against loop context cancellation and `b.done`; both poll paths use the cancellation-aware helper.
- Finding 4: documented webhook secret length validation as byte-based on `WebhookHandler` and `SetWebhook`.
- Covering test: `TestPollingWebhookRecheckFailuresAreThrottled` (in addition to existing polling lifecycle tests).
- Exact verification command: `gofmt -l . && go vet ./... && timeout 240 go test -race ./... -timeout 180s -count=1`
- Output: `gofmt -l .` produced no output; `go vet ./...` produced no diagnostics; `go test -race ./...` reported `77 passed in 2 packages`.
