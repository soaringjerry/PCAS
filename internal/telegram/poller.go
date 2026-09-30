package telegram

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type store interface {
	Snapshot(context.Context, memory.Scope) (workspace.State, error)
	DeskTurn(context.Context, memory.Scope, workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error)
	DeskTurns(context.Context, memory.Scope, string) (workspace.DeskTurnsResponse, error)
	Undo(context.Context, memory.Scope, string) (workspace.State, error)
	IngestAttachment(context.Context, memory.Scope, memory.IngestRequest, io.Reader) (memory.IngestResult, error)
}
type transcriber interface {
	Transcribe(context.Context, io.Reader, string) (string, error)
}

type turnRef struct {
	Conversation string `json:"conversation"`
	Turn         string `json:"turn"`
}

// Prompt associations contain only identifiers. Answers/options stay in
// DeskTurns and are re-read when clicked.
type state struct {
	Messages map[string]turnRef
	Pending  *preparedTurn
}

type preparedTurn struct {
	Request workspace.DeskTurnRequest
	Prefix  string
}

type poller struct {
	settings notify.Settings
	store    store
	models   transcriber
	scope    memory.Scope
	api      botAPI
	interval time.Duration
	retry    time.Duration
	logger   *slog.Logger
	state    state
}

// Run is started only by serve. The supervisor cancels in-flight polling and
// handling on credential changes; one session at a time owns getUpdates.
func Run(ctx context.Context, s store, models transcriber, settings notify.Settings, owner memory.ID, logger *slog.Logger) {
	p := &poller{settings: settings, store: s, models: models, scope: memory.Scope{OwnerID: owner, PrincipalID: "telegram", IsOwner: true}, api: newAPI(), interval: 5 * time.Second, retry: 3 * time.Second, logger: logger}
	p.run(ctx)
}

func (p *poller) run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	var token, chatID string
	var cancel context.CancelFunc
	stop := func() {
		if cancel != nil {
			cancel()
			cancel = nil
		}
	}
	defer stop()
	for {
		c, err := p.settings.Read()
		if err != nil || c.TelegramToken != token || c.TelegramChatID != chatID {
			stop()
			token, chatID = "", ""
			if err == nil {
				token, chatID = c.TelegramToken, c.TelegramChatID
				if token != "" && chatID != "" {
					cancel = p.start(ctx, token, chatID)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *poller) start(ctx context.Context, token, chatID string) context.CancelFunc {
	session, cancel := context.WithCancel(ctx)
	// DeskTurn can finish an accepted turn after cancellation. A separate
	// session owns its transient state so new credentials take effect promptly.
	sessionPoller := *p
	sessionPoller.state = state{Messages: make(map[string]turnRef)}
	go func() { defer cancel(); sessionPoller.poll(session, token, chatID) }()
	return cancel
}

func (p *poller) poll(ctx context.Context, token, chatID string) {
	for ctx.Err() == nil {
		if err := p.step(ctx, token, chatID); err != nil {
			if ctx.Err() != nil {
				return
			}
			if p.logger != nil {
				p.logger.Warn("Telegram polling will retry")
			}
			if !pause(ctx, p.retry) {
				return
			}
		}
	}
}
func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (p *poller) step(ctx context.Context, token, chatID string) error {
	c, err := p.settings.Read()
	if err != nil {
		return err
	}
	if c.TelegramToken != token || c.TelegramChatID != chatID || token == "" || chatID == "" {
		return context.Canceled
	}
	var updates []update
	if err = p.api.call(ctx, token, "getUpdates", map[string]any{"offset": c.TelegramOffset, "timeout": 30, "limit": 100, "allowed_updates": []string{"message", "callback_query"}}, &updates); err != nil {
		return err
	}
	sort.SliceStable(updates, func(i, j int) bool { return updates[i].ID < updates[j].ID })
	for _, u := range updates {
		c, err = p.settings.Read()
		if err != nil {
			return err
		}
		if c.TelegramToken != token || c.TelegramChatID != chatID {
			return context.Canceled
		}
		if u.ID < c.TelegramOffset {
			continue
		}
		if p.state.Messages == nil {
			p.state.Messages = make(map[string]turnRef)
		}
		if err = p.handle(ctx, c, &p.state, u); err != nil {
			return err
		}
		c, err = p.settings.Read()
		if err != nil {
			return err
		}
		if err = p.settings.UpdateTelegramProgress(token, chatID, u.ID+1, c.TelegramConversation); err != nil {
			return err
		}
		p.state.Pending = nil
	}
	// Real long polls block; this also bounds a gateway returning empty batches immediately.
	if len(updates) == 0 && !pause(ctx, 100*time.Millisecond) {
		return ctx.Err()
	}
	return nil
}

func authorized(m *message, chatID string) bool {
	return m != nil && m.Chat.Type == "private" && strconv.FormatInt(m.Chat.ID, 10) == chatID
}

func requestID(chatID, event string) string {
	b := sha256.Sum256([]byte("PCAS telegram\x00" + chatID + "\x00" + event))
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (p *poller) say(ctx context.Context, c notify.Credentials, text string) error {
	_, err := p.api.send(ctx, c.TelegramToken, c.TelegramChatID, text, nil)
	return err
}

func (p *poller) conversation(c *notify.Credentials) error {
	if c.TelegramConversation != "" {
		return nil
	}
	c.TelegramConversation = string(memory.NewID())
	return p.settings.UpdateTelegramProgress(c.TelegramToken, c.TelegramChatID, c.TelegramOffset, c.TelegramConversation)
}

func (p *poller) handle(ctx context.Context, c notify.Credentials, st *state, u update) error {
	if u.Callback != nil {
		q := u.Callback
		if !authorized(q.Message, c.TelegramChatID) || strconv.FormatInt(q.From.ID, 10) != c.TelegramChatID {
			return nil
		}
		return p.callback(ctx, c, st, q)
	}
	m := u.Message
	if !authorized(m, c.TelegramChatID) {
		return nil
	}
	if strings.TrimSpace(m.Text) == "/new" {
		c.TelegramConversation = requestID(c.TelegramChatID, fmt.Sprintf("new:%d", m.ID))
		if err := p.settings.UpdateTelegramProgress(c.TelegramToken, c.TelegramChatID, c.TelegramOffset, c.TelegramConversation); err != nil {
			return err
		}
		return p.say(ctx, c, "已开始新对话。")
	}
	if err := p.conversation(&c); err != nil {
		return err
	}
	event := fmt.Sprintf("message:%d", m.ID)
	text, prefix := strings.TrimSpace(m.Text), ""
	var f *file
	audio := false
	switch {
	case m.Voice != nil:
		f, audio = m.Voice, true
	case m.Audio != nil:
		f, audio = m.Audio, true
	case m.Document != nil:
		f = m.Document
	case len(m.Photo) > 0:
		f = &m.Photo[len(m.Photo)-1]
	}
	if audio && st.Pending != nil && st.Pending.Request.RequestID == requestID(c.TelegramChatID, event) {
		return p.turn(ctx, c, st, event, c.TelegramConversation, st.Pending.Request.Text, st.Pending.Prefix)
	}
	if f != nil {
		data, path, err := p.api.download(ctx, c.TelegramToken, *f)
		if errors.Is(err, errFile) {
			return p.say(ctx, c, "附件无法下载，请发送不超过 20 MB 的文件。")
		}
		if err != nil {
			return err
		}
		name := f.Name
		if name == "" {
			name = filepath.Base(path)
		}
		if audio {
			var transcript string
			if p.models == nil {
				err = memory.ErrUnavailable
			} else {
				transcript, err = p.models.Transcribe(ctx, bytes.NewReader(data), name)
			}
			if err == nil {
				text = strings.TrimSpace(transcript)
				prefix = "🎤 听到：" + text
			} else {
				if saveErr := p.ingest(ctx, c, m, *f, data, name); saveErr != nil {
					if errors.Is(saveErr, memory.ErrInvalid) {
						return p.say(ctx, c, "这种音频格式暂时无法存进资料，请换成 MP3、WAV 或 OGG。")
					}
					return saveErr
				}
				if errors.Is(err, memory.ErrUnavailable) {
					return p.say(ctx, c, "还没有配置语音转写，先发文字吧")
				}
				return p.say(ctx, c, "语音转写暂时失败，音频已存进资料，先发文字吧")
			}
		} else {
			if err = p.ingest(ctx, c, m, *f, data, name); err != nil {
				if errors.Is(err, memory.ErrInvalid) {
					return p.say(ctx, c, "这种文件格式暂时不支持，请发送 PDF 或图片。")
				}
				return err
			}
			text = strings.TrimSpace(m.Caption)
			prefix = "收到，已存进资料"
			if text == "" {
				return p.say(ctx, c, prefix)
			}
		}
	}
	if text == "" {
		return nil
	}
	return p.turn(ctx, c, st, event, c.TelegramConversation, text, prefix)
}

func (p *poller) ingest(ctx context.Context, c notify.Credentials, m *message, f file, data []byte, name string) error {
	media := f.MIME
	if media == "" {
		media = http.DetectContentType(data)
	}
	if m.Voice != nil {
		media = "audio/ogg"
	}
	_, err := p.store.IngestAttachment(ctx, p.scope, memory.IngestRequest{Connector: "telegram", ExternalID: c.TelegramChatID + ":" + strconv.FormatInt(m.ID, 10), ExternalVersion: "1", Title: clip(name, 200), MediaType: media}, bytes.NewReader(data))
	return err
}

func (p *poller) turn(ctx context.Context, c notify.Credentials, st *state, event, conversation, text, prefix string) error {
	request := workspace.DeskTurnRequest{RequestID: requestID(c.TelegramChatID, event), ConversationID: &conversation, Text: text}
	if st.Pending != nil && st.Pending.Request.RequestID == request.RequestID {
		request, prefix = st.Pending.Request, st.Pending.Prefix
		text = request.Text
	}
	if utf8.RuneCountInString(text) > 4000 {
		return p.say(ctx, c, "这句话超过 4000 字，请分成几句发给我。")
	}
	st.Pending = &preparedTurn{Request: request, Prefix: prefix}
	out, err := p.store.DeskTurn(ctx, p.scope, request)
	if errors.Is(err, memory.ErrConflict) {
		// A restart may retranscribe an accepted voice differently. B1 rejects
		// the changed body; acknowledge it rather than blocking every later update.
		return p.say(ctx, c, "这条消息已经处理过，请在首页查看回执。")
	}
	if err != nil {
		return err
	}
	if out.Turn.Text == "" {
		prefix = ""
	}
	reply, rows := format(out.Turn, prefix, out.State.Settings.Timezone)
	id, err := p.api.send(ctx, c.TelegramToken, c.TelegramChatID, reply, rows)
	if err != nil {
		return err
	}
	if out.Turn.Ask != nil {
		st.Messages[strconv.FormatInt(id, 10)] = turnRef{Conversation: out.ConversationID, Turn: out.Turn.ID}
		// Bound transient prompt associations to the last 100 messages.
		if len(st.Messages) > 100 {
			var oldest int64 = 1<<63 - 1
			for key := range st.Messages {
				if n, _ := strconv.ParseInt(key, 10, 64); n < oldest {
					oldest = n
				}
			}
			delete(st.Messages, strconv.FormatInt(oldest, 10))
		}
	}
	return nil
}

func (p *poller) callback(ctx context.Context, c notify.Credentials, st *state, q *callback) error {
	if strings.HasPrefix(q.Data, "u:") {
		id := strings.TrimPrefix(q.Data, "u:")
		if !memory.ID(id).Valid() {
			return p.api.answer(ctx, c.TelegramToken, q.ID, "这条回执已失效。")
		}
		_, err := p.store.Undo(ctx, p.scope, id)
		text := "已撤销"
		switch {
		case errors.Is(err, workspace.ErrChangedSince):
			text = "这件事之后又改过，没法直接撤销。"
		case errors.Is(err, workspace.ErrWorkStarted):
			text = "副手已经开始做了，没法撤销。"
		case errors.Is(err, workspace.ErrAlreadyUndone):
			text = "已经撤销过了。"
		case errors.Is(err, memory.ErrNotFound):
			text = "这条回执已失效。"
		case err != nil:
			return err
		}
		return p.api.answer(ctx, c.TelegramToken, q.ID, text)
	}
	if strings.HasPrefix(q.Data, "a:") {
		index, err := strconv.Atoi(strings.TrimPrefix(q.Data, "a:"))
		ref, ok := st.Messages[strconv.FormatInt(q.Message.ID, 10)]
		if err != nil || index < 0 || c.TelegramConversation == "" {
			return p.api.answer(ctx, c.TelegramToken, q.ID, "这个选项已失效，请再说一句。")
		}
		if !ok {
			// Telegram returns the original bot message with the callback. After
			// restart, recover only a unique, still-current prompt matching that
			// message; no extra persisted settings or copied answer text is needed.
			ref, err = p.recoverPrompt(ctx, c, q.Message.Text)
			if err != nil {
				return err
			}
		}
		if ref.Conversation != c.TelegramConversation {
			return p.api.answer(ctx, c.TelegramToken, q.ID, "这个选项已失效，请再说一句。")
		}
		turns, err := p.store.DeskTurns(ctx, p.scope, ref.Conversation)
		if err != nil {
			return err
		}
		for _, turn := range turns.Turns {
			if turn.ID != ref.Turn || turn.Ask == nil || turn.Text == "" || index >= len(turn.Ask.Options) {
				continue
			}
			if err = p.api.answer(ctx, c.TelegramToken, q.ID, "收到"); err != nil {
				return err
			}
			// One selection per prompt is idempotent even on a second button click.
			if err = p.turn(ctx, c, st, "answer:"+ref.Turn, ref.Conversation, turn.Ask.Options[index], ""); err != nil {
				return err
			}
			delete(st.Messages, strconv.FormatInt(q.Message.ID, 10))
			return nil
		}
		return p.api.answer(ctx, c.TelegramToken, q.ID, "这个选项已失效，请再说一句。")
	}
	return p.api.answer(ctx, c.TelegramToken, q.ID, "这个按钮已失效。")
}

func (p *poller) recoverPrompt(ctx context.Context, c notify.Credentials, original string) (turnRef, error) {
	if original == "" {
		return turnRef{}, nil
	}
	turns, err := p.store.DeskTurns(ctx, p.scope, c.TelegramConversation)
	if err != nil {
		return turnRef{}, err
	}
	current, err := p.store.Snapshot(ctx, p.scope)
	if err != nil {
		return turnRef{}, err
	}
	prefix := ""
	first, _, _ := strings.Cut(original, "\n")
	if strings.HasPrefix(first, "🎤 听到：") || first == "收到，已存进资料" {
		prefix = first
	}
	var ref turnRef
	matches := 0
	for _, turn := range turns.Turns {
		if turn.Ask == nil || turn.Text == "" {
			continue
		}
		text, _ := format(turn, prefix, current.Settings.Timezone)
		if text == original {
			ref = turnRef{Conversation: c.TelegramConversation, Turn: turn.ID}
			matches++
		}
	}
	if matches != 1 || len(turns.Turns) == 0 || turns.Turns[len(turns.Turns)-1].ID != ref.Turn {
		return turnRef{}, nil
	}
	return ref, nil
}
