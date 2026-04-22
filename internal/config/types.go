package config

// Constants used throughout the glocker application
const (
	InstallPath          = "/usr/local/bin/glocker"
	GlocklockInstallPath = "/usr/local/bin/glocklock"
	GlockpeekInstallPath = "/usr/local/bin/glockpeek"
	GlockerConfigFile    = "/etc/glocker/config.yaml"
	HostsMarkerStart     = "### GLOCKER START ###"
	SudoersPath          = "/etc/sudoers"
	SudoersBackup        = "/etc/sudoers.glocker.backup"
	SudoersMarker        = "# GLOCKER-MANAGED"
	SystemdFile          = "./extras/glocker.service"
	GlockerSock          = "/tmp/glocker.sock"
	EmailCooldownMinutes = 15 // Minimum time between emails for the same event type
)

// TimeWindow represents a time-based blocking window with specific days.
type TimeWindow struct {
	Start string   `yaml:"start"` // HH:MM format
	End   string   `yaml:"end"`   // HH:MM format
	Days  []string `yaml:"days"`  // Mon, Tue, Wed, Thu, Fri, Sat, Sun
}

// Domain represents a domain to be blocked with its blocking rules.
// BlockWindows names the semantic explicitly: the domain is BLOCKED during
// these windows (and accessible outside them). An empty list means always
// blocked.
type Domain struct {
	Name         string       `yaml:"name"`
	BlockWindows []TimeWindow `yaml:"block_windows,omitempty"`
	LogBlocking  bool         `yaml:"log_blocking,omitempty"`
	Unblockable  bool         `yaml:"unblockable,omitempty"` // Set to true to allow temporary unblocking (default: false = permanent)
}

// SudoersConfig controls sudo access restrictions.
// AllowWindows names the semantic explicitly: sudo is ALLOWED during these
// windows (and blocked outside them). An empty list means sudo is always
// blocked while sudoers enforcement is enabled.
type SudoersConfig struct {
	Enabled            bool         `yaml:"enabled"`
	User               string       `yaml:"user"`
	AllowedSudoersLine string       `yaml:"allowed_sudoers_line"`
	BlockedSudoersLine string       `yaml:"blocked_sudoers_line"`
	AllowWindows       []TimeWindow `yaml:"allow_windows"`
}

// AccountabilityConfig configures email notifications via Mailgun.
type AccountabilityConfig struct {
	Enabled            bool   `yaml:"enabled"`
	PartnerEmail       string `yaml:"partner_email"`
	FromEmail          string `yaml:"from_email"`
	ApiKey             string `yaml:"api_key"`
	DailyReportTime    string `yaml:"daily_report_time"`
	DailyReportEnabled bool   `yaml:"daily_report_enabled"`
}

// TamperConfig controls file integrity monitoring and tamper detection.
type TamperConfig struct {
	Enabled       bool   `yaml:"enabled"`
	CheckInterval int    `yaml:"check_interval_seconds"`
	AlarmCommand  string `yaml:"alarm_command"`
}

// WebTrackingConfig controls the web tracking server for browser integration.
type WebTrackingConfig struct {
	Enabled bool   `yaml:"enabled"`
	Command string `yaml:"command"`
}

// ContentMonitoringConfig controls content/keyword monitoring via browser extension.
type ContentMonitoringConfig struct {
	Enabled bool   `yaml:"enabled"`
	LogFile string `yaml:"log_file"`
}

// ExtensionKeywordsConfig defines keywords for browser extension monitoring.
type ExtensionKeywordsConfig struct {
	URLKeywords     []string `yaml:"url_keywords"`
	ContentKeywords []string `yaml:"content_keywords"`
	Whitelist       []string `yaml:"whitelist"`
}

// ViolationTrackingConfig controls violation threshold tracking and enforcement.
type ViolationTrackingConfig struct {
	Enabled           bool   `yaml:"enabled"`
	MaxViolations     int    `yaml:"max_violations"`
	TimeWindowMinutes int    `yaml:"time_window_minutes"`
	Command           string `yaml:"command"`
	ResetDaily        bool   `yaml:"reset_daily"`
	ResetTime         string `yaml:"reset_time"`
	LockDuration      string `yaml:"lock_duration"`  // Duration for screen lock (e.g., "1m", "5m")
	MindfulText       string `yaml:"mindful_text"`   // Text that must be typed to unlock
	Background        string `yaml:"background"`     // Path to PNG/JPG background image
}

// UnblockingConfig controls temporary unblocking behavior.
type UnblockingConfig struct {
	Reasons         []string `yaml:"reasons"`
	LogFile         string   `yaml:"log_file"`
	TempUnblockTime int      `yaml:"temp_unblock_time"` // Minutes
}

// LifecycleConfig controls install/uninstall logging behavior.
type LifecycleConfig struct {
	LogFile string   `yaml:"log_file"`
	Reasons []string `yaml:"reasons"`
}

// ForbiddenProgram represents a program to be killed during blocking periods.
// KillWindows lists times when the program is KILLED; AllowWindows lists
// times when it is permitted. Precedence at evaluation time:
//   - In any KillWindow → killed.
//   - Else AllowWindows non-empty → killed iff not in any AllowWindow.
//   - Else both empty → always killed (legacy default).
//   - Else (only KillWindows, no match) → allowed.
type ForbiddenProgram struct {
	Name         string       `yaml:"name"`
	KillWindows  []TimeWindow `yaml:"kill_windows"`
	AllowWindows []TimeWindow `yaml:"allow_windows"`
}

// ForbiddenProgramsConfig controls process killing behavior.
type ForbiddenProgramsConfig struct {
	Enabled       bool               `yaml:"enabled"`
	CheckInterval int                `yaml:"check_interval_seconds"`
	Programs      []ForbiddenProgram `yaml:"programs"`
}

// Config is the main configuration structure for glocker.
type Config struct {
	EnableHosts             bool                    `yaml:"enable_hosts"`
	EnableFirewall          bool                    `yaml:"enable_firewall"`
	EnableForbiddenPrograms bool                    `yaml:"enable_forbidden_programs"`
	Domains                 []Domain                `yaml:"domains"`
	HostsPath               string                  `yaml:"hosts_path"`
	SelfHeal                bool                    `yaml:"enable_self_healing"`
	EnforceInterval         int                     `yaml:"enforce_interval_seconds"`
	Sudoers                 SudoersConfig           `yaml:"sudoers"`
	TamperDetection         TamperConfig            `yaml:"tamper_detection"`
	Accountability          AccountabilityConfig    `yaml:"accountability"`
	WebTracking             WebTrackingConfig       `yaml:"web_tracking"`
	ContentMonitoring       ContentMonitoringConfig `yaml:"content_monitoring"`
	ForbiddenPrograms       ForbiddenProgramsConfig `yaml:"forbidden_programs"`
	ExtensionKeywords       ExtensionKeywordsConfig `yaml:"extension_keywords"`
	ViolationTracking       ViolationTrackingConfig `yaml:"violation_tracking"`
	Unblocking              UnblockingConfig        `yaml:"unblocking"`
	Lifecycle               LifecycleConfig         `yaml:"lifecycle"`
	MindfulDelay            int                     `yaml:"mindful_delay"` // Seconds
	NotificationCommand     string                  `yaml:"notification_command"`
	PanicCommand            string                  `yaml:"panic_command"`
	Dev                     bool                    `yaml:"dev"`
	LogLevel                string                  `yaml:"log_level"`
}
