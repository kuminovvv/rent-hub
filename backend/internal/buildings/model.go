package buildings

import "time"

const SourceFRT = "frt"

type Info struct {
	Source                string     `json:"source"`
	SourceHouseID         *int64     `json:"source_house_id,omitempty"`
	HouseGUID             string     `json:"house_guid"`
	Address               string     `json:"address"`
	BuiltYear             *int       `json:"built_year,omitempty"`
	ExploitationStartYear *int       `json:"exploitation_start_year,omitempty"`
	ProjectType           *string    `json:"project_type,omitempty"`
	HouseType             *string    `json:"house_type,omitempty"`
	IsAlarm               *bool      `json:"is_alarm,omitempty"`
	FloorCountMax         *int       `json:"floor_count_max,omitempty"`
	FloorCountMin         *int       `json:"floor_count_min,omitempty"`
	EntranceCount         *int       `json:"entrance_count,omitempty"`
	ElevatorsCount        *int       `json:"elevators_count,omitempty"`
	EnergyEfficiency      *string    `json:"energy_efficiency,omitempty"`
	QuartersCount         *int       `json:"quarters_count,omitempty"`
	LivingQuartersCount   *int       `json:"living_quarters_count,omitempty"`
	FoundationType        *string    `json:"foundation_type,omitempty"`
	FloorType             *string    `json:"floor_type,omitempty"`
	WallMaterial          *string    `json:"wall_material,omitempty"`
	GasType               *string    `json:"gas_type,omitempty"`
	SourceDatasetDate     *time.Time `json:"source_dataset_date,omitempty"`
	ImportedAt            time.Time  `json:"imported_at"`
}

type PostRef struct {
	ChannelID int64
	MessageID int
}

type Record struct {
	Info
	MatchKey string
}

type ImportStats struct {
	RowsRead        int
	Buildings       int
	DuplicateRows   int
	AmbiguousKeys   int
	ListingsSeen    int
	ListingsMatched int
}
