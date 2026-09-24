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

- No multipart/file upload; the API takes URLs/IDs, not binary uploads.
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
├── dispatcher.go   // unexported: intake, router, per-chat queues, workers, fan-out
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
func (b *Bot) SendMessage(ctx context.Context, chatID, text string, o *SendMessageOptions) (*Message, error)
func (b *Bot) SendPhoto(ctx context.Context, chatID, photo string, o *SendPhotoOptions) (*Message, error)
func (b *Bot) SendSticker(ctx context.Context, chatID, sticker string) (*Message, error)
func (b *Bot) SendVoice(ctx context.Context, chatID, voiceURL string) (*Message, error)
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
| `WithIntakeBuffer(int)` | 1024 | Global intake capacity. |
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
    Name        string `json:"name"`   // tolerant: some clients report `name`
    Avatar      string `json:"avatar"`
    IsBot       bool   `json:"is_bot"`
}
// Custom UnmarshalJSON normalizes DisplayName: prefer `display_name`, else `name`.

type BotInfo struct { // lenient: account_name or name
    ID            string `json:"id"`
    AccountName   string `json:"account_name"`
    Name          string `json:"name"`
    AccountType   string `json:"account_type"`
    CanJoinGroups bool   `json:"can_join_groups"`
}

type Chat struct {
    ID   string   `json:"id"`
    Type ChatType `json:"chat_type"`
}

type Message struct {
    From        *User  `json:"from"`
    Chat        *Chat  `json:"chat"`
    Text        string `json:"text"`
    Photo       string `json:"photo"`
    PhotoURL    string `json:"photo_url"` // tolerant alternative
    Caption     string `json:"caption"`
    Sticker     string `json:"sticker"`
    URL         string `json:"url"` // sticker URL
    VoiceURL    string `json:"voice_url"`
    MessageType string `json:"message_type"`
    MessageID   string `json:"message_id"`
    Date        int64  `json:"date"` // epoch milliseconds
}
func (m *Message) Time() time.Time     // Date ms -> time.Time
func (m *Message) ImageURL() string    // Photo, else PhotoURL

type Update struct {
    EventName EventName
    Message   *Message
    Raw       json.RawMessage // copy of the decoded result object
}

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

`SendMessageOptions{ ParseMode ParseMode; TextStyles []TextStyle }`. `SendPhotoOptions{ Caption string }`. `SendSticker` and `SendVoice` take no options (the docs define none; voice explicitly has no caption). Send results decode to `*Message` (`MessageID`, `Date`). `SendChatAction` returns only `error` (its response has no `result`).

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

func IsUnauthorized(err error) bool   // APIError 401
func IsRateLimited(err error) bool    // 429 or 426
func IsPollingTimeout(err error) bool // 408
```

- **Token redaction:** `TransportError` wraps `urlErr.Err` (never the `*url.Error` itself) and stores a redacted URL (`…/bot<TOKEN>/sendMessage`). This keeps `errors.Is(err, context.DeadlineExceeded)` and `net.Error.Timeout()` working with no token leak.
- **HTTP status fallback:** when the body carries no code, `HTTPStatus` is used (e.g. a 502 HTML page from a proxy).
- **Timeouts:** no global client timeout. Each request uses its caller's context; polling derives `context.WithTimeout(ctx, pollTimeout+5s)`. `Start` returns a `ValidationError` if `httpClient.Timeout > 0 && httpClient.Timeout <= pollTimeout+5s`.
- **`OnError`:** optional; defaults to `slog.Default().Error`. May be called concurrently. Panic reports include the stack.
- npm mapping: `ZaloError`→`APIError`, `ParseError`→`DecodeError`, `FatalError`→`ErrNoToken`/`TransportError`.

## 7. Dispatcher & concurrency

The dispatcher is owned by the `Bot`, started lazily on first enqueue, and torn down by shutdown — independent of polling. A webhook-only bot never calls `Start` and still runs handlers.

**Pipeline:** `enqueue → intake (global, bounded) → router goroutine → per-chat queue (bounded) → drainer goroutine (one per chat, serial) → handlers → Updates() fan-out`.

- **Per-chat serial queues** give real isolation: same `chat.id` always maps to the same queue, preserving order; different chats run concurrently. There are no hash-shard collisions.
- **Drainers** are limited by a semaphore of `WithWorkers` (default 4). A chat with a non-empty queue and no active drainer gets one; the drainer runs until the queue is empty, then reaps the entry and releases the semaphore.
- **Isolation guarantee:** a slow chat can fill only its own `WithPerChatBuffer`. The router keeps consuming intake, dropping that chat's overflow with one `OnError` per overflow episode, so other chats and the poll loop never stall. A global `WithMaxBuffered` ceiling bounds total memory across all chats.

**Enqueue contracts:**

| Path | Behavior | Errors |
|---|---|---|
| `ProcessUpdate` (public) | non-blocking | `ErrQueueFull`, `ErrStopped` |
| `WebhookHandler` | calls `ProcessUpdate`; both errors → HTTP 503 | — |
| Poll loop | blocking, `select` on intake send / shutdown / ctx | `ErrStopped`, `ctx.Err()` |

**No queue/worker channels are closed to signal shutdown.** Queues are slices guarded by a mutex; an atomic `stopped` flag makes enqueue return `ErrStopped`. This eliminates send-on-closed-channel panics when a webhook request races shutdown. Only the two signalling channels — `Updates()` and `Done()` — are closed, once, at the very end of shutdown.

**Handler context.** Handlers receive a bot-lifetime `handlerCtx`, cancelled only when the drain completes or its deadline passes, so in-flight handlers can still send replies during shutdown.

**Shutdown semantics (`Shutdown(ctx)`, idempotent):**

1. Set stopped; stop accepting new updates.
2. Let the router and drainers finish queued work.
3. Wait for workers `select`-ed against `ctx`.
4. On deadline: cancel `handlerCtx`; return `ctx.Err()`.
5. Close `Updates()`; close `Done()`.

`handlerCtx` carries a private marker value. If `Shutdown` receives a context carrying that marker (i.e. it was called from inside a handler), it initiates shutdown and **returns immediately**; the caller waits on `Done()`. This prevents the self-deadlock where a handler waits for the drain that is waiting for it. `Stop()` is `Shutdown(context.Background())` bounded by `WithDrainTimeout`; from a handler, prefer `Shutdown(ctx)`.

`Err()` is the terminal error (e.g. a fatal 401) and is **nil after a graceful stop**; a drain-timeout is reported by `Shutdown`'s return value, not `Err()`.

**`Updates()` semantics:** a separate fan-out channel, buffered (`WithUpdatesBuffer`, default 64). It is **lazy** — only filled after `Updates()` is first called; earlier updates are not replayed. Both handlers and channel consumers see every update. When full, an update is dropped **for channel consumers only** (handlers still run) with one `OnError` per full-buffer episode. Closed by shutdown even if never read.

**Shared message safety:** handlers and channel consumers receive the same `*Message`; it is decoded once and never mutated, so it must be treated as read-only.

## 8. Polling

`Start(ctx)`:

1. `ErrStopped` if already stopped; `ErrAlreadyPolling` if running.
2. If `WithAutoDeleteWebhook(true)` → `DeleteWebhook`; otherwise `GetWebhookInfo` and, if `URL != ""`, return `WebhookActiveError{URL}` **without touching it**.
3. Validate the HTTP client timeout (see §6).
4. Launch the loop: `GetUpdates(ctx, {Timeout})` → enqueue each update (blocking, shutdown-aware). A watcher goroutine runs the same idempotent shutdown when `ctx` is cancelled.

`Start` returns `nil` once the loop is launched; it does not block. Setup failures (steps 1–3) are returned synchronously. Runtime failures are reported through `OnError`, `Err()`, and `Done()`.

Loop error handling:

- **408** (`IsPollingTimeout`) is a **normal empty poll**: no `OnError`, no backoff, continue.
- **401** (`IsUnauthorized`) is **fatal only for the poll loop**: stop, record `Err()`, fire `OnError`, close `Done()`. A 401 from a `sendMessage` call just returns an `*APIError` and does not stop the bot.
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
5. `ProcessUpdate` (non-blocking enqueue) → **503** on `ErrQueueFull`/`ErrStopped`, else **200** `{"ok":true}` immediately.

Handlers run asynchronously after the 200 (the docs call out slow endpoints as a cause of `webhook.err.unreachable`; redirects are never followed). `WebhookHandler` owns no TLS/redirect logic; it is mounted on the caller's server.

`ProcessUpdate(raw []byte) error` takes the **full envelope body**, unwraps `result`, enqueues, and returns parse/queue errors. It never dispatches on the caller's goroutine, so ordering guarantees hold and handlers cannot race polled updates. `Updates()` sees webhook updates.

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
- **Golden fixtures** from the docs samples: the webhook payload, `setWebhook` `verification`, the `testWebhook` 403 hint, and `sendChatAction`'s `{"ok":true}` with no `result`.
- **408** (no `OnError`, no backoff) and **401** (loop exits, `Err()` set) tests.
- **Queue-full** tests for polling and webhook; `ProcessUpdate` returning `ErrQueueFull`/`ErrStopped`.
- **Stress test** of enqueue racing `Shutdown` under `-race`.
- `Shutdown` called from a handler returns promptly.
- A **slow-chat isolation** test demonstrating that a stalled chat does not delay others.
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

## 14. References

- npm: <https://www.npmjs.com/package/node-zalo-bot>
- Docs: <https://docs.zaloplatforms.com/docs/BOT>
- API pages: `call_api`, `sendMessage`, `sendPhoto`, `sendSticker`, `sendVoice`, `sendChatAction`, `getMe`, `getUpdates`, `setWebhook`, `deleteWebhook`, `getWebhookInfo`, `testWebhook`, `webhook`, `error_code`.
