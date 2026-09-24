package zalobot

import (
	"context"
	"fmt"
	"log/slog"
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
		h.call(ctx, fn, u)
	}
	if u.EventName == EventUnsupportedReceived || u.Message == nil {
		return
	}
	for _, fn := range messages {
		h.callMessage(ctx, fn, u.Message)
	}
	if u.Message.Text == "" {
		return
	}
	for _, th := range texts {
		if m := th.re.FindStringSubmatch(u.Message.Text); m != nil {
			h.callText(ctx, th, u.Message, m)
		}
	}
	for _, ch := range commands {
		if name, args, ok := parseCommand(u.Message.Text); ok && name == ch.name {
			h.callCommand(ctx, ch, u.Message, args)
		}
	}
}

func (h *handlerSet) call(ctx context.Context, fn func(context.Context, Update), u Update) {
	defer h.recover()
	fn(ctx, u)
}

func (h *handlerSet) callMessage(ctx context.Context, fn func(context.Context, *Message), m *Message) {
	defer h.recover()
	fn(ctx, m)
}

func (h *handlerSet) callText(ctx context.Context, th textHandler, m *Message, match []string) {
	defer h.recover()
	th.fn(ctx, m, match)
}

func (h *handlerSet) callCommand(ctx context.Context, ch commandHandler, m *Message, args []string) {
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

// OnEvent registers a handler for a specific event name. It fires for every
// matching update, including ones OnMessage and OnText do not see.
func (b *Bot) OnEvent(name EventName, fn func(context.Context, Update)) {
	b.handlers.OnEvent(name, fn)
}

// OnMessage registers a handler that runs for every message event. It is not
// called for events that carry no message.
func (b *Bot) OnMessage(fn func(context.Context, *Message)) { b.handlers.OnMessage(fn) }

// OnText registers a handler that runs when a message text matches re. The
// match slice contains the full match and any capture groups.
func (b *Bot) OnText(re *regexp.Regexp, fn func(context.Context, *Message, []string)) {
	b.handlers.OnText(re, fn)
}

// OnCommand registers a handler for messages of the form "/name args". The
// args slice holds the whitespace-separated arguments after the command.
func (b *Bot) OnCommand(name string, fn func(context.Context, *Message, []string)) {
	b.handlers.OnCommand(name, fn)
}

// OnError registers a handler for asynchronous errors such as polling
// failures, dropped Updates, and handler panics. Handlers may be called
// concurrently. If none is registered, errors are logged with slog.
func (b *Bot) OnError(fn func(error)) { b.handlers.OnError(fn) }

// Updates returns a best-effort channel of updates. Updates admitted before
// the first call are not replayed, and updates may be dropped if the buffer is
// full; prefer handlers when delivery matters.
func (b *Bot) Updates() <-chan Update {
	b.updatesOn.Store(true)
	return b.updatesCh
}

// ProcessUpdate parses a webhook payload (the full envelope, or a bare update
// object) and admits it for asynchronous processing. It returns ErrQueueFull
// or ErrStopped when the update cannot be admitted.
func (b *Bot) ProcessUpdate(raw []byte) error {
	u, err := decodeUpdateEnvelope(raw)
	if err != nil {
		return err
	}
	return b.disp.admitNonBlocking(u)
}
