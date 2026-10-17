package buildings

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

type keyedBuilding struct {
	id    int64
	guid  string
	score int
}

func (s *PostgresStore) Replace(ctx context.Context, records []Record, datasetDate *time.Time) (ImportStats, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ImportStats{}, fmt.Errorf("begin building catalog refresh: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "DELETE FROM listing_building_matches"); err != nil {
		return ImportStats{}, fmt.Errorf("clear listing building matches: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM building_catalog WHERE source = $1", SourceFRT); err != nil {
		return ImportStats{}, fmt.Errorf("clear FRT building catalog: %w", err)
	}
	const insertBuilding = `
		INSERT INTO building_catalog (
			source, source_house_id, house_guid, match_key, address,
			built_year, exploitation_start_year, project_type, house_type, is_alarm,
			floor_count_max, floor_count_min, entrance_count, elevators_count,
			energy_efficiency, quarters_count, living_quarters_count,
			foundation_type, floor_type, wall_material, gas_type, source_dataset_date, imported_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,NOW())
		RETURNING id
	`
	byKey := make(map[string][]keyedBuilding)
	for _, record := range records {
		var id int64
		if err := tx.QueryRow(ctx, insertBuilding,
			record.Source, record.SourceHouseID, record.HouseGUID, record.MatchKey, record.Address,
			record.BuiltYear, record.ExploitationStartYear, record.ProjectType, record.HouseType, record.IsAlarm,
			record.FloorCountMax, record.FloorCountMin, record.EntranceCount, record.ElevatorsCount,
			record.EnergyEfficiency, record.QuartersCount, record.LivingQuartersCount,
			record.FoundationType, record.FloorType, record.WallMaterial, record.GasType, datasetDate,
		).Scan(&id); err != nil {
			return ImportStats{}, fmt.Errorf("insert FRT building %q: %w", record.HouseGUID, err)
		}
		byKey[record.MatchKey] = append(byKey[record.MatchKey], keyedBuilding{
			id: id, guid: record.HouseGUID, score: completeness(record),
		})
	}
	unique := make(map[string]int64)
	ambiguous := 0
	for key, values := range byKey {
		buildingID, ok := resolveBuildingKey(values)
		if !ok {
			ambiguous++
			continue
		}
		unique[key] = buildingID
	}
	rows, err := tx.Query(ctx, `
		SELECT channel_id, message_id, address_key, address
		FROM telegram_post_facts
		WHERE kind = 'offer' AND address_key IS NOT NULL AND address_key <> ''
	`)
	if err != nil {
		return ImportStats{}, fmt.Errorf("load listing addresses for building matching: %w", err)
	}
	type listingAddress struct {
		channelID           int64
		messageID           int
		addressKey, address string
	}
	listings := make([]listingAddress, 0)
	for rows.Next() {
		var listing listingAddress
		if err := rows.Scan(&listing.channelID, &listing.messageID, &listing.addressKey, &listing.address); err != nil {
			rows.Close()
			return ImportStats{}, fmt.Errorf("scan listing address for building matching: %w", err)
		}
		listings = append(listings, listing)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ImportStats{}, fmt.Errorf("iterate listing addresses for building matching: %w", err)
	}
	rows.Close()
	matched := 0
	const insertMatch = `INSERT INTO listing_building_matches (channel_id,message_id,address_key,building_id,matched_at) VALUES ($1,$2,$3,$4,NOW())`
	for _, listing := range listings {
		key := ListingMatchKey(listing.address)
		buildingID, ok := unique[key]
		if !ok || key == "" {
			continue
		}
		if _, err := tx.Exec(ctx, insertMatch, listing.channelID, listing.messageID, listing.addressKey, buildingID); err != nil {
			return ImportStats{}, fmt.Errorf("match listing to building: %w", err)
		}
		matched++
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportStats{}, fmt.Errorf("commit building catalog refresh: %w", err)
	}
	return ImportStats{Buildings: len(records), AmbiguousKeys: ambiguous, ListingsSeen: len(listings), ListingsMatched: matched}, nil
}

func resolveBuildingKey(values []keyedBuilding) (int64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	if len(values) == 1 {
		return values[0].id, true
	}
	best := values[0]
	secondScore := -1
	for _, value := range values[1:] {
		if value.score > best.score {
			secondScore = best.score
			best = value
		} else if value.score > secondScore {
			secondScore = value.score
		}
	}
	// FRT contains legacy duplicate GUIDs for the same exact structured address.
	// Only resolve that conflict when one record is meaningfully populated and
	// clearly dominates every alternative. Equally complete/conflicting rows
	// remain deliberately unmatched.
	if best.score >= 5 && best.score-secondScore >= 4 {
		return best.id, true
	}
	return 0, false
}

func (s *PostgresStore) GetForPost(ctx context.Context, channelID int64, messageID int) (*Info, error) {
	info, err := s.getCachedForPost(ctx, channelID, messageID)
	if err != nil || info != nil {
		return info, err
	}

	var addressKey, address string
	if err := s.pool.QueryRow(ctx, `
		SELECT address_key, address
		FROM telegram_post_facts
		WHERE channel_id = $1
		  AND message_id = $2
		  AND kind = 'offer'
		  AND address_key IS NOT NULL
		  AND address_key <> ''
	`, channelID, messageID).Scan(&addressKey, &address); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get listing address for building lookup: %w", err)
	}

	matchKey := ListingMatchKey(address)
	if matchKey == "" {
		return nil, nil
	}
	buildingID, info, err := s.resolveCatalogMatch(ctx, matchKey)
	if err != nil || info == nil {
		return info, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO listing_building_matches (
			channel_id, message_id, address_key, building_id, matched_at
		) VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (channel_id, message_id) DO UPDATE
		SET address_key = EXCLUDED.address_key,
		    building_id = EXCLUDED.building_id,
		    matched_at = EXCLUDED.matched_at
	`, channelID, messageID, addressKey, buildingID); err != nil {
		return nil, fmt.Errorf("cache listing building match: %w", err)
	}
	return info, nil
}

func (s *PostgresStore) GetForPosts(ctx context.Context, refs []PostRef) (map[PostRef]*Info, error) {
	result := make(map[PostRef]*Info, len(refs))
	if len(refs) == 0 {
		return result, nil
	}
	channelIDs := make([]int64, len(refs))
	messageIDs := make([]int32, len(refs))
	for index, ref := range refs {
		channelIDs[index] = ref.ChannelID
		messageIDs[index] = int32(ref.MessageID)
	}

	rows, err := s.pool.Query(ctx, `
		WITH requested AS (
			SELECT * FROM unnest($1::bigint[], $2::integer[]) AS r(channel_id, message_id)
		)
		SELECT r.channel_id,r.message_id,
		       b.source,b.source_house_id,b.house_guid,b.address,b.built_year,b.exploitation_start_year,
		       b.project_type,b.house_type,b.is_alarm,b.floor_count_max,b.floor_count_min,b.entrance_count,
		       b.elevators_count,b.energy_efficiency,b.quarters_count,b.living_quarters_count,
		       b.foundation_type,b.floor_type,b.wall_material,b.gas_type,b.source_dataset_date,b.imported_at
		FROM requested r
		JOIN listing_building_matches m USING (channel_id, message_id)
		JOIN telegram_post_facts f ON f.channel_id=m.channel_id AND f.message_id=m.message_id AND f.address_key=m.address_key
		JOIN building_catalog b ON b.id=m.building_id
	`, channelIDs, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("get cached buildings for posts: %w", err)
	}
	for rows.Next() {
		var ref PostRef
		var info Info
		if err := rows.Scan(
			&ref.ChannelID, &ref.MessageID,
			&info.Source, &info.SourceHouseID, &info.HouseGUID, &info.Address, &info.BuiltYear, &info.ExploitationStartYear,
			&info.ProjectType, &info.HouseType, &info.IsAlarm, &info.FloorCountMax, &info.FloorCountMin, &info.EntranceCount,
			&info.ElevatorsCount, &info.EnergyEfficiency, &info.QuartersCount, &info.LivingQuartersCount,
			&info.FoundationType, &info.FloorType, &info.WallMaterial, &info.GasType, &info.SourceDatasetDate, &info.ImportedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan cached building for post: %w", err)
		}
		result[ref] = &info
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate cached buildings for posts: %w", err)
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		WITH requested AS (
			SELECT * FROM unnest($1::bigint[], $2::integer[]) AS r(channel_id, message_id)
		)
		SELECT r.channel_id,r.message_id,f.address_key,f.address
		FROM requested r
		JOIN telegram_post_facts f USING (channel_id, message_id)
		WHERE f.kind='offer' AND f.address_key IS NOT NULL AND f.address_key <> ''
	`, channelIDs, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("load listing addresses for batch building lookup: %w", err)
	}
	type pendingListing struct {
		ref        PostRef
		addressKey string
		matchKey   string
	}
	pending := make([]pendingListing, 0, len(refs))
	matchKeySet := make(map[string]struct{})
	for rows.Next() {
		var item pendingListing
		var address string
		if err := rows.Scan(&item.ref.ChannelID, &item.ref.MessageID, &item.addressKey, &address); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan listing address for batch building lookup: %w", err)
		}
		if result[item.ref] != nil {
			continue
		}
		item.matchKey = ListingMatchKey(address)
		if item.matchKey == "" {
			continue
		}
		pending = append(pending, item)
		matchKeySet[item.matchKey] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate listing addresses for batch building lookup: %w", err)
	}
	rows.Close()
	if len(pending) == 0 {
		return result, nil
	}

	matchKeys := make([]string, 0, len(matchKeySet))
	for key := range matchKeySet {
		matchKeys = append(matchKeys, key)
	}
	rows, err = s.pool.Query(ctx, `
		SELECT b.match_key,b.id,b.source,b.source_house_id,b.house_guid,b.address,b.built_year,b.exploitation_start_year,
		       b.project_type,b.house_type,b.is_alarm,b.floor_count_max,b.floor_count_min,b.entrance_count,
		       b.elevators_count,b.energy_efficiency,b.quarters_count,b.living_quarters_count,
		       b.foundation_type,b.floor_type,b.wall_material,b.gas_type,b.source_dataset_date,b.imported_at
		FROM building_catalog b
		WHERE b.source=$1 AND b.match_key=ANY($2::text[])
	`, SourceFRT, matchKeys)
	if err != nil {
		return nil, fmt.Errorf("find building candidates for posts: %w", err)
	}
	type candidate struct {
		id    int64
		info  Info
		score int
	}
	candidatesByKey := make(map[string][]candidate, len(matchKeys))
	for rows.Next() {
		var key string
		var item candidate
		if err := rows.Scan(
			&key, &item.id, &item.info.Source, &item.info.SourceHouseID, &item.info.HouseGUID, &item.info.Address,
			&item.info.BuiltYear, &item.info.ExploitationStartYear, &item.info.ProjectType, &item.info.HouseType,
			&item.info.IsAlarm, &item.info.FloorCountMax, &item.info.FloorCountMin, &item.info.EntranceCount,
			&item.info.ElevatorsCount, &item.info.EnergyEfficiency, &item.info.QuartersCount,
			&item.info.LivingQuartersCount, &item.info.FoundationType, &item.info.FloorType,
			&item.info.WallMaterial, &item.info.GasType, &item.info.SourceDatasetDate, &item.info.ImportedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan building candidate for posts: %w", err)
		}
		item.score = completeness(Record{Info: item.info})
		candidatesByKey[key] = append(candidatesByKey[key], item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate building candidates for posts: %w", err)
	}
	rows.Close()

	resolved := make(map[string]candidate, len(candidatesByKey))
	for key, candidates := range candidatesByKey {
		keyed := make([]keyedBuilding, 0, len(candidates))
		byID := make(map[int64]candidate, len(candidates))
		for _, item := range candidates {
			keyed = append(keyed, keyedBuilding{id: item.id, guid: item.info.HouseGUID, score: item.score})
			byID[item.id] = item
		}
		if id, ok := resolveBuildingKey(keyed); ok {
			resolved[key] = byID[id]
		}
	}

	var matchedChannels []int64
	var matchedMessages []int32
	var matchedAddressKeys []string
	var matchedBuildingIDs []int64
	for _, item := range pending {
		building, ok := resolved[item.matchKey]
		if !ok {
			continue
		}
		info := building.info
		result[item.ref] = &info
		matchedChannels = append(matchedChannels, item.ref.ChannelID)
		matchedMessages = append(matchedMessages, int32(item.ref.MessageID))
		matchedAddressKeys = append(matchedAddressKeys, item.addressKey)
		matchedBuildingIDs = append(matchedBuildingIDs, building.id)
	}
	if len(matchedChannels) > 0 {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO listing_building_matches (channel_id,message_id,address_key,building_id,matched_at)
			SELECT channel_id,message_id,address_key,building_id,NOW()
			FROM unnest($1::bigint[],$2::integer[],$3::text[],$4::bigint[])
			     AS x(channel_id,message_id,address_key,building_id)
			ON CONFLICT (channel_id,message_id) DO UPDATE
			SET address_key=EXCLUDED.address_key, building_id=EXCLUDED.building_id, matched_at=EXCLUDED.matched_at
		`, matchedChannels, matchedMessages, matchedAddressKeys, matchedBuildingIDs); err != nil {
			return nil, fmt.Errorf("cache batch listing building matches: %w", err)
		}
	}
	return result, nil
}

func (s *PostgresStore) getCachedForPost(ctx context.Context, channelID int64, messageID int) (*Info, error) {
	const query = `
		SELECT b.source,b.source_house_id,b.house_guid,b.address,b.built_year,b.exploitation_start_year,
		       b.project_type,b.house_type,b.is_alarm,b.floor_count_max,b.floor_count_min,b.entrance_count,
		       b.elevators_count,b.energy_efficiency,b.quarters_count,b.living_quarters_count,
		       b.foundation_type,b.floor_type,b.wall_material,b.gas_type,b.source_dataset_date,b.imported_at
		FROM listing_building_matches m
		JOIN telegram_post_facts f ON f.channel_id=m.channel_id AND f.message_id=m.message_id AND f.address_key=m.address_key
		JOIN building_catalog b ON b.id=m.building_id
		WHERE m.channel_id=$1 AND m.message_id=$2
	`
	var info Info
	if err := s.pool.QueryRow(ctx, query, channelID, messageID).Scan(
		&info.Source, &info.SourceHouseID, &info.HouseGUID, &info.Address, &info.BuiltYear, &info.ExploitationStartYear,
		&info.ProjectType, &info.HouseType, &info.IsAlarm, &info.FloorCountMax, &info.FloorCountMin, &info.EntranceCount,
		&info.ElevatorsCount, &info.EnergyEfficiency, &info.QuartersCount, &info.LivingQuartersCount,
		&info.FoundationType, &info.FloorType, &info.WallMaterial, &info.GasType, &info.SourceDatasetDate, &info.ImportedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get building for listing: %w", err)
	}
	return &info, nil
}

func (s *PostgresStore) resolveCatalogMatch(ctx context.Context, matchKey string) (int64, *Info, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id,b.source,b.source_house_id,b.house_guid,b.address,b.built_year,b.exploitation_start_year,
		       b.project_type,b.house_type,b.is_alarm,b.floor_count_max,b.floor_count_min,b.entrance_count,
		       b.elevators_count,b.energy_efficiency,b.quarters_count,b.living_quarters_count,
		       b.foundation_type,b.floor_type,b.wall_material,b.gas_type,b.source_dataset_date,b.imported_at
		FROM building_catalog b
		WHERE b.source = $1 AND b.match_key = $2
	`, SourceFRT, matchKey)
	if err != nil {
		return 0, nil, fmt.Errorf("find building candidates: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		id    int64
		info  Info
		score int
	}
	candidates := make([]candidate, 0, 2)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.id, &item.info.Source, &item.info.SourceHouseID, &item.info.HouseGUID, &item.info.Address,
			&item.info.BuiltYear, &item.info.ExploitationStartYear, &item.info.ProjectType, &item.info.HouseType,
			&item.info.IsAlarm, &item.info.FloorCountMax, &item.info.FloorCountMin, &item.info.EntranceCount,
			&item.info.ElevatorsCount, &item.info.EnergyEfficiency, &item.info.QuartersCount,
			&item.info.LivingQuartersCount, &item.info.FoundationType, &item.info.FloorType,
			&item.info.WallMaterial, &item.info.GasType, &item.info.SourceDatasetDate, &item.info.ImportedAt,
		); err != nil {
			return 0, nil, fmt.Errorf("scan building candidate: %w", err)
		}
		item.score = completeness(Record{Info: item.info})
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("iterate building candidates: %w", err)
	}
	if len(candidates) == 0 {
		return 0, nil, nil
	}
	keyed := make([]keyedBuilding, 0, len(candidates))
	byID := make(map[int64]*Info, len(candidates))
	for index := range candidates {
		item := &candidates[index]
		keyed = append(keyed, keyedBuilding{id: item.id, guid: item.info.HouseGUID, score: item.score})
		byID[item.id] = &item.info
	}
	buildingID, ok := resolveBuildingKey(keyed)
	if !ok {
		return 0, nil, nil
	}
	return buildingID, byID[buildingID], nil
}
