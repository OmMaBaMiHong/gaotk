//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type savingsChannelRepo struct {
	ChannelRepository
	channels []Channel
}

func (r savingsChannelRepo) ListAll(context.Context) ([]Channel, error) { return r.channels, nil }

type savingsGroupRepo struct {
	GroupRepository
	groups []Group
}

func (r savingsGroupRepo) ListActiveByPlatform(_ context.Context, platform string) ([]Group, error) {
	var groups []Group
	for _, g := range r.groups {
		if g.Platform == platform && g.Status == StatusActive {
			groups = append(groups, g)
		}
	}
	return groups, nil
}

func (r savingsGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	for _, g := range r.groups {
		if g.ID == id {
			return &g, nil
		}
	}
	return nil, ErrGroupNotFound
}

func savingsTestChannel(groups, receiving []int64) Channel {
	return Channel{ID: 1, Status: StatusActive, GroupIDs: groups, FeaturesConfig: map[string]any{
		"token_savings": TokenSavingsConfig{Enabled: true, OwnerShareBPS: 8000, AdminUserID: 1, ReceivingGroupIDs: receiving},
	}}
}

func TestSavingsReceivingGroupsUsesExistingChannelAndPlatform(t *testing.T) {
	first := savingsTestChannel([]int64{11, 12, 13}, []int64{11, 12, 99})
	second := savingsTestChannel([]int64{14}, []int64{14})
	second.ID = 2
	disabled := savingsTestChannel([]int64{15}, []int64{15})
	disabled.Status = StatusDisabled
	s := &ChannelService{
		repo: savingsChannelRepo{channels: []Channel{first, second, disabled}},
		groupRepo: savingsGroupRepo{groups: []Group{
			{ID: 11, Platform: PlatformOpenAI, Status: StatusActive},
			{ID: 12, Platform: PlatformAnthropic, Status: StatusActive},
			{ID: 13, Platform: PlatformOpenAI, Status: StatusActive},
			{ID: 14, Platform: PlatformOpenAI, Status: StatusActive},
			{ID: 15, Platform: PlatformOpenAI, Status: StatusActive},
			{ID: 99, Platform: PlatformOpenAI, Status: StatusActive},
		}},
	}
	groups, err := s.GetSavingsReceivingGroups(context.Background(), PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, []int64{11, 14}, groups, "only configured, linked, active groups of the selected platform")
	_, err = s.GetSavingsReceivingGroups(context.Background(), PlatformDeepseek)
	require.ErrorIs(t, err, ErrSavingsUnavailable)
}

func TestSavingsConfigRejectsInvalidShareAndReceivingGroups(t *testing.T) {
	for _, share := range []any{-1, 10001, 80.5, "8000"} {
		channel := Channel{FeaturesConfig: map[string]any{"token_savings": map[string]any{"enabled": true, "owner_share_bps": share, "admin_user_id": 1, "receiving_group_ids": []int64{1}}}}
		_, err := channel.SavingsConfig()
		require.ErrorIs(t, err, ErrSavingsChannel)
	}
	s := &ChannelService{groupRepo: savingsGroupRepo{groups: []Group{{ID: 1, Platform: PlatformOpenAI, Status: StatusActive}, {ID: 2, Platform: PlatformComposite, Status: StatusActive}}}}
	valid := savingsTestChannel([]int64{1}, []int64{1})
	require.NoError(t, s.validateSavingsConfig(context.Background(), &valid))
	outside := savingsTestChannel([]int64{1}, []int64{3})
	require.ErrorIs(t, s.validateSavingsConfig(context.Background(), &outside), ErrSavingsChannel)
	composite := savingsTestChannel([]int64{2}, []int64{2})
	require.ErrorIs(t, s.validateSavingsConfig(context.Background(), &composite), ErrSavingsChannel)
	legacy := Channel{FeaturesConfig: map[string]any{"web_search": true}}
	require.NoError(t, s.validateSavingsConfig(context.Background(), &legacy), "existing channel configs are unaffected")
}
