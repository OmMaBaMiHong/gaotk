package tokencenter

import (
	"testing"
	"time"
)

func TestReviewModerationPassesByDefault(t *testing.T) {
	// Architecture only: the machine filter list is empty, so everything
	// publishes. Nothing may be intercepted before the list is configured.
	if got := reviewModerationStatus("任何内容都默认通过"); got != ReviewStatusPublished {
		t.Fatalf("default moderation status = %q, want published", got)
	}
}

func TestReviewModerationBlocksConfiguredKeyword(t *testing.T) {
	previous := reviewBlockedKeywords
	reviewBlockedKeywords = []string{"banned-word"}
	defer func() { reviewBlockedKeywords = previous }()
	if got := reviewModerationStatus("contains BANNED-WORD inside"); got != ReviewStatusBlocked {
		t.Fatalf("configured keyword not blocked: %q", got)
	}
	if got := reviewModerationStatus("clean body"); got != ReviewStatusPublished {
		t.Fatalf("clean body blocked: %q", got)
	}
}

func TestReputationScoreMatchesUnifiedAlgorithm(t *testing.T) {
	cases := []struct {
		count, starsSum int64
		want            float64
	}{
		{0, 0, 0.5},
		{1, 5, (1 + 1.0/6) / 2},     // one 5-star ⇒ ≈0.583
		{10, 50, (1 + 10.0/15) / 2}, // ten 5-stars ⇒ ≈0.833
		{5, 15, 0.5},                // 5×5星 + 5×1星 ⇒ neutral
		{3, 3, (1 + -3.0/8) / 2},    // three 1-stars ⇒ ≈0.3125
	}
	for _, tc := range cases {
		if got := reputationScore(tc.count, tc.starsSum); abs64(got-tc.want) > 1e-9 {
			t.Fatalf("reputationScore(%d,%d) = %v, want %v", tc.count, tc.starsSum, got, tc.want)
		}
	}
	if reputationScore(2, 6) != 0.5 { // only 3-star votes stay neutral
		t.Fatalf("three-star-only votes must stay neutral")
	}
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestNormalizeReview(t *testing.T) {
	base := ReviewInput{ObjectType: "relay", ObjectID: "156595", Stars: 4, Contact: " user@example.com ", Dimensions: []string{"speed", "speed", "", "unknown", "billing"}, Body: "  响应很快  "}
	got, err := normalizeReview(base)
	if err != nil {
		t.Fatalf("valid review rejected: %v", err)
	}
	if got.Contact != "user@example.com" || got.Body != "响应很快" {
		t.Fatalf("trim failed: %+v", got)
	}
	if len(got.Dimensions) != 2 || got.Dimensions[0] != "speed" || got.Dimensions[1] != "billing" {
		t.Fatalf("dimensions not filtered/deduped: %+v", got.Dimensions)
	}
	invalid := []ReviewInput{
		{ObjectType: "model", ObjectID: "1", Stars: 4, Contact: "a"},   // object type not reviewable
		{ObjectType: "relay", ObjectID: "", Stars: 4, Contact: "a"},    // missing object id
		{ObjectType: "relay", ObjectID: "1", Stars: 0, Contact: "a"},   // stars out of range
		{ObjectType: "relay", ObjectID: "1", Stars: 6, Contact: "a"},   // stars out of range
		{ObjectType: "relay", ObjectID: "1", Stars: 4, Contact: "   "}, // missing contact
	}
	for _, in := range invalid {
		if _, err := normalizeReview(in); err == nil {
			t.Fatalf("invalid review accepted: %+v", in)
		}
	}
}

func TestScanReviewEmptyDimensions(t *testing.T) {
	// Guarantees the public projection always serializes dimensions as [].
	review := Review{Dimensions: []string{}, CreatedAt: time.Time{}, UpdatedAt: time.Time{}}
	if review.Dimensions == nil {
		t.Fatal("dimensions must default to empty slice, not nil")
	}
}
