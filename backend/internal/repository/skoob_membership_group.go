package repository

import (
	"context"
	"fmt"
	"strings"
)

// Include off-sale plans: taking a membership off sale does not turn it into model access.
func loadSkoobMembershipGroups(ctx context.Context, sqlq sqlExecutor, groupIDs []int64) (map[int64]bool, error) {
	ids := make(map[int64]bool)
	if len(groupIDs) == 0 {
		return ids, nil
	}
	placeholders := make([]string, len(groupIDs))
	args := make([]any, len(groupIDs))
	for i, id := range groupIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := sqlq.QueryContext(ctx, `SELECT DISTINCT group_id FROM subscription_plans
		WHERE group_id IN (`+strings.Join(placeholders, ",")+`) AND lower(trim(product_name)) = 'skoob'`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}
