package config

import (
	"reflect"

	"github.com/spf13/viper"
)

type BotConfig interface {
	Token() string
	AdminIDs() []int64
	GrantChatIDs() []int64
	MailingChannelID() int64
	AvatarChannelID() int64
	IntroChannelID() int64
	PassChannelID() int64
	QRChannelID() int64
	ValidEmailDomains() []string
}

type botConfig struct {
	token             string
	adminIDs          []int64
	grantChatIDs      []int64
	mailingChannelID  int64
	avatarChannelID   int64
	introChannelID    int64
	passChannelID     int64
	qrChannelID       int64
	validEmailDomains []string
}

func NewBotConfig() BotConfig {
	return &botConfig{
		token:             viper.GetString("bot.token"),
		adminIDs:          intSliceToInt64Slice(viper.GetIntSlice("bot.admin-ids")),
		grantChatIDs:      getInt64Slice("bot.auth.grant-chat-id"),
		mailingChannelID:  viper.GetInt64("bot.mailing.channel-id"),
		avatarChannelID:   viper.GetInt64("bot.avatar.channel-id"),
		introChannelID:    viper.GetInt64("bot.intro.channel-id"),
		passChannelID:     viper.GetInt64("settings.pass.channel-id"),
		qrChannelID:       viper.GetInt64("bot.qr.channel-id"),
		validEmailDomains: viper.GetStringSlice("bot.auth.valid-email-domains"),
	}
}

func intSliceToInt64Slice(values []int) []int64 {
	result := make([]int64, len(values))
	for i, v := range values {
		result[i] = int64(v)
	}
	return result
}

func getInt64Slice(key string) []int64 {
	value := viper.Get(key)
	if value == nil {
		return nil
	}

	kind := reflect.TypeOf(value).Kind()
	if kind == reflect.Slice || kind == reflect.Array {
		return intSliceToInt64Slice(viper.GetIntSlice(key))
	}

	return []int64{viper.GetInt64(key)}
}

func (cfg *botConfig) Token() string {
	return cfg.token
}

func (cfg *botConfig) AdminIDs() []int64 {
	return cfg.adminIDs
}

func (cfg *botConfig) GrantChatIDs() []int64 {
	return cfg.grantChatIDs
}

func (cfg *botConfig) MailingChannelID() int64 {
	return cfg.mailingChannelID
}

func (cfg *botConfig) AvatarChannelID() int64 {
	return cfg.avatarChannelID
}

func (cfg *botConfig) IntroChannelID() int64 {
	return cfg.introChannelID
}

func (cfg *botConfig) PassChannelID() int64 {
	return cfg.passChannelID
}

func (cfg *botConfig) QRChannelID() int64 {
	return cfg.qrChannelID
}

func (cfg *botConfig) ValidEmailDomains() []string {
	return cfg.validEmailDomains
}
