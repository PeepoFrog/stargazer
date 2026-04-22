package catalogdb

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrSkipProductRow = errors.New("catalogdb: skip product row")

type Store struct {
	db   *sql.DB
	path string
}

type ProductRecord struct {
	RowKey         string
	Source         string
	TargetName     string
	TargetNameNorm string
	FilterName     string
	DataURL        string
	RawJSON        string
	CreatedAt      string
	UpdatedAt      string
}

type CandidateRecord struct {
	Source           string
	TargetName       string
	TargetNameNorm   string
	ObservationKey   string
	ObservationID    string
	RowsCount        int
	FiltersCSV       string
	Quality          string
	SelectionMode    string
	ProductKind      string
	Score            float64
	AvgDist          float64
	FallbackPenalty  float64
	DuplicatePenalty float64

	RedFilter   string
	GreenFilter string
	BlueFilter  string

	RedDataURL   string
	GreenDataURL string
	BlueDataURL  string

	UpdatedAt string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{
		db:   db,
		path: path,
	}

	if err := s.applyPragmas(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := s.InitSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) applyPragmas() error {
	stmts := []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA synchronous = NORMAL;`,
		`PRAGMA temp_store = MEMORY;`,
		`PRAGMA foreign_keys = ON;`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("apply pragma %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *Store) InitSchema() error {
	stmts := []string{
		`
CREATE TABLE IF NOT EXISTS catalog_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`,
		`
CREATE TABLE IF NOT EXISTS catalog_products (
	row_key TEXT PRIMARY KEY,
	source TEXT NOT NULL,
	target_name TEXT NOT NULL,
	target_name_norm TEXT NOT NULL,
	filter_name TEXT NOT NULL,
	data_url TEXT NOT NULL,
	raw_json TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`,
		`
CREATE INDEX IF NOT EXISTS idx_catalog_products_source_target
ON catalog_products(source, target_name);
`,
		`
CREATE INDEX IF NOT EXISTS idx_catalog_products_source_target_norm
ON catalog_products(source, target_name_norm);
`,
		`
CREATE INDEX IF NOT EXISTS idx_catalog_products_source_filter
ON catalog_products(source, filter_name);
`,
		`
CREATE TABLE IF NOT EXISTS catalog_candidates (
	source TEXT NOT NULL,
	target_name TEXT NOT NULL,
	target_name_norm TEXT NOT NULL,
	observation_key TEXT NOT NULL,
	observation_id TEXT NOT NULL,
	rows_count INTEGER NOT NULL,
	filters_csv TEXT NOT NULL,
	quality TEXT NOT NULL,
	selection_mode TEXT NOT NULL,
	product_kind TEXT NOT NULL,
	score REAL NOT NULL,
	avg_dist REAL NOT NULL,
	fallback_penalty REAL NOT NULL,
	duplicate_penalty REAL NOT NULL,
	red_filter TEXT NOT NULL,
	green_filter TEXT NOT NULL,
	blue_filter TEXT NOT NULL,
	red_data_url TEXT NOT NULL,
	green_data_url TEXT NOT NULL,
	blue_data_url TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (source, target_name)
);
`,
		`
CREATE INDEX IF NOT EXISTS idx_catalog_candidates_source_score
ON catalog_candidates(source, score DESC);
`,
		`
CREATE INDEX IF NOT EXISTS idx_catalog_candidates_source_target_norm
ON catalog_candidates(source, target_name_norm);
`,
		`
CREATE INDEX IF NOT EXISTS idx_catalog_candidates_source_quality
ON catalog_candidates(source, quality);
`,
		`
CREATE INDEX IF NOT EXISTS idx_catalog_candidates_source_observation_id
ON catalog_candidates(source, observation_id);
`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`
INSERT INTO catalog_meta(key, value)
VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
`, key, value)
	if err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}
	return nil
}

func (s *Store) GetMeta(key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM catalog_meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get meta %q: %w", key, err)
	}
	return value, true, nil
}

func (s *Store) HasCompletedSync() (bool, string, error) {
	value, ok, err := s.GetMeta("sync.completed_at")
	if err != nil {
		return false, "", err
	}
	if !ok || strings.TrimSpace(value) == "" {
		return false, "", nil
	}
	return true, value, nil
}

func (s *Store) UpsertProducts(records []ProductRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin upsert products tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.Prepare(`
INSERT INTO catalog_products(
	row_key,
	source,
	target_name,
	target_name_norm,
	filter_name,
	data_url,
	raw_json,
	created_at,
	updated_at
)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(row_key) DO UPDATE SET
	source = excluded.source,
	target_name = excluded.target_name,
	target_name_norm = excluded.target_name_norm,
	filter_name = excluded.filter_name,
	data_url = excluded.data_url,
	raw_json = excluded.raw_json,
	updated_at = excluded.updated_at
`)
	if err != nil {
		return fmt.Errorf("prepare upsert products: %w", err)
	}
	defer stmt.Close()

	for _, r := range records {
		if _, err := stmt.Exec(
			r.RowKey,
			r.Source,
			r.TargetName,
			r.TargetNameNorm,
			r.FilterName,
			r.DataURL,
			r.RawJSON,
			r.CreatedAt,
			r.UpdatedAt,
		); err != nil {
			return fmt.Errorf("exec upsert product row_key=%q: %w", r.RowKey, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert products: %w", err)
	}
	return nil
}

func (s *Store) ClearCandidates(source string) error {
	_, err := s.db.Exec(`DELETE FROM catalog_candidates WHERE source = ?`, source)
	if err != nil {
		return fmt.Errorf("clear candidates for source=%q: %w", source, err)
	}
	return nil
}

func (s *Store) UpsertCandidate(r CandidateRecord) error {
	_, err := s.db.Exec(`
INSERT INTO catalog_candidates(
	source,
	target_name,
	target_name_norm,
	observation_key,
	observation_id,
	rows_count,
	filters_csv,
	quality,
	selection_mode,
	product_kind,
	score,
	avg_dist,
	fallback_penalty,
	duplicate_penalty,
	red_filter,
	green_filter,
	blue_filter,
	red_data_url,
	green_data_url,
	blue_data_url,
	updated_at
)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, target_name) DO UPDATE SET
	target_name_norm = excluded.target_name_norm,
	observation_key = excluded.observation_key,
	observation_id = excluded.observation_id,
	rows_count = excluded.rows_count,
	filters_csv = excluded.filters_csv,
	quality = excluded.quality,
	selection_mode = excluded.selection_mode,
	product_kind = excluded.product_kind,
	score = excluded.score,
	avg_dist = excluded.avg_dist,
	fallback_penalty = excluded.fallback_penalty,
	duplicate_penalty = excluded.duplicate_penalty,
	red_filter = excluded.red_filter,
	green_filter = excluded.green_filter,
	blue_filter = excluded.blue_filter,
	red_data_url = excluded.red_data_url,
	green_data_url = excluded.green_data_url,
	blue_data_url = excluded.blue_data_url,
	updated_at = excluded.updated_at
`,
		r.Source,
		r.TargetName,
		r.TargetNameNorm,
		r.ObservationKey,
		r.ObservationID,
		r.RowsCount,
		r.FiltersCSV,
		r.Quality,
		r.SelectionMode,
		r.ProductKind,
		r.Score,
		r.AvgDist,
		r.FallbackPenalty,
		r.DuplicatePenalty,
		r.RedFilter,
		r.GreenFilter,
		r.BlueFilter,
		r.RedDataURL,
		r.GreenDataURL,
		r.BlueDataURL,
		r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert candidate target=%q: %w", r.TargetName, err)
	}
	return nil
}

func (s *Store) ProductCount(source string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM catalog_products WHERE source = ?`, source).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("product count source=%q: %w", source, err)
	}
	return count, nil
}

func (s *Store) CandidateCount(source string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM catalog_candidates WHERE source = ?`, source).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("candidate count source=%q: %w", source, err)
	}
	return count, nil
}

func (s *Store) DistinctTargetCount(source string) (int, error) {
	var count int
	err := s.db.QueryRow(`
SELECT COUNT(*) FROM (
	SELECT target_name
	FROM catalog_products
	WHERE source = ?
	GROUP BY target_name
)
`, source).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("distinct target count source=%q: %w", source, err)
	}
	return count, nil
}

func (s *Store) ListTargetNames(source string, limit, offset int) ([]string, error) {
	rows, err := s.db.Query(`
SELECT target_name
FROM catalog_products
WHERE source = ?
GROUP BY target_name
ORDER BY target_name
LIMIT ? OFFSET ?
`, source, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list target names source=%q: %w", source, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			return nil, fmt.Errorf("scan target name: %w", err)
		}
		out = append(out, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate target names: %w", err)
	}
	return out, nil
}

func (s *Store) LoadRawRowsForTarget(source, targetName string) ([]map[string]any, error) {
	rows, err := s.db.Query(`
SELECT raw_json
FROM catalog_products
WHERE source = ? AND target_name = ?
ORDER BY filter_name, data_url
`, source, targetName)
	if err != nil {
		return nil, fmt.Errorf("load raw rows source=%q target=%q: %w", source, targetName, err)
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan raw_json: %w", err)
		}

		var row map[string]any
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			return nil, fmt.Errorf("unmarshal raw_json for target=%q: %w", targetName, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate raw rows target=%q: %w", targetName, err)
	}

	return out, nil
}

func NewProductRecord(source string, row map[string]any) (ProductRecord, error) {
	targetName := normalizeTargetName(extractTargetName(row))
	if targetName == "" {
		return ProductRecord{}, ErrSkipProductRow
	}

	rawJSONBytes, err := json.Marshal(row)
	if err != nil {
		return ProductRecord{}, fmt.Errorf("marshal product row: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	filterName := strings.ToUpper(strings.TrimSpace(asString(row["filters"])))
	dataURL := strings.TrimSpace(asString(row["dataURL"]))

	return ProductRecord{
		RowKey:         buildRowKey(source, rawJSONBytes),
		Source:         strings.ToLower(strings.TrimSpace(source)),
		TargetName:     targetName,
		TargetNameNorm: normalizeNameForSearch(targetName),
		FilterName:     filterName,
		DataURL:        dataURL,
		RawJSON:        string(rawJSONBytes),
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func buildRowKey(source string, rawJSON []byte) string {
	sum := sha1.Sum(append([]byte(source+"\x00"), rawJSON...))
	return hex.EncodeToString(sum[:])
}

func extractTargetName(row map[string]any) string {
	for _, key := range []string{
		"target_name",
		"target",
		"targetid",
		"target_id",
		"obs_target_name",
	} {
		v := strings.TrimSpace(asString(row[key]))
		if v != "" {
			return v
		}
	}
	return ""
}

func normalizeTargetName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

func normalizeNameForSearch(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
