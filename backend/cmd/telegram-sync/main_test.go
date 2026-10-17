package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
)

func TestReadSourcesParsesAndDeduplicatesHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.txt")
	content := "# synthetic test sources\n@channel_one\nchannel_two\nCHANNEL_ONE\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readSources(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"channel_one", "channel_two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected parsed channels: got %v, want %v", got, want)
	}
}

func TestStructuringLoopRunsIndependentlyUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan struct{}, 2)
	done := make(chan struct{})
	go func() {
		runStructuringLoop(ctx, 10*time.Millisecond, func() { called <- struct{}{} })
		close(done)
	}()
	for range 2 {
		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatal("structuring loop did not run")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("structuring loop did not stop after cancellation")
	}
}

func TestPollLookbackUsesBoundedOverlap(t *testing.T) {
	if got := pollLookback(time.Minute); got != 15*time.Minute {
		t.Fatalf("minute poll lookback = %s, want 15m overlap", got)
	}
	if got := pollLookback(20 * time.Second); got != 15*time.Minute {
		t.Fatalf("short poll lookback = %s, want 15m minimum overlap", got)
	}
	if got := pollLookback(10 * time.Minute); got != 20*time.Minute {
		t.Fatalf("long poll lookback = %s, want 20m", got)
	}
}

func TestCodeDeliveryMethodReportsOnlyType(t *testing.T) {
	if got := codeDeliveryMethod(&tg.AuthSentCode{Type: &tg.AuthSentCodeTypeApp{}}); got != "Telegram app" {
		t.Fatalf("got %q, want Telegram app", got)
	}
	if got := codeDeliveryMethod(&tg.AuthSentCode{Type: &tg.AuthSentCodeTypeSMS{}}); got != "SMS" {
		t.Fatalf("got %q, want SMS", got)
	}
	if got := codeDeliveryMethod(nil); got != "unspecified method" {
		t.Fatalf("got %q for missing delivery type", got)
	}
}

func TestPrintQRWritesTerminalMatrixWithoutTokenURL(t *testing.T) {
	secret := "test-only-qr-payload"
	token := qrlogin.NewToken([]byte(secret), int(time.Now().Add(time.Minute).Unix()))
	var output bytes.Buffer
	if err := printQR(&output, token); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "Scan this QR in Telegram") || !strings.ContainsAny(got, "█▀▄") {
		t.Fatalf("QR output is missing instructions or matrix: %q", got)
	}
	if strings.Contains(got, secret) || strings.Contains(got, "tg://login?token=") {
		t.Fatal("QR token or login URL was printed as text")
	}
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	rawLines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	lines := strings.Split(strings.TrimSuffix(ansi.ReplaceAllString(got, ""), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("QR matrix is missing: got %d lines", len(lines))
	}
	matrix := lines[2:]
	rawMatrix := rawLines[2:]
	if len(matrix) >= 60 {
		t.Fatalf("QR matrix is too tall for a terminal: %d lines", len(matrix))
	}
	width := 0
	for i, line := range matrix {
		if !strings.HasPrefix(rawMatrix[i], "\x1b[30;47m") || !strings.HasSuffix(rawMatrix[i], "\x1b[0m") {
			t.Fatalf("QR row lacks explicit black-on-white terminal colors: %q", rawMatrix[i])
		}
		if lineWidth := utf8.RuneCountInString(line); lineWidth > width {
			width = lineWidth
		}
		if !strings.HasPrefix(line, "    ") || !strings.HasSuffix(line, "    ") {
			t.Fatalf("QR row lacks a four-module quiet border: %q", line)
		}
	}
	if width >= 100 {
		t.Fatalf("QR matrix is too wide for a terminal: %d columns", width)
	}
	if strings.TrimSpace(matrix[0]) != "" || strings.TrimSpace(matrix[1]) != "" {
		t.Fatal("top quiet border is not blank")
	}
}
