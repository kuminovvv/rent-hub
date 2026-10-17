package freshness

import (
	"math"
	"regexp"
	"strings"
	"time"

	"spb-rent/backend/internal/domain"
)

const defaultLifetimeHours = 7 * 24

var (
	standaloneInactive = regexp.MustCompile(`(?im)(^|\n)\s*(?:✅\s*)?(?:сдано|сдана|не\s*актуально|неактуально|снято|закрыто)\s*[.!✅\s]*(?:$|\n)`)
	objectInactive     = regexp.MustCompile(`(?i)(?:квартира|комната|апартамент(?:ы)?|объект|объявление)\s+(?:уже\s+)?(?:сдана|сдан|сдано|не\s*актуальн[а-я]*)`)
	tenantFound        = regexp.MustCompile(`(?i)арендатор\s+(?:уже\s+)?найден`)
	alreadyRented      = regexp.MustCompile(`(?i)уже\s+сдал[иаи]`)
)

func ExplicitInactive(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	// Avoid building-completion phrases such as "дом будет сдан" and direct
	// negations such as "квартира не сдана".
	if strings.Contains(lower, "не сдан") {
		return false
	}
	return standaloneInactive.MatchString(text) || objectInactive.MatchString(text) || tenantFound.MatchString(text) || alreadyRented.MatchString(text)
}

type Input struct {
	PostedAt              time.Time
	EditedAt              *time.Time
	LastSeenAt            time.Time
	Deleted               bool
	InactiveReason        string
	SourceLifetimeHours   float64
	SourceLifetimeSamples int
	SupersededByChannelID *int64
	SupersededByMessageID *int
}

func Evaluate(now time.Time, input Input) domain.ListingFreshness {
	lastVerified := input.LastSeenAt
	if lastVerified.IsZero() {
		lastVerified = input.PostedAt
	}
	result := domain.ListingFreshness{LastVerifiedAt: lastVerified, Basis: "estimate"}
	if input.Deleted || input.InactiveReason == "deleted" {
		result.Status, result.Score, result.Reason, result.Basis = "inactive", 0, "deleted", "fact"
		return result
	}
	if input.InactiveReason == "explicit_text" {
		result.Status, result.Score, result.Reason, result.Basis = "inactive", 0, "explicit_text", "fact"
		return result
	}

	lifetime := float64(defaultLifetimeHours)
	if input.SourceLifetimeSamples >= 5 && input.SourceLifetimeHours > 0 {
		lifetime = math.Max(24, math.Min(30*24, input.SourceLifetimeHours))
	}
	ageHours := math.Max(0, now.Sub(input.PostedAt).Hours())
	score := math.Pow(0.5, ageHours/lifetime)
	if input.EditedAt != nil && now.Sub(*input.EditedAt) >= 0 && now.Sub(*input.EditedAt) <= 48*time.Hour {
		score = math.Min(1, score*1.15)
	}
	if input.InactiveReason == "superseded" {
		score = math.Min(score, 0.1)
		result.Reason = "superseded"
		if input.SupersededByChannelID != nil && input.SupersededByMessageID != nil {
			result.SupersededBy = &domain.TelegramPostRef{ChannelID: *input.SupersededByChannelID, MessageID: *input.SupersededByMessageID}
		}
	}
	result.Score = math.Round(score*1000) / 1000
	switch {
	case score >= 0.70:
		result.Status = "active"
	case score >= 0.35:
		result.Status = "possibly_stale"
		if result.Reason == "" {
			result.Reason = "age"
		}
	default:
		result.Status = "likely_inactive"
		if result.Reason == "" {
			result.Reason = "age"
		}
	}
	return result
}
