package domain

import (
	"encoding/json"
	"time"

	"spb-rent/backend/internal/buildings"
)

// TelegramSource identifies a public broadcast channel selected by the user.
type TelegramSource struct {
	ChannelID int64  `json:"channel_id"`
	Username  string `json:"username"`
	Title     string `json:"title"`
	Enabled   bool   `json:"enabled"`
}

// TelegramPost preserves the original post without guessing apartment fields.
type TelegramPost struct {
	ChannelID      int64             `json:"channel_id"`
	MessageID      int               `json:"message_id"`
	SourceUsername string            `json:"source_username"`
	SourceTitle    string            `json:"source_title"`
	SourceURL      string            `json:"source_url"`
	Text           string            `json:"text"`
	Media          json.RawMessage   `json:"media,omitempty"`
	PostedAt       time.Time         `json:"posted_at"`
	EditedAt       *time.Time        `json:"edited_at,omitempty"`
	Deleted        bool              `json:"deleted"`
	Facts          *ListingFacts     `json:"facts,omitempty"`
	Freshness      *ListingFreshness `json:"freshness,omitempty"`
	Building       *buildings.Info   `json:"building,omitempty"`
}

type TelegramPostRef struct {
	ChannelID int64 `json:"channel_id"`
	MessageID int   `json:"message_id"`
}

type ListingFreshness struct {
	Status         string           `json:"status"`
	Score          float64          `json:"score"`
	LastVerifiedAt time.Time        `json:"last_verified_at"`
	Reason         string           `json:"reason,omitempty"`
	Basis          string           `json:"basis"`
	SupersededBy   *TelegramPostRef `json:"superseded_by,omitempty"`
}

type TelegramPostFilter struct {
	ChannelID    int64
	MessageID    int
	Source       string
	Query        string
	HasText      bool
	OfferOnly    bool
	BuildingType string
	Rooms        []int
	RoomsPlus    bool
	PriceMin     *int
	PriceMax     *int
	Metro        string
	Since        *time.Time
	Until        *time.Time
	Limit        int
	Offset       int
	Cursor       *TelegramPostCursor
	MapBounds    *TelegramMapBounds
	MapZoom      int
}

type TelegramPostCursor struct {
	PostedAt  time.Time
	ChannelID int64
	MessageID int
}

type TelegramMapBounds struct {
	West  float64
	South float64
	East  float64
	North float64
}

// TelegramMapItem is a compact classified offer with known exact coordinates.
type TelegramMapItem struct {
	ChannelID    int64    `json:"channel_id"`
	MessageID    int      `json:"message_id"`
	SourceURL    string   `json:"source_url"`
	SourceTitle  string   `json:"source_title"`
	PriceRub     *int     `json:"price_rub"`
	Rooms        *int     `json:"rooms"`
	AreaSQM      *float64 `json:"area_sqm,omitempty"`
	BuildingType string   `json:"building_type"`
	Address      string   `json:"address"`
	Latitude     float64  `json:"latitude"`
	Longitude    float64  `json:"longitude"`
}

type TelegramMapPage struct {
	Items     []TelegramMapItem `json:"items"`
	Total     int               `json:"total"`
	Truncated bool              `json:"truncated,omitempty"`
}

type TelegramPostPage struct {
	Items      []TelegramPost `json:"items"`
	Limit      int            `json:"limit"`
	HasMore    bool           `json:"has_more"`
	NextCursor string         `json:"next_cursor,omitempty"`
	// Offset is retained only for compatibility with older clients. New clients
	// should use NextCursor; ordinary pagination no longer requires OFFSET.
	Offset int `json:"offset,omitempty"`
}

// ListingFacts contains rental attributes extracted conservatively from a post.
// Nil/empty values mean the source text did not provide a reliable value.
