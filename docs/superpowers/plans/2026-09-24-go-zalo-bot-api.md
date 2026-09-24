# Go Zalo Bot API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an idiomatic Go SDK (`zalobot`) for the Zalo Bot Platform covering all 11 documented endpoints, typed update models, long-polling, a webhook `http.Handler`, and UTF-16-correct rich text.

**Architecture:** A thin root package `zalobot` composes an internal HTTP transport (`internal/api`), a handler registry, and a dispatcher. Admission is synchronous into per-chat queues; a fixed pool of `WithWorkers` goroutines drains ready chats in bounded quanta with FIFO requeueing, so per-chat order is preserved and no chat monopolizes a worker. Lifecycle is explicit: `Start` launches polling, `Shutdown` drains with a deadline and closes `Done()` only when workers have truly exited.

**Tech Stack:** Go 1.25, standard library only for the library (`net/http`, `encoding/json`, `context`, `unicode/utf16`, `sync`, `log/slog`, `math/rand/v2`). `go.uber.org/goleak` is a **test-only** dependency.

**Spec:** `docs/superpowers/specs/2026-09-24-go-zalo-bot-api-design.md`

## Global Constraints

- Module path: `github.com/lucanhost/go-zalo-bot-api`; root package name `zalobot`; Go 1.25 (`go.mod` already present).
- Base URL default: `https://bot-api.zaloplatforms.com`.
- Library dependencies: **standard library only**. `go.uber.org/goleak` may appear only in `_test.go` files.
- All public methods take `context.Context` first (except constructors/registration) and return `error` last.
- Never log or expose the bot token: `TransportError` wraps `urlErr.Err` and stores a redacted URL only.
- `gofmt` and `go vet ./...` clean; `go test -race ./...` green at the end of every task.
- Sentinel errors: `ErrNoToken`, `ErrStopped`, `ErrAlreadyPolling`, `ErrQueueFull`, `ErrSubstringNotFound`, `ErrUpdatesDropped`.

## Review Focus

The spec implies these inputs/conditions that no single happy-path test exercises. Each gets its own test in the named task.

1. **Idle/empty long-poll shape** — `getUpdates` may return a bare object, `null`, or an empty result on timeout; `GetUpdates` must return no updates and no error. (Task 3 + Task 10)
2. **Non-JSON error bodies** — a proxy returns 401/429 as HTML; predicates must still classify by HTTP status. (Task 1 + Task 2)
3. **UTF-16 offset correctness** — Vietnamese in NFD and emoji (surrogate pairs) must map to correct `start`/`len`. (Task 4)
4. **`message.unsupported.received` with no `message`** — must not panic and must not invoke `OnMessage`/`OnText`. (Task 3 + Task 6)
5. **Handler panic and handler-ignores-cancellation** — a panic must be reported without killing the worker; `Done()` must not close while a handler still runs. (Task 6 + Task 9)

---

### Task 1: Internal HTTP transport

**Files:**
- Create: `internal/api/errors.go`
- Create: `internal/api/envelope.go`
- Create: `internal/api/client.go`
- Test: `internal/api/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `api.NewClient(token, baseURL string, hc *http.Client) *api.Client`; `(*api.Client).Call(ctx context.Context, method string, params any) (json.RawMessage, error)`; `(*api.Client).URL(method string) string`; `(*api.Client).RedactedURL(method string) string`; types `api.APIError{Code int; Description, Method string; HTTPStatus int}`, `api.TransportError{Method, RedactedURL string; Err error}`, `api.DecodeError{Method string; HTTPStatus int; Err error}`.

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallSuccessDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["chat_id"] != "abc" {
			t.Errorf("chat_id = %v", got["chat_id"])
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":"m1","date":123}}`))
	}))
	defer srv.Close()

	c := NewClient("TOKEN", srv.URL, srv.Client())
	raw, err := c.Call(context.Background(), "sendMessage", map[string]any{"chat_id": "abc", "text": "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(raw) != `{"message_id":"m1","date":123}` {
		t.Fatalf("raw = %s", raw)
	}
}

func TestCallAPIErrorMapsCodeAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient("TOKEN", srv.URL, srv.Client())
	_, err := c.Call(context.Background(), "getMe", map[string]any{})
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v (%T), want *APIError", err, err)
	}
	if ae.Code != 401 || ae.HTTPStatus != 401 || ae.Description != "Unauthorized" {
		t.Fatalf("APIError = %+v", ae)
	}
}

func TestCallAcceptsCamelCaseErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"errorCode":426,"description":"quota"}`))
	}))
	defer srv.Close()
	c := NewClient("TOKEN", srv.URL, srv.Client())
	_, err := c.Call(context.Background(), "testWebhook", map[string]any{})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != 426 {
		t.Fatalf("err = %v", err)
	}
}

func TestCallHTMLBodyIsDecodeErrorWithStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`<html>unauthorized</html>`))
	}))
	defer srv.Close()
	c := NewClient("TOKEN", srv.URL, srv.Client())
	_, err := c.Call(context.Background(), "getMe", map[string]any{})
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want *DecodeError", err, err)
	}
	if de.HTTPStatus != 401 {
		t.Fatalf("HTTPStatus = %d", de.HTTPStatus)
	}
}

func TestTransportErrorRedactsTokenAndKeepsCause(t *testing.T) {
	c := NewClient("SECRET", "http://127.0.0.1:1", &http.Client{})
	_, err := c.Call(context.Background(), "getMe", map[string]any{})
	var te *TransportError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v (%T), want *TransportError", err, err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("token leaked: %v", err)
	}
	if !strings.Contains(te.RedactedURL, "/bot<TOKEN>/getMe") {
		t.Fatalf("redacted url = %q", te.RedactedURL)
	}
	if te.Unwrap() == nil {
		t.Fatal("Unwrap() = nil, want underlying cause")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestCall -v`
Expected: FAIL with "undefined: NewClient" / package does not compile.

- [ ] **Step 3: Write minimal implementation**

`internal/api/errors.go`:

```go
package api

import "fmt"

type APIError struct {
	Code        int
	Description string
	Method      string
	HTTPStatus  int
}

func (e *APIError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("zalobot: %s failed: %d %s", e.Method, e.Code, e.Description)
	}
	return fmt.Sprintf("zalobot: %s failed: %d", e.Method, e.Code)
}

type TransportError struct {
	Method      string
	RedactedURL string
	Err         error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("zalobot: %s transport error (%s): %v", e.Method, e.RedactedURL, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

type DecodeError struct {
	Method     string
	HTTPStatus int
	Err        error
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("zalobot: %s: cannot decode response (HTTP %d): %v", e.Method, e.HTTPStatus, e.Err)
}

func (e *DecodeError) Unwrap() error { return e.Err }
```

`internal/api/envelope.go`:

```go
package api

import "encoding/json"

type envelope struct {
	OK             bool            `json:"ok"`
	Result         json.RawMessage `json:"result"`
	Description    string          `json:"description"`
	ErrorCode      int             `json:"error_code"`
	ErrorCodeCamel int             `json:"errorCode"`
}
```

`internal/api/client.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBytes = 4 << 20

type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

func NewClient(token, baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{token: token, baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

func (c *Client) URL(method string) string {
	return c.baseURL + "/bot" + c.token + "/" + method
}

func (c *Client) RedactedURL(method string) string {
	return c.baseURL + "/bot<TOKEN>/" + method
}

func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var body io.Reader
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, &DecodeError{Method: method, Err: err}
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL(method), body)
	if err != nil {
		return nil, &TransportError{Method: method, RedactedURL: c.RedactedURL(method), Err: err}
	}
	if params != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &TransportError{Method: method, RedactedURL: c.RedactedURL(method), Err: unwrapURLErr(err)}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, &TransportError{Method: method, RedactedURL: c.RedactedURL(method), Err: err}
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, &DecodeError{Method: method, HTTPStatus: resp.StatusCode, Err: err}
	}
	if !env.OK {
		code := env.ErrorCode
		if code == 0 {
			code = env.ErrorCodeCamel
		}
		if code == 0 {
			code = resp.StatusCode
		}
		return nil, &APIError{Code: code, Description: env.Description, Method: method, HTTPStatus: resp.StatusCode}
	}
	return env.Result, nil
}

func unwrapURLErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -v`
Expected: PASS (all five tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/api && go vet ./internal/api/
git add internal/api
git commit -m "feat(api): internal HTTP transport with envelope and error mapping"
```

---

### Task 2: Public error taxonomy and predicates

**Files:**
- Create: `errors.go`
- Test: `errors_test.go`

**Interfaces:**
- Consumes: `api.APIError`, `api.DecodeError` from Task 1.
- Produces: aliases `APIError`, `TransportError`, `DecodeError`; `ValidationError{Field, Reason string}`; `WebhookActiveError{URL string}`; sentinels `ErrNoToken`, `ErrStopped`, `ErrAlreadyPolling`, `ErrQueueFull`, `ErrSubstringNotFound`, `ErrUpdatesDropped`; predicates `IsUnauthorized`, `IsRateLimited`, `IsPollingTimeout`, `IsWebhookQuotaExceeded`.

- [ ] **Step 1: Write the failing test**

```go
package zalobot

import (
	"errors"
	"testing"

	"github.com/lucanhost/go-zalo-bot-api/internal/api"
)

func TestPredicatesUseCodeOrHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		fn   func(error) bool
		want bool
	}{
		{"api 401", &APIError{Code: 401}, IsUnauthorized, true},
		{"http 401 decode", &DecodeError{HTTPStatus: 401}, IsUnauthorized, true},
		{"api 429", &APIError{Code: 429}, IsRateLimited, true},
		{"http 429 decode", &DecodeError{HTTPStatus: 429}, IsRateLimited, true},
		{"426 not rate limited", &APIError{Code: 426}, IsRateLimited, false},
		{"426 quota", &APIError{Code: 426}, IsWebhookQuotaExceeded, true},
		{"408 timeout", &APIError{Code: 408}, IsPollingTimeout, true},
		{"wrapped", errors.New("x"), IsUnauthorized, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.err); got != tc.want {
				t.Fatalf("predicate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAliasesResolveToInternalTypes(t *testing.T) {
	var _ *api.APIError = &APIError{}
	var _ *api.DecodeError = &DecodeError{}
	var _ *api.TransportError = &TransportError{}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestPredicates -v`
Expected: FAIL, undefined `APIError`, `IsUnauthorized`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
package zalobot

import (
	"errors"
	"fmt"

	"github.com/lucanhost/go-zalo-bot-api/internal/api"
)

type (
	APIError       = api.APIError
	TransportError = api.TransportError
	DecodeError    = api.DecodeError
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("zalobot: invalid %s: %s", e.Field, e.Reason)
}

type WebhookActiveError struct{ URL string }

func (e *WebhookActiveError) Error() string {
	return fmt.Sprintf("zalobot: a webhook is active (%s); call DeleteWebhook or set WithAutoDeleteWebhook(true)", e.URL)
}

var (
	ErrNoToken           = errors.New("zalobot: bot token is empty")
	ErrStopped           = errors.New("zalobot: bot is stopped")
	ErrAlreadyPolling    = errors.New("zalobot: bot is already polling")
	ErrQueueFull         = errors.New("zalobot: update queue is full")
	ErrSubstringNotFound = errors.New("zalobot: substring not found")
	ErrUpdatesDropped    = errors.New("zalobot: updates channel buffer full; update dropped for Updates() consumers")
)

func apiCode(err error) (int, bool) {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code, true
	}
	return 0, false
}

func httpStatus(err error) (int, bool) {
	var ae *APIError
	if errors.As(err, &ae) && ae.HTTPStatus != 0 {
		return ae.HTTPStatus, true
	}
	var de *DecodeError
	if errors.As(err, &de) && de.HTTPStatus != 0 {
		return de.HTTPStatus, true
	}
	return 0, false
}

func matchesStatus(err error, status int) bool {
	if c, ok := apiCode(err); ok && c == status {
		return true
	}
	if s, ok := httpStatus(err); ok && s == status {
		return true
	}
	return false
}

func IsUnauthorized(err error) bool         { return matchesStatus(err, 401) }
func IsRateLimited(err error) bool          { return matchesStatus(err, 429) }
func IsPollingTimeout(err error) bool       { return matchesStatus(err, 408) }
func IsWebhookQuotaExceeded(err error) bool { return matchesStatus(err, 426) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run 'TestPredicates|TestAliases' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w errors.go errors_test.go && go vet .
git add errors.go errors_test.go
git commit -m "feat: public error taxonomy and status predicates"
```

---

### Task 3: Update and message model

**Files:**
- Create: `update.go`
- Test: `update_test.go`

**Interfaces:**
- Consumes: `ErrSubstringNotFound` is unrelated here; only stdlib.
- Produces: `EventName` + five constants; `ChatType` + `ChatPrivate`/`ChatGroup`; `User`, `BotInfo`, `Chat`, `Message`, `Update`, `SentMessage`; `(*Message).Time()`, `(*SentMessage).Time()`; `decodeUpdates(raw json.RawMessage) ([]Update, error)`; `decodeUpdateEnvelope(raw []byte) (Update, error)`.

- [ ] **Step 1: Write the failing test**

```go
package zalobot

import (
	"encoding/json"
	"testing"
)

func TestUserNormalizesDisplayName(t *testing.T) {
	var u User
	if err := json.Unmarshal([]byte(`{"id":"1","name":"Fallback"}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.DisplayName != "Fallback" {
		t.Fatalf("DisplayName = %q", u.DisplayName)
	}
	var u2 User
	_ = json.Unmarshal([]byte(`{"id":"1","display_name":"Preferred","name":"Fallback"}`), &u2)
	if u2.DisplayName != "Preferred" {
		t.Fatalf("DisplayName = %q", u2.DisplayName)
	}
}

func TestMessageNormalizesPhotoAndTime(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"photo_url":"https://x/y.jpg","date":1750316131602}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Photo != "https://x/y.jpg" {
		t.Fatalf("Photo = %q", m.Photo)
	}
	if m.Time().UnixMilli() != 1750316131602 {
		t.Fatalf("Time = %v", m.Time())
	}
}

func TestUpdateUnmarshalSetsEventAndRawCopy(t *testing.T) {
	body := []byte(`{"event_name":"message.text.received","message":{"text":"hi"}}`)
	var u Update
	if err := json.Unmarshal(body, &u); err != nil {
		t.Fatal(err)
	}
	if u.EventName != EventTextReceived || u.Message == nil || u.Message.Text != "hi" {
		t.Fatalf("Update = %+v", u)
	}
	body[0] = 'X' // mutate the input; Raw must be a copy
	if u.Raw[0] == 'X' {
		t.Fatal("Raw aliases the input buffer")
	}
}

func TestUnsupportedEventHasNoMessage(t *testing.T) {
	raw := json.RawMessage(`{"event_name":"message.unsupported.received"}`)
	ups, err := decodeUpdates(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 1 || ups[0].Message != nil || ups[0].EventName != EventUnsupportedReceived {
		t.Fatalf("updates = %+v", ups)
	}
}

func TestDecodeUpdatesTolerant(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"array", `[{"event_name":"message.text.received"}]`, 1},
		{"object", `{"event_name":"message.text.received"}`, 1},
		{"empty object", `{}`, 0},
		{"null", `null`, 0},
		{"absent", ``, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ups, err := decodeUpdates(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(ups) != tc.want {
				t.Fatalf("len = %d, want %d", len(ups), tc.want)
			}
		})
	}
}

func TestDecodeUpdateEnvelopeAcceptsEnvelopeAndBare(t *testing.T) {
	env := []byte(`{"ok":true,"result":{"event_name":"message.text.received","message":{"text":"a"}}}`)
	if u, err := decodeUpdateEnvelope(env); err != nil || u.Message.Text != "a" {
		t.Fatalf("envelope: %+v %v", u, err)
	}
	bare := []byte(`{"event_name":"message.text.received","message":{"text":"b"}}`)
	if u, err := decodeUpdateEnvelope(bare); err != nil || u.Message.Text != "b" {
		t.Fatalf("bare: %+v %v", u, err)
	}
	if _, err := decodeUpdateEnvelope([]byte(`{}`)); err == nil {
		t.Fatal("want error for missing event_name")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestUser|TestMessage|TestUpdate|TestUnsupported|TestDecodeUpdates|TestDecodeUpdateEnvelope' -v`
Expected: FAIL, undefined `User`, `Message`, `decodeUpdates`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
package zalobot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

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
	ChatGroup   ChatType = "GROUP"
)

type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
	IsBot       bool   `json:"is_bot"`
}

func (u *User) UnmarshalJSON(b []byte) error {
	type alias User
	var a struct {
		*alias
		Name string `json:"name"`
	}
	a.alias = (*alias)(u)
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if u.DisplayName == "" {
		u.DisplayName = a.Name
	}
	return nil
}

type BotInfo struct {
	ID            string `json:"id"`
	AccountName   string `json:"account_name"`
	AccountType   string `json:"account_type"`
	CanJoinGroups bool   `json:"can_join_groups"`
}

func (b *BotInfo) UnmarshalJSON(data []byte) error {
	type alias BotInfo
	var a struct {
		*alias
		Name string `json:"name"`
	}
	a.alias = (*alias)(b)
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if b.AccountName == "" {
		b.AccountName = a.Name
	}
	return nil
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
	Caption     string `json:"caption"`
	Sticker     string `json:"sticker"`
	URL         string `json:"url"`
	VoiceURL    string `json:"voice_url"`
	MessageType string `json:"message_type"`
	MessageID   string `json:"message_id"`
	Date        int64  `json:"date"`
}

func (m *Message) UnmarshalJSON(b []byte) error {
	type alias Message
	var a struct {
		*alias
		PhotoURL string `json:"photo_url"`
	}
	a.alias = (*alias)(m)
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if m.Photo == "" {
		m.Photo = a.PhotoURL
	}
	return nil
}

func (m *Message) Time() time.Time { return time.UnixMilli(m.Date) }

type Update struct {
	EventName EventName       `json:"event_name"`
	Message   *Message        `json:"message"`
	Raw       json.RawMessage `json:"-"`
}

func (u *Update) UnmarshalJSON(b []byte) error {
	type alias Update
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*u = Update(a)
	u.Raw = append(json.RawMessage(nil), b...)
	return nil
}

type SentMessage struct {
	MessageID string `json:"message_id"`
	Date      int64  `json:"date"`
}

func (s *SentMessage) Time() time.Time { return time.UnixMilli(s.Date) }

func decodeUpdates(raw json.RawMessage) ([]Update, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil, nil
	}
	switch t[0] {
	case '[':
		var arr []Update
		if err := json.Unmarshal(t, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	case '{':
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(t, &probe); err != nil {
			return nil, err
		}
		if len(probe) == 0 {
			return nil, nil
		}
		var u Update
		if err := json.Unmarshal(t, &u); err != nil {
			return nil, err
		}
		return []Update{u}, nil
	default:
		return nil, fmt.Errorf("zalobot: unexpected getUpdates result %q", t[:1])
	}
}

func decodeUpdateEnvelope(raw []byte) (Update, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return Update{}, &ValidationError{Field: "update", Reason: "empty body"}
	}
	var env struct {
		OK     *bool           `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(t, &env); err != nil {
		return Update{}, &ValidationError{Field: "update", Reason: "invalid JSON"}
	}
	if env.OK != nil && env.Result != nil && !bytes.Equal(bytes.TrimSpace(env.Result), []byte("null")) {
		var u Update
		if err := json.Unmarshal(env.Result, &u); err != nil {
			return Update{}, &ValidationError{Field: "update", Reason: "invalid result"}
		}
		return u, nil
	}
	var u Update
	if err := json.Unmarshal(t, &u); err != nil {
		return Update{}, &ValidationError{Field: "update", Reason: "invalid JSON"}
	}
	if u.EventName == "" {
		return Update{}, &ValidationError{Field: "event_name", Reason: "missing"}
	}
	return u, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run 'TestUser|TestMessage|TestUpdate|TestUnsupported|TestDecodeUpdates|TestDecodeUpdateEnvelope' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w update.go update_test.go && go vet .
git add update.go update_test.go
git commit -m "feat: typed update/message model with tolerant decoding"
```

---

### Task 4: Rich text (UTF-16 style ranges)

**Files:**
- Create: `richtext.go`
- Test: `richtext_test.go`

**Interfaces:**
- Consumes: `ValidationError`, `ErrSubstringNotFound` from Task 2.
- Produces: `ParseMode` + constants; `ChatAction` + constants; `StyleCode` + constants; `TextStyle{Start, Len int; Styles []StyleCode}`; `StyleRange(text, substr string, codes ...StyleCode) (TextStyle, error)`; `StyleRangeN(text, substr string, occurrence int, codes ...StyleCode) (TextStyle, error)`.

- [ ] **Step 1: Write the failing test**

```go
package zalobot

import (
	"errors"
	"testing"
	"unicode/utf16"
)

func utf16Slice(s string) []uint16 { return utf16.Encode([]rune(s)) }

func TestStyleRangeASCII(t *testing.T) {
	ts, err := StyleRange("hello world", "world", StyleBold)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Start != 6 || ts.Len != 5 {
		t.Fatalf("ts = %+v", ts)
	}
	if len(ts.Styles) != 1 || ts.Styles[0] != StyleBold {
		t.Fatalf("styles = %v", ts.Styles)
	}
}

func TestStyleRangeVietnameseNFCAndNFD(t *testing.T) {
	forms := []struct{ text, sub string }{
		{"Xin chào bạn", "chào"},                  // composed
		{"Xin cha\u0300o ba\u0323n", "cha\u0300o"}, // decomposed
	}
	for _, f := range forms {
		ts, err := StyleRange(f.text, f.sub, StyleItalic)
		if err != nil {
			t.Fatalf("%q: %v", f.text, err)
		}
		u := utf16Slice(f.text)
		got := string(utf16.Decode(u[ts.Start : ts.Start+ts.Len]))
		if got != f.sub {
			t.Fatalf("%q: slice = %q, want %q", f.text, got, f.sub)
		}
	}
}

func TestStyleRangeEmojiSurrogatePair(t *testing.T) {
	s := "hi 😀 there"
	ts, err := StyleRange(s, "😀", StyleUnderline)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Len != 2 { // surrogate pair = 2 UTF-16 units
		t.Fatalf("Len = %d, want 2", ts.Len)
	}
	u := utf16Slice(s)
	if got := string(utf16.Decode(u[ts.Start : ts.Start+ts.Len])); got != "😀" {
		t.Fatalf("slice = %q", got)
	}
}

func TestStyleRangeN(t *testing.T) {
	ts, err := StyleRangeN("a a a", "a", 2, StyleBold)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Start != 2 {
		t.Fatalf("Start = %d, want 2", ts.Start)
	}
	if _, err := StyleRangeN("a a a", "a", 9); !errors.Is(err, ErrSubstringNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestStyleRangeRejectsBadInput(t *testing.T) {
	if _, err := StyleRange("abc", ""); err == nil {
		t.Fatal("want error for empty substr")
	}
	if _, err := StyleRangeN("abc", "a", 0); err == nil {
		t.Fatal("want error for occurrence < 1")
	}
	if _, err := StyleRange("abc", "z"); !errors.Is(err, ErrSubstringNotFound) {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestStyle -v`
Expected: FAIL, undefined `StyleRange`.

- [ ] **Step 3: Write minimal implementation**

```go
package zalobot

import (
	"strings"
	"unicode/utf16"
)

type ParseMode string

const (
	ParseModeNone     ParseMode = ""
	ParseModeMarkdown ParseMode = "markdown"
	ParseModeHTML     ParseMode = "html"
)

type ChatAction string

const (
	ChatActionTyping      ChatAction = "typing"
	ChatActionUploadPhoto ChatAction = "upload_photo"
)

type StyleCode string

const (
	StyleBold          StyleCode = "b"
	StyleItalic        StyleCode = "i"
	StyleUnderline     StyleCode = "u"
	StyleStrike        StyleCode = "s"
	StyleSizeSmall     StyleCode = "f_13"
	StyleSizeNormal    StyleCode = "f_15"
	StyleSizeLarge     StyleCode = "f_18"
	StyleSizeHuge      StyleCode = "f_20"
	ColorDefault       StyleCode = "c_050a19"
	ColorGreen         StyleCode = "c_15a85f"
	ColorYellow        StyleCode = "c_f7b503"
	ColorOrange        StyleCode = "c_f27806"
	ColorRed           StyleCode = "c_db342e"
	StyleListUnordered StyleCode = "lst_1"
	StyleListOrdered   StyleCode = "lst_2"
	StyleIndent1       StyleCode = "ind_1"
	StyleIndent2       StyleCode = "ind_2"
	StyleIndent3       StyleCode = "ind_3"
	StyleIndent4       StyleCode = "ind_4"
	StyleIndent5       StyleCode = "ind_5"
)

type TextStyle struct {
	Start  int         `json:"start"`
	Len    int         `json:"len"`
	Styles []StyleCode `json:"st"`
}

func StyleRange(text, substr string, codes ...StyleCode) (TextStyle, error) {
	return StyleRangeN(text, substr, 1, codes...)
}

func StyleRangeN(text, substr string, occurrence int, codes ...StyleCode) (TextStyle, error) {
	if substr == "" {
		return TextStyle{}, &ValidationError{Field: "substr", Reason: "must not be empty"}
	}
	if occurrence < 1 {
		return TextStyle{}, &ValidationError{Field: "occurrence", Reason: "must be >= 1"}
	}
	idx := -1
	from := 0
	for i := 0; i < occurrence; i++ {
		j := strings.Index(text[from:], substr)
		if j < 0 {
			return TextStyle{}, ErrSubstringNotFound
		}
		idx = from + j
		from = idx + len(substr)
	}
	start := len(utf16.Encode([]rune(text[:idx])))
	length := len(utf16.Encode([]rune(text[idx : idx+len(substr)])))
	return TextStyle{Start: start, Len: length, Styles: append([]StyleCode(nil), codes...)}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestStyle -v`
Expected: PASS (including the NFC and NFD forms and the emoji surrogate pair).

- [ ] **Step 5: Commit**

```bash
gofmt -w richtext.go richtext_test.go && go vet .
git add richtext.go richtext_test.go
git commit -m "feat: rich text with UTF-16 correct style ranges"
```

---

### Task 5: Dispatcher (admission, scheduler, fan-out)

**Files:**
- Create: `dispatcher.go`
- Test: `dispatcher_test.go`

**Interfaces:**
- Consumes: `Update`, `ErrStopped`, `ErrQueueFull`, `ErrUpdatesDropped`.
- Produces: `dispatchConfig{workers, quantum, perChat, maxBuffered int}`; `newDispatcher(cfg dispatchConfig, updatesCh chan Update, updatesOn *atomic.Bool, onError func(error), handle func(Update)) *dispatcher`; `(*dispatcher).admitNonBlocking(Update) error`; `(*dispatcher).admitBlocking(context.Context, Update) error`; `(*dispatcher).stop()`; `(*dispatcher).wait()`.

- [ ] **Step 1: Write the failing test**

```go
package zalobot

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestDispatcher(cfg dispatchConfig, handle func(Update)) (*dispatcher, chan Update) {
	ch := make(chan Update, 64)
	var on atomic.Bool
	on.Store(true)
	return newDispatcher(cfg, ch, &on, func(error) {}, handle), ch
}

func msgUpdate(chat, text string) Update {
	return Update{EventName: EventTextReceived, Message: &Message{Chat: &Chat{ID: chat}, Text: text}}
}

func TestDispatcherProcessesInOrderPerChat(t *testing.T) {
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	d, _ := newTestDispatcher(dispatchConfig{workers: 2, quantum: 1, perChat: 8, maxBuffered: 64}, func(u Update) {
		mu.Lock()
		got = append(got, u.Message.Text)
		if len(got) == 3 {
			close(done)
		}
		mu.Unlock()
	})
	for _, s := range []string{"1", "2", "3"} {
		if err := d.admitNonBlocking(msgUpdate("c1", s)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	mu.Lock()
	defer mu.Unlock()
	if got[0] != "1" || got[1] != "2" || got[2] != "3" {
		t.Fatalf("order = %v", got)
	}
}

func TestDispatcherNonBlockingReturnsQueueFull(t *testing.T) {
	release := make(chan struct{})
	d, _ := newTestDispatcher(dispatchConfig{workers: 1, quantum: 1, perChat: 1, maxBuffered: 10}, func(Update) { <-release })
	_ = d.admitNonBlocking(msgUpdate("c1", "a")) // taken by worker (quantum 1)
	// Fill the per-chat queue to its cap, then expect ErrQueueFull.
	_ = d.admitNonBlocking(msgUpdate("c1", "b"))
	deadline := time.After(time.Second)
	for {
		err := d.admitNonBlocking(msgUpdate("c1", "c"))
		if err == ErrQueueFull {
			break
		}
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		select {
		case <-deadline:
			t.Fatal("queue never filled")
		case <-time.After(time.Millisecond):
		}
	}
	close(release)
	d.stop()
	d.wait()
}

func TestDispatcherBlockingAdmissionAbortsOnContext(t *testing.T) {
	release := make(chan struct{})
	d, _ := newTestDispatcher(dispatchConfig{workers: 1, quantum: 1, perChat: 1, maxBuffered: 10}, func(Update) { <-release })
	_ = d.admitNonBlocking(msgUpdate("c1", "a"))
	_ = d.admitNonBlocking(msgUpdate("c1", "b"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := d.admitBlocking(ctx, msgUpdate("c1", "c")); err == nil {
		t.Fatal("want context error")
	}
	close(release)
	d.stop()
	d.wait()
}

func TestDispatcherStopUnblocksAdmission(t *testing.T) {
	release := make(chan struct{})
	d, _ := newTestDispatcher(dispatchConfig{workers: 1, quantum: 1, perChat: 1, maxBuffered: 10}, func(Update) { <-release })
	_ = d.admitNonBlocking(msgUpdate("c1", "a"))
	_ = d.admitNonBlocking(msgUpdate("c1", "b"))
	errCh := make(chan error, 1)
	go func() { errCh <- d.admitBlocking(context.Background(), msgUpdate("c1", "c")) }()
	time.Sleep(20 * time.Millisecond)
	d.stop()
	select {
	case err := <-errCh:
		if err != ErrStopped {
			t.Fatalf("err = %v, want ErrStopped", err)
		}
	case <-time.After(time.Second):
		t.Fatal("admission did not unblock")
	}
	close(release)
	d.wait()
}

func TestDispatcherQuantumPreventsMonopolization(t *testing.T) {
	var mu sync.Mutex
	var order []string
	release := make(chan struct{})
	first := make(chan struct{})
	var once sync.Once
	d, _ := newTestDispatcher(dispatchConfig{workers: 1, quantum: 1, perChat: 8, maxBuffered: 64}, func(u Update) {
		mu.Lock()
		order = append(order, u.Message.Chat.ID)
		mu.Unlock()
		once.Do(func() { close(first) })
		<-release
	})
	// Flood chat A, then admit chat B while A is still being drained.
	for i := 0; i < 3; i++ {
		_ = d.admitNonBlocking(msgUpdate("A", "x"))
	}
	<-first
	_ = d.admitNonBlocking(msgUpdate("B", "y"))
	close(release)
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(order)
		var sawB bool
		for _, id := range order {
			if id == "B" {
				sawB = true
			}
		}
		mu.Unlock()
		if sawB && n >= 4 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("chat B starved behind chat A")
		case <-time.After(5 * time.Millisecond):
		}
	}
	d.stop()
	d.wait()
}

func TestDispatcherFanoutDropsWithSingleError(t *testing.T) {
	var drops int32
	ch := make(chan Update, 1)
	var on atomic.Bool
	on.Store(true)
	d := newDispatcher(dispatchConfig{workers: 1, quantum: 10, perChat: 16, maxBuffered: 64}, ch, &on, func(err error) {
		if err == ErrUpdatesDropped {
			atomic.AddInt32(&drops, 1)
		}
	}, func(Update) {})
	for i := 0; i < 5; i++ {
		_ = d.admitNonBlocking(msgUpdate("c", "x"))
	}
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&drops) == 0 {
		select {
		case <-deadline:
			t.Fatal("no drop error")
		case <-time.After(5 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond)
	if n := atomic.LoadInt32(&drops); n != 1 {
		t.Fatalf("drop errors = %d, want 1 per episode", n)
	}
	d.stop()
	d.wait()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestDispatcher -v`
Expected: FAIL, undefined `dispatcher`, `newDispatcher`, `dispatchConfig`.

- [ ] **Step 3: Write minimal implementation**

```go
package zalobot

import (
	"context"
	"sync"
	"sync/atomic"
)

type dispatchConfig struct {
	workers     int
	quantum     int
	perChat     int
	maxBuffered int
}

type chatQueue struct {
	items  []Update
	active bool
}

type dispatcher struct {
	mu        sync.Mutex
	cond      *sync.Cond
	queues    map[string]*chatQueue
	ready     []string
	total     int
	stopped   bool
	cfg       dispatchConfig
	wg        sync.WaitGroup
	startOnce sync.Once
	handle    func(Update)

	updatesCh   chan Update
	updatesOn   *atomic.Bool
	onError     func(error)
	dropEpisode atomic.Bool
}

func newDispatcher(cfg dispatchConfig, updatesCh chan Update, updatesOn *atomic.Bool, onError func(error), handle func(Update)) *dispatcher {
	d := &dispatcher{
		queues:    make(map[string]*chatQueue),
		cfg:       cfg,
		updatesCh: updatesCh,
		updatesOn: updatesOn,
		onError:   onError,
		handle:    handle,
	}
	d.cond = sync.NewCond(&d.mu)
	return d
}

func chatKey(u Update) string {
	if u.Message != nil && u.Message.Chat != nil && u.Message.Chat.ID != "" {
		return u.Message.Chat.ID
	}
	return "event:" + string(u.EventName)
}

func (d *dispatcher) admitNonBlocking(u Update) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.admitLocked(u, nil)
}

func (d *dispatcher) admitBlocking(ctx context.Context, u Update) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	stop := context.AfterFunc(ctx, func() {
		d.mu.Lock()
		d.cond.Broadcast()
		d.mu.Unlock()
	})
	defer stop()
	return d.admitLocked(u, ctx)
}

func (d *dispatcher) admitLocked(u Update, ctx context.Context) error {
	key := chatKey(u)
	for {
		if d.stopped {
			return ErrStopped
		}
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		cq := d.queues[key]
		if cq == nil {
			cq = &chatQueue{}
			d.queues[key] = cq
		}
		if len(cq.items) < d.cfg.perChat && d.total < d.cfg.maxBuffered {
			cq.items = append(cq.items, u)
			d.total++
			if !cq.active {
				d.ready = append(d.ready, key)
				d.cond.Broadcast()
			}
			d.startOnce.Do(d.start)
			return nil
		}
		if ctx == nil {
			return ErrQueueFull
		}
		d.cond.Wait()
	}
}

func (d *dispatcher) start() {
	for i := 0; i < d.cfg.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

func (d *dispatcher) worker() {
	defer d.wg.Done()
	for {
		d.mu.Lock()
		for len(d.ready) == 0 && !d.stopped {
			d.cond.Wait()
		}
		if len(d.ready) == 0 && d.stopped {
			d.mu.Unlock()
			return
		}
		key := d.ready[0]
		d.ready = d.ready[1:]
		cq := d.queues[key]
		if cq == nil || len(cq.items) == 0 {
			d.mu.Unlock()
			continue
		}
		cq.active = true
		n := d.cfg.quantum
		if n < 1 {
			n = 1
		}
		batch := make([]Update, 0, n)
		for len(cq.items) > 0 && len(batch) < n {
			batch = append(batch, cq.items[0])
			cq.items = cq.items[1:]
			d.total--
		}
		d.mu.Unlock()

		for _, u := range batch {
			d.handle(u)
			d.fanout(u)
		}

		d.mu.Lock()
		if len(cq.items) > 0 {
			d.ready = append(d.ready, key)
			d.cond.Broadcast()
		} else {
			cq.active = false
			delete(d.queues, key)
		}
		d.mu.Unlock()
	}
}

func (d *dispatcher) fanout(u Update) {
	if d.updatesOn == nil || !d.updatesOn.Load() {
		return
	}
	select {
	case d.updatesCh <- u:
		d.dropEpisode.Store(false)
	default:
		if d.dropEpisode.CompareAndSwap(false, true) {
			if d.onError != nil {
				d.onError(ErrUpdatesDropped)
			}
		}
	}
}

func (d *dispatcher) stop() {
	d.mu.Lock()
	d.stopped = true
	d.cond.Broadcast()
	d.mu.Unlock()
}

func (d *dispatcher) wait() { d.wg.Wait() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race . -run TestDispatcher -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w dispatcher.go dispatcher_test.go && go vet .
git add dispatcher.go dispatcher_test.go
git commit -m "feat: per-chat dispatcher with bounded worker pool and quantum fairness"
```

---

### Task 6: Bot construction, handler registry, routing

**Files:**
- Create: `options.go`
- Create: `bot.go`
- Create: `handlers.go`
- Test: `bot_test.go`
- Test: `handlers_test.go`

**Interfaces:**
- Consumes: `dispatcher`, `newDispatcher`, `api.NewClient`, `decodeUpdateEnvelope`.
- Produces: `Option`; option funcs `WithHTTPClient`, `WithBaseURL`, `WithPolling`, `WithWorkers`, `WithQuantum`, `WithUpdatesBuffer`, `WithPerChatBuffer`, `WithMaxBuffered`, `WithDrainTimeout`, `WithAutoDeleteWebhook`; `PollingOptions{Timeout time.Duration}`; `SendMessageOptions`, `SendPhotoOptions`, `GetUpdatesOptions`; `New(token string, opts ...Option) (*Bot, error)`; `(*Bot).OnEvent/OnMessage/OnText/OnCommand/OnError/Updates/ProcessUpdate`; `(*Bot).reportError`.

- [ ] **Step 1: Write the failing test**

`bot_test.go`:

```go
package zalobot

import (
	"errors"
	"testing"
)

func TestNewRejectsEmptyToken(t *testing.T) {
	if _, err := New(""); !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	b, err := New("TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if b.cfg.baseURL != "https://bot-api.zaloplatforms.com" {
		t.Fatalf("baseURL = %q", b.cfg.baseURL)
	}
	if b.cfg.workers != 4 || b.cfg.quantum != 1 || b.cfg.updatesBuffer != 64 {
		t.Fatalf("cfg = %+v", b.cfg)
	}
}

func TestOptionsOverride(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(8), WithQuantum(3), WithBaseURL("http://x"), WithUpdatesBuffer(7))
	if b.cfg.workers != 8 || b.cfg.quantum != 3 || b.cfg.baseURL != "http://x" || b.cfg.updatesBuffer != 7 {
		t.Fatalf("cfg = %+v", b.cfg)
	}
}
```

`handlers_test.go`:

```go
package zalobot

import (
	"context"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestRoutingOrderAndAllMatching(t *testing.T) {
	b, _ := New("TOKEN")
	var mu sync.Mutex
	var calls []string
	b.OnEvent(EventTextReceived, func(context.Context, Update) { mu.Lock(); calls = append(calls, "event"); mu.Unlock() })
	b.OnMessage(func(context.Context, *Message) { mu.Lock(); calls = append(calls, "message"); mu.Unlock() })
	b.OnText(regexp.MustCompile(`^/hi (\w+)$`), func(_ context.Context, _ *Message, m []string) {
		mu.Lock()
		calls = append(calls, "text:"+m[1])
		mu.Unlock()
	})
	b.OnCommand("hi", func(_ context.Context, _ *Message, args []string) {
		mu.Lock()
		calls = append(calls, "command:"+args[0])
		mu.Unlock()
	})
	u := msgUpdate("c", "/hi bob")
	b.handlers.run(b.handlerCtx, u)
	want := []string{"event", "message", "text:bob", "command:bob"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestUnsupportedEventSkipsMessageAndText(t *testing.T) {
	b, _ := New("TOKEN")
	var messages, texts int
	b.OnMessage(func(context.Context, *Message) { messages++ })
	b.OnText(regexp.MustCompile(`.`), func(context.Context, *Message, []string) { texts++ })
	var events int
	b.OnEvent(EventUnsupportedReceived, func(context.Context, Update) { events++ })
	b.handlers.run(b.handlerCtx, Update{EventName: EventUnsupportedReceived})
	if messages != 0 || texts != 0 || events != 1 {
		t.Fatalf("messages=%d texts=%d events=%d", messages, texts, events)
	}
}

func TestHandlerPanicIsReportedAndDoesNotStopOthers(t *testing.T) {
	b, _ := New("TOKEN")
	var reported bool
	b.OnError(func(error) { reported = true })
	b.OnMessage(func(context.Context, *Message) { panic("boom") })
	ran := false
	b.OnMessage(func(context.Context, *Message) { ran = true })
	b.handlers.run(b.handlerCtx, msgUpdate("c", "hi"))
	if !reported || !ran {
		t.Fatalf("reported=%v ran=%v", reported, ran)
	}
}

func TestProcessUpdateAdmitsAndUpdatesChannelSeesIt(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(1))
	got := make(chan Update, 1)
	ch := b.Updates()
	b.OnMessage(func(context.Context, *Message) {})
	if err := b.ProcessUpdate([]byte(`{"ok":true,"result":{"event_name":"message.text.received","message":{"text":"x"}}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case u := <-ch:
		got <- u
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	_ = got
	b.disp.stop()
	b.disp.wait()
}
```

Add `"time"` to the imports of `handlers_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestNew|TestOptions|TestRouting|TestUnsupportedEventSkips|TestHandlerPanic|TestProcessUpdate' -v`
Expected: FAIL, undefined `New`, `Option`, `handlerSet`, etc.

- [ ] **Step 3: Write minimal implementation**

`options.go`:

```go
package zalobot

import (
	"net/http"
	"time"
)

type Option func(*config)

type config struct {
	httpClient        *http.Client
	baseURL           string
	pollTimeout       time.Duration
	workers           int
	quantum           int
	updatesBuffer     int
	perChatBuffer     int
	maxBuffered       int
	drainTimeout      time.Duration
	autoDeleteWebhook bool
}

func defaultConfig() *config {
	return &config{
		httpClient:    http.DefaultClient,
		baseURL:       "https://bot-api.zaloplatforms.com",
		pollTimeout:   30 * time.Second,
		workers:       4,
		quantum:       1,
		updatesBuffer: 64,
		perChatBuffer: 512,
		maxBuffered:   10000,
		drainTimeout:  10 * time.Second,
	}
}

func WithHTTPClient(c *http.Client) Option       { return func(cfg *config) { cfg.httpClient = c } }
func WithBaseURL(u string) Option                { return func(cfg *config) { cfg.baseURL = u } }
func WithWorkers(n int) Option                   { return func(cfg *config) { cfg.workers = n } }
func WithQuantum(n int) Option                   { return func(cfg *config) { cfg.quantum = n } }
func WithUpdatesBuffer(n int) Option             { return func(cfg *config) { cfg.updatesBuffer = n } }
func WithPerChatBuffer(n int) Option             { return func(cfg *config) { cfg.perChatBuffer = n } }
func WithMaxBuffered(n int) Option               { return func(cfg *config) { cfg.maxBuffered = n } }
func WithDrainTimeout(d time.Duration) Option    { return func(cfg *config) { cfg.drainTimeout = d } }
func WithAutoDeleteWebhook(b bool) Option        { return func(cfg *config) { cfg.autoDeleteWebhook = b } }

type PollingOptions struct{ Timeout time.Duration }

func WithPolling(o PollingOptions) Option {
	return func(cfg *config) {
		if o.Timeout > 0 {
			cfg.pollTimeout = o.Timeout
		}
	}
}

type SendMessageOptions struct {
	ParseMode  ParseMode
	TextStyles []TextStyle
}

type SendPhotoOptions struct{ Caption string }

type GetUpdatesOptions struct{ Timeout time.Duration }
```

`bot.go`:

```go
package zalobot

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/lucanhost/go-zalo-bot-api/internal/api"
)

type handlerCtxKey struct{}

type Bot struct {
	token string
	cfg   *config
	api   *api.Client
	disp  *dispatcher

	handlers *handlerSet

	mu         sync.Mutex
	err        error
	stopped    bool
	polling    atomic.Bool
	pollCancel context.CancelFunc

	handlerCtx    context.Context
	handlerCancel context.CancelFunc

	updatesCh chan Update
	updatesOn atomic.Bool

	wg        sync.WaitGroup
	done      chan struct{}
	closeOnce sync.Once
}

func New(token string, opts ...Option) (*Bot, error) {
	if token == "" {
		return nil, ErrNoToken
	}
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}
	b := &Bot{
		token:     token,
		cfg:       cfg,
		api:       api.NewClient(token, cfg.baseURL, cfg.httpClient),
		handlers:  newHandlerSet(),
		updatesCh: make(chan Update, cfg.updatesBuffer),
		done:      make(chan struct{}),
	}
	hctx, hcancel := context.WithCancel(context.Background())
	b.handlerCtx = context.WithValue(hctx, handlerCtxKey{}, true)
	b.handlerCancel = hcancel

	b.disp = newDispatcher(dispatchConfig{
		workers:     cfg.workers,
		quantum:     cfg.quantum,
		perChat:     cfg.perChatBuffer,
		maxBuffered: cfg.maxBuffered,
	}, b.updatesCh, &b.updatesOn, b.reportError, func(u Update) {
		b.handlers.run(b.handlerCtx, u)
	})
	b.handlers.onPanic = b.reportError
	return b, nil
}

func (b *Bot) reportError(err error) {
	if err == nil {
		return
	}
	if len(b.handlers.errorHandlers()) == 0 {
		slog.Default().Error("zalobot", "error", err)
		return
	}
	for _, h := range b.handlers.errorHandlers() {
		func() {
			defer func() { _ = recover() }()
			h(err)
		}()
	}
}

func (b *Bot) Done() <-chan struct{} { return b.done }

func (b *Bot) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

func (b *Bot) setErr(err error) {
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	b.mu.Unlock()
}

func (b *Bot) IsPolling() bool { return b.polling.Load() }
```

`handlers.go`:

```go
package zalobot

import (
	"context"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
)

type handlerSet struct {
	mu       sync.RWMutex
	events   map[EventName][]func(context.Context, Update)
	messages []func(context.Context, *Message)
	texts    []textHandler
	commands []commandHandler
	onErrors []func(error)
	onPanic  func(error)
}

type textHandler struct {
	re *regexp.Regexp
	fn func(context.Context, *Message, []string)
}

type commandHandler struct {
	name string
	fn   func(context.Context, *Message, []string)
}

func newHandlerSet() *handlerSet {
	return &handlerSet{events: make(map[EventName][]func(context.Context, Update))}
}

func (h *handlerSet) OnEvent(name EventName, fn func(context.Context, Update)) {
	h.mu.Lock()
	h.events[name] = append(h.events[name], fn)
	h.mu.Unlock()
}

func (h *handlerSet) OnMessage(fn func(context.Context, *Message)) {
	h.mu.Lock()
	h.messages = append(h.messages, fn)
	h.mu.Unlock()
}

func (h *handlerSet) OnText(re *regexp.Regexp, fn func(context.Context, *Message, []string)) {
	h.mu.Lock()
	h.texts = append(h.texts, textHandler{re: re, fn: fn})
	h.mu.Unlock()
}

func (h *handlerSet) OnCommand(name string, fn func(context.Context, *Message, []string)) {
	h.mu.Lock()
	h.commands = append(h.commands, commandHandler{name: name, fn: fn})
	h.mu.Unlock()
}

func (h *handlerSet) OnError(fn func(error)) {
	h.mu.Lock()
	h.onErrors = append(h.onErrors, fn)
	h.mu.Unlock()
}

func (h *handlerSet) errorHandlers() []func(error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]func(error){}, h.onErrors...)
}

func (h *handlerSet) run(ctx context.Context, u Update) {
	h.mu.RLock()
	events := append([]func(context.Context, Update){}, h.events[u.EventName]...)
	messages := append([]func(context.Context, *Message){}, h.messages...)
	texts := append([]textHandler{}, h.texts...)
	commands := append([]commandHandler{}, h.commands...)
	h.mu.RUnlock()

	for _, fn := range events {
		h.call(fn, ctx, u)
	}
	if u.Message == nil {
		return
	}
	for _, fn := range messages {
		h.callMessage(fn, ctx, u.Message)
	}
	if u.Message.Text == "" {
		return
	}
	for _, th := range texts {
		if m := th.re.FindStringSubmatch(u.Message.Text); m != nil {
			h.callText(th, ctx, u.Message, m)
		}
	}
	for _, ch := range commands {
		if name, args, ok := parseCommand(u.Message.Text); ok && name == ch.name {
			h.callCommand(ch, ctx, u.Message, args)
		}
	}
}

func (h *handlerSet) call(fn func(context.Context, Update), ctx context.Context, u Update) {
	defer h.recover()
	fn(ctx, u)
}

func (h *handlerSet) callMessage(fn func(context.Context, *Message), ctx context.Context, m *Message) {
	defer h.recover()
	fn(ctx, m)
}

func (h *handlerSet) callText(th textHandler, ctx context.Context, m *Message, match []string) {
	defer h.recover()
	th.fn(ctx, m, match)
}

func (h *handlerSet) callCommand(ch commandHandler, ctx context.Context, m *Message, args []string) {
	defer h.recover()
	ch.fn(ctx, m, args)
}

func (h *handlerSet) recover() {
	if r := recover(); r != nil {
		err := fmt.Errorf("zalobot: handler panic: %v\n%s", r, debug.Stack())
		if h.onPanic != nil {
			h.onPanic(err)
		} else {
			slog.Default().Error("zalobot: handler panic", "panic", r)
		}
	}
}

func parseCommand(text string) (name string, args []string, ok bool) {
	if !strings.HasPrefix(text, "/") {
		return "", nil, false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", nil, false
	}
	name = strings.TrimPrefix(fields[0], "/")
	if name == "" {
		return "", nil, false
	}
	return name, fields[1:], true
}
```

Add the public `On*` methods and `ProcessUpdate`/`Updates` in `handlers.go`:

```go
func (b *Bot) OnEvent(name EventName, fn func(context.Context, Update)) { b.handlers.OnEvent(name, fn) }
func (b *Bot) OnMessage(fn func(context.Context, *Message))            { b.handlers.OnMessage(fn) }
func (b *Bot) OnText(re *regexp.Regexp, fn func(context.Context, *Message, []string)) {
	b.handlers.OnText(re, fn)
}
func (b *Bot) OnCommand(name string, fn func(context.Context, *Message, []string)) {
	b.handlers.OnCommand(name, fn)
}
func (b *Bot) OnError(fn func(error)) { b.handlers.OnError(fn) }

func (b *Bot) Updates() <-chan Update {
	b.updatesOn.Store(true)
	return b.updatesCh
}

func (b *Bot) ProcessUpdate(raw []byte) error {
	u, err := decodeUpdateEnvelope(raw)
	if err != nil {
		return err
	}
	return b.disp.admitNonBlocking(u)
}
```

Note: `reportError`'s panic recovery logs the stack only for handler panics; for `reportError` itself the `handlerSet.recover` logs via `slog`. To satisfy `TestHandlerPanicIsReportedAndDoesNotStopOthers`, route the panic through `b.reportError`. Change `handlerSet` to hold an `onPanic func(error)` set by `New`:

```go
// in handlerSet
onPanic func(error)
// in recover():
if r := recover(); r != nil {
    err := fmt.Errorf("zalobot: handler panic: %v\n%s", r, debug.Stack())
    if h.onPanic != nil { h.onPanic(err) } else { slog.Default().Error("zalobot: handler panic", "panic", r) }
}
```

and in `New`: `b.handlers.onPanic = b.reportError`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race . -run 'TestNew|TestOptions|TestRouting|TestUnsupportedEventSkips|TestHandlerPanic|TestProcessUpdate' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w options.go bot.go handlers.go bot_test.go handlers_test.go && go vet .
git add options.go bot.go handlers.go bot_test.go handlers_test.go
git commit -m "feat: bot construction, handler registry, routing and process-update"
```

---

### Task 7: Send methods

**Files:**
- Create: `send.go`
- Test: `send_test.go`

**Interfaces:**
- Consumes: `Bot`, `api.Client.Call`, `SentMessage`, validation errors.
- Produces: `(*Bot).call(ctx, method string, params any, out any) error`; `SendMessage`, `SendPhoto`, `SendSticker`, `SendVoice`, `SendChatAction`.

- [ ] **Step 1: Write the failing test**

```go
package zalobot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeBot(t *testing.T, handler func(method string, body map[string]any) string) *Bot {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := r.URL.Path[len("/botTOKEN/"):]
		_, _ = w.Write([]byte(handler(method, body)))
	}))
	t.Cleanup(srv.Close)
	b, err := New("TOKEN", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSendMessageEncodesOptions(t *testing.T) {
	var seen map[string]any
	b := fakeBot(t, func(method string, body map[string]any) string {
		if method != "sendMessage" {
			t.Errorf("method = %s", method)
		}
		seen = body
		return `{"ok":true,"result":{"message_id":"m1","date":5}}`
	})
	got, err := b.SendMessage(context.Background(), "chat", "hi", &SendMessageOptions{
		TextStyles: []TextStyle{{Start: 0, Len: 2, Styles: []StyleCode{StyleBold}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "m1" || got.Date != 5 {
		t.Fatalf("got = %+v", got)
	}
	if seen["chat_id"] != "chat" || seen["text"] != "hi" {
		t.Fatalf("body = %v", seen)
	}
	styles, ok := seen["text_styles"].([]any)
	if !ok || len(styles) != 1 {
		t.Fatalf("text_styles = %v", seen["text_styles"])
	}
}

func TestSendMessageRejectsParseModeWithTextStyles(t *testing.T) {
	b := fakeBot(t, func(string, map[string]any) string { return `{"ok":true,"result":{}}` })
	_, err := b.SendMessage(context.Background(), "c", "hi", &SendMessageOptions{
		ParseMode:  ParseModeMarkdown,
		TextStyles: []TextStyle{{Start: 0, Len: 1}},
	})
	if _, ok := err.(*ValidationError); !ok {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
}

func TestSendMessageLengthCheckSkippedWithParseMode(t *testing.T) {
	b := fakeBot(t, func(string, map[string]any) string { return `{"ok":true,"result":{}}` })
	long := make([]byte, 0, 3000)
	for i := 0; i < 3000; i++ {
		long = append(long, 'x')
	}
	if _, err := b.SendMessage(context.Background(), "c", string(long), &SendMessageOptions{ParseMode: ParseModeHTML}); err != nil {
		t.Fatalf("parse_mode should skip length check: %v", err)
	}
	if _, err := b.SendMessage(context.Background(), "c", string(long), nil); err == nil {
		t.Fatal("want length validation error")
	}
}

func TestSendVoiceRequiresAAC(t *testing.T) {
	b := fakeBot(t, func(string, map[string]any) string { return `{"ok":true,"result":{}}` })
	if _, err := b.SendVoice(context.Background(), "c", "https://x/y.mp3"); err == nil {
		t.Fatal("want .aac validation error")
	}
	if _, err := b.SendVoice(context.Background(), "c", "https://x/y.aac"); err != nil {
		t.Fatalf("aac should pass: %v", err)
	}
}

func TestSendChatActionIgnoresMissingResult(t *testing.T) {
	b := fakeBot(t, func(string, map[string]any) string { return `{"ok":true}` })
	if err := b.SendChatAction(context.Background(), "c", ChatActionTyping); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestSend' -v`
Expected: FAIL, undefined `SendMessage`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
package zalobot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

func (b *Bot) call(ctx context.Context, method string, params any, out any) error {
	raw, err := b.api.Call(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &DecodeError{Method: method, Err: err}
	}
	return nil
}

func validateText(field, text string, parseMode ParseMode) error {
	if parseMode != ParseModeNone {
		return nil
	}
	if n := utf8.RuneCountInString(text); n < 1 || n > 2000 {
		return &ValidationError{Field: field, Reason: fmt.Sprintf("must be 1..2000 characters, got %d", n)}
	}
	return nil
}

func (b *Bot) SendMessage(ctx context.Context, chatID, text string, o *SendMessageOptions) (*SentMessage, error) {
	if chatID == "" {
		return nil, &ValidationError{Field: "chatID", Reason: "must not be empty"}
	}
	var parseMode ParseMode
	var styles []TextStyle
	if o != nil {
		parseMode = o.ParseMode
		styles = o.TextStyles
	}
	if parseMode != ParseModeNone && len(styles) > 0 {
		return nil, &ValidationError{Field: "options", Reason: "ParseMode and TextStyles are mutually exclusive"}
	}
	if err := validateText("text", text, parseMode); err != nil {
		return nil, err
	}
	params := map[string]any{"chat_id": chatID, "text": text}
	if parseMode != ParseModeNone {
		params["parse_mode"] = string(parseMode)
	}
	if len(styles) > 0 {
		params["text_styles"] = styles
	}
	var out SentMessage
	if err := b.call(ctx, "sendMessage", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bot) SendPhoto(ctx context.Context, chatID, photo string, o *SendPhotoOptions) (*SentMessage, error) {
	if chatID == "" {
		return nil, &ValidationError{Field: "chatID", Reason: "must not be empty"}
	}
	if photo == "" {
		return nil, &ValidationError{Field: "photo", Reason: "must not be empty"}
	}
	params := map[string]any{"chat_id": chatID, "photo": photo}
	if o != nil && o.Caption != "" {
		if err := validateText("caption", o.Caption, ParseModeNone); err != nil {
			return nil, err
		}
		params["caption"] = o.Caption
	}
	var out SentMessage
	if err := b.call(ctx, "sendPhoto", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bot) SendSticker(ctx context.Context, chatID, sticker string) (*SentMessage, error) {
	if chatID == "" {
		return nil, &ValidationError{Field: "chatID", Reason: "must not be empty"}
	}
	if sticker == "" {
		return nil, &ValidationError{Field: "sticker", Reason: "must not be empty"}
	}
	var out SentMessage
	if err := b.call(ctx, "sendSticker", map[string]any{"chat_id": chatID, "sticker": sticker}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bot) SendVoice(ctx context.Context, chatID, voiceURL string) (*SentMessage, error) {
	if chatID == "" {
		return nil, &ValidationError{Field: "chatID", Reason: "must not be empty"}
	}
	u, err := url.Parse(voiceURL)
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Path), ".aac") {
		return nil, &ValidationError{Field: "voiceURL", Reason: "must be a URL with a .aac extension"}
	}
	var out SentMessage
	if err := b.call(ctx, "sendVoice", map[string]any{"chat_id": chatID, "voice_url": voiceURL}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bot) SendChatAction(ctx context.Context, chatID string, action ChatAction) error {
	if chatID == "" {
		return &ValidationError{Field: "chatID", Reason: "must not be empty"}
	}
	return b.call(ctx, "sendChatAction", map[string]any{"chat_id": chatID, "action": string(action)}, nil)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race . -run 'TestSend' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w send.go send_test.go && go vet .
git add send.go send_test.go
git commit -m "feat: send methods with client-side validation"
```

---

### Task 8: Webhook handler and webhook client methods

**Files:**
- Create: `webhook.go`
- Test: `webhook_test.go`

**Interfaces:**
- Consumes: `Bot.ProcessUpdate`, `Bot.call`, `ErrQueueFull`, `ErrStopped`, `ValidationError`.
- Produces: `WebhookInfo{URL string; UpdatedAt int64; HasCustomCertificate bool; Verification *WebhookTestResult}`; `WebhookTestResult{OK bool; URL, Outcome, Hint string}`; `VerifyWebhookSecret(got, want string) bool`; `(*Bot).WebhookHandler(secret string) (http.Handler, error)`; `SetWebhook`, `DeleteWebhook`, `GetWebhookInfo`, `TestWebhook`.

- [ ] **Step 1: Write the failing test**

```go
package zalobot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVerifyWebhookSecret(t *testing.T) {
	if !VerifyWebhookSecret("abc", "abc") {
		t.Fatal("equal secrets should match")
	}
	if VerifyWebhookSecret("abc", "abd") {
		t.Fatal("different secrets should not match")
	}
	if VerifyWebhookSecret("", "") {
		t.Fatal("empty want must be false")
	}
}

func TestWebhookHandlerValidatesSecretLength(t *testing.T) {
	b, _ := New("TOKEN")
	if _, err := b.WebhookHandler("short"); err == nil {
		t.Fatal("want validation error")
	}
}

func TestWebhookHandlerOrderAndAsyncDispatch(t *testing.T) {
	b, _ := New("TOKEN")
	h, err := b.WebhookHandler("secret-123")
	if err != nil {
		t.Fatal(err)
	}
	handled := make(chan struct{}, 1)
	b.OnMessage(func(context.Context, *Message) { handled <- struct{}{} })

	// 405
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("405: code=%d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}

	// 403
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set("X-Bot-Api-Secret-Token", "wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("403: code=%d", rec.Code)
	}

	// 200 + async dispatch
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ok":true,"result":{"event_name":"message.text.received","message":{"text":"x"}}}`))
	req.Header.Set("X-Bot-Api-Secret-Token", "secret-123")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("200: code=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not run")
	}
	b.disp.stop()
	b.disp.wait()
}

func TestWebhookHandlerBadJSONIs400(t *testing.T) {
	b, _ := New("TOKEN")
	h, _ := b.WebhookHandler("secret-123")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{`))
	req.Header.Set("X-Bot-Api-Secret-Token", "secret-123")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestWebhookHandlerFullQueueIs503(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(1), WithPerChatBuffer(1), WithMaxBuffered(2))
	h, _ := b.WebhookHandler("secret-123")
	release := make(chan struct{})
	b.OnMessage(func(context.Context, *Message) { <-release })
	payload := `{"ok":true,"result":{"event_name":"message.text.received","message":{"chat":{"id":"c"},"text":"x"}}}`
	code := 0
	for i := 0; i < 20; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
		req.Header.Set("X-Bot-Api-Secret-Token", "secret-123")
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusServiceUnavailable {
			code = rec.Code
			break
		}
	}
	if code != http.StatusServiceUnavailable {
		t.Fatalf("never returned 503, last code = %d", code)
	}
	close(release)
	b.disp.stop()
	b.disp.wait()
}

func TestSetWebhookValidatesInputs(t *testing.T) {
	b, _ := New("TOKEN")
	if _, err := b.SetWebhook(context.Background(), "http://insecure", "secret-123"); err == nil {
		t.Fatal("want https validation error")
	}
	if _, err := b.SetWebhook(context.Background(), "https://x", "short"); err == nil {
		t.Fatal("want secret length validation error")
	}
}

func TestSetWebhookReturnsVerificationWithoutError(t *testing.T) {
	b := fakeBot(t, func(method string, body map[string]any) string {
		if method != "setWebhook" {
			t.Errorf("method = %s", method)
		}
		return `{"ok":true,"result":{"url":"https://x","updated_at":1,"verification":{"ok":false,"url":"https://x","outcome":"webhook.http.403","hint":"blocked"}}}`
	})
	info, err := b.SetWebhook(context.Background(), "https://x", "secret-123")
	if err != nil {
		t.Fatalf("failed verification must not be an error: %v", err)
	}
	if info.Verification == nil || info.Verification.Outcome != "webhook.http.403" {
		t.Fatalf("info = %+v", info)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestVerifyWebhook|TestWebhookHandler|TestSetWebhook' -v`
Expected: FAIL, undefined `WebhookHandler`, `WebhookInfo`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
package zalobot

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxWebhookBody = 1 << 20

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

func VerifyWebhookSecret(got, want string) bool {
	if want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (b *Bot) WebhookHandler(secret string) (http.Handler, error) {
	if n := len(secret); n < 8 || n > 256 {
		return nil, &ValidationError{Field: "secret", Reason: "must be 8..256 characters"}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !VerifyWebhookSecret(r.Header.Get("X-Bot-Api-Secret-Token"), secret) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := b.ProcessUpdate(raw); err != nil {
			if errors.Is(err, ErrQueueFull) || errors.Is(err, ErrStopped) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}), nil
}

func (b *Bot) SetWebhook(ctx context.Context, rawURL, secret string) (*WebhookInfo, error) {
	if !strings.HasPrefix(rawURL, "https://") {
		return nil, &ValidationError{Field: "url", Reason: "must be an https URL"}
	}
	if n := len(secret); n < 8 || n > 256 {
		return nil, &ValidationError{Field: "secret", Reason: "must be 8..256 characters"}
	}
	var info WebhookInfo
	if err := b.call(ctx, "setWebhook", map[string]any{"url": rawURL, "secret_token": secret}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (b *Bot) DeleteWebhook(ctx context.Context) (*WebhookInfo, error) {
	var info WebhookInfo
	if err := b.call(ctx, "deleteWebhook", map[string]any{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (b *Bot) GetWebhookInfo(ctx context.Context) (*WebhookInfo, error) {
	var info WebhookInfo
	if err := b.call(ctx, "getWebhookInfo", map[string]any{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (b *Bot) TestWebhook(ctx context.Context) (*WebhookTestResult, error) {
	var res WebhookTestResult
	if err := b.call(ctx, "testWebhook", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race . -run 'TestVerifyWebhook|TestWebhookHandler|TestSetWebhook' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w webhook.go webhook_test.go && go vet .
git add webhook.go webhook_test.go
git commit -m "feat: webhook handler with secret verification and webhook client methods"
```

---

### Task 9: Lifecycle shutdown

**Files:**
- Modify: `bot.go` (add `Shutdown`, `Stop`, `closeSignals`, `finishShutdown`)
- Test: `lifecycle_test.go`
- Modify: `go.mod` / `go.sum` (add test-only `go.uber.org/goleak`)

**Interfaces:**
- Consumes: `dispatcher.stop`, `dispatcher.wait`, `handlerCtxKey`.
- Produces: `(*Bot).Shutdown(ctx context.Context) error`; `(*Bot).Stop() error`.

- [ ] **Step 1: Write the failing test**

```go
package zalobot

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestShutdownFromHandlerReturnsPromptly(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(1))
	done := make(chan struct{})
	b.OnMessage(func(ctx context.Context, _ *Message) {
		_ = b.Shutdown(ctx) // must not deadlock
		close(done)
	})
	if err := b.ProcessUpdate([]byte(`{"event_name":"message.text.received","message":{"text":"x"}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown from handler deadlocked")
	}
	select {
	case <-b.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() never closed")
	}
	if b.Err() != nil {
		t.Fatalf("Err() = %v, want nil after graceful stop", b.Err())
	}
}

func TestDoneWaitsForHandlerThatIgnoresCancellation(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(1), WithDrainTimeout(20*time.Millisecond))
	var release atomic.Bool
	entered := make(chan struct{})
	b.OnMessage(func(context.Context, *Message) {
		close(entered)
		for !release.Load() {
			time.Sleep(5 * time.Millisecond)
		}
	})
	if err := b.ProcessUpdate([]byte(`{"event_name":"message.text.received","message":{"text":"x"}}`)); err != nil {
		t.Fatal(err)
	}
	<-entered
	err := b.Stop()
	if err == nil {
		t.Fatal("Stop should report the drain deadline")
	}
	select {
	case <-b.Done():
		t.Fatal("Done() closed while a handler was still running")
	case <-time.After(50 * time.Millisecond):
	}
	release.Store(true)
	select {
	case <-b.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() never closed after handler exited")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race . -run 'TestShutdownFromHandler|TestDoneWaits' -v`
Expected: FAIL, undefined `Shutdown`/`Stop` (or compile error on goleak).

- [ ] **Step 3: Write minimal implementation**

Add `go.uber.org/goleak`:

```bash
go get go.uber.org/goleak@latest
```

Add to `bot.go`:

```go
func (b *Bot) Shutdown(ctx context.Context) error {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return nil
	}
	b.stopped = true
	cancel := b.pollCancel
	b.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	b.disp.stop()

	if ctx.Value(handlerCtxKey{}) != nil {
		// Called from inside a handler: initiate and return immediately.
		go func() {
			dctx, dcancel := context.WithTimeout(context.Background(), b.cfg.drainTimeout)
			defer dcancel()
			_ = b.finishShutdown(dctx)
		}()
		return nil
	}
	return b.finishShutdown(ctx)
}

func (b *Bot) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), b.cfg.drainTimeout)
	defer cancel()
	return b.Shutdown(ctx)
}

func (b *Bot) finishShutdown(ctx context.Context) error {
	waited := make(chan struct{})
	go func() {
		b.wg.Wait()
		b.disp.wait()
		close(waited)
	}()
	select {
	case <-waited:
		b.closeSignals()
		return nil
	case <-ctx.Done():
		b.handlerCancel()
		go func() {
			<-waited
			b.closeSignals()
		}()
		return ctx.Err()
	}
}

func (b *Bot) closeSignals() {
	b.closeOnce.Do(func() {
		close(b.updatesCh)
		close(b.done)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race . -run 'TestShutdownFromHandler|TestDoneWaits' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w bot.go lifecycle_test.go && go mod tidy && go vet .
git add bot.go lifecycle_test.go go.mod go.sum
git commit -m "feat: lifecycle shutdown with drain deadline and prompt handler shutdown"
```

---

### Task 10: Polling

**Files:**
- Create: `polling.go`
- Test: `polling_test.go`

**Interfaces:**
- Consumes: `Bot`, `api.Client.Call`, `decodeUpdates`, predicates, `WebhookActiveError`.
- Produces: `(*Bot).GetMe(ctx) (*BotInfo, error)`; `(*Bot).GetUpdates(ctx, *GetUpdatesOptions) ([]Update, error)`; `(*Bot).Start(ctx) error`; internal `pollLoop`.

- [ ] **Step 1: Write the failing test**

```go
package zalobot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartRejectsBadClientTimeout(t *testing.T) {
	b, _ := New("TOKEN", WithPolling(PollingOptions{Timeout: 30 * time.Second}),
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}))
	if err := b.Start(context.Background()); err == nil {
		t.Fatal("want validation error for client timeout")
	}
}

func TestStartErrorsWhenWebhookActive(t *testing.T) {
	b := fakeBot(t, func(method string, _ map[string]any) string {
		return `{"ok":true,"result":{"url":"https://hook","updated_at":1}}`
	})
	err := b.Start(context.Background())
	var wae *WebhookActiveError
	if !errors.As(err, &wae) {
		t.Fatalf("err = %v, want *WebhookActiveError", err)
	}
}

func TestPollingTreats408AsEmptyPoll(t *testing.T) {
	var calls atomic.Int32
	b := fakeBot(t, func(method string, _ map[string]any) string {
		if method == "getWebhookInfo" {
			return `{"ok":true,"result":{"url":""}}`
		}
		calls.Add(1)
		return `{"ok":false,"error_code":408,"description":"timeout"}`
	})
	var onErrorCalls atomic.Int32
	b.OnError(func(error) { onErrorCalls.Add(1) })
	ctx, cancel := context.WithCancel(context.Background())
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("poll loop did not continue after 408")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-b.Done()
	if onErrorCalls.Load() != 0 {
		t.Fatalf("OnError called %d times for 408", onErrorCalls.Load())
	}
}

func TestPolling401IsFatalAndStops(t *testing.T) {
	b := fakeBot(t, func(method string, _ map[string]any) string {
		if method == "getWebhookInfo" {
			return `{"ok":true,"result":{"url":""}}`
		}
		return `{"ok":false,"error_code":401,"description":"Unauthorized"}`
	})
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("Done() not closed after fatal 401")
	}
	if !IsUnauthorized(b.Err()) {
		t.Fatalf("Err() = %v, want 401", b.Err())
	}
}

func TestGetUpdatesTolerantShapes(t *testing.T) {
	for _, payload := range []string{
		`{"ok":true,"result":{"event_name":"message.text.received"}}`,
		`{"ok":true,"result":[{"event_name":"message.text.received"}]}`,
		`{"ok":true,"result":null}`,
		`{"ok":true}`,
	} {
		b := fakeBot(t, func(string, map[string]any) string { return payload })
		ups, err := b.GetUpdates(context.Background(), &GetUpdatesOptions{Timeout: time.Second})
		if err != nil {
			t.Fatalf("%s: %v", payload, err)
		}
		if len(ups) > 1 {
			t.Fatalf("%s: len = %d", payload, len(ups))
		}
	}
}

func TestGetUpdatesSendsTimeoutAsString(t *testing.T) {
	var seen map[string]any
	b := fakeBot(t, func(_ string, body map[string]any) string { seen = body; return `{"ok":true}` })
	_, _ = b.GetUpdates(context.Background(), &GetUpdatesOptions{Timeout: 30 * time.Second})
	if v, ok := seen["timeout"].(string); !ok || v != "30" {
		t.Fatalf("timeout = %#v, want string \"30\"", seen["timeout"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race . -run 'TestStart|TestPolling|TestGetUpdates' -v`
Expected: FAIL, undefined `Start`, `GetUpdates`.

- [ ] **Step 3: Write minimal implementation**

```go
package zalobot

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"
)

func (b *Bot) GetMe(ctx context.Context) (*BotInfo, error) {
	var info BotInfo
	if err := b.call(ctx, "getMe", map[string]any{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (b *Bot) GetUpdates(ctx context.Context, o *GetUpdatesOptions) ([]Update, error) {
	params := map[string]any{}
	if o != nil && o.Timeout > 0 {
		params["timeout"] = strconv.Itoa(int(o.Timeout.Seconds()))
	}
	raw, err := b.api.Call(ctx, "getUpdates", params)
	if err != nil {
		return nil, err
	}
	return decodeUpdates(raw)
}

func (b *Bot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return ErrStopped
	}
	b.mu.Unlock()
	if b.polling.Load() {
		return ErrAlreadyPolling
	}
	if b.cfg.httpClient != nil && b.cfg.httpClient.Timeout > 0 && b.cfg.httpClient.Timeout <= b.cfg.pollTimeout+5*time.Second {
		return &ValidationError{Field: "httpClient.Timeout", Reason: "must exceed the polling timeout + 5s"}
	}
	if b.cfg.autoDeleteWebhook {
		if _, err := b.DeleteWebhook(ctx); err != nil {
			return err
		}
	} else {
		info, err := b.GetWebhookInfo(ctx)
		if err != nil {
			return err
		}
		if info.URL != "" {
			return &WebhookActiveError{URL: info.URL}
		}
	}

	pollCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.pollCancel = cancel
	b.mu.Unlock()

	b.polling.Store(true)
	b.wg.Add(1)
	go b.pollLoop(pollCtx)

	go func() {
		<-ctx.Done()
		_ = b.Stop()
	}()
	return nil
}

func (b *Bot) pollLoop(ctx context.Context) {
	defer b.wg.Done()
	defer b.polling.Store(false)
	backoff := time.Second
	empty := 0
	for {
		if b.isStopped() {
			return
		}
		pollCtx, cancel := context.WithTimeout(ctx, b.cfg.pollTimeout+5*time.Second)
		started := time.Now()
		updates, err := b.GetUpdates(pollCtx, &GetUpdatesOptions{Timeout: b.cfg.pollTimeout})
		cancel()
		if err != nil {
			if ctx.Err() != nil || b.isStopped() {
				return
			}
			if IsPollingTimeout(err) {
				empty++
				if b.recheckWebhook(empty) {
					return
				}
				sleepRemainder(started)
				continue
			}
			if IsUnauthorized(err) {
				b.setErr(err)
				b.reportError(err)
				go func() { _ = b.Stop() }()
				return
			}
			b.reportError(err)
			if IsRateLimited(err) {
				backoff = 30 * time.Second
			} else if backoff < time.Second {
				backoff = time.Second
			}
			delay := jitter(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-b.done:
				return
			case <-time.After(delay):
			}
			continue
		}
		backoff = time.Second
		if len(updates) == 0 {
			empty++
			if b.recheckWebhook(empty) {
				return
			}
			sleepRemainder(started)
			continue
		}
		empty = 0
		for _, u := range updates {
			if err := b.disp.admitBlocking(ctx, u); err != nil {
				return
			}
		}
	}
}

func (b *Bot) recheckWebhook(empty int) bool {
	if empty < 30 {
		return false
	}
	info, err := b.GetWebhookInfo(context.Background())
	if err != nil {
		return false
	}
	if info.URL != "" {
		werr := &WebhookActiveError{URL: info.URL}
		b.setErr(werr)
		b.reportError(werr)
		go func() { _ = b.Stop() }()
		return true
	}
	return false
}

func (b *Bot) isStopped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopped
}

func sleepRemainder(started time.Time) {
	if d := time.Second - time.Since(started); d > 0 {
		time.Sleep(d)
	}
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race . -run 'TestStart|TestPolling|TestGetUpdates' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w polling.go polling_test.go && go vet .
git add polling.go polling_test.go
git commit -m "feat: resilient long-polling loop with 408/401 handling and backoff"
```

---

### Task 11: Integration suite, examples, README

**Files:**
- Create: `integration_test.go`
- Create: `example_test.go`
- Create: `README.md`

**Interfaces:**
- Consumes: the full public API.
- Produces: runnable documentation and an opt-in live suite.

- [ ] **Step 1: Write the integration test (failing because it needs a token)**

```go
//go:build integration

package zalobot

import (
	"context"
	"os"
	"testing"
	"time"
)

func liveBot(t *testing.T) *Bot {
	t.Helper()
	token := os.Getenv("ZALO_BOT_TOKEN")
	if token == "" {
		t.Skip("ZALO_BOT_TOKEN not set")
	}
	b, err := New(token)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLiveGetMe(t *testing.T) {
	b := liveBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := b.GetMe(ctx)
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if info.ID == "" {
		t.Fatalf("empty bot info: %+v", info)
	}
}

func TestLiveGetUpdatesShape(t *testing.T) {
	b := liveBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	ups, err := b.GetUpdates(ctx, &GetUpdatesOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	t.Logf("getUpdates returned %d update(s)", len(ups))
}
```

- [ ] **Step 2: Run to verify it is skipped without a token**

Run: `go test -tags integration ./... -run TestLive -v`
Expected: SKIP ("ZALO_BOT_TOKEN not set").

- [ ] **Step 3: Write examples and README**

`example_test.go`:

```go
package zalobot_test

import (
	"context"
	"fmt"
	"log"
	"regexp"

	zalobot "github.com/lucanhost/go-zalo-bot-api"
)

func ExampleBot_OnText() {
	bot, err := zalobot.New("YOUR_BOT_TOKEN")
	if err != nil {
		log.Fatal(err)
	}
	bot.OnText(regexp.MustCompile(`^/echo (.+)$`), func(ctx context.Context, m *zalobot.Message, match []string) {
		_, _ = bot.SendMessage(ctx, m.Chat.ID, match[1], nil)
	})
	fmt.Println("handler registered")
	// Output: handler registered
}

func ExampleStyleRange() {
	ts, err := zalobot.StyleRange("Xin chào bạn", "chào", zalobot.StyleBold, zalobot.ColorRed)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("start=%d len=%d\n", ts.Start, ts.Len)
	// Output: start=4 len=4
}
```

`README.md`:

````markdown
# go-zalo-bot-api

Idiomatic Go SDK for the [Zalo Bot Platform](https://docs.zaloplatforms.com/docs/BOT).

## Install

```bash
go get github.com/lucanhost/go-zalo-bot-api
```

## Polling

```go
package main

import (
	"context"
	"log"

	zalobot "github.com/lucanhost/go-zalo-bot-api"
)

func main() {
	bot, err := zalobot.New("YOUR_BOT_TOKEN", zalobot.WithPolling(zalobot.PollingOptions{Timeout: 30 * time.Second}))
	if err != nil {
		log.Fatal(err)
	}
	bot.OnMessage(func(ctx context.Context, m *zalobot.Message) {
		_, _ = bot.SendMessage(ctx, m.Chat.ID, "Xin chào!", nil)
	})
	if err := bot.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	<-bot.Done()
}
```

## Webhook

```go
handler, err := bot.WebhookHandler("YOUR_SECRET_TOKEN")
if err != nil {
	log.Fatal(err)
}
http.Handle("/webhook", handler)
log.Fatal(http.ListenAndServe(":8080", nil))
```

## Rich text

```go
style, _ := zalobot.StyleRange("Xin chào bạn", "chào", zalobot.StyleBold, zalobot.ColorRed)
_, _ = bot.SendMessage(ctx, chatID, "Xin chào bạn", &zalobot.SendMessageOptions{
	TextStyles: []zalobot.TextStyle{style},
})
```

See `docs/superpowers/specs/2026-09-24-go-zalo-bot-api-design.md` for the full design.
````

- [ ] **Step 4: Run the full suite**

Run: `gofmt -l . && go vet ./... && go test -race ./...`
Expected: no output from `gofmt -l`; vet clean; all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add integration_test.go example_test.go README.md
git commit -m "docs: examples, README, and opt-in live integration suite"
```

---

## Self-Review

**Spec coverage:**
- §3 layout → Tasks 1–11 create every listed file (`internal/api`, errors, update, richtext, options, bot, handlers, dispatcher, send, webhook, polling).
- §4 public API → Tasks 6–10 (lifecycle in 6/9/10, routing in 6, send in 7, webhook in 8, info in 10).
- §5 data model → Task 3 (`SentMessage`, normalized `User`/`BotInfo`/`Message`, `Update.Raw`).
- §6 transport/errors → Tasks 1–2.
- §7 dispatcher → Task 5 (admission, worker pool, quantum, fan-out) + Task 9 (shutdown/reaper/marker).
- §8 polling → Task 10 (408/401, backoff, tight-loop, recheck).
- §9 webhook → Task 8.
- §10 validation/rich text → Tasks 4 + 7.
- §11 testing → each task, plus Task 11 (goleak is in Task 9, fuzz target below).
- §12 live checklist → Task 11 integration suite.

**Additions beyond the spec (call out in the PR):** `ErrUpdatesDropped` sentinel (used for the one-error-per-overflow-episode contract) and a `polling.go` file (the spec folded polling into `bot.go`).

**Placeholder scan:** no "TBD"/"TODO"/"handle edge cases" remain; every code step shows real code.

**Type consistency:** `SentMessage` is used by all send methods (Task 7) and defined in Task 3; `admitNonBlocking`/`admitBlocking` match between Tasks 5, 6, 8, 10; `WebhookInfo`/`WebhookTestResult` match between Tasks 8 and 10.

**Review Focus coverage:**
1. Empty/idle poll shapes → `TestDecodeUpdatesTolerant` (Task 3) + `TestGetUpdatesTolerantShapes` (Task 10).
2. Non-JSON error bodies → `TestCallHTMLBodyIsDecodeErrorWithStatus` (Task 1) + `TestPredicatesUseCodeOrHTTPStatus` (Task 2).
3. UTF-16 NFD/emoji → `TestStyleRangeVietnameseNFCAndNFD`, `TestStyleRangeEmojiSurrogatePair` (Task 4).
4. Unsupported event with no message → `TestUnsupportedEventHasNoMessage` (Task 3) + `TestUnsupportedEventSkipsMessageAndText` (Task 6).
5. Handler panic / ignores cancellation → `TestHandlerPanicIsReportedAndDoesNotStopOthers` (Task 6) + `TestDoneWaitsForHandlerThatIgnoresCancellation` (Task 9).

**Fuzz target** (add to Task 4 before committing): a property test that `utf16.Encode(text)[ts.Start:ts.Start+ts.Len]` decodes back to `substr`:

```go
func FuzzStyleRange(f *testing.F) {
	f.Add("Xin chào bạn", "chào")
	f.Add("hi 😀 there", "😀")
	f.Fuzz(func(t *testing.T, text, sub string) {
		if sub == "" {
			t.Skip()
		}
		ts, err := StyleRange(text, sub)
		if err != nil {
			t.Skip()
		}
		u := utf16.Encode([]rune(text))
		if got := string(utf16.Decode(u[ts.Start : ts.Start+ts.Len])); got != sub {
			t.Fatalf("roundtrip = %q, want %q", got, sub)
		}
	})
}
```
