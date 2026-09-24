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
