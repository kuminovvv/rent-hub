package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"rsc.io/qr"

	"spb-rent/backend/internal/domain"
	"spb-rent/backend/internal/listingai"
	"spb-rent/backend/internal/qwen"
	"spb-rent/backend/internal/repository/postgres"
	"spb-rent/backend/internal/storage"
	"spb-rent/backend/internal/telegram"
)

type terminalPrompter struct{ reader *bufio.Reader }

func (p terminalPrompter) Phone(context.Context) (string, error) {
	return p.line("Telegram phone: ")
}

func (p terminalPrompter) Code(_ context.Context, sent *tg.AuthSentCode) (string, error) {
	fmt.Fprintf(os.Stderr, "Telegram code delivery method: %s\n", codeDeliveryMethod(sent))
	return p.line("Telegram login code: ")
}

func (p terminalPrompter) Password(context.Context) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("2FA password requires an interactive terminal")
	}
	fmt.Fprint(os.Stderr, "Telegram 2FA password: ")
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(value), err
}

func (p terminalPrompter) line(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	value, err := p.reader.ReadString('\n')
	return strings.TrimSpace(value), err
}

type sourceStore interface {
	UpsertSource(context.Context, domain.TelegramSource) error
	ListSources(context.Context) ([]domain.TelegramSource, error)
	UpsertPost(context.Context, domain.TelegramPost) error
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) != 2 || !contains([]string{"login", "login-qr", "import", "run", "enrich"}, os.Args[1]) {
		return errors.New("usage: telegram-sync login|login-qr|import|run|enrich")
	}
	if os.Args[1] == "enrich" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return enrichCatalog(ctx)
	}
	appID, err := strconv.Atoi(strings.TrimSpace(os.Getenv("TELEGRAM_APP_ID")))
	if err != nil || appID <= 0 {
		return errors.New("set TELEGRAM_APP_ID to the api_id from my.telegram.org")
	}
	mediaDownloadLimit, err := strconv.Atoi(env("TELEGRAM_MEDIA_DOWNLOAD_LIMIT", "0"))
	if err != nil || mediaDownloadLimit < 0 {
		return errors.New("TELEGRAM_MEDIA_DOWNLOAD_LIMIT must be a non-negative integer")
	}
	client, err := telegram.New(telegram.Config{
		AppID:              appID,
		AppHash:            os.Getenv("TELEGRAM_APP_HASH"),
		SessionFile:        env("TELEGRAM_SESSION_FILE", "./data/telegram-session.json"),
		MediaDir:           env("TELEGRAM_MEDIA_DIR", "./data/media"),
		MediaDownloadLimit: mediaDownloadLimit,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if os.Args[1] == "login" {
		return client.Login(ctx, terminalPrompter{reader: bufio.NewReader(os.Stdin)})
	}
	if os.Args[1] == "login-qr" {
		prompt := terminalPrompter{reader: bufio.NewReader(os.Stdin)}
		if err := client.LoginQR(ctx, func(_ context.Context, token qrlogin.Token) error {
			return printQR(os.Stderr, token)
		}, prompt.Password); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Telegram session is authorized.")
		return nil
	}
	var qwenExtractor qwen.Extractor
	qwenBatchSize := 20
	qwenWorkers := 4
	qwenPollInterval := 5 * time.Second
	if apiKey := strings.TrimSpace(os.Getenv("QWEN_API_KEY")); apiKey != "" {
		qwenBatchSize, err = strconv.Atoi(env("QWEN_MAX_PER_CYCLE", "20"))
		if err != nil || qwenBatchSize < 1 || qwenBatchSize > 500 {
			return errors.New("QWEN_MAX_PER_CYCLE must be an integer from 1 to 500")
		}
		qwenWorkers, err = strconv.Atoi(env("QWEN_WORKERS", "4"))
		if err != nil || qwenWorkers < 1 || qwenWorkers > 16 {
			return errors.New("QWEN_WORKERS must be an integer from 1 to 16")
		}
		qwenMinInterval, err := time.ParseDuration(env("QWEN_MIN_INTERVAL", "200ms"))
		if err != nil || qwenMinInterval < 0 {
			return errors.New("QWEN_MIN_INTERVAL must be a non-negative duration")
		}
		qwenPollInterval, err = time.ParseDuration(env("QWEN_POLL_INTERVAL", "5s"))
		if err != nil || qwenPollInterval < time.Second {
			return errors.New("QWEN_POLL_INTERVAL must be at least 1s")
		}
		qwenExtractor, err = qwen.New(qwen.Config{
			APIKey:      apiKey,
			Model:       env("QWEN_MODEL", "qwen-flash"),
			Endpoint:    env("QWEN_ENDPOINT", "https://maas.qwencloudapi.com/compatible-mode/v1"),
			MinInterval: qwenMinInterval,
		})
		if err != nil {
			return err
		}
	}

	pool, err := storage.NewPostgres(ctx, env("DATABASE_URL", ""))
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()
	repo := postgres.NewTelegramRepository(pool)

	return client.Run(ctx, func(ctx context.Context, session *telegram.Session) error {
		structurePending := func() {
			if qwenExtractor == nil {
				return
			}
			processed, err := listingai.ProcessPendingConcurrent(ctx, repo, qwenExtractor, qwenBatchSize, qwenWorkers)
			if err != nil {
				log.Printf("Qwen structuring had errors after %d posts: %v", processed, err)
				return
			}
			if processed > 0 {
				log.Printf("Qwen structured %d Telegram posts", processed)
			}
		}
		interval, err := time.ParseDuration(env("TELEGRAM_POLL_INTERVAL", "1m"))
		if err != nil || interval < 30*time.Second {
			return errors.New("TELEGRAM_POLL_INTERVAL must be a duration of at least 30s")
		}
		sourceWorkers, err := strconv.Atoi(env("TELEGRAM_SOURCE_WORKERS", "3"))
		if err != nil || sourceWorkers < 1 || sourceWorkers > 16 {
			return errors.New("TELEGRAM_SOURCE_WORKERS must be an integer from 1 to 16")
		}
		if os.Args[1] == "import" {
			usernames, err := readSources(env("TELEGRAM_SOURCES_FILE", "./telegram_sources.txt"))
			if err != nil {
				return err
			}
			if err := importSources(ctx, session, repo, usernames); err != nil {
				return err
			}
			structurePending()
			return nil
		}
		if qwenExtractor != nil {
			go runStructuringLoop(ctx, qwenPollInterval, structurePending)
		}
		// Existing history is loaded by `import`. Keep startup lightweight so a
		// restart resumes the minute-based watcher without downloading the whole
		// month of history and its media before polling can start.
		if err := syncSources(ctx, session, repo, pollLookback(interval), sourceWorkers); err != nil {
			log.Printf("initial Telegram sync had errors: %v", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				lookback := pollLookback(interval)
				if err := syncSources(ctx, session, repo, lookback, sourceWorkers); err != nil {
					log.Printf("Telegram sync failed: %v", err)
				}
			}
		}
	})
}

func runStructuringLoop(ctx context.Context, interval time.Duration, structure func()) {
	structure()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			structure()
		}
	}
}

func pollLookback(interval time.Duration) time.Duration {
	lookback := 2 * interval
	if lookback < 15*time.Minute {
		return 15 * time.Minute
	}
	return lookback
}

func enrichCatalog(ctx context.Context) error {
	apiKey := strings.TrimSpace(os.Getenv("QWEN_API_KEY"))
	if apiKey == "" {
		return errors.New("set QWEN_API_KEY in backend/.env before running enrich")
	}
	batchSize, err := strconv.Atoi(env("QWEN_MAX_PER_CYCLE", "20"))
	if err != nil || batchSize < 1 || batchSize > 500 {
		return errors.New("QWEN_MAX_PER_CYCLE must be an integer from 1 to 500")
	}
	workers, err := strconv.Atoi(env("QWEN_WORKERS", "4"))
	if err != nil || workers < 1 || workers > 16 {
		return errors.New("QWEN_WORKERS must be an integer from 1 to 16")
	}
	minInterval, err := time.ParseDuration(env("QWEN_MIN_INTERVAL", "200ms"))
	if err != nil || minInterval < 0 {
		return errors.New("QWEN_MIN_INTERVAL must be a non-negative duration")
	}
	maxPosts, err := strconv.Atoi(env("QWEN_BACKFILL_MAX_POSTS", "2200"))
	if err != nil || maxPosts < 1 || maxPosts > 50000 {
		return errors.New("QWEN_BACKFILL_MAX_POSTS must be an integer from 1 to 50000")
	}
	extractor, err := qwen.New(qwen.Config{
		APIKey:      apiKey,
		Model:       env("QWEN_MODEL", "qwen-flash"),
		Endpoint:    env("QWEN_ENDPOINT", "https://maas.qwencloudapi.com/compatible-mode/v1"),
		MinInterval: minInterval,
	})
	if err != nil {
		return err
	}
	pool, err := storage.NewPostgres(ctx, env("DATABASE_URL", ""))
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()
	repo := postgres.NewTelegramRepository(pool)
	processed := 0
	for processed < maxPosts {
		limit := min(batchSize, maxPosts-processed)
		count, err := listingai.ProcessPendingConcurrent(ctx, repo, extractor, limit, workers)
		processed += count
		inputTokens, outputTokens := extractor.Usage()
		fmt.Printf("Qwen usage: input=%d tokens, output=%d tokens.\n", inputTokens, outputTokens)
		if err != nil {
			return fmt.Errorf("Qwen backfill stopped after %d posts: %w", processed, err)
		}
		if count == 0 {
			fmt.Printf("Qwen structured %d Telegram posts; no pending posts remain.\n", processed)
			return nil
		}
		if processed%100 == 0 || processed == maxPosts {
			fmt.Printf("Qwen structured %d/%d Telegram posts.\n", processed, maxPosts)
		}
	}
	pending, err := repo.ListUnprocessedListingPosts(ctx, 1)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("Qwen backfill reached QWEN_BACKFILL_MAX_POSTS=%d; run again to continue", maxPosts)
	}
	fmt.Printf("Qwen structured %d Telegram posts; no pending posts remain.\n", processed)
	return nil
}

func codeDeliveryMethod(sent *tg.AuthSentCode) string {
	if sent == nil {
		return "unspecified method"
	}
	switch sent.Type.(type) {
	case *tg.AuthSentCodeTypeApp:
		return "Telegram app"
	case *tg.AuthSentCodeTypeSMS:
		return "SMS"
	case *tg.AuthSentCodeTypeCall:
		return "phone call"
	case *tg.AuthSentCodeTypeFlashCall:
		return "flash call"
	case *tg.AuthSentCodeTypeMissedCall:
		return "missed call"
	case *tg.AuthSentCodeTypeEmailCode:
		return "email"
	case *tg.AuthSentCodeTypeSetUpEmailRequired:
		return "email setup required"
	case *tg.AuthSentCodeTypeFragmentSMS:
		return "Fragment SMS"
	case *tg.AuthSentCodeTypeFirebaseSMS:
		return "Firebase SMS"
	case *tg.AuthSentCodeTypeSMSWord:
		return "SMS word"
	case *tg.AuthSentCodeTypeSMSPhrase:
		return "SMS phrase"
	default:
		return "unspecified method"
	}
}

func printQR(w io.Writer, token qrlogin.Token) error {
	code, err := qr.Encode(token.URL(), qr.M)
	if err != nil {
		return fmt.Errorf("render Telegram login QR: %w", err)
	}
	if _, err := fmt.Fprintf(w, "Scan this QR in Telegram: Settings → Devices → Link Desktop Device\nToken expires at %s; a replacement QR will appear if needed.\n", token.Expires().Local().Format(time.RFC3339)); err != nil {
		return err
	}
	const quietZone = 4
	width := code.Size + quietZone*2
	for y := 0; y < width; y += 2 {
		var row strings.Builder
		for x := 0; x < width; x++ {
			top := qrModuleBlack(code, x-quietZone, y-quietZone)
			bottom := y+1 < width && qrModuleBlack(code, x-quietZone, y+1-quietZone)
			switch {
			case top && bottom:
				row.WriteString("█")
			case top:
				row.WriteString("▀")
			case bottom:
				row.WriteString("▄")
			default:
				row.WriteByte(' ')
			}
		}
		if _, err := fmt.Fprintf(w, "\x1b[30;47m%s\x1b[0m\n", row.String()); err != nil {
			return err
		}
	}
	return nil
}

func qrModuleBlack(code *qr.Code, x, y int) bool {
	return x >= 0 && x < code.Size && y >= 0 && y < code.Size && code.Black(x, y)
}

func importSources(ctx context.Context, session *telegram.Session, repo sourceStore, usernames []string) error {
	var failures int
	for _, username := range usernames {
		source, err := session.ResolveChannel(ctx, username)
		if err == nil {
			err = repo.UpsertSource(ctx, source)
		}
		if err == nil {
			err = syncSource(ctx, session, repo, source, 30*24*time.Hour)
		}
		if err != nil {
			failures++
			log.Printf("import @%s failed: %v", username, err)
			continue
		}
		log.Printf("imported @%s", source.Username)
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d sources failed; see log", failures, len(usernames))
	}
	return nil
}

func syncSources(ctx context.Context, session *telegram.Session, repo sourceStore, lookback time.Duration, workers int) error {
	sources, err := repo.ListSources(ctx)
	if err != nil {
		return err
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan domain.TelegramSource)
	var failures atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for source := range jobs {
				resolved, err := session.ResolveChannel(ctx, source.Username)
				if err == nil {
					err = repo.UpsertSource(ctx, resolved)
				}
				if err == nil {
					err = syncSource(ctx, session, repo, resolved, lookback)
				}
				if err != nil {
					failures.Add(1)
					log.Printf("sync @%s failed: %v", source.Username, err)
				}
			}
		}()
	}
	for _, source := range sources {
		if !source.Enabled {
			continue
		}
		select {
		case jobs <- source:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if failed := failures.Load(); failed > 0 {
		return fmt.Errorf("%d Telegram sources failed", failed)
	}
	return nil
}

func syncSource(ctx context.Context, session *telegram.Session, repo sourceStore, source domain.TelegramSource, lookback time.Duration) error {
	posts, err := session.FetchPosts(ctx, source, time.Now().UTC().Add(-lookback))
	if err != nil {
		return err
	}
	for _, post := range posts {
		if err := repo.UpsertPost(ctx, post); err != nil {
			return err
		}
	}
	log.Printf("synced @%s: %d posts", source.Username, len(posts))
	return nil
}

func readSources(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Telegram sources: %w", err)
	}
	seen := make(map[string]bool)
	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(line), "@"))
		if line == "" || strings.HasPrefix(line, "#") || seen[line] {
			continue
		}
		if strings.ContainsAny(line, "/ \\?&") {
			return nil, fmt.Errorf("invalid Telegram username %q", line)
		}
		seen[line] = true
		result = append(result, line)
	}
	if len(result) == 0 {
		return nil, errors.New("Telegram source list is empty")
	}
	return result, nil
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
