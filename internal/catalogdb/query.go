package catalogdb

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type ListCandidatesOptions struct {
	Source  string
	Query   string
	Quality string
	Limit   int
	Offset  int
}

func (s *Store) ListCandidates(opts ListCandidatesOptions) ([]CandidateRecord, error) {
	source := strings.ToLower(strings.TrimSpace(opts.Source))
	if source == "" {
		return nil, errors.New("catalogdb: ListCandidates requires source")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	var (
		whereParts = []string{"source = ?"}
		args       = []any{source}
	)

	if q := normalizeNameForSearch(opts.Query); q != "" {
		whereParts = append(whereParts, "target_name_norm LIKE ?")
		args = append(args, "%"+q+"%")
	}

	if quality := strings.TrimSpace(opts.Quality); quality != "" {
		whereParts = append(whereParts, "quality = ?")
		args = append(args, quality)
	}

	query := `
SELECT
	source,
	target_name,
	target_name_norm,
	target_classification,
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
FROM catalog_candidates
WHERE ` + strings.Join(whereParts, " AND ") + `
ORDER BY score DESC, target_name ASC
LIMIT ? OFFSET ?
`
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()

	var out []CandidateRecord
	for rows.Next() {
		rec, err := scanCandidateRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidate rows: %w", err)
	}

	return out, nil
}

func (s *Store) CountCandidatesFiltered(opts ListCandidatesOptions) (int, error) {
	source := strings.ToLower(strings.TrimSpace(opts.Source))
	if source == "" {
		return 0, errors.New("catalogdb: CountCandidatesFiltered requires source")
	}

	var (
		whereParts = []string{"source = ?"}
		args       = []any{source}
	)

	if q := normalizeNameForSearch(opts.Query); q != "" {
		whereParts = append(whereParts, "target_name_norm LIKE ?")
		args = append(args, "%"+q+"%")
	}

	if quality := strings.TrimSpace(opts.Quality); quality != "" {
		whereParts = append(whereParts, "quality = ?")
		args = append(args, quality)
	}

	query := `
SELECT COUNT(*)
FROM catalog_candidates
WHERE ` + strings.Join(whereParts, " AND ")

	var count int
	if err := s.db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count candidates filtered: %w", err)
	}
	return count, nil
}

func (s *Store) GetCandidate(source, targetName string) (CandidateRecord, bool, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	targetName = strings.TrimSpace(targetName)

	if source == "" {
		return CandidateRecord{}, false, errors.New("catalogdb: GetCandidate requires source")
	}
	if targetName == "" {
		return CandidateRecord{}, false, errors.New("catalogdb: GetCandidate requires targetName")
	}

	row := s.db.QueryRow(`
SELECT
	source,
	target_name,
	target_name_norm,
	target_classification,
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
FROM catalog_candidates
WHERE source = ? AND target_name = ?
`, source, targetName)

	rec, err := scanCandidateRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateRecord{}, false, nil
	}
	if err != nil {
		return CandidateRecord{}, false, fmt.Errorf("get candidate source=%q target=%q: %w", source, targetName, err)
	}
	return rec, true, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCandidateRecord(rs rowScanner) (CandidateRecord, error) {
	var rec CandidateRecord
	err := rs.Scan(
		&rec.Source,
		&rec.TargetName,
		&rec.TargetNameNorm,
		&rec.TargetClassification,
		&rec.ObservationKey,
		&rec.ObservationID,
		&rec.RowsCount,
		&rec.FiltersCSV,
		&rec.Quality,
		&rec.SelectionMode,
		&rec.ProductKind,
		&rec.Score,
		&rec.AvgDist,
		&rec.FallbackPenalty,
		&rec.DuplicatePenalty,
		&rec.RedFilter,
		&rec.GreenFilter,
		&rec.BlueFilter,
		&rec.RedDataURL,
		&rec.GreenDataURL,
		&rec.BlueDataURL,
		&rec.UpdatedAt,
	)
	if err != nil {
		return CandidateRecord{}, err
	}
	return rec, nil
}
