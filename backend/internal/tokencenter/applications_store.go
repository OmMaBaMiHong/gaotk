package tokencenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
)

const applicationColumns = `id, request, state, revision, created_at, updated_at`

type applicationScanner interface{ Scan(...any) error }

func scanApplication(row applicationScanner) (Application, error) {
	var a Application
	var request, state []byte
	err := row.Scan(&a.ID, &request, &state, &a.Revision, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrApplicationNotFound
	}
	if err != nil {
		return a, err
	}
	if err = json.Unmarshal(request, &a.ApplicationInput); err != nil {
		return a, err
	}
	err = json.Unmarshal(state, &a.ApplicationState)
	return a, err
}
func (s *SQLStore) CreateApplication(ctx context.Context, input ApplicationInput, hash string) (Application, error) {
	input, err := normalizeApplication(input)
	if err != nil {
		return Application{}, err
	}
	request, state, err := applicationJSON(input, ApplicationState{Status: "pending", PublicNote: "申请已保存，等待处理。", History: []ApplicationEvent{}})
	if err != nil {
		return Application{}, err
	}
	return scanApplication(s.db.QueryRowContext(ctx, `INSERT INTO tokencenter_applications(receipt_hash,request,state) VALUES($1,$2::jsonb,$3::jsonb) RETURNING `+applicationColumns, hash, string(request), string(state)))
}
func (s *SQLStore) ApplicationByReceipt(ctx context.Context, hash string) (Application, error) {
	return scanApplication(s.db.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM tokencenter_applications WHERE receipt_hash=$1`, hash))
}
func (s *SQLStore) ApplicationByID(ctx context.Context, id int64) (Application, error) {
	return scanApplication(s.db.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM tokencenter_applications WHERE id=$1`, id))
}
func (s *SQLStore) ListApplications(ctx context.Context, status string, before int64) (ApplicationPage, error) {
	page := ApplicationPage{Items: []Application{}}
	rows, err := s.db.QueryContext(ctx, `SELECT `+applicationColumns+` FROM tokencenter_applications WHERE ($1='' OR state->>'status'=$1) AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 51`, status, before)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, a)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > 50 {
		page.Items = page.Items[:50]
		page.NextBefore = strconv.FormatInt(page.Items[49].ID, 10)
	}
	return page, nil
}
func (s *SQLStore) ReviewApplication(ctx context.Context, id int64, review ApplicationReview, actor string) (Application, error) {
	// Compare-and-swap and append history in the same single-table UPDATE.
	row, err := scanApplication(s.db.QueryRowContext(ctx, `UPDATE tokencenter_applications SET state=jsonb_build_object('status',$3::text,'publicNote',$4::text,'siteId',$5::text,'history',COALESCE(state->'history','[]'::jsonb)||jsonb_build_array(jsonb_build_object('at',NOW(),'status',$3::text,'note',$4::text,'actor',$6::text))), revision=revision+1,updated_at=NOW() WHERE id=$1 AND revision=$2 RETURNING `+applicationColumns, id, review.Revision, review.Status, review.Note, review.SiteID, actor))
	if errors.Is(err, ErrApplicationNotFound) {
		return row, ErrApplicationConflict
	}
	return row, err
}
