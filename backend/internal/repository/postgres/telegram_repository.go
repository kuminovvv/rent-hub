package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"spb-rent/backend/internal/buildings"
	"spb-rent/backend/internal/domain"
	"spb-rent/backend/internal/listingextract"
)

type TelegramRepository struct {
	pool *pgxpool.Pool
}

func NewTelegramRepository(pool *pgxpool.Pool) *TelegramRepository {
	return &TelegramRepository{pool: pool}
}

func (r *TelegramRepository) UpsertSource(ctx context.Context, source domain.TelegramSource) error {
	const query = `
		INSERT INTO telegram_sources (channel_id, username, title, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id) DO UPDATE SET
			username = EXCLUDED.username,
			title = EXCLUDED.title,
			enabled = EXCLUDED.enabled
	`
	if _, err := r.pool.Exec(ctx, query, source.ChannelID, source.Username, source.Title, source.Enabled); err != nil {
		return fmt.Errorf("upsert telegram source: %w", err)
	}
	return nil
}

func (r *TelegramRepository) ListSources(ctx context.Context) ([]domain.TelegramSource, error) {
	const query = `
		SELECT channel_id, username, title, enabled
		FROM telegram_sources
		ORDER BY title, username
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list telegram sources: %w", err)
	}
	defer rows.Close()

	sources := make([]domain.TelegramSource, 0)
	for rows.Next() {
		var source domain.TelegramSource
		if err := rows.Scan(&source.ChannelID, &source.Username, &source.Title, &source.Enabled); err != nil {
			return nil, fmt.Errorf("scan telegram source: %w", err)
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telegram sources: %w", err)
	}
	return sources, nil
}

func (r *TelegramRepository) UpsertPost(ctx context.Context, post domain.TelegramPost) error {
	const query = `
		INSERT INTO telegram_posts (
			channel_id, message_id, source_username, source_title, source_url,
			text, media, posted_at, deleted, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (channel_id, message_id) DO UPDATE SET
			source_username = EXCLUDED.source_username,
			source_title = EXCLUDED.source_title,
			source_url = EXCLUDED.source_url,
			text = EXCLUDED.text,
			media = COALESCE(EXCLUDED.media, telegram_posts.media),
			posted_at = EXCLUDED.posted_at,
			deleted = EXCLUDED.deleted,
			updated_at = NOW()
	`
	var media any
	if len(post.Media) > 0 {
		media = []byte(post.Media)
	}
	facts := post.Facts
	if facts == nil {
		parsed := listingextract.Extract(post.Text)
		facts = &parsed
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin upsert telegram post: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existingText string
	var hasFacts bool
	err = tx.QueryRow(ctx, `
		SELECT p.text, f.channel_id IS NOT NULL
		FROM telegram_posts p
		LEFT JOIN telegram_post_facts f USING (channel_id, message_id)
		WHERE p.channel_id=$1 AND p.message_id=$2
		FOR UPDATE OF p`, post.ChannelID, post.MessageID).Scan(&existingText, &hasFacts)
	textChanged := errors.Is(err, pgx.ErrNoRows) || (err == nil && existingText != post.Text)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read existing Telegram post: %w", err)
	}
	if _, err := tx.Exec(ctx, query,
		post.ChannelID,
		post.MessageID,
		post.SourceUsername,
		post.SourceTitle,
		post.SourceURL,
		post.Text,
		media,
		post.PostedAt,
		post.Deleted,
	); err != nil {
		return fmt.Errorf("upsert telegram post: %w", err)
	}
	if textChanged {
		if _, err := tx.Exec(ctx, `UPDATE telegram_post_facts SET ai_text_hash='' WHERE channel_id=$1 AND message_id=$2`, post.ChannelID, post.MessageID); err != nil {
			return fmt.Errorf("mark changed Telegram post for AI structuring: %w", err)
		}
	}
	if textChanged || !hasFacts {
		if err := upsertListingFacts(ctx, tx, post.ChannelID, post.MessageID, *facts); err != nil {
			return fmt.Errorf("upsert telegram post facts: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit telegram post upsert: %w", err)
	}
	return nil
}

// ListUnprocessedListingPosts returns posts whose current text has not yet been
// structured by Qwen. The text hash makes repeated polling idempotent.
func (r *TelegramRepository) ListUnprocessedListingPosts(ctx context.Context, limit int) ([]domain.TelegramPost, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.channel_id, p.message_id, p.text
		FROM telegram_posts p
		LEFT JOIN telegram_post_facts f USING (channel_id, message_id)
		WHERE p.deleted = FALSE AND BTRIM(p.text) <> ''
		  AND (COALESCE(f.ai_version, 0) < 2 OR COALESCE(f.ai_text_hash, '') <> md5(p.text))
		ORDER BY p.posted_at DESC, p.channel_id, p.message_id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list Telegram posts pending Qwen structuring: %w", err)
	}
	defer rows.Close()
	posts := make([]domain.TelegramPost, 0, limit)
	for rows.Next() {
		var post domain.TelegramPost
		if err := rows.Scan(&post.ChannelID, &post.MessageID, &post.Text); err != nil {
			return nil, fmt.Errorf("scan Telegram post pending Qwen structuring: %w", err)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Telegram posts pending Qwen structuring: %w", err)
	}
	return posts, nil
}

// SaveListingFacts persists validated model output and marks only the exact text
// that was processed, so an edit during an API call remains pending.
func (r *TelegramRepository) SaveListingFacts(ctx context.Context, post domain.TelegramPost, facts domain.ListingFacts) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin save Qwen listing facts: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentText string
	err = tx.QueryRow(ctx, `SELECT text FROM telegram_posts WHERE channel_id=$1 AND message_id=$2 FOR UPDATE`, post.ChannelID, post.MessageID).Scan(&currentText)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && currentText != post.Text) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check Telegram post text before saving Qwen facts: %w", err)
	}
	if facts.Kind == "offer" {
		parsed := listingextract.Extract(post.Text)
		if parsed.PriceRub != nil {
			facts.PriceRub = parsed.PriceRub
		}
		if parsed.AreaSQM != nil {
			facts.AreaSQM = parsed.AreaSQM
		}
	}
	if err := upsertListingFacts(ctx, tx, post.ChannelID, post.MessageID, facts); err != nil {
		return fmt.Errorf("save Qwen listing facts: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE telegram_post_facts f
		SET ai_text_hash = md5(p.text), ai_version = 2, updated_at = NOW()
		FROM telegram_posts p
		WHERE f.channel_id=$1 AND f.message_id=$2
		  AND p.channel_id=f.channel_id AND p.message_id=f.message_id
		  AND p.text=$3`, post.ChannelID, post.MessageID, post.Text); err != nil {
		return fmt.Errorf("mark Qwen listing facts processed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Qwen listing facts: %w", err)
	}
	return nil
}

func (r *TelegramRepository) ListPosts(ctx context.Context, filter domain.TelegramPostFilter) (domain.TelegramPostPage, error) {
	where, args := postConditions(filter)
	condition := strings.Join(where, " AND ")
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.Cursor != nil {
		postedAt := arg(filter.Cursor.PostedAt)
		channelID := arg(filter.Cursor.ChannelID)
		messageID := arg(filter.Cursor.MessageID)
		condition += ` AND (p.posted_at, p.channel_id, p.message_id) < (` + postedAt + `, ` + channelID + `, ` + messageID + `)`
	}
	queryLimit := filter.Limit + 1
	query := `
		SELECT p.channel_id, p.message_id, p.source_username, p.source_title, p.source_url,
		       p.text, p.media, p.posted_at, p.deleted,
		       f.kind, f.building_type, f.rooms, f.price_rub, f.area_sqm, f.metro, f.address, f.latitude, f.longitude,
		       COALESCE(f.details, '{}'::jsonb)
		FROM telegram_posts p
		LEFT JOIN telegram_post_facts f USING (channel_id, message_id)
		WHERE ` + condition + `
		ORDER BY p.posted_at DESC, p.channel_id DESC, p.message_id DESC
		LIMIT ` + arg(queryLimit)
	if filter.Offset > 0 {
		// Deprecated compatibility path. New clients should use cursor pagination.
		query += ` OFFSET ` + arg(filter.Offset)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.TelegramPostPage{}, fmt.Errorf("list telegram posts: %w", err)
	}
	defer rows.Close()

	page := domain.TelegramPostPage{
		Items:  make([]domain.TelegramPost, 0, queryLimit),
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}
	for rows.Next() {
		var post domain.TelegramPost
		var facts domain.ListingFacts
		var kind, buildingType, metro, address *string
		var detailsRaw []byte
		if err := rows.Scan(
			&post.ChannelID, &post.MessageID, &post.SourceUsername, &post.SourceTitle,
			&post.SourceURL, &post.Text, &post.Media, &post.PostedAt, &post.Deleted,
			&kind, &buildingType, &facts.Rooms, &facts.PriceRub, &facts.AreaSQM, &metro, &address, &facts.Latitude, &facts.Longitude,
			&detailsRaw,
		); err != nil {
			return domain.TelegramPostPage{}, fmt.Errorf("scan telegram post: %w", err)
		}
		if kind != nil {
			facts.Kind = *kind
			if buildingType != nil {
				facts.BuildingType = *buildingType
			}
			if metro != nil {
				facts.Metro = *metro
			}
			if address != nil {
				facts.Address = *address
			}
			var details domain.ListingDetails
			if len(detailsRaw) > 0 && json.Unmarshal(detailsRaw, &details) == nil {
				facts.Details = &details
			}
			applyReadTimePriceFallback(&facts, post.Text)
			post.Facts = &facts
		}
		page.Items = append(page.Items, post)
	}
	if err := rows.Err(); err != nil {
		return domain.TelegramPostPage{}, fmt.Errorf("iterate telegram posts: %w", err)
	}
	if len(page.Items) > filter.Limit {
		page.HasMore = true
		page.Items = page.Items[:filter.Limit]
	}
	if page.HasMore && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		page.NextCursor = fmt.Sprintf("%d:%d:%d", last.PostedAt.UnixMicro(), last.ChannelID, last.MessageID)
	}
	refs := make([]buildings.PostRef, len(page.Items))
	for index := range page.Items {
		refs[index] = buildings.PostRef{ChannelID: page.Items[index].ChannelID, MessageID: page.Items[index].MessageID}
	}
	buildingByPost, err := buildings.NewPostgresStore(r.pool).GetForPosts(ctx, refs)
	if err != nil {
		return domain.TelegramPostPage{}, fmt.Errorf("enrich telegram posts with buildings: %w", err)
	}
	for index := range page.Items {
		page.Items[index].Building = buildingByPost[refs[index]]
	}
	return page, nil
}

func applyReadTimePriceFallback(facts *domain.ListingFacts, text string) {
	if facts == nil || facts.Kind != "offer" || facts.PriceRub != nil {
		return
	}
	if parsed := listingextract.Extract(text); parsed.PriceRub != nil {
		facts.PriceRub = parsed.PriceRub
	}
}

func (r *TelegramRepository) ListMapPosts(ctx context.Context, filter domain.TelegramPostFilter) (domain.TelegramMapPage, error) {
	where, args := postConditions(filter)
	where = append(where, "f.kind = 'offer'", "f.latitude IS NOT NULL", "f.longitude IS NOT NULL")
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.MapBounds != nil {
		where = append(where,
			"f.longitude >= "+arg(filter.MapBounds.West),
			"f.longitude <= "+arg(filter.MapBounds.East),
			"f.latitude >= "+arg(filter.MapBounds.South),
			"f.latitude <= "+arg(filter.MapBounds.North),
		)
	}
	condition := strings.Join(where, " AND ")
	limit := 1500
	if filter.MapZoom > 0 && filter.MapZoom < 10 {
		limit = 750
	} else if filter.MapZoom >= 14 {
		limit = 2500
	}
	queryLimit := limit + 1
	args = append(args, queryLimit)
	rows, err := r.pool.Query(ctx, `
		SELECT p.channel_id, p.message_id, p.source_url, p.source_title,
		       f.price_rub, f.rooms, f.area_sqm, f.building_type, f.address, f.latitude, f.longitude
		FROM telegram_posts p
		JOIN telegram_post_facts f USING (channel_id, message_id)
		WHERE `+condition+`
		ORDER BY p.posted_at DESC, p.channel_id, p.message_id DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return domain.TelegramMapPage{}, fmt.Errorf("list telegram map posts: %w", err)
	}
	defer rows.Close()
	page := domain.TelegramMapPage{Items: make([]domain.TelegramMapItem, 0, queryLimit)}
	for rows.Next() {
		var item domain.TelegramMapItem
		if err := rows.Scan(&item.ChannelID, &item.MessageID, &item.SourceURL, &item.SourceTitle,
			&item.PriceRub, &item.Rooms, &item.AreaSQM, &item.BuildingType, &item.Address, &item.Latitude, &item.Longitude); err != nil {
			return domain.TelegramMapPage{}, fmt.Errorf("scan telegram map post: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.TelegramMapPage{}, fmt.Errorf("iterate telegram map posts: %w", err)
	}
	if len(page.Items) > limit {
		page.Truncated = true
		page.Items = page.Items[:limit]
	}
	page.Total = len(page.Items)
	return page, nil
}

func upsertListingFacts(ctx context.Context, tx pgx.Tx, channelID int64, messageID int, facts domain.ListingFacts) error {
	details := facts.Details
	if details == nil {
		details = &domain.ListingDetails{}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode listing details: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO telegram_post_facts (
			channel_id, message_id, kind, building_type, rooms, price_rub, area_sqm, metro, address, latitude, longitude, details, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
		ON CONFLICT (channel_id, message_id) DO UPDATE SET
			kind = EXCLUDED.kind,
			building_type = EXCLUDED.building_type,
			rooms = EXCLUDED.rooms,
			price_rub = EXCLUDED.price_rub,
			area_sqm = EXCLUDED.area_sqm,
			metro = EXCLUDED.metro,
			latitude = CASE WHEN telegram_post_facts.address IS NOT DISTINCT FROM EXCLUDED.address
			                THEN COALESCE(EXCLUDED.latitude, telegram_post_facts.latitude) ELSE EXCLUDED.latitude END,
			longitude = CASE WHEN telegram_post_facts.address IS NOT DISTINCT FROM EXCLUDED.address
			                 THEN COALESCE(EXCLUDED.longitude, telegram_post_facts.longitude) ELSE EXCLUDED.longitude END,
			address = EXCLUDED.address,
			details = EXCLUDED.details,
			updated_at = NOW()
	`, channelID, messageID, normalizedFact(facts.Kind), normalizedFact(facts.BuildingType), facts.Rooms, facts.PriceRub,
		facts.AreaSQM, strings.TrimSpace(facts.Metro), strings.TrimSpace(facts.Address), facts.Latitude, facts.Longitude, detailsJSON)
	return err
}

func normalizedFact(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func postConditions(filter domain.TelegramPostFilter) ([]string, []any) {
	where := []string{"p.deleted = FALSE"}
	if filter.HasText {
		where = append(where, "p.has_text")
	}
	args := make([]any, 0, 5)
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.ChannelID > 0 {
		where = append(where, "p.channel_id = "+arg(filter.ChannelID))
	}
	if filter.MessageID > 0 {
		where = append(where, "p.message_id = "+arg(filter.MessageID))
	}

	if value := strings.TrimSpace(filter.Source); value != "" {
		where = append(where, "LOWER(p.source_username) = LOWER("+arg(value)+")")
	}
	if value := strings.TrimSpace(filter.Query); value != "" {
		p := arg("%" + value + "%")
		where = append(where, "(p.text ILIKE "+p+" OR p.source_title ILIKE "+p+" OR p.source_username ILIKE "+p+")")
	}
	if filter.Since != nil {
		where = append(where, "p.posted_at >= "+arg(*filter.Since))
	}
	if filter.Until != nil {
		where = append(where, "p.posted_at <= "+arg(*filter.Until))
	}
	if filter.OfferOnly {
		where = append(where, "f.kind = 'offer'")
	}
	if filter.BuildingType != "" {
		where = append(where, "f.building_type = "+arg(filter.BuildingType))
	}
	roomConditions := make([]string, 0, len(filter.Rooms)+1)
	for _, rooms := range filter.Rooms {
		roomConditions = append(roomConditions, "f.rooms = "+arg(rooms))
	}
	if filter.RoomsPlus {
		roomConditions = append(roomConditions, "f.rooms >= 4")
	}
	if len(roomConditions) == 1 {
		where = append(where, roomConditions[0])
	} else if len(roomConditions) > 1 {
		where = append(where, "("+strings.Join(roomConditions, " OR ")+")")
	}
	if filter.PriceMin != nil {
		where = append(where, "f.price_rub >= "+arg(*filter.PriceMin))
	}
	if filter.PriceMax != nil {
		where = append(where, "f.price_rub <= "+arg(*filter.PriceMax))
	}
	if value := strings.TrimSpace(filter.Metro); value != "" {
		where = append(where, "f.metro <> ''", "f.metro ILIKE "+arg("%"+value+"%"))
	}
	return where, args
}
