package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"free-classrooms-bot-unitn/internal/rooms"
	"free-classrooms-bot-unitn/internal/usage"

	tele "gopkg.in/telebot.v3"
)

func TestIsCommand(t *testing.T) {
	tests := []struct {
		text        string
		command     string
		botUsername string
		want        bool
	}{
		{text: "/start", command: "start", botUsername: "FreeClassroomsBot", want: true},
		{text: "/start payload", command: "start", botUsername: "FreeClassroomsBot", want: true},
		{text: "/start@FreeClassroomsBot", command: "start", botUsername: "FreeClassroomsBot", want: true},
		{text: "/start@FreeClassroomsBot payload", command: "start", botUsername: "FreeClassroomsBot", want: true},
		{text: "/start@OtherBot", command: "start", botUsername: "FreeClassroomsBot", want: false},
		{text: "/aiuto", command: "aiuto", botUsername: "FreeClassroomsBot", want: true},
		{text: "/aiuto@FreeClassroomsBot", command: "aiuto", botUsername: "FreeClassroomsBot", want: true},
	}

	for _, tt := range tests {
		if got := isCommand(tt.text, tt.command, tt.botUsername); got != tt.want {
			t.Fatalf("isCommand(%q, %q, %q) = %v, want %v", tt.text, tt.command, tt.botUsername, got, tt.want)
		}
	}
}

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

func TestOnlySupportedInteractionsAreRecorded(t *testing.T) {
	var replies atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		replies.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()
	tg, err := tele.NewBot(tele.Settings{
		URL: server.URL, Client: server.Client(), Offline: true, Synchronous: true,
		OnError: func(err error, _ tele.Context) { t.Error(err) },
	})
	if err != nil {
		t.Fatal(err)
	}
	tg.Me = &tele.User{ID: 99, Username: "rooms"}
	roomClient, err := rooms.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	app := App{bot: tg, roomClient: roomClient, departments: rooms.Departments()}
	app.departments[0].Rooms = []*rooms.Room{{Name: "A101"}}
	var recorded int
	app.registerHandlers(context.Background(), func(_ context.Context, id int64, chat usage.Chat) error {
		recorded++
		if id != 42 || chat.ID != -9 || chat.Type != "Group" || chat.Title != "Study group" {
			t.Errorf("unexpected usage for user %d: %+v", id, chat)
		}
		return nil
	})

	chat := &tele.Chat{ID: -9, Type: tele.ChatGroup, Title: "Study group"}
	user := &tele.User{ID: 42}
	message := func(text string) tele.Update {
		return tele.Update{Message: &tele.Message{Chat: chat, Sender: user, Text: text}}
	}
	callback := func(data string) tele.Update {
		return tele.Update{Callback: &tele.Callback{Message: message("").Message, Sender: user, Data: data}}
	}
	tg.ProcessUpdate(message("ciao"))
	tg.ProcessUpdate(tele.Update{Message: &tele.Message{Chat: chat, Sender: user, Photo: &tele.Photo{}}})
	tg.ProcessUpdate(tele.Update{Message: &tele.Message{Chat: chat, Sender: user, UsersJoined: []tele.User{*tg.Me}}})
	tg.ProcessUpdate(callback("free;unknown;now"))
	tg.ProcessUpdate(callback("free;povo;invalid"))
	if recorded != 0 {
		t.Fatalf("unrelated updates recorded %d interactions", recorded)
	}

	tg.ProcessUpdate(message("/start@rooms"))
	tg.ProcessUpdate(message("/aiuto"))
	tg.ProcessUpdate(message("/povo"))
	tg.ProcessUpdate(callback("free;povo;future"))
	if recorded != 4 {
		t.Errorf("recorded %d interactions, want 4", recorded)
	}
	if got := replies.Load(); got != 7 { // Help fallback, greeting, three command replies, callback edit and answer.
		t.Errorf("Telegram API calls = %d, want 7", got)
	}
}
