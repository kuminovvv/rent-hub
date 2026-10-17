// Package telegram provides a small user-account MTProto client for reading
// public Telegram channel history. It never uses the Bot API or t.me HTML.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"spb-rent/backend/internal/domain"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	tdauth "github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// Config contains Telegram application credentials and the private session
// file path. Obtain AppID and AppHash from my.telegram.org and provide them at
// runtime; do not commit them to source control.
type Config struct {
	AppID              int
	AppHash            string
	SessionFile        string
	MediaDir           string
	MediaDownloadLimit int
}

// Prompter supplies login values interactively. Implementations should read
// the 2FA password without echoing it. Values are used only during Login and
// are not written to disk; only gotd's authorization session is persisted.
type Prompter interface {
	Phone(context.Context) (string, error)
	Code(context.Context, *tg.AuthSentCode) (string, error)
	Password(context.Context) (string, error)
}

// Client owns runtime configuration. Use Login once to create an authorized
// session, then Run for channel operations.
type Client struct {
	cfg Config
}

// New validates configuration and secures the session directory and file.
// SessionFile should be under a dedicated private directory (for example,
// ./data/telegram/session.json); existing parent directories must already be
// private (0700 or stricter) and are never chmodded by this package.
func New(cfg Config) (*Client, error) {
	cfg.AppHash = strings.TrimSpace(cfg.AppHash)
	cfg.SessionFile = strings.TrimSpace(cfg.SessionFile)
	if cfg.AppID <= 0 || cfg.AppHash == "" || cfg.SessionFile == "" {
		return nil, errors.New("telegram app id, app hash, and session file are required")
	}
	if cfg.MediaDownloadLimit < 0 {
		return nil, errors.New("telegram media download limit cannot be negative")
	}
	if err := prepareSessionPath(cfg.SessionFile); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg}, nil
}

// Login authenticates the Telegram user with phone, code, and optional 2FA
// callbacks. It reuses an already authorized session when possible.
func (c *Client) Login(ctx context.Context, prompt Prompter) error {
	if prompt == nil {
		return errors.New("telegram login prompt is required")
	}
	gclient := c.newGotdClient()
	flow := tdauth.NewFlow(loginAuthenticator{prompt: prompt}, tdauth.SendCodeOptions{})
	return gclient.Run(ctx, func(ctx context.Context) error {
		return gclient.Auth().IfNecessary(ctx, flow)
	})
}

// LoginQR authenticates with Telegram's QR login flow. The show callback is
// invoked for each token, including replacements after expiration. If the
// account requires 2FA, password is called without echoing its value to logs.
// It reuses an already authorized session when possible.
func (c *Client) LoginQR(
	ctx context.Context,
	show func(context.Context, qrlogin.Token) error,
	password func(context.Context) (string, error),
) error {
	if show == nil {
		return errors.New("telegram QR display callback is required")
	}
	if password == nil {
		return errors.New("telegram 2FA password prompt is required")
	}
	dispatcher := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(&dispatcher)
	gclient := telegram.NewClient(c.cfg.AppID, c.cfg.AppHash, telegram.Options{
		SessionStorage: &privateSessionStorage{path: c.cfg.SessionFile},
		UpdateHandler:  dispatcher,
	})
	return gclient.Run(ctx, func(ctx context.Context) error {
		status, err := gclient.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("check telegram authorization: %w", err)
		}
		if status.Authorized {
			return nil
		}
		if _, err := gclient.QR().Auth(ctx, loggedIn, show); err != nil {
			if !tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
				return err
			}
			secret, err := password(ctx)
			if err != nil {
				return fmt.Errorf("read Telegram 2FA password: %w", err)
			}
			if _, err := gclient.Auth().Password(ctx, secret); err != nil {
				return fmt.Errorf("complete Telegram 2FA login: %w", err)
			}
		}
		return nil
	})
}

// Run opens an authorized user session and calls fn while it is live. It does
// not start login; call Login separately if the session is not yet authorized.
func (c *Client) Run(ctx context.Context, fn func(context.Context, *Session) error) error {
	if fn == nil {
		return errors.New("telegram session callback is required")
	}
	gclient := c.newGotdClient()
	return gclient.Run(ctx, func(ctx context.Context) error {
		status, err := gclient.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("check telegram authorization: %w", err)
		}
		if !status.Authorized {
			return errors.New("telegram session is not authorized; run Login first")
		}
		return fn(ctx, newSessionWithMedia(gclient.API(), c.cfg.MediaDir, c.cfg.MediaDownloadLimit, func(ctx context.Context, photo *tg.Photo, size photoSize, writer io.Writer, cancel context.CancelFunc) error {
			location := photo.AsInput().AsInputPhotoFileLocation(size.Type)
			_, err := gclient.Downloader().Download(gclient.API(), location).
				WithRetryHandler(photoRetryHandler(cancel)).
				Stream(ctx, writer)
			return err
		}))
	})
}

func (c *Client) newGotdClient() *telegram.Client {
	return telegram.NewClient(c.cfg.AppID, c.cfg.AppHash, telegram.Options{
		SessionStorage: &privateSessionStorage{path: c.cfg.SessionFile},
	})
}

// Session is valid only inside the callback passed to Client.Run.
type Session struct {
	api                telegramAPI
	sourceMu           sync.RWMutex
	hashes             map[int64]int64
	resolvedSources    map[string]domain.TelegramSource
	mediaDir           string
	mediaDownloadLimit int
	photoDownloads     int
	mediaLimitLogged   bool
	photoMu            sync.Mutex
	downloadPhoto      func(context.Context, *tg.Photo, photoSize, io.Writer, context.CancelFunc) error
	pacer              requestPacer
	photoPacer         requestPacer
	sleep              func(context.Context, time.Duration) error
	logWait            func(source string, delay time.Duration)
}

const (
	requestInterval   = time.Second
	floodWaitBuffer   = time.Second
	maxFloodWaitRetry = 3
)

func newSession(api telegramAPI) *Session {
	return newSessionWithMedia(api, "", 0, nil)
}

type photoSize struct {
	Type string
	W    int
	H    int
	Size int
}

func newSessionWithMedia(api telegramAPI, mediaDir string, downloadLimit int, download func(context.Context, *tg.Photo, photoSize, io.Writer, context.CancelFunc) error) *Session {
	sleep := waitContext
	session := &Session{
		api:                api,
		mediaDir:           mediaDir,
		mediaDownloadLimit: downloadLimit,
		hashes:             make(map[int64]int64),
		resolvedSources:    make(map[string]domain.TelegramSource),
		pacer: requestPacer{
			interval: requestInterval,
			now:      time.Now,
			sleep:    sleep,
		},
		photoPacer: requestPacer{interval: 250 * time.Millisecond, now: time.Now, sleep: sleep},
		sleep:      sleep,
		logWait: func(source string, delay time.Duration) {
			log.Printf("Telegram FLOOD_WAIT: @%s %s", source, delay)
		},
	}
	if download != nil {
		session.downloadPhoto = download
	}
	return session
}

type requestPacer struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
}

func (p *requestPacer) wait(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if !p.last.IsZero() {
		remaining := p.interval - now.Sub(p.last)
		if remaining > 0 {
			if err := p.sleep(ctx, remaining); err != nil {
				return err
			}
			now = p.now()
		}
	}
	p.last = now
	return nil
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Session) retryFloodWait(ctx context.Context, source string, request func() error) error {
	for retries := 0; ; retries++ {
		err := request()
		if err == nil {
			return nil
		}
		delay, ok := tgerr.AsFloodWait(err)
		if !ok {
			return err
		}
		if retries >= maxFloodWaitRetry {
			return fmt.Errorf("Telegram FLOOD_WAIT continued after %d retries for @%s: %w", retries, source, err)
		}
		delay += floodWaitBuffer
		if s.logWait != nil {
			s.logWait(source, delay)
		}
		if err := s.sleep(ctx, delay); err != nil {
			return fmt.Errorf("wait for Telegram rate limit for @%s: %w", source, err)
		}
	}
}

type telegramAPI interface {
	ContactsResolveUsername(context.Context, *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	MessagesGetHistory(context.Context, *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error)
}

// ResolveChannel resolves a public channel username and caches its access hash
// for subsequent FetchPosts calls in this session.
func (s *Session) ResolveChannel(ctx context.Context, username string) (domain.TelegramSource, error) {
	username = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	if username == "" {
		return domain.TelegramSource{}, errors.New("telegram channel username is required")
	}
	s.sourceMu.RLock()
	if source, ok := s.resolvedSources[username]; ok {
		s.sourceMu.RUnlock()
		return source, nil
	}
	s.sourceMu.RUnlock()
	var resolved *tg.ContactsResolvedPeer
	err := s.retryFloodWait(ctx, username, func() error {
		if err := s.pacer.wait(ctx); err != nil {
			return err
		}
		var err error
		resolved, err = s.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
		return err
	})
	if err != nil {
		return domain.TelegramSource{}, fmt.Errorf("resolve telegram channel @%s: %w", username, err)
	}
	peer, ok := resolved.Peer.(*tg.PeerChannel)
	if !ok {
		return domain.TelegramSource{}, fmt.Errorf("@%s does not resolve to a broadcast channel", username)
	}
	for _, chat := range resolved.Chats {
		channel, ok := chat.(*tg.Channel)
		if !ok || channel.ID != peer.ChannelID {
			continue
		}
		if !channel.Broadcast {
			return domain.TelegramSource{}, fmt.Errorf("@%s is not a broadcast channel", username)
		}
		source := domain.TelegramSource{
			ChannelID: channel.ID,
			Username:  channel.Username,
			Title:     channel.Title,
			Enabled:   true,
		}
		s.sourceMu.Lock()
		s.hashes[channel.ID] = channel.AccessHash
		s.resolvedSources[username] = source
		if normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(channel.Username), "@")); normalized != "" {
			s.resolvedSources[normalized] = source
		}
		s.sourceMu.Unlock()
		return source, nil
	}
	return domain.TelegramSource{}, fmt.Errorf("Telegram returned no channel details for @%s", username)
}

// FetchPosts reads a channel's history newest-to-oldest until it reaches the
// cutoff. Results are returned in chronological order. Photo failures do not
// prevent importing the text post; a later sync can retry missing photos.
func (s *Session) FetchPosts(ctx context.Context, source domain.TelegramSource, cutoff time.Time) ([]domain.TelegramPost, error) {
	s.sourceMu.RLock()
	accessHash, ok := s.hashes[source.ChannelID]
	s.sourceMu.RUnlock()
	if !ok {
		return nil, errors.New("channel must be resolved in this session before fetching posts")
	}
	peer := &tg.InputPeerChannel{ChannelID: source.ChannelID, AccessHash: accessHash}
	const pageSize = 100
	var messages []*tg.Message
	offsetID := 0
	for {
		var page tg.MessagesMessagesClass
		err := s.retryFloodWait(ctx, source.Username, func() error {
			if err := s.pacer.wait(ctx); err != nil {
				return err
			}
			var err error
			page, err = s.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
				Peer: peer, OffsetID: offsetID, Limit: pageSize,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("fetch Telegram history for @%s: %w", source.Username, err)
		}
		items := historyItems(page)
		if len(items) == 0 {
			break
		}
		oldestID := 0
		cutoffReached := false
		for _, item := range items {
			if offsetID != 0 && item.id >= offsetID {
				continue
			}
			if oldestID == 0 || item.id < oldestID {
				oldestID = item.id
			}
			if !item.postedAt.IsZero() && item.postedAt.Before(cutoff) {
				cutoffReached = true
				continue
			}
			if item.message != nil {
				messages = append(messages, item.message)
			}
		}
		if cutoffReached || oldestID == 0 || (offsetID != 0 && oldestID >= offsetID) {
			break
		}
		offsetID = oldestID
	}
	// Telegram returns each page newest-first. Reverse before grouping so album
	// representatives and their media stay in Telegram's chronological order.
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return s.convertMessages(ctx, source, messages), nil
}

const (
	maxPhotoBytes        = 10 << 20
	maxPhotoDimension    = 1280
	maxPhotoFloodRetries = 3
	photoDownloadTimeout = 2 * time.Minute
)

var errMediaDownloadLimitReached = errors.New("Telegram media download limit reached")

type photoMedia struct {
	Kind    string `json:"kind"`
	PhotoID int64  `json:"photo_id"`
	URL     string `json:"url"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Bytes   int64  `json:"bytes"`
}

func (s *Session) convertMessages(ctx context.Context, source domain.TelegramSource, messages []*tg.Message) []domain.TelegramPost {
	grouped := make(map[int64][]*tg.Message)
	for _, message := range messages {
		if message == nil {
			continue
		}
		if groupedID, ok := message.GetGroupedID(); ok {
			grouped[groupedID] = append(grouped[groupedID], message)
		}
	}
	seenGroups := make(map[int64]bool)
	posts := make([]domain.TelegramPost, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		groupedID, groupedOK := message.GetGroupedID()
		if !groupedOK {
			post := convertMessage(source, message)
			s.attachPhoto(ctx, message, &post)
			posts = append(posts, post)
			continue
		}
		if seenGroups[groupedID] {
			continue
		}
		seenGroups[groupedID] = true
		album := grouped[groupedID]
		representative := album[0]
		for _, candidate := range album {
			if strings.TrimSpace(candidate.Message) != "" {
				representative = candidate
				break
			}
		}
		post := convertMessage(source, representative)
		s.attachAlbumPhotos(ctx, album, &post)
		posts = append(posts, post)
	}
	return posts
}

func selectPhotoSize(photo *tg.Photo) (photoSize, bool) {
	if photo == nil {
		return photoSize{}, false
	}
	var best photoSize
	bestArea := 0
	for _, candidate := range photo.Sizes {
		var current photoSize
		switch value := candidate.(type) {
		case *tg.PhotoSize:
			current = photoSize{Type: value.Type, W: value.W, H: value.H, Size: value.Size}
		case *tg.PhotoSizeProgressive:
			current = photoSize{Type: value.Type, W: value.W, H: value.H}
			if len(value.Sizes) > 0 {
				current.Size = value.Sizes[len(value.Sizes)-1]
			}
		default:
			continue
		}
		if current.Type == "" || current.W <= 0 || current.H <= 0 || max(current.W, current.H) > maxPhotoDimension {
			continue
		}
		area := current.W * current.H
		if area > bestArea {
			best, bestArea = current, area
		}
	}
	return best, bestArea > 0
}

func (s *Session) attachPhoto(ctx context.Context, message *tg.Message, post *domain.TelegramPost) {
	media, ok := s.downloadPhotoMedia(ctx, message)
	if !ok || post == nil {
		return
	}
	encoded, err := json.Marshal(media)
	if err != nil {
		log.Printf("Telegram photo %d skipped: encode metadata: %v", media.PhotoID, err)
		return
	}
	post.Media = encoded
}

func (s *Session) attachAlbumPhotos(ctx context.Context, messages []*tg.Message, post *domain.TelegramPost) {
	if post == nil {
		return
	}
	media := make([]photoMedia, 0, len(messages))
	for _, message := range messages {
		if item, ok := s.downloadPhotoMedia(ctx, message); ok {
			media = append(media, item)
		}
	}
	if len(media) == 0 {
		return
	}
	encoded, err := json.Marshal(media)
	if err != nil {
		log.Printf("Telegram album skipped: encode metadata: %v", err)
		return
	}
	post.Media = encoded
}

func (s *Session) downloadPhotoMedia(ctx context.Context, message *tg.Message) (photoMedia, bool) {
	if message == nil || s.mediaDir == "" {
		return photoMedia{}, false
	}
	media, ok := message.Media.(*tg.MessageMediaPhoto)
	if !ok || media == nil || media.TTLSeconds > 0 {
		return photoMedia{}, false
	}
	photo, ok := media.Photo.(*tg.Photo)
	if !ok || photo == nil || photo.ID <= 0 {
		return photoMedia{}, false
	}
	size, ok := selectPhotoSize(photo)
	if !ok {
		return photoMedia{}, false
	}
	if size.Size > maxPhotoBytes {
		log.Printf("Telegram photo %d skipped: declared size exceeds %d bytes", photo.ID, maxPhotoBytes)
		return photoMedia{}, false
	}
	if err := s.ensurePhoto(ctx, photo, size); err != nil {
		if errors.Is(err, errMediaDownloadLimitReached) {
			return photoMedia{}, false
		}
		log.Printf("Telegram photo %d skipped: %v", photo.ID, err)
		return photoMedia{}, false
	}
	path := filepath.Join(s.mediaDir, fmt.Sprintf("%d.jpg", photo.ID))
	info, err := os.Lstat(path)
	if err != nil {
		log.Printf("Telegram photo %d skipped: inspect saved file: %v", photo.ID, err)
		return photoMedia{}, false
	}
	return photoMedia{
		Kind: "photo", PhotoID: photo.ID,
		URL:   fmt.Sprintf("/api/v1/telegram/media/%d", photo.ID),
		Width: size.W, Height: size.H, Bytes: info.Size(),
	}, true
}

func (s *Session) ensurePhoto(parent context.Context, photo *tg.Photo, size photoSize) error {
	if s.mediaDir == "" || s.downloadPhoto == nil {
		return errors.New("photo storage is not configured")
	}
	if err := prepareMediaDir(s.mediaDir); err != nil {
		return err
	}
	path := filepath.Join(s.mediaDir, fmt.Sprintf("%d.jpg", photo.ID))
	if validJPEG(path) {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("secure existing photo file: %w", err)
		}
		return nil
	}
	s.photoMu.Lock()
	if s.mediaDownloadLimit > 0 && s.photoDownloads >= s.mediaDownloadLimit {
		if !s.mediaLimitLogged {
			log.Printf("Telegram media download limit reached (%d)", s.mediaDownloadLimit)
			s.mediaLimitLogged = true
		}
		s.photoMu.Unlock()
		return errMediaDownloadLimitReached
	}
	s.photoDownloads++
	s.photoMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, photoDownloadTimeout)
	defer cancel()
	if err := s.photoPacer.wait(ctx); err != nil {
		return err
	}
	file, err := os.CreateTemp(s.mediaDir, ".telegram-photo-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary photo file: %w", err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("secure temporary photo file: %w", err)
	}
	limited := &maxBytesWriter{writer: file, remaining: maxPhotoBytes}
	err = s.downloadPhoto(ctx, photo, size, limited, cancel)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("download photo: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close temporary photo: %w", closeErr)
	}
	if !validJPEG(tempPath) {
		return errors.New("downloaded photo is not a complete JPEG")
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish photo file: %w", err)
	}
	return nil
}

func prepareMediaDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create Telegram media directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect Telegram media directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Telegram media path must be a directory, not a symlink")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure Telegram media directory: %w", err)
	}
	return nil
}

func validJPEG(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxPhotoBytes {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	_, err = jpeg.Decode(file)
	return err == nil
}

type maxBytesWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *maxBytesWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("photo exceeds 10 MB limit")
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

func photoRetryHandler(cancel context.CancelFunc) downloader.RetryHandler {
	var mu sync.Mutex
	var attempts int
	return func(event downloader.RetryEvent) {
		if _, ok := tgerr.AsFloodWait(event.Err); !ok {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts > maxPhotoFloodRetries {
			// Downloader waits the server-provided delay before reporting the
			// retry event; canceling stops the next attempt.
			cancel()
		}
	}
}

type historyItem struct {
	id       int
	postedAt time.Time
	message  *tg.Message
}

func historyItems(page tg.MessagesMessagesClass) []historyItem {
	var raw []tg.MessageClass
	switch value := page.(type) {
	case *tg.MessagesMessages:
		raw = value.Messages
	case *tg.MessagesMessagesSlice:
		raw = value.Messages
	case *tg.MessagesChannelMessages:
		raw = value.Messages
	}
	result := make([]historyItem, 0, len(raw))
	for _, item := range raw {
		switch message := item.(type) {
		case *tg.Message:
			result = append(result, historyItem{id: message.ID, postedAt: time.Unix(int64(message.Date), 0).UTC(), message: message})
		case *tg.MessageService:
			result = append(result, historyItem{id: message.ID, postedAt: time.Unix(int64(message.Date), 0).UTC()})
		case *tg.MessageEmpty:
			result = append(result, historyItem{id: message.ID})
		}
	}
	return result
}

func convertMessage(source domain.TelegramSource, message *tg.Message) domain.TelegramPost {
	username := source.Username
	if username == "" {
		username = "channel"
	}
	post := domain.TelegramPost{
		ChannelID:      source.ChannelID,
		MessageID:      message.ID,
		SourceUsername: source.Username,
		SourceTitle:    source.Title,
		SourceURL:      fmt.Sprintf("https://t.me/%s/%d", username, message.ID),
		Text:           message.Message,
		PostedAt:       time.Unix(int64(message.Date), 0).UTC(),
	}
	if editDate, ok := message.GetEditDate(); ok && editDate > 0 {
		editedAt := time.Unix(int64(editDate), 0).UTC()
		post.EditedAt = &editedAt
	}
	return post
}

type loginAuthenticator struct{ prompt Prompter }

func (a loginAuthenticator) Phone(ctx context.Context) (string, error) {
	return a.prompt.Phone(ctx)
}
func (a loginAuthenticator) Code(ctx context.Context, sent *tg.AuthSentCode) (string, error) {
	return a.prompt.Code(ctx, sent)
}
func (a loginAuthenticator) Password(ctx context.Context) (string, error) {
	return a.prompt.Password(ctx)
}
func (loginAuthenticator) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return errors.New("Telegram sign-up terms cannot be accepted by this read-only client")
}
func (loginAuthenticator) SignUp(context.Context) (tdauth.UserInfo, error) {
	return tdauth.UserInfo{}, errors.New("creating a Telegram account is not supported")
}

func prepareSessionPath(path string) error {
	parent := filepath.Dir(path)
	_, statErr := os.Stat(parent)
	parentExisted := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect Telegram session directory: %w", statErr)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create Telegram session directory: %w", err)
	}
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("inspect Telegram session directory: %w", err)
	}
	if !parentInfo.IsDir() {
		return errors.New("Telegram session parent must be a directory")
	}
	if parentExisted {
		if parentInfo.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("Telegram session directory %q must not be accessible by group or others", parent)
		}
	} else if err := os.Chmod(parent, 0o700); err != nil {
		return fmt.Errorf("secure newly created Telegram session directory: %w", err)
	}
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("Telegram session path must be a regular file")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("secure Telegram session file: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Telegram session file: %w", err)
	}
	return nil
}

// privateSessionStorage enforces private permissions even when gotd updates an
// existing session file.
type privateSessionStorage struct{ path string }

func (s *privateSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, session.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *privateSessionStorage) StoreSession(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepareSessionPath(s.path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".telegram-session-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	return os.Chmod(s.path, 0o600)
}
