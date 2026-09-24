package zalobot

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
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
	if !utf8.ValidString(text) || !utf8.ValidString(substr) {
		return TextStyle{}, &ValidationError{Field: "text", Reason: "must be valid UTF-8"}
	}
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
