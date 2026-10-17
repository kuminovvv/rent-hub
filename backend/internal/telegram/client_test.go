package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spb-rent/backend/internal/domain"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func TestConvertMessagePreservesOriginalWithoutMedia(t *testing.T) {
	postedAt := time.Date(2026, 10, 8, 12, 34, 56, 0, time.UTC)
	source := domain.TelegramSource{ChannelID: 42, Username: "rent_spb", Title: "Аренда СПб"}
	message := &tg.Message{ID: 123, Date: int(postedAt.Unix()), Message: "Сдам квартиру"}

	got := convertMessage(source, message)
	if got.ChannelID != source.ChannelID || got.MessageID != message.ID {
		t.Fatalf("unexpected identifiers: %#v", got)
	}
	if got.SourceUsername != source.Username || got.SourceTitle != source.Title {
		t.Fatalf("source metadata not preserved: %#v", got)
	}
	if got.SourceURL != "https://t.me/example_channel/123" || got.Text != message.Message {
		t.Fatalf("unexpected post content: %#v", got)
	}
	if !got.PostedAt.Equal(postedAt) || got.Deleted || len(got.Media) != 0 {
		t.Fatalf("unexpected post metadata: %#v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), `"media"`) {
		t.Fatalf("media should be omitted from JSON: %s", encoded)
	}
}

func TestSelectPhotoSizeChoosesLargestAtOrBelow1280(t *testing.T) {
	photo := &tg.Photo{Sizes: []tg.PhotoSizeClass{
		&tg.PhotoSizeEmpty{Type: "i"},
		&tg.PhotoSize{Type: "m", W: 320, H: 240, Size: 1000},
		&tg.PhotoSizeProgressive{Type: "y", W: 1280, H: 960, Sizes: []int{100, 5000}},
		&tg.PhotoSize{Type: "w", W: 2560, H: 1920, Size: 10000},
	}}
	size, ok := selectPhotoSize(photo)
	if !ok || size.Type != "y" || size.W != 1280 || size.H != 960 || size.Size != 5000 {
		t.Fatalf("unexpected selected size: %#v, ok=%v", size, ok)
	}
}

func TestEnsurePhotoAtomicallySavesAndSkipsValidFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "media")
	content := testJPEG(t)
	var downloads int
	session := newSessionWithMedia(nil, dir, 0, func(_ context.Context, _ *tg.Photo, _ photoSize, writer io.Writer, _ context.CancelFunc) error {
		downloads++
		_, err := writer.Write(content)
		return err
	})
	session.photoPacer.interval = 0
	photo := &tg.Photo{ID: 91234}
	size := photoSize{Type: "y", W: 3, H: 2}
	if err := session.ensurePhoto(context.Background(), photo, size); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "91234.jpg")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file permissions = %o, want 600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory permissions = %v, err=%v; want 700", dirInfo, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := session.ensurePhoto(context.Background(), photo, size); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("existing file permissions = %v, err=%v; want 600", info, err)
	}
	if downloads != 1 {
		t.Fatalf("downloaded %d times, want existing complete file to be skipped", downloads)
	}
}

func TestMediaDownloadLimitBoundsNewFiles(t *testing.T) {
	var downloads int
	session := newSessionWithMedia(nil, t.TempDir(), 1, func(context.Context, *tg.Photo, photoSize, io.Writer, context.CancelFunc) error {
		downloads++
		return errors.New("expected test stop")
	})
	session.photoPacer.interval = 0
	for _, id := range []int64{101, 102} {
		_ = session.ensurePhoto(context.Background(), &tg.Photo{ID: id}, photoSize{Type: "y", W: 1000, H: 700})
	}
	if downloads != 1 {
		t.Fatalf("download attempts = %d, want limit of 1", downloads)
	}
}

func TestPhotoFailureDoesNotBreakTextImportAndTTLPhotoIsSkipped(t *testing.T) {
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	message := &tg.Message{
		ID: 42, Date: int(base.Add(time.Hour).Unix()), Message: "text remains available",
		Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 55, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "y", W: 1200, H: 800, Size: 100}}}},
	}
	api := &fakeHistoryAPI{pages: []tg.MessagesMessagesClass{
		&tg.MessagesMessages{Messages: []tg.MessageClass{message}},
		&tg.MessagesMessages{},
	}}
	var attempts int
	session := newSessionWithMedia(api, t.TempDir(), 0, func(context.Context, *tg.Photo, photoSize, io.Writer, context.CancelFunc) error {
		attempts++
		return errors.New("test download failure")
	})
	session.hashes[7] = 99
	session.photoPacer.interval = 0
	posts, err := session.FetchPosts(context.Background(), domain.TelegramSource{ChannelID: 7, Username: "rent_spb"}, base)
	if err != nil || len(posts) != 1 || posts[0].Text != message.Message || len(posts[0].Media) != 0 {
		t.Fatalf("photo failure lost text post: posts=%#v err=%v", posts, err)
	}
	if attempts != 1 {
		t.Fatalf("photo attempts = %d, want 1", attempts)
	}

	message.Media = &tg.MessageMediaPhoto{TTLSeconds: 30, Photo: &tg.Photo{ID: 56, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "y", W: 1200, H: 800, Size: 100}}}}
	post := convertMessage(domain.TelegramSource{ChannelID: 7, Username: "rent_spb"}, message)
	session.attachPhoto(context.Background(), message, &post)
	if attempts != 1 {
		t.Fatalf("TTL photo was downloaded; attempts=%d", attempts)
	}
}

func TestAttachPhotoWritesRequiredMediaJSON(t *testing.T) {
	dir := t.TempDir()
	content := testJPEG(t)
	session := newSessionWithMedia(nil, dir, 0, func(_ context.Context, _ *tg.Photo, _ photoSize, writer io.Writer, _ context.CancelFunc) error {
		_, err := writer.Write(content)
		return err
	})
	session.photoPacer.interval = 0
	message := &tg.Message{Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 77, Sizes: []tg.PhotoSizeClass{
		&tg.PhotoSize{Type: "y", W: 1000, H: 700, Size: len(content)},
	}}}}
	post := domain.TelegramPost{}
	session.attachPhoto(context.Background(), message, &post)
	var got photoMedia
	if err := json.Unmarshal(post.Media, &got); err != nil {
		t.Fatalf("invalid Media JSON %q: %v", post.Media, err)
	}
	if got.Kind != "photo" || got.PhotoID != 77 || got.URL != "/api/v1/telegram/media/77" || got.Width != 1000 || got.Height != 700 || got.Bytes != int64(len(content)) {
		t.Fatalf("unexpected media payload: %#v", got)
	}
}

func TestFetchPostsCollapsesGroupedAlbumIntoOneListingWithAllPhotos(t *testing.T) {
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	content := testJPEG(t)
	photoMessage := func(id int, photoID int64, text string) *tg.Message {
		message := &tg.Message{
			ID: id, Date: int(base.Add(time.Duration(id-100) * time.Minute).Unix()),
			Message: text,
			Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: photoID, Sizes: []tg.PhotoSizeClass{
				&tg.PhotoSize{Type: "y", W: 1000, H: 700, Size: len(content)},
			}}},
		}
		message.SetGroupedID(987654321)
		return message
	}
	api := &fakeHistoryAPI{pages: []tg.MessagesMessagesClass{
		&tg.MessagesMessages{Messages: []tg.MessageClass{
			photoMessage(103, 803, ""),
			photoMessage(102, 802, ""),
			photoMessage(101, 801, "Сдам квартиру с тремя фото"),
		}},
		&tg.MessagesMessages{},
	}}
	session := newSessionWithMedia(api, t.TempDir(), 0, func(_ context.Context, _ *tg.Photo, _ photoSize, writer io.Writer, _ context.CancelFunc) error {
		_, err := writer.Write(content)
		return err
	})
	session.hashes[7] = 99
	session.photoPacer.interval = 0
	useFakeClock(session)

	posts, err := session.FetchPosts(context.Background(), domain.TelegramSource{ChannelID: 7, Username: "rent_spb"}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d posts for one album, want 1", len(posts))
	}
	if posts[0].MessageID != 101 || posts[0].Text != "Сдам квартиру с тремя фото" {
		t.Fatalf("unexpected logical listing representative: %#v", posts[0])
	}
	var media []photoMedia
	if err := json.Unmarshal(posts[0].Media, &media); err != nil {
		t.Fatalf("album media is not an array: %q: %v", posts[0].Media, err)
	}
	if len(media) != 3 || media[0].PhotoID != 801 || media[1].PhotoID != 802 || media[2].PhotoID != 803 {
		t.Fatalf("unexpected album media: %#v", media)
	}
}

func TestFetchPostsGroupsAlbumAcrossHistoryPages(t *testing.T) {
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	content := testJPEG(t)
	photoMessage := func(id int, photoID int64, text string) *tg.Message {
		message := &tg.Message{
			ID: id, Date: int(base.Add(time.Duration(id-100) * time.Minute).Unix()), Message: text,
			Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: photoID, Sizes: []tg.PhotoSizeClass{
				&tg.PhotoSize{Type: "y", W: 1000, H: 700, Size: len(content)},
			}}},
		}
		message.SetGroupedID(123456789)
		return message
	}
	api := &fakeHistoryAPI{pages: []tg.MessagesMessagesClass{
		&tg.MessagesMessages{Messages: []tg.MessageClass{photoMessage(103, 803, ""), photoMessage(102, 802, "")}},
		&tg.MessagesMessages{Messages: []tg.MessageClass{photoMessage(101, 801, "album caption")}},
		&tg.MessagesMessages{},
	}}
	session := newSessionWithMedia(api, t.TempDir(), 0, func(_ context.Context, _ *tg.Photo, _ photoSize, writer io.Writer, _ context.CancelFunc) error {
		_, err := writer.Write(content)
		return err
	})
	session.hashes[7] = 99
	session.photoPacer.interval = 0
	useFakeClock(session)

	posts, err := session.FetchPosts(context.Background(), domain.TelegramSource{ChannelID: 7, Username: "rent_spb"}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0].MessageID != 101 || posts[0].Text != "album caption" {
		t.Fatalf("album split across pages was not collapsed: %#v", posts)
	}
	var media []photoMedia
	if err := json.Unmarshal(posts[0].Media, &media); err != nil {
		t.Fatal(err)
	}
	if len(media) != 3 || media[0].PhotoID != 801 || media[1].PhotoID != 802 || media[2].PhotoID != 803 {
		t.Fatalf("unexpected cross-page album media: %#v", media)
	}
}

func TestConvertMessagesKeepsUngroupedSinglePhotoMediaObject(t *testing.T) {
	dir := t.TempDir()
	content := testJPEG(t)
	session := newSessionWithMedia(nil, dir, 0, func(_ context.Context, _ *tg.Photo, _ photoSize, writer io.Writer, _ context.CancelFunc) error {
		_, err := writer.Write(content)
		return err
	})
	session.photoPacer.interval = 0
	message := &tg.Message{
		ID: 42, Date: int(time.Now().Unix()), Message: "single",
		Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 901, Sizes: []tg.PhotoSizeClass{
			&tg.PhotoSize{Type: "y", W: 1000, H: 700, Size: len(content)},
		}}},
	}
	posts := session.convertMessages(context.Background(), domain.TelegramSource{ChannelID: 7, Username: "rent_spb"}, []*tg.Message{message})
	if len(posts) != 1 {
		t.Fatalf("got %d posts, want 1", len(posts))
	}
	var object photoMedia
	if err := json.Unmarshal(posts[0].Media, &object); err != nil {
		t.Fatalf("single media JSON must remain an object: %q: %v", posts[0].Media, err)
	}
	if object.PhotoID != 901 {
		t.Fatalf("unexpected single media: %#v", object)
	}
}

func TestConvertMessagesSkipsNilMessage(t *testing.T) {
	session := newSession(nil)
	posts := session.convertMessages(context.Background(), domain.TelegramSource{ChannelID: 7}, []*tg.Message{nil})
	if len(posts) != 0 {
		t.Fatalf("got %d posts, want 0", len(posts))
	}
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.White)
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, nil); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type fakeHistoryAPI struct {
	pages        []tg.MessagesMessagesClass
	offsets      []int
	resolveCalls int
}

func (f *fakeHistoryAPI) ContactsResolveUsername(_ context.Context, request *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	f.resolveCalls++
	return &tg.ContactsResolvedPeer{
		Peer: &tg.PeerChannel{ChannelID: 7},
		Chats: []tg.ChatClass{&tg.Channel{
			ID: 7, AccessHash: 99, Title: "Rent Hub", Username: request.Username, Broadcast: true,
		}},
	}, nil
}

func (f *fakeHistoryAPI) MessagesGetHistory(_ context.Context, request *tg.MessagesGetHistoryRequest) (tg.MessagesMessagesClass, error) {
	f.offsets = append(f.offsets, request.OffsetID)
	if len(f.pages) == 0 {
		return &tg.MessagesMessages{}, nil
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	return page, nil
}

func TestResolveChannelCachesPeerAcrossSyncCycles(t *testing.T) {
	api := &fakeHistoryAPI{}
	session := newSession(api)
	first, err := session.ResolveChannel(context.Background(), "@Rent_SPB")
	if err != nil {
		t.Fatal(err)
	}
	second, err := session.ResolveChannel(context.Background(), "rent_spb")
	if err != nil {
		t.Fatal(err)
	}
	if api.resolveCalls != 1 {
		t.Fatalf("resolved username %d times, want one API call", api.resolveCalls)
	}
	if first != second || first.ChannelID != 7 {
		t.Fatalf("cached source mismatch: first=%#v second=%#v", first, second)
	}
}

func TestFetchPostsPaginatesUntilCutoff(t *testing.T) {
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	message := func(id int, postedAt time.Time) tg.MessageClass {
		return &tg.Message{ID: id, Date: int(postedAt.Unix()), Message: string(rune('A' + id - 101))}
	}
	api := &fakeHistoryAPI{pages: []tg.MessagesMessagesClass{
		&tg.MessagesMessages{Messages: []tg.MessageClass{message(106, base.Add(6*time.Hour)), message(105, base.Add(5*time.Hour))}},
		&tg.MessagesMessages{Messages: []tg.MessageClass{&tg.MessageService{ID: 104, Date: int(base.Add(4 * time.Hour).Unix())}}},
		&tg.MessagesMessages{Messages: []tg.MessageClass{message(103, base.Add(3*time.Hour)), message(102, base.Add(-time.Hour))}},
	}}
	session := newSession(api)
	session.hashes[7] = 99
	useFakeClock(session)
	posts, err := session.FetchPosts(context.Background(), domain.TelegramSource{ChannelID: 7, Username: "rent_spb"}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 3 {
		t.Fatalf("got %d posts, want 3", len(posts))
	}
	if got, want := []int{posts[0].MessageID, posts[1].MessageID, posts[2].MessageID}, []int{103, 105, 106}; !equalInts(got, want) {
		t.Fatalf("posts are not chronological: got %v, want %v", got, want)
	}
	if len(api.offsets) != 3 || api.offsets[0] != 0 || api.offsets[1] != 105 || api.offsets[2] != 104 {
		t.Fatalf("unexpected page offsets: %v", api.offsets)
	}
}

func TestFloodWaitRetriesAndAddsSafetyBuffer(t *testing.T) {
	session := newSession(nil)
	var waits []time.Duration
	session.sleep = func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		waits = append(waits, delay)
		return nil
	}
	session.pacer.sleep = session.sleep
	var loggedSource string
	var loggedDuration time.Duration
	session.logWait = func(source string, delay time.Duration) {
		loggedSource, loggedDuration = source, delay
	}
	calls := 0
	err := session.retryFloodWait(context.Background(), "rent_spb", func() error {
		calls++
		if calls < 3 {
			return tgerr.New(420, "FLOOD_WAIT_3")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(waits) != 2 || waits[0] != 4*time.Second || waits[1] != 4*time.Second {
		t.Fatalf("unexpected retry schedule: calls=%d waits=%v", calls, waits)
	}
	if loggedSource != "rent_spb" || loggedDuration != 4*time.Second {
		t.Fatalf("wait log should contain only source and duration, got %q %s", loggedSource, loggedDuration)
	}
}

func TestFloodWaitCancellationStopsRetry(t *testing.T) {
	session := newSession(nil)
	var waits int
	session.sleep = func(ctx context.Context, _ time.Duration) error {
		waits++
		return ctx.Err()
	}
	session.pacer.sleep = session.sleep
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := session.retryFloodWait(ctx, "rent_spb", func() error {
		calls++
		return tgerr.New(420, "FLOOD_WAIT_23")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || waits != 1 {
		t.Fatalf("cancellation did not stop retry: err=%v calls=%d waits=%d", err, calls, waits)
	}
}

func TestFloodWaitRetriesAreBounded(t *testing.T) {
	session := newSession(nil)
	var waits int
	session.sleep = func(context.Context, time.Duration) error {
		waits++
		return nil
	}
	session.pacer.sleep = session.sleep
	calls := 0
	err := session.retryFloodWait(context.Background(), "rent_spb", func() error {
		calls++
		return tgerr.New(420, "FLOOD_WAIT_1")
	})
	if err == nil || calls != maxFloodWaitRetry+1 || waits != maxFloodWaitRetry {
		t.Fatalf("retry limit not enforced: err=%v calls=%d waits=%d", err, calls, waits)
	}
}

func TestRequestPacerWaitsBetweenCalls(t *testing.T) {
	now := time.Unix(100, 0)
	var waits []time.Duration
	sleep := func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		waits = append(waits, delay)
		now = now.Add(delay)
		return nil
	}
	pacer := requestPacer{interval: time.Second, now: func() time.Time { return now }, sleep: sleep}
	if err := pacer.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(250 * time.Millisecond)
	if err := pacer.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] != 750*time.Millisecond {
		t.Fatalf("unexpected pacing delay: %v", waits)
	}
}

func useFakeClock(session *Session) {
	now := time.Unix(100, 0)
	sleep := func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		now = now.Add(delay)
		return nil
	}
	session.sleep = sleep
	session.pacer.now = func() time.Time { return now }
	session.pacer.sleep = sleep
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestPrepareSessionPathDoesNotChangeExistingDirectoryPermissions(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o750); err != nil {
		t.Fatal(err)
	}
	err := prepareSessionPath(filepath.Join(parent, "session.json"))
	if err == nil {
		t.Fatal("expected a permission error for a group-accessible directory")
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("existing directory permissions changed: got %04o, want 0750", got)
	}
}

func TestPrivateSessionStorageUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "session.json")
	storage := &privateSessionStorage{path: path}
	want := []byte("session fixture")
	if err := storage.StoreSession(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := storage.LoadSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("session payload changed: got %q, want %q", got, want)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("session file permissions = %04o, want 0600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("session directory permissions = %04o, want 0700", got)
	}
}
