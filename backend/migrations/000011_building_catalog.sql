CREATE TABLE IF NOT EXISTS building_catalog (
    id BIGSERIAL PRIMARY KEY,
    source TEXT NOT NULL,
    source_house_id BIGINT,
    house_guid TEXT NOT NULL,
    match_key TEXT NOT NULL,
    address TEXT NOT NULL,
    built_year INTEGER,
    exploitation_start_year INTEGER,
    project_type TEXT,
    house_type TEXT,
    is_alarm BOOLEAN,
    floor_count_max INTEGER,
    floor_count_min INTEGER,
    entrance_count INTEGER,
    elevators_count INTEGER,
    energy_efficiency TEXT,
    quarters_count INTEGER,
    living_quarters_count INTEGER,
    foundation_type TEXT,
    floor_type TEXT,
    wall_material TEXT,
    gas_type TEXT,
    source_dataset_date DATE,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source, house_guid)
);

CREATE INDEX IF NOT EXISTS idx_building_catalog_match_key
    ON building_catalog (source, match_key);

CREATE TABLE IF NOT EXISTS listing_building_matches (
    channel_id BIGINT NOT NULL,
    message_id INTEGER NOT NULL,
    address_key TEXT NOT NULL,
    building_id BIGINT NOT NULL REFERENCES building_catalog (id) ON DELETE CASCADE,
    matched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, message_id),
    FOREIGN KEY (channel_id, message_id)
        REFERENCES telegram_post_facts (channel_id, message_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_listing_building_matches_building
    ON listing_building_matches (building_id);
