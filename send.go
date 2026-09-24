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
