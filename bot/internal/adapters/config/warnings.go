package config

import (
	"fmt"
	"reflect"
	"regexp"
	"time"
)

// Warning represents a configuration warning
type Warning struct {
	Field   string
	Message string
}

// WarningsManager manages all configuration warnings
type WarningsManager struct {
	warnings []Warning
}

// NewWarningsManager creates a new warnings manager
func NewWarningsManager() *WarningsManager {
	return &WarningsManager{
		warnings: make([]Warning, 0),
	}
}

// AddWarning adds a warning to the manager
func (wm *WarningsManager) AddWarning(field, message string) {
	wm.warnings = append(wm.warnings, Warning{
		Field:   field,
		Message: message,
	})
}

// AddWarningf adds a formatted warning to the manager
func (wm *WarningsManager) AddWarningf(field, format string, args ...interface{}) {
	wm.warnings = append(wm.warnings, Warning{
		Field:   field,
		Message: fmt.Sprintf(format, args...),
	})
}

// PrintWarnings prints all warnings to stdout
func (wm *WarningsManager) PrintWarnings() {
	for _, warning := range wm.warnings {
		fmt.Printf("Warning: %s: %s\n", warning.Field, warning.Message)
	}
}

// HasWarnings returns true if there are any warnings
func (wm *WarningsManager) HasWarnings() bool {
	return len(wm.warnings) > 0
}

// CheckEmptyString warns if a string field is empty
func (wm *WarningsManager) CheckEmptyString(field, value, context string) {
	if value == "" {
		wm.AddWarning(field, fmt.Sprintf("is empty, %s", context))
	}
}

// CheckEmptySlice warns if a slice field is empty
func (wm *WarningsManager) CheckEmptySlice(field string, value interface{}, context string) {
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Slice && v.Len() == 0 {
		wm.AddWarning(field, fmt.Sprintf("is empty, %s", context))
	}
}

// CheckZeroInt64 warns if an int64 field is zero (when it shouldn't be)
func (wm *WarningsManager) CheckZeroInt64(field string, value int64, context string) {
	if value == 0 {
		wm.AddWarning(field, fmt.Sprintf("is not configured, %s", context))
	}
}

// CheckZeroInt64Slice warns if an int64 slice contains a zero value.
func (wm *WarningsManager) CheckZeroInt64Slice(field string, values []int64, context string) {
	for i, value := range values {
		if value == 0 {
			wm.AddWarningf(field, "contains zero value at index %d, %s", i, context)
		}
	}
}

// CheckConditionalInt64 warns if an int64 field is zero when a condition is true
func (wm *WarningsManager) CheckConditionalInt64(field string, value int64, condition bool, context string) {
	if condition && value == 0 {
		wm.AddWarning(field, fmt.Sprintf("is not configured but %s", context))
	}
}

// CheckZeroDuration warns if a duration field is zero
func (wm *WarningsManager) CheckZeroDuration(field string, value time.Duration, context string) {
	if value == 0 {
		wm.AddWarning(field, fmt.Sprintf("is zero, %s", context))
	}
}

// CheckConditionalString warns if a string field is empty when a condition is true
func (wm *WarningsManager) CheckConditionalString(field, value string, condition bool, context string) {
	if condition && value == "" {
		wm.AddWarning(field, fmt.Sprintf("is empty but %s", context))
	}
}

// extractHostFromDSN extracts host from PostgreSQL DSN string
func extractHostFromDSN(dsn string) string {
	hostRegex := regexp.MustCompile(`host=([^ ]+)`)
	matches := hostRegex.FindStringSubmatch(dsn)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractUserFromDSN extracts user from PostgreSQL DSN string
func extractUserFromDSN(dsn string) string {
	userRegex := regexp.MustCompile(`user=([^ ]+)`)
	matches := userRegex.FindStringSubmatch(dsn)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractDBNameFromDSN extracts database name from PostgreSQL DSN string
func extractDBNameFromDSN(dsn string) string {
	dbRegex := regexp.MustCompile(`dbname=([^ ]+)`)
	matches := dbRegex.FindStringSubmatch(dsn)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// ValidateConfig validates the entire configuration and collects warnings
func (wm *WarningsManager) ValidateConfig(cfg *Config) {
	// Bot warnings (critical fields)
	wm.CheckEmptyString("Bot.Token", cfg.Bot.Token(), "bot functionality may not work")
	wm.CheckEmptySlice("Bot.AdminIDs", cfg.Bot.AdminIDs(), "admin functionality may not work properly")
	wm.CheckEmptySlice("Bot.ValidEmailDomains", cfg.Bot.ValidEmailDomains(), "email domain validation may not work")
	wm.CheckEmptySlice("Bot.GrantChatIDs", cfg.Bot.GrantChatIDs(), "grant chat functionality may not work")
	wm.CheckZeroInt64Slice("Bot.GrantChatIDs", cfg.Bot.GrantChatIDs(), "grant chat functionality may not work")

	// Bot channel warnings
	wm.CheckZeroInt64("Bot.MailingChannelID", cfg.Bot.MailingChannelID(), "mailing functionality may not work")
	wm.CheckZeroInt64("Bot.AvatarChannelID", cfg.Bot.AvatarChannelID(), "avatar uploads may not work")
	wm.CheckZeroInt64("Bot.IntroChannelID", cfg.Bot.IntroChannelID(), "intro uploads may not work")
	wm.CheckZeroInt64("Bot.PassChannelID", cfg.Bot.PassChannelID(), "pass functionality may not work")
	wm.CheckZeroInt64("Bot.QRChannelID", cfg.Bot.QRChannelID(), "QR functionality may not work")

	// Logger warnings
	wm.CheckConditionalInt64("Logger.ChannelID", cfg.Logger.ChannelID(), cfg.Logger.LogToChannel(), "LogToChannel is enabled")
	wm.CheckConditionalString("Logger.LogsDir", cfg.Logger.LogsDir(), cfg.Logger.LogToFile(), "LogToFile is enabled")

	// App warnings
	wm.CheckEmptyString("App.Timezone", cfg.App.Timezone(), "timezone functionality may not work")
	wm.CheckConditionalInt64("App.VersionChannelID", cfg.App.VersionChannelID(), cfg.App.VersionNotifyOnStartup(), "VersionNotifyOnStartup is enabled")
	wm.CheckEmptySlice("App.PassLocationSubstrings", cfg.App.PassLocationSubstrings(), "pass location validation may not work")
	wm.CheckEmptySlice("App.PassEmails", cfg.App.PassEmails(), "pass email notifications may not work")
	wm.CheckEmptySlice("App.PassExcludedRoles", cfg.App.PassExcludedRoles(), "pass role validation may not work")
	wm.CheckEmptyString("App.EmailConfirmationTemplate", cfg.App.EmailConfirmationTemplate(), "email confirmation may not work")
	wm.CheckEmptyString("App.QRLogoPath", cfg.App.QRLogoPath(), "QR codes may not have logo")

	// SMTP warnings (critical for email functionality)
	wm.CheckEmptyString("SMTP.Host", cfg.SMTP.Host(), "SMTP functionality may not work")
	wm.CheckZeroInt64("SMTP.Port", int64(cfg.SMTP.Port()), "SMTP functionality may not work")
	wm.CheckEmptyString("SMTP.Login", cfg.SMTP.Login(), "SMTP functionality may not work")
	wm.CheckEmptyString("SMTP.Password", cfg.SMTP.Password(), "SMTP functionality may not work")
	wm.CheckEmptyString("SMTP.Email", cfg.SMTP.Email(), "SMTP functionality may not work")
	wm.CheckEmptyString("SMTP.Domain", cfg.SMTP.Domain(), "SMTP functionality may not work")

	// PostgreSQL warnings (critical for database)
	wm.CheckEmptyString("PostgreSQL.Host", extractHostFromDSN(cfg.PG.DSN()), "database connection may not work")
	wm.CheckEmptyString("PostgreSQL.User", extractUserFromDSN(cfg.PG.DSN()), "database connection may not work")
	wm.CheckEmptyString("PostgreSQL.Database", extractDBNameFromDSN(cfg.PG.DSN()), "database connection may not work")

	// Redis warnings (critical for caching and sessions)
	wm.CheckEmptyString("Redis.Host", cfg.RedisConf.Host(), "Redis functionality may not work")
	wm.CheckEmptyString("Redis.Port", cfg.RedisConf.Port(), "Redis functionality may not work")
	wm.CheckEmptyString("Redis.Password", cfg.RedisConf.Password(), "Redis functionality may not work")

	// Session warnings (check for zero duration which might indicate missing config)
	wm.CheckZeroDuration("Session.TTL", cfg.Session.TTL(), "session management may not work")
	wm.CheckZeroDuration("Session.AuthTTL", cfg.Session.AuthTTL(), "auth sessions may not work")
	wm.CheckZeroDuration("Session.ResendTTL", cfg.Session.ResendTTL(), "resend functionality may not work")
	wm.CheckZeroDuration("Session.EmailTTL", cfg.Session.EmailTTL(), "email sessions may not work")
	wm.CheckZeroDuration("Session.EventIDTTL", cfg.Session.EventIDTTL(), "event sessions may not work")

	// Banner warnings (these are required, but we'll warn instead of error for some)
	wm.CheckEmptyString("Banner.AuthID", cfg.Banner.AuthID(), "auth banner may not work")
	wm.CheckEmptyString("Banner.MenuID", cfg.Banner.MenuID(), "menu banner may not work")
	wm.CheckEmptyString("Banner.PersonalAccountID", cfg.Banner.PersonalAccountID(), "personal account banner may not work")
	wm.CheckEmptyString("Banner.ClubsID", cfg.Banner.ClubsID(), "clubs banner may not work")
	wm.CheckEmptyString("Banner.ClubOwnerID", cfg.Banner.ClubOwnerID(), "club owner banner may not work")
	wm.CheckEmptyString("Banner.EventsID", cfg.Banner.EventsID(), "events banner may not work")
}
