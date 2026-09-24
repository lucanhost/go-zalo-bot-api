package zalobot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// EventName identifies the kind of an incoming update.
type EventName string

// Event names delivered by the Zalo Bot Platform.
const (
	EventTextReceived        EventName = "message.text.received"
	EventImageReceived       EventName = "message.image.received"
	EventStickerReceived     EventName = "message.sticker.received"
	EventVoiceReceived       EventName = "message.voice.received"
	EventUnsupportedReceived EventName = "message.unsupported.received"
)

// ChatType identifies whether a conversation is private or a group.
type ChatType string

// Chat types reported in Chat.Type.
const (
	ChatPrivate ChatType = "PRIVATE"
	ChatGroup   ChatType = "GROUP"
)

// User describes the sender of a message.
type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
	IsBot       bool   `json:"is_bot"`
}

// UnmarshalJSON decodes a User, normalizing the display name from either
// `display_name` or the compatibility field `name`.
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

// BotInfo describes the bot itself, as returned by GetMe.
type BotInfo struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	AccountName   string `json:"account_name"`
	AccountType   string `json:"account_type"`
	CanJoinGroups bool   `json:"can_join_groups"`
}

// UnmarshalJSON decodes BotInfo, normalizing the account name from either
// `account_name` or the compatibility field `name`.
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

// Chat describes the conversation an update belongs to. Use ID when sending
// a reply.
type Chat struct {
	ID   string   `json:"id"`
	Type ChatType `json:"chat_type"`
}

// Message is the content of a message event. Only the fields relevant to the
// event are populated.
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

// UnmarshalJSON decodes a Message, normalizing the image URL from either
// `photo` or the compatibility field `photo_url`.
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

// Time returns the message timestamp. Date is in milliseconds since the Unix
// epoch.
func (m *Message) Time() time.Time { return time.UnixMilli(m.Date) }

// Update is a single event delivered by polling or a webhook.
type Update struct {
	EventName EventName       `json:"event_name"`
	Message   *Message        `json:"message"`
	Raw       json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes an Update and keeps a copy of the raw object in Raw.
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

// SentMessage is the receipt returned by the send methods.
type SentMessage struct {
	MessageID string `json:"message_id"`
	Date      int64  `json:"date"`
}

// Time returns the send timestamp. Date is in milliseconds since the Unix
// epoch.
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
