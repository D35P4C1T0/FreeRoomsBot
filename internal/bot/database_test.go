package bot

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	tele "gopkg.in/telebot.v3"
)

func TestChatTypeStringMatchesCSharpEnumNames(t *testing.T) {
	tests := []struct {
		in   tele.ChatType
		want string
	}{
		{in: tele.ChatPrivate, want: "Private"},
		{in: tele.ChatGroup, want: "Group"},
		{in: tele.ChatSuperGroup, want: "Supergroup"},
		{in: tele.ChatChannel, want: "Channel"},
		{in: tele.ChatChannelPrivate, want: "privatechannel"},
	}

	for _, tt := range tests {
		if got := chatTypeString(tt.in); got != tt.want {
			t.Fatalf("chatTypeString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLogEntityBSONFieldNamesMatchCSharp(t *testing.T) {
	doc := LogEntity{
		ChatID:           123,
		At:               time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC),
		RequestType:      RequestCallbackQuery,
		Department:       "povo",
		AvailabilityType: AvailabilityOccupied,
	}

	raw, err := bson.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out bson.M
	if err := bson.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"ChatId", "At", "RequestType", "Department", "AvailabilityType"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("missing BSON key %q in %#v", key, out)
		}
	}
	if _, ok := out["chatId"]; ok {
		t.Fatalf("found lower-case chatId key in %#v", out)
	}
}

func TestEmptyStringToNil(t *testing.T) {
	if got := emptyStringToNil(""); got != nil {
		t.Fatalf("emptyStringToNil(\"\") = %#v, want nil", got)
	}
	if got := emptyStringToNil("kirbychan"); got != "kirbychan" {
		t.Fatalf("emptyStringToNil(non-empty) = %#v, want string", got)
	}
}

func TestDefaultLogRetention(t *testing.T) {
	t.Setenv("Bot__BotToken", "token")
	t.Setenv("APPSETTINGS_PATH", t.TempDir()+"/missing.json")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.LogRetentionDays != 90 {
		t.Fatalf("LogRetentionDays = %d, want 90", cfg.Database.LogRetentionDays)
	}
}
