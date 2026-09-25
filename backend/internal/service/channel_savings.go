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
	AccountTypes []string `json:"account_types"`
}

func IsTokenSavingsPlan(plan string) bool {
	switch plan {
	case "pro", "prolite", "plus", "max", "team", "selfservebusinessprolite":
		return true
	}
	return false
}

func IsTokenSavingsPlanForPlatform(platform, plan string) bool {
	if platform == PlatformAnthropic {
		return plan == "pro" || plan == "max"
	}
	return platform == PlatformOpenAI && plan != "max" && IsTokenSavingsPlan(plan)
}

// Missing types preserve official subscription OAuth only. An explicit empty
// array disables receiving, including for an otherwise valid subscription plan.
func (r *SavingsReceivingRule) TypesForPlatform(platform string) []string {
	if r != nil && r.AccountTypes != nil {
		return r.AccountTypes
	}
	if platform == PlatformOpenAI || platform == PlatformAnthropic {
		return []string{AccountTypeOAuth}
	}
	return []string{}
}

func (r *SavingsReceivingRule) AllowsAccount(platform, accountType, plan string) bool {
	if !r.validForPlatform(platform) {
		return false
	}
	allowed := false
	for _, typ := range r.TypesForPlatform(platform) {
		if typ == accountType {
			allowed = true
		}
	}
	if !allowed {
		return false
	}
	if accountType == AccountTypeAPIKey {
		return true
	}
	return accountType == AccountTypeOAuth && IsTokenSavingsPlanForPlatform(platform, plan) && r.AllowsPlan(plan)
}

func (r *SavingsReceivingRule) validForPlatform(platform string) bool {
	oauth := false
	for _, typ := range r.TypesForPlatform(platform) {
		if typ == AccountTypeOAuth {
			oauth = true
		}
	}
	if oauth {
		if platform != PlatformOpenAI && platform != PlatformAnthropic || r == nil || len(r.AllowedPlans) == 0 {
			return false
		}
		for _, plan := range r.AllowedPlans {
			if !IsTokenSavingsPlanForPlatform(platform, plan) {
				return false
			}
		}
	} else if r != nil && len(r.AllowedPlans) > 0 {
		return false
	}
	return true
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
		types := map[string]bool{}
		for _, typ := range rule.AccountTypes {
			if (typ != AccountTypeOAuth && typ != AccountTypeAPIKey) || types[typ] {
				return nil, ErrSavingsChannel
			}
			types[typ] = true
		}
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
		if !rule.validForPlatform(g.Platform) {
			return ErrSavingsChannel
		}
	}
	return nil
}

type SavingsPlatformCapability struct {
	Platform     string   `json:"platform"`
	AccountTypes []string `json:"account_types"`
}

type savingsReceivingGroup struct {
	group Group
	rule  *SavingsReceivingRule
}

// Both discovery and admission use the same active channel/group configuration.
func (s *ChannelService) savingsReceivingGroups(ctx context.Context) ([]savingsReceivingGroup, error) {
	channels, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := []savingsReceivingGroup{}
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
			if !linked[id] {
				continue
			}
			group, err := s.groupRepo.GetByID(ctx, id)
			if err != nil {
				return nil, err
			}
			if group == nil || group.Status != StatusActive || group.Platform == PlatformComposite {
				continue
			}
			rule := config.ReceivingRule(id)
			if !rule.validForPlatform(group.Platform) {
				continue
			}
			out = append(out, savingsReceivingGroup{group: *group, rule: rule})
		}
	}
	return out, nil
}

func (s *ChannelService) GetSavingsCapabilities(ctx context.Context) ([]SavingsPlatformCapability, error) {
	groups, err := s.savingsReceivingGroups(ctx)
	if err != nil {
		return nil, err
	}
	platforms := map[string]map[string]bool{}
	for _, entry := range groups {
		for _, typ := range entry.rule.TypesForPlatform(entry.group.Platform) {
			if platforms[entry.group.Platform] == nil {
				platforms[entry.group.Platform] = map[string]bool{}
			}
			platforms[entry.group.Platform][typ] = true
		}
	}
	out := []SavingsPlatformCapability{}
	for platform, types := range platforms {
		capability := SavingsPlatformCapability{Platform: platform, AccountTypes: []string{}}
		for _, typ := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			if types[typ] {
				capability.AccountTypes = append(capability.AccountTypes, typ)
			}
		}
		out = append(out, capability)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Platform < out[j].Platform })
	return out, nil
}

// GetSavingsReceivingGroups selects exactly one configured receiving group.
func (s *ChannelService) GetSavingsReceivingGroups(ctx context.Context, platform, accountType, verifiedPlan string) ([]int64, error) {
	groups, err := s.savingsReceivingGroups(ctx)
	if err != nil {
		return nil, err
	}
	var selected int64
	bestPriority := -1
	for _, entry := range groups {
		if entry.group.Platform != platform || !entry.rule.AllowsAccount(platform, accountType, verifiedPlan) {
			continue
		}
		priority := 0
		if entry.rule != nil {
			priority = entry.rule.Priority
		}
		if priority > bestPriority || (priority == bestPriority && (selected == 0 || entry.group.ID < selected)) {
			selected, bestPriority = entry.group.ID, priority
		}
	}
	if selected == 0 {
		return nil, ErrSavingsUnavailable
	}
	return []int64{selected}, nil
}
