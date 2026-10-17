package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"spb-rent/backend/internal/domain"
)

func TestTelegramRepositoryAgainstPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL for the PostgreSQL integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewTelegramRepository(pool)
	const channelID int64 = -88008008
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM telegram_posts WHERE channel_id=$1", channelID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM telegram_sources WHERE channel_id=$1", channelID)
		pool.Close()
	})
	source := domain.TelegramSource{ChannelID: channelID, Username: "integration_spb", Title: "Integration", Enabled: true}
	if err := repo.UpsertSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	postedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	post := domain.TelegramPost{
		ChannelID: channelID, MessageID: 7, SourceUsername: source.Username,
		SourceTitle: source.Title, SourceURL: "https://t.me/example_channel/7",
		Text: "Первая версия", Media: json.RawMessage(`{"kind":"photo","photo_id":123}`), PostedAt: postedAt,
		Facts: &domain.ListingFacts{Kind: "offer", BuildingType: "new", Rooms: intPointer(2), PriceRub: intPointer(45000), AreaSQM: floatPointer(42.5),
			Metro: "Пионерская", Address: "улица Примерная, 10", Latitude: floatPointer(60.0), Longitude: floatPointer(30.0)},
	}
	if err := repo.UpsertPost(ctx, post); err != nil {
		t.Fatal(err)
	}
	post.Text = "Обновлённая версия"
	post.Media = nil // A text-only update must retain media stored by an earlier sync.
	if err := repo.UpsertPost(ctx, post); err != nil {
		t.Fatal(err)
	}
	aiFacts := domain.ListingFacts{Kind: "offer", PriceRub: intPointer(45000), AreaSQM: floatPointer(42.5), Details: &domain.ListingDetails{
		DepositRub: intPointer(45000), ContactName: "Дмитрий", ContactMethods: []string{"phone"},
	}}
	if err := repo.SaveListingFacts(ctx, post, aiFacts); err != nil {
		t.Fatal(err)
	}
	post.Facts = &domain.ListingFacts{Kind: "offer"} // An unchanged sync must preserve Qwen facts.
	if err := repo.UpsertPost(ctx, post); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListPosts(ctx, domain.TelegramPostFilter{Source: source.Username, Query: "Обновлённая", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.HasMore || page.Items[0].Text != post.Text {
		t.Fatalf("unexpected page after upsert: %+v", page)
	}
	if page.Items[0].Facts == nil || page.Items[0].Facts.Kind != "offer" || page.Items[0].Facts.Rooms == nil || *page.Items[0].Facts.Rooms != 2 {
		t.Fatalf("facts were not returned with the post: %+v", page.Items[0].Facts)
	}
	if page.Items[0].Facts.AreaSQM == nil || *page.Items[0].Facts.AreaSQM != 42.5 {
		t.Fatalf("area was not persisted and returned: %+v", page.Items[0].Facts)
	}
	if page.Items[0].Facts.PriceRub == nil || *page.Items[0].Facts.PriceRub != 45000 || page.Items[0].Facts.Details == nil || page.Items[0].Facts.Details.ContactName != "Дмитрий" || page.Items[0].Facts.Details.DepositRub == nil || *page.Items[0].Facts.Details.DepositRub != 45000 {
		t.Fatalf("Qwen facts were not preserved and returned: %+v", page.Items[0].Facts)
	}
	filtered, err := repo.ListPosts(ctx, domain.TelegramPostFilter{
		OfferOnly: true, BuildingType: "new", Rooms: []int{2}, PriceMin: intPointer(40000), PriceMax: intPointer(50000), Metro: "Пионерская", Limit: 10,
	})
	if err != nil || len(filtered.Items) != 1 || filtered.HasMore {
		t.Fatalf("catalog filters returned page=%+v err=%v", filtered, err)
	}
	mapPage, err := repo.ListMapPosts(ctx, domain.TelegramPostFilter{
		Source: source.Username, OfferOnly: true, BuildingType: "new",
		MapBounds: &domain.TelegramMapBounds{West: 29.5, South: 59.5, East: 30.5, North: 60.5},
		MapZoom:   12,
	})
	if err != nil || mapPage.Total != 1 || len(mapPage.Items) != 1 || mapPage.Items[0].Address != "улица Примерная, 10" || mapPage.Items[0].AreaSQM == nil || *mapPage.Items[0].AreaSQM != 42.5 {
		t.Fatalf("map returned page=%+v err=%v", mapPage, err)
	}
	var media map[string]any
	if err := json.Unmarshal(page.Items[0].Media, &media); err != nil {
		t.Fatal(err)
	}
	if media["kind"] != "photo" || media["photo_id"] != float64(123) {
		t.Fatalf("media mismatch: %s", page.Items[0].Media)
	}
}

func intPointer(value int) *int           { return &value }
func floatPointer(value float64) *float64 { return &value }
