package service

import (
	"context"
	"encoding/json"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// TokenSavingsConfig extends the existing channel. Prices still come from the
// normal group/channel pricing resolver; this configuration only allocates revenue.
type TokenSavingsConfig struct {
	Enabled           bool                   `json:"enabled"`
	OwnerShareBPS     int                    `json:"owner_share_bps"`
	AdminUserID       int64                  `json:"admin_user_id"`
	ReceivingGroupIDs []int64                `json:"receiving_group_ids"`
	ReceivingRules    []SavingsReceivingRule `json:"receiving_rules,omitempty"`
}

type SavingsReceivingRule struct {
	GroupID      int64    `json:"group_id"`
	Priority     int      `json:"priority"`
	AllowedPlans []string `json:"allowed_plans"`
}

func IsTokenSavingsPlan(plan string) bool {
	switch plan {
	case "pro", "prolite", "plus", "free", "team", "selfservebusinessprolite":
		return true
	}
	return false
}

func (c *TokenSavingsConfig) ReceivingRule(groupID int64) *SavingsReceivingRule {
	for i := range c.ReceivingRules {
		if c.ReceivingRules[i].GroupID == groupID {
			return &c.ReceivingRules[i]
		}
	}
	return nil
}

func (r *SavingsReceivingRule) AllowsPlan(plan string) bool {
	if r == nil || !IsTokenSavingsPlan(plan) {
		return false
	}
	for _, allowed := range r.AllowedPlans {
		if allowed == plan {
			return true
		}
	}
	return false
}

var ErrSavingsChannel = infraerrors.BadRequest("SAVINGS_CHANNEL_INVALID", "Token 储蓄渠道配置无效，请检查分成、收款管理员和接收分组")
var ErrSavingsUnavailable = infraerrors.BadRequest("SAVINGS_UNAVAILABLE", "该平台或账号套餐暂无符合条件的 Token 储蓄接收分组")

func (c *Channel) SavingsConfig() (*TokenSavingsConfig, error) {
	if c == nil || c.FeaturesConfig == nil || c.FeaturesConfig["token_savings"] == nil {
		return nil, nil
	}
	b, err := json.Marshal(c.FeaturesConfig["token_savings"])
	if err != nil {
		return nil, ErrSavingsChannel
	}
	var config TokenSavingsConfig
	if err := json.Unmarshal(b, &config); err != nil || config.OwnerShareBPS < 0 || config.OwnerShareBPS > 10000 {
		return nil, ErrSavingsChannel
	}
	if config.Enabled && (config.AdminUserID <= 0 || len(config.ReceivingGroupIDs) == 0) {
		return nil, ErrSavingsChannel
	}
	membership := map[int64]bool{}
	for _, id := range config.ReceivingGroupIDs {
		if id <= 0 || membership[id] {
			return nil, ErrSavingsChannel
		}
		membership[id] = true
	}
	seen := map[int64]bool{}
	for _, rule := range config.ReceivingRules {
		if !membership[rule.GroupID] || seen[rule.GroupID] || rule.Priority < 0 || rule.Priority > 1000 {
			return nil, ErrSavingsChannel
		}
		seen[rule.GroupID] = true
		plans := map[string]bool{}
		for _, plan := range rule.AllowedPlans {
			if !IsTokenSavingsPlan(plan) || plans[plan] {
				return nil, ErrSavingsChannel
			}
			plans[plan] = true
		}
	}
	return &config, nil
}

func (s *ChannelService) validateSavingsConfig(ctx context.Context, channel *Channel) error {
	config, err := channel.SavingsConfig()
	if err != nil || config == nil || !config.Enabled {
		return err
	}
	linked := make(map[int64]bool, len(channel.GroupIDs))
	for _, id := range channel.GroupIDs {
		linked[id] = true
	}
	for _, id := range config.ReceivingGroupIDs {
		if !linked[id] {
			return ErrSavingsChannel
		}
		g, err := s.groupRepo.GetByID(ctx, id)
		if err != nil || g == nil || g.Status != StatusActive || g.Platform == PlatformComposite {
			return ErrSavingsChannel
		}
		rule := config.ReceivingRule(id)
		if g.Platform == PlatformOpenAI && (rule == nil || len(rule.AllowedPlans) == 0) {
			return ErrSavingsChannel
		}
		if g.Platform != PlatformOpenAI && rule != nil && len(rule.AllowedPlans) > 0 {
			return ErrSavingsChannel
		}
	}
	return nil
}

// GetSavingsReceivingGroups only chooses administrator-configured receiving
// groups. Account creation and future group management use the existing flow.
func (s *ChannelService) GetSavingsReceivingGroups(ctx context.Context, platform string, verifiedPlan string) ([]int64, error) {
	channels, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := s.groupRepo.ListActiveByPlatform(ctx, platform)
	if err != nil {
		return nil, err
	}
	active := make(map[int64]bool, len(groups))
	for _, group := range groups {
		active[group.ID] = true
	}
	var selected int64
	bestPriority := -1
	for _, channel := range channels {
		if channel.Status != StatusActive {
			continue
		}
		config, err := channel.SavingsConfig()
		if err != nil {
			return nil, err
		}
		if config == nil || !config.Enabled {
			continue
		}
		linked := map[int64]bool{}
		for _, id := range channel.GroupIDs {
			linked[id] = true
		}
		for _, id := range config.ReceivingGroupIDs {
			if active[id] && linked[id] {
				rule := config.ReceivingRule(id)
				if platform == PlatformOpenAI && !rule.AllowsPlan(verifiedPlan) {
					continue
				}
				priority := 0
				if rule != nil {
					priority = rule.Priority
				}
				if priority > bestPriority || (priority == bestPriority && (selected == 0 || id < selected)) {
					selected, bestPriority = id, priority
				}
			}
		}
	}
	if selected == 0 {
		return nil, ErrSavingsUnavailable
	}
	return []int64{selected}, nil
}
