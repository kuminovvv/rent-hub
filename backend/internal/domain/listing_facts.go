package domain

// ListingFacts stores rental attributes parsed from the text of a Telegram post.
// Empty values mean the source did not provide a reliable value.
type ListingFacts struct {
	Kind         string          `json:"kind"`
	BuildingType string          `json:"building_type"`
	Rooms        *int            `json:"rooms,omitempty"`
	PriceRub     *int            `json:"price_rub,omitempty"`
	AreaSQM      *float64        `json:"area_sqm,omitempty"`
	Metro        string          `json:"metro,omitempty"`
	Address      string          `json:"address,omitempty"`
	Latitude     *float64        `json:"latitude,omitempty"`
	Longitude    *float64        `json:"longitude,omitempty"`
	Details      *ListingDetails `json:"details,omitempty"`
}

// ListingDetails contains secondary facts that do not participate in filtering.
// Values are kept only when the source text states them explicitly.
type ListingDetails struct {
	Floor              *int     `json:"floor,omitempty"`
	FloorsTotal        *int     `json:"floors_total,omitempty"`
	DepositRub         *int     `json:"deposit_rub,omitempty"`
	DepositTerms       string   `json:"deposit_terms,omitempty"`
	UtilitiesTerms     string   `json:"utilities_terms,omitempty"`
	CommissionTerms    string   `json:"commission_terms,omitempty"`
	LeaseTerm          string   `json:"lease_term,omitempty"`
	TenantRequirements string   `json:"tenant_requirements,omitempty"`
	ContactName        string   `json:"contact_name,omitempty"`
	ContactPhone       string   `json:"contact_phone,omitempty"`
	ContactTelegram    string   `json:"contact_telegram,omitempty"`
	ContactVK          string   `json:"contact_vk,omitempty"`
	ContactMethods     []string `json:"contact_methods,omitempty"`
	Features           []string `json:"features,omitempty"`
}
