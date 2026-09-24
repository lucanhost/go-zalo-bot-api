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
