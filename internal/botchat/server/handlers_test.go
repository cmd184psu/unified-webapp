package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/botchat/apikey"
	"cmd184psu/unified-webapp/internal/botchat/auth"
	"cmd184psu/unified-webapp/internal/botchat/chat"
)

type testEnv struct {
	srv    http.Handler
	keySvc *apikey.Service
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	keySvc, err := apikey.NewService(filepath.Join(t.TempDir(), "apikeys.json"))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	s := New(Options{
		Auth:   auth.NoAuth{},
		KeySvc: keySvc,
		Store:  chat.NewStore(),
		Hub:    chat.NewHub(),
	})
	return &testEnv{srv: s.Handler(), keySvc: keySvc}
}

func (e *testEnv) mintToken(t *testing.T, name string) string {
	t.Helper()
	res, err := e.keySvc.Mint(context.Background(), name)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	return res.Token
}

func (e *testEnv) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.srv.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	return v
}

func TestRoomLifecycleAndMessaging(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")

	// register callsign
	if w := e.do(t, http.MethodPost, "/api/v1/callsign", token, map[string]string{"callsign": "nimbus-owl"}); w.Code != http.StatusOK {
		t.Fatalf("register callsign: got %d body=%s", w.Code, w.Body.String())
	}

	// create a room -> caller becomes moderator
	w := e.do(t, http.MethodPost, "/api/v1/rooms", token, map[string]string{"name": "wave-42", "waveId": "42"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create room: got %d body=%s", w.Code, w.Body.String())
	}
	room := decode[chat.Room](t, w)
	if room.ID == "" {
		t.Fatal("expected room id")
	}

	// post a message; it should carry the callsign supplied in "from"
	w = e.do(t, http.MethodPost, "/api/v1/rooms/"+room.ID+"/messages", token, map[string]string{"from": "nimbus-owl", "to": "everyone", "content": "working on internal/server"})
	if w.Code != http.StatusCreated {
		t.Fatalf("post message: got %d body=%s", w.Code, w.Body.String())
	}
	msg := decode[chat.Message](t, w)
	if msg.Callsign != "nimbus-owl" {
		t.Fatalf("message callsign = %q, want nimbus-owl", msg.Callsign)
	}

	// tear down the room (moderator)
	if w := e.do(t, http.MethodDelete, "/api/v1/rooms/"+room.ID, token, nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete room: got %d body=%s", w.Code, w.Body.String())
	}
	// posting to a deleted room now fails
	if w := e.do(t, http.MethodPost, "/api/v1/rooms/"+room.ID+"/messages", token, map[string]string{"from": "nimbus-owl", "to": "everyone", "content": "hi"}); w.Code != http.StatusNotFound {
		t.Fatalf("post to deleted room: got %d, want 404", w.Code)
	}
}

func TestPostRequiresFromAndTo(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")

	// missing "from"
	if w := e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"to": "everyone", "content": "hi"}); w.Code != http.StatusBadRequest {
		t.Fatalf("post without from: got %d, want 400", w.Code)
	}
	// missing "to"
	if w := e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "nimbus-owl", "content": "hi"}); w.Code != http.StatusBadRequest {
		t.Fatalf("post without to: got %d, want 400", w.Code)
	}
	// with both from and to, no callsign registration required
	w := e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "nimbus-owl", "to": "everyone", "content": "hi"})
	if w.Code != http.StatusCreated {
		t.Fatalf("post with from+to: got %d body=%s", w.Code, w.Body.String())
	}
	if msg := decode[chat.Message](t, w); msg.Callsign != "nimbus-owl" || msg.To != "everyone" {
		t.Fatalf("message = %+v, want from nimbus-owl to everyone", msg)
	}
}

func TestDirectedMessageBetweenCallsigns(t *testing.T) {
	e := newTestEnv(t)
	tokenA := e.mintToken(t, "agent-a")
	tokenB := e.mintToken(t, "agent-b")

	if w := e.do(t, http.MethodPost, "/api/v1/callsign", tokenA, map[string]string{"callsign": "nimbus-owl"}); w.Code != http.StatusOK {
		t.Fatalf("register a: %d", w.Code)
	}
	if w := e.do(t, http.MethodPost, "/api/v1/callsign", tokenB, map[string]string{"callsign": "copper-fox"}); w.Code != http.StatusOK {
		t.Fatalf("register b: %d", w.Code)
	}

	// directed to an existing callsign
	w := e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", tokenA, map[string]string{"from": "nimbus-owl", "content": "are you touching auth?", "to": "copper-fox"})
	if w.Code != http.StatusCreated {
		t.Fatalf("directed post: got %d body=%s", w.Code, w.Body.String())
	}
	msg := decode[chat.Message](t, w)
	if msg.To != "copper-fox" || msg.Callsign != "nimbus-owl" {
		t.Fatalf("directed message = %+v", msg)
	}

	// directed to a callsign nobody registered still succeeds (unenforced)
	w = e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", tokenA, map[string]string{"from": "nimbus-owl", "content": "hi", "to": "ghost"})
	if w.Code != http.StatusCreated {
		t.Fatalf("directed to arbitrary callsign: got %d, want 201", w.Code)
	}
}

func TestOnlyModeratorCanDeleteRoom(t *testing.T) {
	e := newTestEnv(t)
	tokenA := e.mintToken(t, "agent-a")
	tokenB := e.mintToken(t, "agent-b")
	e.do(t, http.MethodPost, "/api/v1/callsign", tokenA, map[string]string{"callsign": "nimbus-owl"})
	e.do(t, http.MethodPost, "/api/v1/callsign", tokenB, map[string]string{"callsign": "copper-fox"})

	w := e.do(t, http.MethodPost, "/api/v1/rooms", tokenA, map[string]string{"name": "wave-7"})
	room := decode[chat.Room](t, w)

	// non-moderator cannot delete
	if w := e.do(t, http.MethodDelete, "/api/v1/rooms/"+room.ID, tokenB, nil); w.Code != http.StatusForbidden {
		t.Fatalf("non-moderator delete: got %d, want 403", w.Code)
	}
	// moderator can
	if w := e.do(t, http.MethodDelete, "/api/v1/rooms/"+room.ID, tokenA, nil); w.Code != http.StatusNoContent {
		t.Fatalf("moderator delete: got %d, want 204", w.Code)
	}
}

func TestAgentEndpointsRequireAPIKey(t *testing.T) {
	e := newTestEnv(t)
	if w := e.do(t, http.MethodPost, "/api/v1/callsign", "", map[string]string{"callsign": "x"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing key: got %d, want 401", w.Code)
	}
}

func TestCallsignReregistration(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")

	if w := e.do(t, http.MethodPost, "/api/v1/callsign", token, map[string]string{"callsign": "nimbus-owl"}); w.Code != http.StatusOK {
		t.Fatalf("first register: %d %s", w.Code, w.Body.String())
	}

	if w := e.do(t, http.MethodPost, "/api/v1/callsign", token, map[string]string{"callsign": "silver-hawk"}); w.Code != http.StatusOK {
		t.Fatalf("re-register different callsign: %d %s", w.Code, w.Body.String())
	}
	got := decode[map[string]string](t, e.do(t, http.MethodPost, "/api/v1/callsign", token, map[string]string{"callsign": "silver-hawk"}))
	if got["callsign"] != "silver-hawk" {
		t.Fatalf("callsign = %q, want silver-hawk", got["callsign"])
	}
}

func TestUnregisterCallsign(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")

	e.do(t, http.MethodPost, "/api/v1/callsign", token, map[string]string{"callsign": "nimbus-owl"})

	if w := e.do(t, http.MethodDelete, "/api/v1/callsign", token, nil); w.Code != http.StatusNoContent {
		t.Fatalf("unregister: got %d %s", w.Code, w.Body.String())
	}

	if w := e.do(t, http.MethodDelete, "/api/v1/callsign", token, nil); w.Code != http.StatusNotFound {
		t.Fatalf("double-unregister: got %d, want 404", w.Code)
	}

	if w := e.do(t, http.MethodPost, "/api/v1/callsign", token, map[string]string{"callsign": "fresh-start"}); w.Code != http.StatusOK {
		t.Fatalf("re-register after delete: %d %s", w.Code, w.Body.String())
	}
}

func TestRegisterCallsignReturnsInstanceAndSuggestion(t *testing.T) {
	e := newTestEnv(t)
	tokenA := e.mintToken(t, "agent-a")
	tokenB := e.mintToken(t, "agent-b")

	a := decode[map[string]any](t, e.do(t, http.MethodPost, "/api/v1/callsign", tokenA, map[string]string{"callsign": "scout"}))
	if a["instanceId"] == nil || a["instanceId"].(string) == "" {
		t.Fatalf("expected instanceId in register response: %v", a)
	}
	if _, ok := a["suggestion"]; ok {
		t.Fatalf("first registration should not suggest an alternative: %v", a)
	}

	// A different key claiming the same name still succeeds but gets a suggestion.
	b := decode[map[string]any](t, e.do(t, http.MethodPost, "/api/v1/callsign", tokenB, map[string]string{"callsign": "scout"}))
	if b["callsign"] != "scout" {
		t.Fatalf("registration should never fail on collision: %v", b)
	}
	if b["suggestion"] != "scout-2" {
		t.Fatalf("expected suggestion scout-2, got %v", b["suggestion"])
	}
}

func TestListCallsignsIncludesInstanceAndTask(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")
	e.do(t, http.MethodPost, "/api/v1/callsign", token, map[string]string{"callsign": "worker"})

	got := decode[struct {
		Callsigns []struct {
			Callsign   string `json:"callsign"`
			InstanceID string `json:"instanceId"`
		} `json:"callsigns"`
	}](t, e.do(t, http.MethodGet, "/api/v1/callsigns", "", nil))
	if len(got.Callsigns) != 1 || got.Callsigns[0].Callsign != "worker" || got.Callsigns[0].InstanceID == "" {
		t.Fatalf("callsign list should carry instanceId: %+v", got.Callsigns)
	}
}

func TestReleaseCallsignByInstanceAndName(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")
	reg := decode[map[string]any](t, e.do(t, http.MethodPost, "/api/v1/callsign", token, map[string]string{"callsign": "ghost"}))
	instanceID := reg["instanceId"].(string)

	// missing selector -> 400
	if w := e.do(t, http.MethodPost, "/api/admin/callsigns/release", "", map[string]string{}); w.Code != http.StatusBadRequest {
		t.Fatalf("release without selector: got %d, want 400", w.Code)
	}
	// unknown instance -> 404
	if w := e.do(t, http.MethodPost, "/api/admin/callsigns/release", "", map[string]string{"instanceId": "nope"}); w.Code != http.StatusNotFound {
		t.Fatalf("release unknown instance: got %d, want 404", w.Code)
	}
	// release by instance
	if w := e.do(t, http.MethodPost, "/api/admin/callsigns/release", "", map[string]string{"instanceId": instanceID}); w.Code != http.StatusOK {
		t.Fatalf("release by instance: got %d %s", w.Code, w.Body.String())
	}
	// callsign is gone from discovery
	list := decode[struct {
		Callsigns []map[string]any `json:"callsigns"`
	}](t, e.do(t, http.MethodGet, "/api/v1/callsigns", "", nil))
	if len(list.Callsigns) != 0 {
		t.Fatalf("callsign should be gone after release: %+v", list.Callsigns)
	}
}

func TestGetRoomMessages(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")

	e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "nimbus-owl", "to": "everyone", "content": "hello"})
	e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "nimbus-owl", "to": "everyone", "content": "world"})

	w := e.do(t, http.MethodGet, "/api/v1/rooms/global/messages", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET messages: %d %s", w.Code, w.Body.String())
	}
	var res struct {
		Messages []chat.Message `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(res.Messages))
	}

	w = e.do(t, http.MethodGet, "/api/v1/rooms/global/messages?limit=1", token, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("limit=1: want 1 message, got %d", len(res.Messages))
	}
	if res.Messages[0].Content != "world" {
		t.Fatalf("limit=1 should return last message, got content=%q", res.Messages[0].Content)
	}

	if w := e.do(t, http.MethodGet, "/api/v1/rooms/room_missing/messages", token, nil); w.Code != http.StatusNotFound {
		t.Fatalf("missing room: want 404, got %d", w.Code)
	}
}

func TestGetRoomMessagesAfterCursor(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")

	e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "a", "to": "everyone", "content": "one"})
	e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "a", "to": "everyone", "content": "two"})

	w := e.do(t, http.MethodGet, "/api/v1/rooms/global/messages", token, nil)
	var res struct {
		Messages []chat.Message `json:"messages"`
		Cursor   int64          `json:"cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Cursor == 0 || len(res.Messages) != 2 {
		t.Fatalf("expected 2 messages and non-zero cursor, got %d msgs cursor=%d", len(res.Messages), res.Cursor)
	}
	firstCursor := res.Messages[0].Seq

	e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "a", "to": "everyone", "content": "three"})

	w = e.do(t, http.MethodGet, fmt.Sprintf("/api/v1/rooms/global/messages?after=%d", firstCursor), token, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Messages) != 2 {
		t.Fatalf("after firstCursor should return 2 newer messages, got %d", len(res.Messages))
	}

	if w := e.do(t, http.MethodGet, "/api/v1/rooms/global/messages?after=-1", token, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("negative after: want 400, got %d", w.Code)
	}
}

func TestInboxEndpoint(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")

	e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "sender", "to": "everyone", "content": "broadcast"})
	e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "sender", "to": "pum-7", "content": "direct one"})
	e.do(t, http.MethodPost, "/api/v1/rooms/global/messages", token, map[string]string{"from": "sender", "to": "pum-7", "content": "direct two"})

	w := e.do(t, http.MethodGet, "/api/v1/inbox?callsign=pum-7", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("inbox: got %d body=%s", w.Code, w.Body.String())
	}
	var res struct {
		Messages []chat.Message `json:"messages"`
		Cursor   int64          `json:"cursor"`
		Unread   int            `json:"unread"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Unread != 2 || len(res.Messages) != 2 {
		t.Fatalf("want 2 direct messages, got unread=%d msgs=%d", res.Unread, len(res.Messages))
	}

	// resume from cursor: nothing new
	w = e.do(t, http.MethodGet, fmt.Sprintf("/api/v1/inbox?callsign=pum-7&after=%d", res.Cursor), token, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Unread != 0 {
		t.Fatalf("after cursor should be empty, got %d", res.Unread)
	}
}

func TestInboxRequiresCallsignAndKey(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent-a")
	if w := e.do(t, http.MethodGet, "/api/v1/inbox", token, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing callsign: want 400, got %d", w.Code)
	}
	if w := e.do(t, http.MethodGet, "/api/v1/inbox?callsign=pum-7", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", w.Code)
	}
}

func TestMintChildKey(t *testing.T) {
	e := newTestEnv(t)
	coordToken := e.mintToken(t, "coordinator")

	w := e.do(t, http.MethodPost, "/api/v1/keys/child", coordToken, map[string]string{"name": "child-alpha"})
	if w.Code != http.StatusCreated {
		t.Fatalf("mint child: %d %s", w.Code, w.Body.String())
	}
	var res struct {
		Key   apiKeyDTO `json:"key"`
		Token string    `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Token == "" {
		t.Fatal("expected non-empty child token")
	}
	if res.Key.Name != "coordinator/child-alpha" {
		t.Fatalf("child key name = %q, want coordinator/child-alpha", res.Key.Name)
	}

	if w := e.do(t, http.MethodPost, "/api/v1/callsign", res.Token, map[string]string{"callsign": "silver-hawk"}); w.Code != http.StatusOK {
		t.Fatalf("child registers callsign: %d %s", w.Code, w.Body.String())
	}
}

func TestTaskLifecycleOverHTTP(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "coordinator")

	// create a task
	w := e.do(t, http.MethodPost, "/api/v1/tasks", token, map[string]string{
		"from": "coord-5daac805", "roomId": "global", "title": "wire the widget", "assignee": "pum-2031",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: got %d body=%s", w.Code, w.Body.String())
	}
	task := decode[chat.Task](t, w)
	if task.ID == "" || task.State != chat.TaskAssigned {
		t.Fatalf("unexpected task %+v", task)
	}

	// list tasks by room
	lw := e.do(t, http.MethodGet, "/api/v1/tasks?roomId=global", "", nil)
	if lw.Code != http.StatusOK {
		t.Fatalf("list tasks: got %d", lw.Code)
	}
	var list struct {
		Tasks []chat.Task `json:"tasks"`
	}
	if err := json.Unmarshal(lw.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(list.Tasks))
	}

	// get single task
	if gw := e.do(t, http.MethodGet, "/api/v1/tasks/"+task.ID, "", nil); gw.Code != http.StatusOK {
		t.Fatalf("get task: got %d", gw.Code)
	}

	// transition through the happy path
	for _, action := range []string{"start", "done", "accept"} {
		tw := e.do(t, http.MethodPost, "/api/v1/tasks/"+task.ID+"/transition", token, map[string]string{"from": "pum-2031", "action": action})
		if tw.Code != http.StatusOK {
			t.Fatalf("transition %q: got %d body=%s", action, tw.Code, tw.Body.String())
		}
	}
	final := decode[chat.Task](t, e.do(t, http.MethodGet, "/api/v1/tasks/"+task.ID, "", nil))
	if final.State != chat.TaskAccepted {
		t.Fatalf("final state = %q, want accepted", final.State)
	}
}

func TestCreateTaskRequiresFields(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "coordinator")
	// missing from
	if w := e.do(t, http.MethodPost, "/api/v1/tasks", token, map[string]string{"roomId": "global", "title": "x"}); w.Code != http.StatusBadRequest {
		t.Fatalf("missing from: got %d, want 400", w.Code)
	}
	// missing title
	if w := e.do(t, http.MethodPost, "/api/v1/tasks", token, map[string]string{"from": "coord", "roomId": "global"}); w.Code != http.StatusBadRequest {
		t.Fatalf("missing title: got %d, want 400", w.Code)
	}
	// unknown room
	if w := e.do(t, http.MethodPost, "/api/v1/tasks", token, map[string]string{"from": "coord", "roomId": "room_nope", "title": "x"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown room: got %d, want 404", w.Code)
	}
}

func TestInvalidTransitionReturns409(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "coordinator")
	task := decode[chat.Task](t, e.do(t, http.MethodPost, "/api/v1/tasks", token, map[string]string{"from": "coord", "roomId": "global", "title": "x"}))
	if w := e.do(t, http.MethodPost, "/api/v1/tasks/"+task.ID+"/transition", token, map[string]string{"from": "coord", "action": "accept"}); w.Code != http.StatusConflict {
		t.Fatalf("accept on assigned task: got %d, want 409", w.Code)
	}
	if w := e.do(t, http.MethodPost, "/api/v1/tasks/TASK-999/transition", token, map[string]string{"from": "coord", "action": "start"}); w.Code != http.StatusNotFound {
		t.Fatalf("transition missing task: got %d, want 404", w.Code)
	}
}

func TestCreateTaskRequiresAPIKey(t *testing.T) {
	e := newTestEnv(t)
	if w := e.do(t, http.MethodPost, "/api/v1/tasks", "", map[string]string{"from": "coord", "roomId": "global", "title": "x"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("no key: got %d, want 401", w.Code)
	}
}

func TestDocsServed(t *testing.T) {
	e := newTestEnv(t)
	w := e.do(t, http.MethodGet, "/docs", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /docs: got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("docs content-type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "/api/v1/callsign") {
		t.Fatal("docs should document the callsign endpoint")
	}
}

func TestPatchPlanRevisesConcurrency(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "planner")
	w := e.do(t, http.MethodPost, "/api/v1/plans", token, map[string]any{"from": "coord", "roomId": "global", "title": "p", "maxConcurrentActive": 12})
	if w.Code != http.StatusCreated {
		t.Fatalf("create plan: got %d body=%s", w.Code, w.Body.String())
	}
	plan := decode[chat.Plan](t, w)
	// PATCH the cap up; the response and a re-read must both show the new value.
	w = e.do(t, http.MethodPatch, "/api/v1/plans/"+plan.ID, token, map[string]any{"maxConcurrentActive": 20})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH plan: got %d body=%.120q", w.Code, w.Body.String())
	}
	got := decode[chat.Plan](t, w)
	if got.MaxConcurrentActive != 20 {
		t.Fatalf("PATCH response maxConcurrentActive = %d, want 20", got.MaxConcurrentActive)
	}
	reread := decode[chat.Plan](t, e.do(t, http.MethodGet, "/api/v1/plans/"+plan.ID, "", nil))
	if reread.MaxConcurrentActive != 20 {
		t.Fatalf("re-read maxConcurrentActive = %d, want 20", reread.MaxConcurrentActive)
	}
	if reread.Revision != plan.Revision+1 {
		t.Fatalf("re-read revision = %d, want %d", reread.Revision, plan.Revision+1)
	}
}

func TestPatchPlanRequiresAPIKey(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "planner")
	plan := decode[chat.Plan](t, e.do(t, http.MethodPost, "/api/v1/plans", token, map[string]any{"from": "coord", "roomId": "global", "title": "p"}))
	if w := e.do(t, http.MethodPatch, "/api/v1/plans/"+plan.ID, "", map[string]any{"maxConcurrentActive": 20}); w.Code != http.StatusUnauthorized {
		t.Fatalf("PATCH without key: got %d, want 401", w.Code)
	}
}

func TestUnknownAPIRouteReturnsJSONNotFound(t *testing.T) {
	e := newTestEnv(t)
	token := e.mintToken(t, "agent")
	// Unknown API paths and wrong-method API calls must fail loudly as JSON,
	// never serve the UI shell with a 200 (a silent no-op that looks like success).
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/definitely-not-a-route"},
		{http.MethodPatch, "/api/v1/rooms/global/messages"},
	} {
		w := e.do(t, tc.method, tc.path, token, map[string]string{"x": "y"})
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: got %d, want 404 (body=%.80q)", tc.method, tc.path, w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("%s %s: content-type = %q, want application/json", tc.method, tc.path, ct)
		}
	}
	// Non-API unknown paths still serve the UI shell.
	w := e.do(t, http.MethodGet, "/some-ui-route", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("UI fallback: got %d, want 200", w.Code)
	}
}
