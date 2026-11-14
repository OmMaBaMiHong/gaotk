package tokencenter

// Reviews are TokenCenter-owned user feedback (1–5 star ratings). Post-style
// per the product decision of 2026-11-13: submissions publish immediately by
// default. The keyword filter is architecture only — the word list is empty,
// nothing is intercepted. Ranking score follows the unified algorithm in
// chaos docs/project/merchant-composite-ranking-v1-2026-11-13.md:
// v = (stars-3)/2, shrunk mean R = Σv/(n+α) with α=5, S = (1+R)/2 ∈ [0,1].
// Contact is a private dedupe key (one vote per contact per object, latest
// wins) and never leaves the service through the public API.
import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ReviewObjectTypeRelay      = "relay"
	ReviewObjectTypeRelayGroup = "relay-group"
	ReviewObjectTypeMerchant   = "merchant"
	ReviewObjectTypeProduct    = "product"
)

const (
	ReviewStatusPublished = "published"
	ReviewStatusBlocked   = "blocked" // machine keyword filter; reserved, list empty by default
	ReviewStatusRevoked   = "revoked" // post-hoc takedown (report/appeal outcome)
	ReviewStatusPending   = "pending" // reserved for future anti-brigading hold; unused now
)

var reviewObjectTypes = map[string]bool{
	ReviewObjectTypeRelay: true, ReviewObjectTypeRelayGroup: true,
	ReviewObjectTypeMerchant: true, ReviewObjectTypeProduct: true,
}

var reviewStatuses = map[string]bool{
	ReviewStatusPublished: true, ReviewStatusBlocked: true,
	ReviewStatusRevoked: true, ReviewStatusPending: true,
}

var reviewDimensions = map[string]bool{
	"authenticity": true, "stability": true, "speed": true, "billing": true, "aftercare": true,
}

// reviewBlockedKeywords is the machine filter word list (politics / illegal /
// advertising). Empty by design: the feature exists, interception is off.
var reviewBlockedKeywords []string

func reviewModerationStatus(body string) string {
	normalized := strings.ToLower(strings.TrimSpace(body))
	for _, keyword := range reviewBlockedKeywords {
		if keyword != "" && strings.Contains(normalized, strings.ToLower(keyword)) {
			return ReviewStatusBlocked
		}
	}
	return ReviewStatusPublished
}

var (
	ErrReviewInvalid     = errors.New("评价内容不完整或不符合要求")
	ErrReviewNotFound    = errors.New("评价不存在")
	ErrReviewUnavailable = errors.New("评价服务暂时不可用，请稍后重试")
)

type ReviewInput struct {
	ObjectType string   `json:"objectType" binding:"required"`
	ObjectID   string   `json:"objectId" binding:"required"`
	Stars      int      `json:"stars"`
	Dimensions []string `json:"dimensions"`
	Body       string   `json:"body"`
	Contact    string   `json:"contact" binding:"required"`
	Evidence   string   `json:"evidence"`
}

type ReviewReceipt struct {
	ID        int64     `json:"id"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Review is the public projection: contact and evidence stay private.
type Review struct {
	ID         int64     `json:"id"`
	Stars      int       `json:"stars"`
	Dimensions []string  `json:"dimensions"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type ReputationSummary struct {
	ObjectType      string           `json:"objectType"`
	ObjectID        string           `json:"objectId"`
	Count           int64            `json:"count"`
	AverageStars    *float64         `json:"averageStars"`
	Distribution    map[string]int64 `json:"distribution"`
	ReputationScore float64          `json:"reputationScore"`
}

type ReviewsResult struct {
	Summary ReputationSummary `json:"summary"`
	Reviews []Review          `json:"reviews"`
}

// reputationScore implements the shrunk mean from the unified algorithm:
// Σv = (Σstars − 3n)/2, R = Σv/(n+α), S = (1+R)/2; 0 votes ⇒ 0.5 neutral.
func reputationScore(count, starsSum int64) float64 {
	const alpha = 5.0
	if count <= 0 {
		return 0.5
	}
	vSum := (float64(starsSum) - 3*float64(count)) / 2
	return (1 + vSum/(float64(count)+alpha)) / 2
}

func normalizeReview(in ReviewInput) (ReviewInput, error) {
	in.ObjectType = strings.ToLower(strings.TrimSpace(in.ObjectType))
	in.ObjectID = strings.TrimSpace(in.ObjectID)
	if !reviewObjectTypes[in.ObjectType] || in.ObjectID == "" || utf8.RuneCountInString(in.ObjectID) > 128 {
		return in, ErrReviewInvalid
	}
	if in.Stars < 1 || in.Stars > 5 {
		return in, ErrReviewInvalid
	}
	dimensions := make([]string, 0, len(in.Dimensions))
	seen := map[string]bool{}
	for _, dimension := range in.Dimensions {
		dimension = strings.TrimSpace(dimension)
		if dimension == "" || !reviewDimensions[dimension] || seen[dimension] || len(dimensions) >= len(reviewDimensions) {
			continue
		}
		seen[dimension] = true
		dimensions = append(dimensions, dimension)
	}
	in.Dimensions = dimensions
	in.Body = strings.TrimSpace(in.Body)
	if utf8.RuneCountInString(in.Body) > 2000 {
		return in, ErrReviewInvalid
	}
	in.Contact = strings.TrimSpace(in.Contact)
	if in.Contact == "" || utf8.RuneCountInString(in.Contact) > 200 {
		return in, ErrReviewInvalid
	}
	in.Evidence = strings.TrimSpace(in.Evidence)
	if utf8.RuneCountInString(in.Evidence) > 2000 {
		return in, ErrReviewInvalid
	}
	return in, nil
}

type reviewsStore interface {
	UpsertReview(context.Context, ReviewInput) (ReviewReceipt, error)
	ObjectReviews(context.Context, string, string, int) (ReviewsResult, error)
	SetReviewStatus(context.Context, int64, string) (ReviewReceipt, error)
}

func (s *Service) SubmitReview(ctx context.Context, in ReviewInput) (ReviewReceipt, error) {
	normalized, err := normalizeReview(in)
	if err != nil {
		return ReviewReceipt{}, err
	}
	store, ok := s.store.(reviewsStore)
	if !ok {
		return ReviewReceipt{}, ErrReviewUnavailable
	}
	return store.UpsertReview(ctx, normalized)
}

func (s *Service) ObjectReviews(ctx context.Context, objectType, objectID string, limit int) (ReviewsResult, error) {
	objectType = strings.ToLower(strings.TrimSpace(objectType))
	objectID = strings.TrimSpace(objectID)
	if !reviewObjectTypes[objectType] || objectID == "" {
		return ReviewsResult{}, ErrReviewInvalid
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	store, ok := s.store.(reviewsStore)
	if !ok {
		return ReviewsResult{}, ErrReviewUnavailable
	}
	return store.ObjectReviews(ctx, objectType, objectID, limit)
}

func (s *Service) SetReviewStatus(ctx context.Context, id int64, status string) (ReviewReceipt, error) {
	if !reviewStatuses[status] {
		return ReviewReceipt{}, ErrReviewInvalid
	}
	store, ok := s.store.(reviewsStore)
	if !ok {
		return ReviewReceipt{}, ErrReviewUnavailable
	}
	return store.SetReviewStatus(ctx, id, status)
}

const reviewSelectColumns = `id, stars, dimensions, body, created_at, updated_at`

func scanReview(row interface{ Scan(...any) error }) (Review, error) {
	var review Review
	var dimensions []byte
	if err := row.Scan(&review.ID, &review.Stars, &dimensions, &review.Body, &review.CreatedAt, &review.UpdatedAt); err != nil {
		return review, err
	}
	review.Dimensions = []string{}
	if len(dimensions) > 0 {
		_ = json.Unmarshal(dimensions, &review.Dimensions)
		if review.Dimensions == nil {
			review.Dimensions = []string{}
		}
	}
	return review, nil
}

func (s *SQLStore) UpsertReview(ctx context.Context, in ReviewInput) (ReviewReceipt, error) {
	dimensions, err := json.Marshal(in.Dimensions)
	if err != nil {
		return ReviewReceipt{}, err
	}
	status := reviewModerationStatus(in.Body)
	return scanReviewReceipt(s.db.QueryRowContext(ctx, `INSERT INTO tokencenter_reviews
		(object_type, object_id, stars, dimensions, body, contact, evidence, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (contact, object_type, object_id) DO UPDATE SET
			stars = EXCLUDED.stars, dimensions = EXCLUDED.dimensions, body = EXCLUDED.body,
			evidence = EXCLUDED.evidence, status = EXCLUDED.status, updated_at = NOW()
		RETURNING id, status, updated_at`,
		in.ObjectType, in.ObjectID, in.Stars, dimensions, in.Body, in.Contact, in.Evidence, status))
}

func scanReviewReceipt(row interface{ Scan(...any) error }) (ReviewReceipt, error) {
	var receipt ReviewReceipt
	err := row.Scan(&receipt.ID, &receipt.Status, &receipt.UpdatedAt)
	return receipt, err
}

func (s *SQLStore) ObjectReviews(ctx context.Context, objectType, objectID string, limit int) (ReviewsResult, error) {
	result := ReviewsResult{Summary: ReputationSummary{
		ObjectType: objectType, ObjectID: objectID,
		Distribution:    map[string]int64{"1": 0, "2": 0, "3": 0, "4": 0, "5": 0},
		ReputationScore: 0.5,
	}, Reviews: []Review{}}
	var count, starsSum int64
	var d1, d2, d3, d4, d5 int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(stars),0),
		COUNT(*) FILTER (WHERE stars = 1), COUNT(*) FILTER (WHERE stars = 2), COUNT(*) FILTER (WHERE stars = 3),
		COUNT(*) FILTER (WHERE stars = 4), COUNT(*) FILTER (WHERE stars = 5)
		FROM tokencenter_reviews WHERE object_type = $1 AND object_id = $2 AND status = 'published'`,
		objectType, objectID).Scan(&count, &starsSum, &d1, &d2, &d3, &d4, &d5); err != nil {
		return result, err
	}
	result.Summary.Count = count
	result.Summary.Distribution["1"], result.Summary.Distribution["2"], result.Summary.Distribution["3"] = d1, d2, d3
	result.Summary.Distribution["4"], result.Summary.Distribution["5"] = d4, d5
	result.Summary.ReputationScore = reputationScore(count, starsSum)
	if count > 0 {
		average := float64(starsSum) / float64(count)
		result.Summary.AverageStars = &average
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+reviewSelectColumns+` FROM tokencenter_reviews
		WHERE object_type = $1 AND object_id = $2 AND status = 'published'
		ORDER BY id DESC LIMIT $3`, objectType, objectID, limit)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		review, err := scanReview(rows)
		if err != nil {
			return result, err
		}
		result.Reviews = append(result.Reviews, review)
	}
	return result, rows.Err()
}

func (s *SQLStore) SetReviewStatus(ctx context.Context, id int64, status string) (ReviewReceipt, error) {
	receipt, err := scanReviewReceipt(s.db.QueryRowContext(ctx,
		`UPDATE tokencenter_reviews SET status = $2, updated_at = NOW() WHERE id = $1 RETURNING id, status, updated_at`, id, status))
	if err != nil {
		return ReviewReceipt{}, err
	}
	return receipt, nil
}
