# Go Zalo Bot API — Design Spec

- **Date:** 2026-09-24
- **Status:** Approved design, awaiting implementation plan
- **Module:** `github.com/lucanhost/go-zalo-bot-api`
- **Root package:** `zalobot`
- **Go:** 1.25
- **Sources of truth:** [`node-zalo-bot`](https://www.npmjs.com/package/node-zalo-bot) (npm SDK) and the [official Zalo Bot docs](https://docs.zaloplatforms.com/docs/BOT)

---

## 1. Goal & scope

Build an idiomatic Go SDK for the Zalo Bot Platform that covers the **full official API** and keeps the ergonomics that make the npm package pleasant (event handlers, regex/text routing, long-polling and webhook modes).

In scope:

- All 11 documented endpoints: `getMe`, `getUpdates`, `setWebhook`, `deleteWebhook`, `getWebhookInfo`, `testWebhook`, `sendMessage`, `sendPhoto`, `sendSticker`, `sendVoice`, `sendChatAction`.
- Typed update/message models and the five documented event names.
- Long-polling client with a resilient loop.
- An `http.Handler` for webhooks plus a `ProcessUpdate` primitive.
- Rich-text support (`parse_mode` and `text_styles`) with UTF-16-correct helpers.
- Typed error taxonomy with predicates.
- Hermetic tests against an `httptest` fake server, plus an opt-in integration suite.

## 2. Non-goals (YAGNI)

- No multipart/file upload in v1: the current documented methods take URLs/IDs, not binary uploads. (The platform lists `multipart/form-data` as an accepted encoding, so this is a future addition, not a platform limitation.)
- No automatic retry on API errors (polling backoff only).
- No offset persistence; `offset`/`limit` are not sent in v1 pending live verification.
- No logger abstraction; `OnError` plus `slog.Default()` as the default sink.
- No `reply_to_message` (present in the npm fork, absent from official docs).
- No npm `testEnvironment` `/test` URL suffix (no documented support).
- No group-beta extras beyond `chat_type`.
- No `@bot` suffix parsing in `OnCommand` (Telegram idiom; Zalo does not document it).

## 3. Package layout

```
go-zalo-bot-api/
├── go.mod
├── bot.go          // package zalobot: Bot, New, Option, Start/Shutdown/Stop/Done/Err
├── handlers.go     // OnMessage, OnText, OnCommand, OnEvent, OnError, Updates, ProcessUpdate
├── dispatcher.go   // unexported: admission, per-chat queues, worker pool, fan-out
├── send.go         // SendMessage/Photo/Sticker/Voice/ChatAction
├── webhook.go      // WebhookHandler, VerifyWebhookSecret, SetWebhook, DeleteWebhook, GetWebhookInfo, TestWebhook
├── update.go       // Update, Message, User, Chat, BotInfo, EventName, ChatType
├── richtext.go     // ParseMode, TextStyle, StyleCode, StyleRange, StyleRangeN
├── options.go      // per-call option structs + Bot Option funcs
├── errors.go       // APIError, TransportError, DecodeError, ValidationError, sentinels, predicates
└── internal/
    └── api/        // transport only: URL build, JSON encode, envelope decode, error mapping
        ├── client.go
        └── envelope.go
```

- Root package owns public types, orchestration, dispatch, and lifecycle.
- `internal/api` knows only HTTP and the response envelope. It returns `json.RawMessage`; root decodes typed results. It does not import root.
- The dispatcher lives in root (unexported `dispatcher.go`) because it routes on root's `Update`/`Message` types; putting it under `internal/` would create an import cycle.

## 4. Public API

```go
package zalobot

func New(token string, opts ...Option) (*Bot, error) // ErrNoToken if token == ""

// Lifecycle
func (b *Bot) Start(ctx context.Context) error     // long-poll loop; ctx cancel triggers shutdown
func (b *Bot) Shutdown(ctx context.Context) error  // idempotent; waits for drain or ctx deadline
func (b *Bot) Stop() error                         // = Shutdown with WithDrainTimeout
func (b *Bot) Done() <-chan struct{}               // closed when fully stopped
func (b *Bot) Err() error                          // terminal error (e.g. 401); nil after graceful stop
func (b *Bot) IsPolling() bool

// Routing
func (b *Bot) OnMessage(fn func(ctx context.Context, m *Message))
func (b *Bot) OnText(re *regexp.Regexp, fn func(ctx context.Context, m *Message, match []string))
func (b *Bot) OnCommand(name string, fn func(ctx context.Context, m *Message, args []string))
func (b *Bot) OnEvent(name EventName, fn func(ctx context.Context, u Update))
func (b *Bot) OnError(fn func(error))
func (b *Bot) Updates() <-chan Update
func (b *Bot) ProcessUpdate(raw []byte) error

// Sending
func (b *Bot) SendMessage(ctx context.Context, chatID, text string, o *SendMessageOptions) (*SentMessage, error)
func (b *Bot) SendPhoto(ctx context.Context, chatID, photo string, o *SendPhotoOptions) (*SentMessage, error)
func (b *Bot) SendSticker(ctx context.Context, chatID, sticker string) (*SentMessage, error)
func (b *Bot) SendVoice(ctx context.Context, chatID, voiceURL string) (*SentMessage, error)
func (b *Bot) SendChatAction(ctx context.Context, chatID string, action ChatAction) error

// Info + webhook
func (b *Bot) GetMe(ctx context.Context) (*BotInfo, error)
func (b *Bot) GetUpdates(ctx context.Context, o *GetUpdatesOptions) ([]Update, error)
func (b *Bot) WebhookHandler(secret string) (http.Handler, error)
func (b *Bot) SetWebhook(ctx context.Context, url, secret string) (*WebhookInfo, error)
func (b *Bot) DeleteWebhook(ctx context.Context) (*WebhookInfo, error)
func (b *Bot) GetWebhookInfo(ctx context.Context) (*WebhookInfo, error)
func (b *Bot) TestWebhook(ctx context.Context) (*WebhookTestResult, error)

func VerifyWebhookSecret(got, want string) bool // false when want == ""
```

### Bot options

| Option | Default | Purpose |
|---|---|---|
| `WithHTTPClient(*http.Client)` | `http.DefaultClient` | Inject transport. Rejected in `Start` if its `Timeout` would kill a long poll. |
| `WithBaseURL(string)` | `https://bot-api.zaloplatforms.com` | Override for tests/proxies. |
| `WithPolling(PollingOptions{Timeout time.Duration})` | 30s | Long-poll timeout. |
| `WithWorkers(int)` | 4 | Max chats processed concurrently. |
| `WithUpdatesBuffer(int)` | 64 | `Updates()` channel capacity. |
| `WithPerChatBuffer(int)` | 512 | Per-chat queue capacity. |
| `WithMaxBuffered(int)` | 10000 | Global queued-update ceiling. |
| `WithDrainTimeout(time.Duration)` | 10s | Bounds `Stop()`/`Shutdown` drain. |
| `WithAutoDeleteWebhook(bool)` | false | If true, `Start` deletes an existing webhook instead of erroring. |

## 5. Data model

```go
type EventName string
const (
    EventTextReceived        EventName = "message.text.received"
    EventImageReceived       EventName = "message.image.received"
    EventStickerReceived     EventName = "message.sticker.received"
    EventVoiceReceived       EventName = "message.voice.received"
    EventUnsupportedReceived EventName = "message.unsupported.received"
)

type ChatType string
const (
    ChatPrivate ChatType = "PRIVATE"
    ChatGroup   ChatType = "GROUP" // beta: bots receive only replies/@mentions
)

type User struct {
    ID          string `json:"id"`
    DisplayName string `json:"display_name"`
    Avatar      string `json:"avatar"`
    IsBot       bool   `json:"is_bot"`
}
// Custom UnmarshalJSON normalizes DisplayName: `display_name`, else `name`.

type BotInfo struct {
    ID            string `json:"id"`
    AccountName   string `json:"account_name"`
    AccountType   string `json:"account_type"`
    CanJoinGroups bool   `json:"can_join_groups"`
}
// Custom UnmarshalJSON normalizes AccountName: `account_name`, else `name`.

type Chat struct {
    ID   string   `json:"id"`
    Type ChatType `json:"chat_type"`
}

type Message struct {
    From        *User  `json:"from"`
    Chat        *Chat  `json:"chat"`
    Text        string `json:"text"`
    Photo       string `json:"photo"` // normalized: `photo`, else `photo_url`
    Caption     string `json:"caption"`
    Sticker     string `json:"sticker"`
    URL         string `json:"url"` // sticker URL
    VoiceURL    string `json:"voice_url"`
    MessageType string `json:"message_type"`
    MessageID   string `json:"message_id"`
    Date        int64  `json:"date"` // epoch milliseconds
}
// Custom UnmarshalJSON normalizes Photo: `photo`, else `photo_url`.
func (m *Message) Time() time.Time // Date ms -> time.Time

type Update struct {
    EventName EventName       `json:"event_name"`
    Message   *Message        `json:"message"`
    Raw       json.RawMessage `json:"-"` // fresh copy of the decoded result object
}
// Custom UnmarshalJSON sets EventName/Message and copies Raw, so `event_name`
// decoding is centralized and never depends on a stray struct tag.

type SentMessage struct { // result of a send* call
    MessageID string `json:"message_id"`
    Date      int64  `json:"date"` // epoch milliseconds
}
func (s *SentMessage) Time() time.Time

type ParseMode string
const (
    ParseModeNone     ParseMode = ""
    ParseModeMarkdown ParseMode = "markdown"
    ParseModeHTML     ParseMode = "html"
)

type ChatAction string
const (
    ChatActionTyping      ChatAction = "typing"
    ChatActionUploadPhoto ChatAction = "upload_photo" // docs: coming soon
)

type TextStyle struct {
    Start  int         `json:"start"` // UTF-16 code units
    Len    int         `json:"len"`   // UTF-16 code units
    Styles []StyleCode `json:"st"`    // JSON key is `st`
}

type WebhookInfo struct {
    URL                  string             `json:"url"`
    UpdatedAt            int64              `json:"updated_at"`
    HasCustomCertificate bool               `json:"has_custom_certificate"`
    Verification         *WebhookTestResult `json:"verification,omitempty"`
}
type WebhookTestResult struct {
    OK      bool   `json:"ok"`
    URL     string `json:"url"`
    Outcome string `json:"outcome"`
    Hint    string `json:"hint"`
}
```

`SendMessageOptions{ ParseMode ParseMode; TextStyles []TextStyle }`. `SendPhotoOptions{ Caption string }`. `SendSticker` and `SendVoice` take no options (the docs define none; voice explicitly has no caption). Send results decode to `*SentMessage` (`MessageID`, `Date`) — a distinct receipt type, never a partially populated inbound `*Message`. `SendChatAction` returns only `error` (its response has no `result`).

Unknown JSON fields are preserved (no `DisallowUnknownFields`); `Update.Raw` is a fresh copy, never a slice into a reused buffer.

## 6. Transport & errors

`internal/api.Client.Call(ctx, method string, params any) (json.RawMessage, error)`:

- URL: `baseURL + "/bot" + token + "/" + method`.
- `POST`, body JSON, `Content-Type: application/json`, `Accept: application/json`, UTF-8.
- Decodes the envelope `{ok, result, description, error_code | errorCode}`; accepts both error-code key spellings.
- `ok:false` → `*APIError{Code, Description, Method, HTTPStatus}`.
- Non-JSON/undecodable body → `*DecodeError{Method, HTTPStatus, Err}`.
- Network/TLS/timeout → `*TransportError{Method, RedactedURL, Err}`.

Error types:

```go
type APIError struct { Code int; Description, Method string; HTTPStatus int }
type TransportError struct { Method, RedactedURL string; Err error } // Unwrap() -> Err
type DecodeError struct { Method string; HTTPStatus int; Err error } // Unwrap() -> Err
type ValidationError struct { Field, Reason string }
type WebhookActiveError struct { URL string } // Error() reports the active URL

var ErrNoToken, ErrStopped, ErrAlreadyPolling, ErrQueueFull error

func IsUnauthorized(err error) bool         // 401 by API code or HTTP status
func IsRateLimited(err error) bool          // 429 by API code or HTTP status
func IsPollingTimeout(err error) bool       // 408 by API code or HTTP status
func IsWebhookQuotaExceeded(err error) bool // 426 (testWebhook daily quota)
```

- **Predicates consider HTTP status too.** They inspect `APIError.Code` *and* the HTTP status carried by `APIError`/`DecodeError`, so a 401/429 returned with a non-JSON body (e.g. an HTML error page from a proxy) is still classified correctly. 426 is modeled separately as the `testWebhook` daily-quota case, not as general rate limiting.

- **Token redaction:** `TransportError` wraps `urlErr.Err` (never the `*url.Error` itself) and stores a redacted URL (`…/bot<TOKEN>/sendMessage`). This keeps `errors.Is(err, context.DeadlineExceeded)` and `net.Error.Timeout()` working with no token leak.
- **HTTP status fallback:** when the body carries no code, `HTTPStatus` is used (e.g. a 502 HTML page from a proxy).
- **Timeouts:** no global client timeout. Each request uses its caller's context; polling derives `context.WithTimeout(ctx, pollTimeout+5s)`. `Start` returns a `ValidationError` if `httpClient.Timeout > 0 && httpClient.Timeout <= pollTimeout+5s`.
- **`OnError`:** optional; defaults to `slog.Default().Error`. May be called concurrently. Panic reports include the stack.
- npm mapping: `ZaloError`→`APIError`, `ParseError`→`DecodeError`, `FatalError`→`ErrNoToken`/`TransportError`.

## 7. Dispatcher & concurrency

The dispatcher is owned by the `Bot`, started lazily on first admission, and torn down by shutdown — independent of polling. A webhook-only bot never calls `Start` and still runs handlers.

**Admission is synchronous into the target per-chat queue.** There is no intermediate buffer that can drop an already-accepted update. When admission returns `nil`, the update will be delivered to handlers unless the bot shuts down. (The separate `Updates()` channel is best-effort; see below.)

`admit(u Update, blocking bool) error`:

1. Lock. If stopped → `ErrStopped`.
2. Key = `chat.id`, or `"event:" + eventName` when there is no chat.
3. If the per-chat queue is at `WithPerChatBuffer`, or `totalQueued` is at `WithMaxBuffered`:
   - `blocking` (poll loop): wait on a condition variable until space frees or the bot stops.
   - non-blocking (webhook / `ProcessUpdate`): return `ErrQueueFull`.
4. Append, increment `totalQueued`, mark the chat ready if not already, `Broadcast()`.
5. Unlock.

**Scheduler: a fixed, bounded worker pool.** Exactly `WithWorkers` (default 4) long-lived goroutines wait on a `sync.Cond` for ready chats, pop the **oldest** ready chat (FIFO fairness), mark it active, and drain it serially. When its queue empties the chat is marked inactive and the worker returns to the pool. No per-chat goroutine is ever spawned, so goroutine count is bounded by `WithWorkers`.

- **Per-chat serial queues** preserve order within a chat (same `chat.id` → same queue).
- **N workers** process N different chats concurrently.
- **No hash sharding**, so unrelated chats never collide.

**Admission contracts:**

| Path | Admission | Errors |
|---|---|---|
| `ProcessUpdate` (public) | non-blocking per-chat | `ErrQueueFull`, `ErrStopped` |
| `WebhookHandler` | calls `ProcessUpdate`; **returns 200 only after successful admission**, else 503 | — |
| Poll loop | blocking per-chat, waits on the condition, aborts on stop/ctx | `ErrStopped`, `ctx.Err()` |

**Backpressure and isolation (honest statement).** Different chats are processed in parallel and per-chat order is preserved. A chat whose queue is full applies backpressure to the **poll loop**, which admits one update at a time; already-admitted updates for other chats continue to be processed by the worker pool. Webhook requests targeting a full chat receive an explicit **503**, never a false 200. No admitted update is silently dropped before handler delivery.

**No queue/worker channels are closed to signal shutdown.** Queues are slices guarded by a mutex and a condition variable; an atomic `stopped` flag makes admission return `ErrStopped`. This eliminates send-on-closed-channel panics when a webhook request races shutdown. Only the two signalling channels — `Updates()` and `Done()` — are closed, once, at the very end of shutdown.

**Handler context.** Handlers receive a bot-lifetime `handlerCtx`, cancelled only when the drain completes or its deadline passes, so in-flight handlers can still send replies during shutdown.

**Shutdown semantics (`Shutdown(ctx)`, idempotent):**

1. Set stopped; `Broadcast()` to wake blocked admitters and idle workers.
2. Let workers finish queued work.
3. Wait for workers `select`-ed against `ctx`.
4. On deadline: cancel `handlerCtx`; return `ctx.Err()`.
5. Close `Updates()`; close `Done()`.

`handlerCtx` carries a private marker value. If `Shutdown` receives a context carrying that marker (i.e. it was called from inside a handler), it initiates shutdown and **returns immediately**; the caller waits on `Done()`. This prevents the self-deadlock where a handler waits for the drain that is waiting for it. `Stop()` is `Shutdown(context.Background())` bounded by `WithDrainTimeout`; from a handler, prefer `Shutdown(ctx)`.

`Done()` closes **only when the bot is fully stopped** — after a graceful `Stop`/`Shutdown` *and* after a fatal polling error, which runs the same shutdown sequence. `Err()` returns the terminal error (e.g. a fatal 401) and is **nil after a graceful stop**; a drain-timeout is reported by `Shutdown`'s return value, not `Err()`.

**Handler ordering, multiple handlers, panics, registration.**

- Per update, handlers run in this order: `OnEvent(name)` → `OnMessage` → `OnText` (every matching regexp) → `OnCommand` (every matching command). Within a category, registration order.
- All matching handlers run; there is no first-match-wins and no suppression.
- Each handler invocation is wrapped in `recover`; a panic is reported via `OnError` with the stack, and the remaining handlers for that update still run.
- Registration is mutex-guarded and safe at any time, but a handler registered after an update was admitted may or may not see that update. Register before `Start` for deterministic behavior.

**`Updates()` semantics (best-effort).** A separate fan-out channel, buffered (`WithUpdatesBuffer`, default 64). It is **lazy** — only filled after `Updates()` is first called; updates admitted before that are **not replayed**. It may drop updates when full (one `OnError` per full-buffer episode); drops affect the channel only, never handler delivery. Closed by shutdown even if never read.

**Shared message safety.** Handlers and channel consumers receive the same `*Message`; it is decoded once and never mutated, so it must be treated as read-only.

## 8. Polling

`Start(ctx)`:

1. `ErrStopped` if already stopped; `ErrAlreadyPolling` if running.
2. If `WithAutoDeleteWebhook(true)` → `DeleteWebhook`; otherwise `GetWebhookInfo` and, if `URL != ""`, return `WebhookActiveError{URL}` **without touching it**.
3. Validate the HTTP client timeout (see §6).
4. Launch the loop: `GetUpdates(ctx, {Timeout})` → admit each update (blocking, shutdown-aware). A watcher goroutine runs the same idempotent shutdown when `ctx` is cancelled.

`Start` returns `nil` once the loop is launched; it does not block. Setup failures (steps 1–3) are returned synchronously. Runtime failures are reported through `OnError`, `Err()`, and `Done()`.

Loop error handling:

- **408** (`IsPollingTimeout`) is a **normal empty poll**: no `OnError`, no backoff, continue.
- **401** (`IsUnauthorized`) is **fatal only for the poll loop**: it triggers the full shutdown sequence (stop admitting, drain, close `Updates()`, close `Done()`), with `Err()` set to the 401, so `Done()` always means fully stopped. A 401 from a `sendMessage` call just returns an `*APIError` and does not stop the bot.
- Other errors: `OnError` + exponential backoff with **jitter**, **reset on success**, and a **longer base on 429**.
- **Tight-loop guard:** if an empty `ok` poll or a 408 returns in under 1s, sleep briefly before the next request.
- **Periodic webhook re-check:** after N consecutive empty polls (default 30), call `GetWebhookInfo`; if a webhook has appeared, stop with `WebhookActiveError`. Guards against another process enabling a webhook after `Start` (verification checklist item 6).

`GetUpdatesOptions{ Timeout time.Duration }` is sent as a **string** (docs type it as a string; sample sends a number — verification item 2). `offset`/`limit` are not sent in v1.

`GetUpdates` decodes tolerantly by first non-space byte of `result`: `[` → `[]Update`; `{` → single `Update` (empty object → none); `null`/absent → none. It therefore returns 0-or-1 elements today and still works if the server sends batches.

**Documented limitation:** cancelling an in-flight poll on shutdown can drop an update the server already handed out (there is no ack or offset). This is another reason to prefer webhooks in production.

## 9. Webhook

`WebhookHandler(secret string) (http.Handler, error)` validates the secret (8–256 chars) at construction.

Request handling order:

1. Non-POST → **405** with an `Allow: POST` header.
2. `X-Bot-Api-Secret-Token` compared with `VerifyWebhookSecret` (`crypto/subtle`, constant time) → **403** on mismatch. `VerifyWebhookSecret` returns false when `want == ""`.
3. `http.MaxBytesReader` (1 MiB) → **413** if exceeded.
4. Parse envelope; also accept a bare update object when it has a top-level `event_name` → **400** on malformed input.
5. `ProcessUpdate` (non-blocking admission) → **503** on `ErrQueueFull`/`ErrStopped`, else **200** `{"ok":true}` immediately.

Handlers run asynchronously after the 200 (the docs call out slow endpoints as a cause of `webhook.err.unreachable`; redirects are never followed). `WebhookHandler` owns no TLS/redirect logic; it is mounted on the caller's server.

`ProcessUpdate(raw []byte) error` takes the **full envelope body**, unwraps `result`, admits it, and returns parse/queue errors. It never dispatches on the caller's goroutine, so ordering guarantees hold and handlers cannot race polled updates. `Updates()` sees webhook updates.

Client methods: `SetWebhook(ctx, url, secret)` validates https and an 8–256-char secret, returns `*WebhookInfo` with `Verification`; a failed verification is **not** an error (the URL is saved regardless). `DeleteWebhook`, `GetWebhookInfo`, `TestWebhook` return their typed results.

## 10. Validation & rich text

- `SendMessage`: `parse_mode` together with `text_styles` → `ValidationError` (the server silently prioritizes `parse_mode`).
- Length limits are **lenient**: rune-count pre-checks (1–2000) only, **skipped when `parse_mode` is set** (the server strips markup and the applicable length is unclear). Captions are validated only when non-empty and marshaled with `omitempty`.
- `SendVoice` validates the URL path ends in `.aac`; documents 1-to-1-only and the group "success but undelivered" quirk.
- `type StyleCode string` with the full closed set:

  | Codes | Meaning |
  |---|---|
  | `b i u s` | bold, italic, underline, strikethrough |
  | `f_13 f_15 f_18 f_20` | font sizes |
  | `c_050a19 c_15a85f c_f7b503 c_f27806 c_db342e` | colors |
  | `lst_1 lst_2` | unordered / ordered list |
  | `ind_1 … ind_5` | indentation levels |

- `StyleRange(text, substr string, codes ...StyleCode) (TextStyle, error)` styles the **first occurrence** and computes `start`/`len` in **UTF-16 code units** via `unicode/utf16` (Go strings are UTF-8; byte/rune offsets are wrong for Vietnamese diacritics and emoji). `StyleRangeN(text, substr string, occurrence int, codes ...StyleCode)` selects a later occurrence.
- `OnCommand(name, fn)` matches text of the form `/name args`, splits args on whitespace, and requires no `@bot` suffix.

## 11. Testing

- `internal/api` (`httptest`): URL/method/header/body assertions; envelope decode including the `errorCode` alias; tolerant `getUpdates` decode (array / object / null / absent); error mapping and token redaction.
- Root, against a fake server: send methods; validation errors; `OnText` matching; handler ordering; nil-message event; polling fetch→cancel→`Stop`; webhook (405/403/400/413/200, async dispatch).
- **Normalized decoding**: `display_name` vs `name`, `photo` vs `photo_url`, `account_name` vs `name`; `SentMessage` decoding; `Update.EventName`/`Raw` decoding.
- **Golden fixtures** from the docs samples: the webhook payload, `setWebhook` `verification`, the `testWebhook` 403 hint, and `sendChatAction`'s `{"ok":true}` with no `result`.
- **408** (no `OnError`, no backoff) and **401** (full shutdown, `Done()` closes, `Err()` set) tests.
- **Predicates with invalid JSON bodies**: a 401/429 returned as an HTML error page is still classified by `IsUnauthorized`/`IsRateLimited`; 426 maps to `IsWebhookQuotaExceeded`.
- **Queue-full** tests: `ProcessUpdate` returns `ErrQueueFull`/`ErrStopped`; polling blocks and resumes; the webhook returns **503 and never a false 200**.
- **Scheduler fairness**: ready chats are served FIFO, and a chat made ready earlier is picked before one made ready later.
- **Goroutine bound**: under sustained load, worker goroutines stay at `WithWorkers` (checked with `goleak`/`runtime.NumGoroutine`).
- **Stress test** of admission racing `Shutdown` under `-race`.
- `Shutdown` called from a handler returns promptly; **concurrent handler registration** while updates dispatch passes under `-race`.
- A **slow-chat** test demonstrating that a stalled chat does not stop other chats' already-admitted updates from processing (the poll loop applies backpressure).
- **`goleak`** (test-only dependency) around `Start`/`Stop`/ctx-cancel/`Shutdown`.
- **Fuzz/property** test for `StyleRange`: slicing the UTF-16 encoding of the text at `start:start+len` must decode back to `substr`. Include Vietnamese in **NFC and NFD** (different UTF-16 lengths) and emoji (surrogate pairs).
- A `//go:build integration` suite gated on `ZALO_BOT_TOKEN` that runs the live-verification checklist.
- `go test -race ./...`; Example functions double as documentation.

## 12. Live verification checklist

Run via the integration suite once a token is available:

1. Shape of the `getUpdates` result (object / array / null) and whether an idle poll holds for the full timeout or returns immediately.
2. `timeout` as a string vs a number.
3. `getWebhookInfo` with no webhook — treat empty, missing, and null all as "none".
4. Photo field name (`photo` vs `photo_url`) and `getMe` field names (`account_name` vs `name`).
5. Whether `message.unsupported.received` includes a `message` key at all.
6. What `getUpdates` does when a webhook is already set (silent empty / 408 / error) — informs the periodic re-check in §8.
7. The text a group `@`mention or reply produces (for `OnCommand`).

## 13. Risks & open questions

- The `getUpdates` response shape and `timeout` type are unverified; §8 decodes tolerantly and sends a string.
- Group-chat behavior is beta and lightly documented.
- Without an ack/offset, in-flight polls can drop updates on shutdown.
- A full per-chat queue applies backpressure to the poll loop by design; webhook deployments avoid it and get an explicit 503 instead.

## 14. References

- npm: <https://www.npmjs.com/package/node-zalo-bot>
- Docs: <https://docs.zaloplatforms.com/docs/BOT>
- API pages: `call_api`, `sendMessage`, `sendPhoto`, `sendSticker`, `sendVoice`, `sendChatAction`, `getMe`, `getUpdates`, `setWebhook`, `deleteWebhook`, `getWebhookInfo`, `testWebhook`, `webhook`, `error_code`.
