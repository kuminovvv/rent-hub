package buildings

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultFRTURL    = "https://xn--80adsazqn.xn--p1aee.xn--p1ai/opendata/export/125"
	maxDownloadBytes = 32 << 20
	maxCSVBytes      = 96 << 20
)

type Replacer interface {
	Replace(context.Context, []Record, *time.Time) (ImportStats, error)
}

type Importer struct {
	URL    string
	Client *http.Client
	Store  Replacer
}

func (i *Importer) Run(ctx context.Context) (ImportStats, error) {
	if i.Store == nil {
		return ImportStats{}, errors.New("building store is required")
	}
	url := strings.TrimSpace(i.URL)
	if url == "" {
		url = DefaultFRTURL
	}
	client := i.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ImportStats{}, fmt.Errorf("build FRT request: %w", err)
	}
	req.Header.Set("Accept", "application/zip, application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return ImportStats{}, fmt.Errorf("download FRT registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ImportStats{}, fmt.Errorf("download FRT registry: unexpected status %d", resp.StatusCode)
	}
	compressed, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return ImportStats{}, fmt.Errorf("read FRT registry: %w", err)
	}
	if len(compressed) > maxDownloadBytes {
		return ImportStats{}, errors.New("FRT registry archive exceeds size limit")
	}
	records, rowsRead, duplicates, archiveDate, err := parseArchive(compressed, resp.Header.Get("Content-Disposition"))
	if err != nil {
		return ImportStats{}, err
	}
	stats, err := i.Store.Replace(ctx, records, archiveDate)
	if err != nil {
		return ImportStats{}, err
	}
	stats.RowsRead = rowsRead
	stats.DuplicateRows = duplicates
	return stats, nil
}

func parseArchive(data []byte, contentDisposition string) ([]Record, int, int, *time.Time, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, 0, 0, nil, fmt.Errorf("open FRT registry archive: %w", err)
	}
	var csvFile *zip.File
	for _, file := range reader.File {
		if !file.FileInfo().IsDir() && strings.EqualFold(filepath.Ext(file.Name), ".csv") {
			if csvFile != nil {
				return nil, 0, 0, nil, errors.New("FRT registry archive contains multiple CSV files")
			}
			csvFile = file
		}
	}
	if csvFile == nil {
		return nil, 0, 0, nil, errors.New("FRT registry archive does not contain a CSV file")
	}
	if csvFile.UncompressedSize64 > maxCSVBytes {
		return nil, 0, 0, nil, errors.New("FRT registry CSV exceeds size limit")
	}
	file, err := csvFile.Open()
	if err != nil {
		return nil, 0, 0, nil, fmt.Errorf("open FRT registry CSV: %w", err)
	}
	defer file.Close()
	records, rowsRead, duplicates, err := parseCSV(io.LimitReader(file, maxCSVBytes+1))
	if err != nil {
		return nil, 0, 0, nil, err
	}
	date := datasetDate(contentDisposition, csvFile.Name)
	return records, rowsRead, duplicates, date, nil
}

func parseCSV(reader io.Reader) ([]Record, int, int, error) {
	csvReader := csv.NewReader(reader)
	csvReader.Comma = ';'
	csvReader.FieldsPerRecord = -1
	header, err := csvReader.Read()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read FRT CSV header: %w", err)
	}
	if len(header) == 0 {
		return nil, 0, 0, errors.New("empty FRT CSV header")
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}
	for _, required := range []string{"id", "formalname_street", "house_number", "block", "building", "letter", "address", "houseguid"} {
		if _, ok := columns[required]; !ok {
			return nil, 0, 0, fmt.Errorf("FRT CSV is missing %q column", required)
		}
	}

	byGUID := make(map[string]Record)
	rowsRead := 0
	duplicates := 0
	for {
		row, readErr := csvReader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, 0, 0, fmt.Errorf("read FRT CSV row: %w", readErr)
		}
		rowsRead++
		record := recordFromRow(row, columns)
		if record.HouseGUID == "" || record.MatchKey == "" {
			continue
		}
		if existing, ok := byGUID[record.HouseGUID]; ok {
			duplicates++
			if completeness(record) > completeness(existing) {
				byGUID[record.HouseGUID] = record
			}
			continue
		}
		byGUID[record.HouseGUID] = record
	}
	records := make([]Record, 0, len(byGUID))
	for _, record := range byGUID {
		records = append(records, record)
	}
	return records, rowsRead, duplicates, nil
}

func recordFromRow(row []string, columns map[string]int) Record {
	value := func(name string) string {
		index, ok := columns[name]
		if !ok || index >= len(row) {
			return ""
		}
		return cleanProviderValue(row[index])
	}
	street := value("formalname_street")
	house := value("house_number")
	block := value("block")
	building := value("building")
	letter := value("letter")
	return Record{
		Info: Info{
			Source:                SourceFRT,
			SourceHouseID:         int64Pointer(value("id")),
			HouseGUID:             strings.ToLower(value("houseguid")),
			Address:               value("address"),
			BuiltYear:             intPointer(value("built_year")),
			ExploitationStartYear: intPointer(value("exploitation_start_year")),
			ProjectType:           stringPointer(value("project_type")),
			HouseType:             stringPointer(value("house_type")),
			IsAlarm:               boolPointer(value("is_alarm")),
			FloorCountMax:         intPointer(value("floor_count_max")),
			FloorCountMin:         intPointer(value("floor_count_min")),
			EntranceCount:         intPointer(value("entrance_count")),
			ElevatorsCount:        intPointer(value("elevators_count")),
			EnergyEfficiency:      stringPointer(value("energy_efficiency")),
			QuartersCount:         intPointer(value("quarters_count")),
			LivingQuartersCount:   intPointer(value("living_quarters_count")),
			FoundationType:        stringPointer(value("foundation_type")),
			FloorType:             stringPointer(value("floor_type")),
			WallMaterial:          stringPointer(value("wall_material")),
			GasType:               stringPointer(value("gas_type")),
		},
		MatchKey: StructuredMatchKey(street, house, block, building, letter),
	}
}

func cleanProviderValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "-", "не заполнено", "не имеется":
		return ""
	default:
		return value
	}
}

func completeness(record Record) int {
	score := 0
	if record.Address != "" {
		score++
	}
	if record.BuiltYear != nil {
		score++
	}
	if record.ExploitationStartYear != nil {
		score++
	}
	if record.ProjectType != nil {
		score++
	}
	if record.HouseType != nil {
		score++
	}
	if record.FloorCountMax != nil {
		score++
	}
	if record.EntranceCount != nil {
		score++
	}
	if record.ElevatorsCount != nil {
		score++
	}
	if record.LivingQuartersCount != nil {
		score++
	}
	if record.WallMaterial != nil {
		score++
	}
	return score
}

func intPointer(value string) *int {
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
}

func int64Pointer(value string) *int64 {
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return nil
	}
	return &parsed
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func boolPointer(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "да", "true", "1":
		value := true
		return &value
	case "нет", "false", "0":
		value := false
		return &value
	default:
		return nil
	}
}

func datasetDate(contentDisposition, filename string) *time.Time {
	if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
		if candidate := params["filename"]; candidate != "" {
			filename = candidate
		}
	}
	for index := 0; index+8 <= len(filename); index++ {
		candidate := filename[index : index+8]
		if _, err := strconv.Atoi(candidate); err != nil {
			continue
		}
		if parsed, err := time.Parse("20060102", candidate); err == nil {
			return &parsed
		}
	}
	return nil
}
