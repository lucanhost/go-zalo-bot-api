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
		{"Xin chào bạn", "chào"},
		{"Xin cha\u0300o ba\u0323n", "cha\u0300o"},
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
	if ts.Len != 2 {
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
