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
		h.call(fn, ctx, u)
	}
	if u.EventName == EventUnsupportedReceived || u.Message == nil {
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

func (b *Bot) OnEvent(name EventName, fn func(context.Context, Update)) {
	b.handlers.OnEvent(name, fn)
}

func (b *Bot) OnMessage(fn func(context.Context, *Message)) { b.handlers.OnMessage(fn) }

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
