package service

import (
	"context"
	"encoding/json"
	"sort"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// TokenSavingsConfig extends the existing channel. Prices still come from the
// normal group/channel pricing resolver; this configuration only allocates revenue.
type TokenSavingsConfig struct {
	Enabled           bool    `json:"enabled"`
	OwnerShareBPS     int     `json:"owner_share_bps"`
	AdminUserID       int64   `json:"admin_user_id"`
	ReceivingGroupIDs []int64 `json:"receiving_group_ids"`
}

var ErrSavingsChannel = infraerrors.BadRequest("SAVINGS_CHANNEL_INVALID", "Token 储蓄渠道配置无效，请检查分成、收款管理员和接收分组")
var ErrSavingsUnavailable = infraerrors.BadRequest("SAVINGS_UNAVAILABLE", "该平台尚未配置 Token 储蓄接收分组")

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
	}
	return nil
}

// GetSavingsReceivingGroups only chooses administrator-configured receiving
// groups. Account creation and future group management use the existing flow.
func (s *ChannelService) GetSavingsReceivingGroups(ctx context.Context, platform string) ([]int64, error) {
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
	selected := map[int64]bool{}
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
				selected[id] = true
			}
		}
	}
	ids := make([]int64, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return nil, ErrSavingsUnavailable
	}
	return ids, nil
}
