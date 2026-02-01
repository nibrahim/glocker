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
type Domain struct {
	Name        string       `yaml:"name"`
	TimeWindows []TimeWindow `yaml:"time_windows,omitempty"`
	LogBlocking bool         `yaml:"log_blocking,omitempty"`
	Unblockable bool         `yaml:"unblockable,omitempty"` // Set to true to allow temporary unblocking (default: false = permanent)
}

// SudoersConfig controls sudo access restrictions.
type SudoersConfig struct {
	Enabled            bool         `yaml:"enabled"`
	User               string       `yaml:"user"`
	AllowedSudoersLine string       `yaml:"allowed_sudoers_line"`
	BlockedSudoersLine string       `yaml:"blocked_sudoers_line"`
	TimeAllowed        []TimeWindow `yaml:"time_allowed"`
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

// UninstallConfig controls uninstall logging behavior.
type UninstallConfig struct {
	LogFile string `yaml:"log_file"`
}

// ForbiddenProgram represents a program to be killed during blocking periods.
type ForbiddenProgram struct {
	Name        string       `yaml:"name"`
	TimeWindows []TimeWindow `yaml:"time_windows"`
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
	Uninstall               UninstallConfig         `yaml:"uninstall"`
	MindfulDelay            int                     `yaml:"mindful_delay"` // Seconds
	NotificationCommand     string                  `yaml:"notification_command"`
	PanicCommand            string                  `yaml:"panic_command"`
	Dev                     bool                    `yaml:"dev"`
	LogLevel                string                  `yaml:"log_level"`
}
