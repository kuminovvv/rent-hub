package domain

import "time"

type Listing struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	PriceMonthly int       `json:"price_monthly"`
	Rooms        int       `json:"rooms"`
	AreaSQM      float64   `json:"area_sqm"`
	Floor        int       `json:"floor"`
	FloorsTotal  int       `json:"floors_total"`
	District     string    `json:"district"`
	Metro        string    `json:"metro"`
	WalkMinutes  int       `json:"walk_minutes"`
	Commission   bool      `json:"commission"`
	AllowsPets   bool      `json:"allows_pets"`
	Furnished    bool      `json:"furnished"`
	SourceName   string    `json:"source_name"`
	SourceURL    string    `json:"source_url"`
	ImageURL     string    `json:"image_url"`
	PublishedAt  time.Time `json:"published_at"`
}

type ListingFilter struct {
	Query        string
	Rooms        *int
	MaxPrice     *int
	NoCommission bool
	Limit         int
	Offset        int
}

type ListingPage struct {
	Items  []Listing `json:"items"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}
