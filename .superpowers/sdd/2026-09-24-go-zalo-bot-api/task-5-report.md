# Task 5: Dispatcher report

## TDD evidence

- RED: `go test . -run TestDispatcher -v` initially failed as expected: `dispatchConfig`, `dispatcher`, and `newDispatcher` were undefined.
- GREEN: implemented dispatcher and tests; synchronized capacity-dependent tests on handler entry (and used `sync.Once` for the handler signal). A race run exposed an initial test-fixture double-close; corrected the fixture synchronization.
- `go test -race . -run TestDispatcher -v -count=1`: PASS, all 6 dispatcher tests.
- `go vet ./...`: PASS.
- `go test -race ./...`: PASS, 42 tests across 2 packages.
- `git diff --cached --check`: PASS.

## Review

Dispatcher provides bounded per-chat and global pending queues, FIFO ready-chat scheduling with a per-chat quantum, blocking/nonblocking admission, nonblocking update fan-out with one drop notification per episode, and stop/wait lifecycle. Capacity release broadcasts to waiting admissions. Tests cover ordering, admission, stop, fairness, and fan-out.

## Commit

`adb0e5c feat: per-chat dispatcher with bounded worker pool and quantum fairness`

## Review fix

- Set `chatQueue.active` at first admission/ready-queue insertion, preventing a second admission from enqueuing a key already awaiting a worker.
- Create chat queues only after capacity is available and the item is accepted. Added regression coverage proving rejected new-chat admissions do not leave map entries.
- Expanded `TestDispatcherProcessesInOrderPerChat` to contend four chats across 24 updates each with quantum 2, checking per-chat order and no simultaneous processing for one chat.
- Required command: `cd /home/dat/dev/go-zalo-bot-api/.worktrees/impl && go test -race . -run TestDispatcher -count=20 -v`
- Output: PASS, `140 passed in 1 packages` (race detector clean).
- `cd /home/dat/dev/go-zalo-bot-api/.worktrees/impl && go vet ./...`: PASS (no output).
