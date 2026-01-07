package notify

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mailgun/mailgun-go/v4"

	"glocker/internal/config"
	"glocker/internal/state"
)

// SendEmail sends an email notification via Mailgun with rate limiting.
// Returns nil if email is disabled, in dev mode, or rate limited.
func SendEmail(cfg *config.Config, subject, body string) error {
	if !cfg.Accountability.Enabled {
		return nil
	}

	// Skip sending emails in dev mode
	if cfg.Dev {
		log.Printf("DEV MODE: Skipping email send - Subject: %s, Body: %s", subject, body)
		return nil
	}

	// Rate limiting: check if we've sent this type of email recently
	lastSent, exists := state.GetLastEmailTime(subject)
	now := time.Now()
	if exists && now.Sub(lastSent) < config.EmailCooldownMinutes*time.Minute {
		log.Printf("Email rate limited - Subject: %s (last sent %v ago)", subject, now.Sub(lastSent).Round(time.Second))
		return nil
	}
	state.SetLastEmailTime(subject, now)

	from := cfg.Accountability.FromEmail
	to := cfg.Accountability.PartnerEmail
	apiKey := cfg.Accountability.ApiKey
	log.Printf("Sending email from %s to %s subject %s", from, to, subject)

	mg := mailgun.NewMailgun("noufalibrahim.name", apiKey)

	// Convert plain text body to HTML
	htmlBody := GenerateHTMLEmail(subject, body)

	mail := mailgun.NewMessage(
		from,
		subject,
		body, // Keep plain text as fallback
		to,
	)

	// Set HTML content
	mail.SetHTML(htmlBody)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	_, _, err := mg.Send(ctx, mail)

	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}

// GenerateHTMLEmail creates a styled HTML email from plain text content.
// Escapes HTML characters and applies styling based on the subject line.
func GenerateHTMLEmail(subject, plainBody string) string {
	// Escape HTML characters in the plain text body
	htmlBody := strings.ReplaceAll(plainBody, "&", "&amp;")
	htmlBody = strings.ReplaceAll(htmlBody, "<", "&lt;")
	htmlBody = strings.ReplaceAll(htmlBody, ">", "&gt;")
	htmlBody = strings.ReplaceAll(htmlBody, "\"", "&quot;")
	htmlBody = strings.ReplaceAll(htmlBody, "'", "&#39;")

	// Convert line breaks to HTML
	htmlBody = strings.ReplaceAll(htmlBody, "\n", "<br>")

	// Determine alert type and styling based on subject
	alertIcon := "ℹ️"
	alertColor := "#1976d2"

	subjectLower := strings.ToLower(subject)
	if strings.Contains(subjectLower, "tamper") {
		alertIcon = "⚠️"
		alertColor = "#d32f2f"
	} else if strings.Contains(subjectLower, "blocked") || strings.Contains(subjectLower, "violation") {
		alertIcon = "🚫"
		alertColor = "#f57c00"
	} else if strings.Contains(subjectLower, "unblock") {
		alertIcon = "🔓"
		alertColor = "#1976d2"
	} else if strings.Contains(subjectLower, "install") {
		alertIcon = "✅"
		alertColor = "#388e3c"
	} else if strings.Contains(subjectLower, "termination") {
		alertIcon = "🛡️"
		alertColor = "#d32f2f"
	}

	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            line-height: 1.6;
            color: #333;
            background-color: #f5f5f5;
            margin: 0;
            padding: 20px;
        }
        .container {
            max-width: 600px;
            margin: 0 auto;
            background-color: white;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, %s, %s);
            color: white;
            padding: 30px 20px;
            text-align: center;
        }
        .header h1 {
            margin: 0;
            font-size: 24px;
            font-weight: 600;
        }
        .header .icon {
            font-size: 48px;
            margin-bottom: 10px;
            display: block;
        }
        .content {
            padding: 30px 20px;
        }
        .alert-box {
            background-color: %s;
            border-left: 4px solid %s;
            padding: 15px 20px;
            margin: 20px 0;
            border-radius: 4px;
        }
        .timestamp {
            background-color: #f8f9fa;
            padding: 10px 15px;
            border-radius: 4px;
            font-family: 'Courier New', monospace;
            font-size: 14px;
            color: #666;
            margin: 15px 0;
        }
        .footer {
            background-color: #f8f9fa;
            padding: 20px;
            text-align: center;
            border-top: 1px solid #e9ecef;
            font-size: 14px;
            color: #666;
        }
        .footer .logo {
            font-weight: bold;
            color: #333;
        }
        pre {
            background-color: #f8f9fa;
            padding: 15px;
            border-radius: 4px;
            overflow-x: auto;
            font-size: 14px;
            border-left: 3px solid %s;
        }
        .highlight {
            background-color: #fff3cd;
            padding: 2px 4px;
            border-radius: 3px;
            font-weight: 500;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <span class="icon">%s</span>
            <h1>Glocker Security Alert</h1>
        </div>
        <div class="content">
            <div class="alert-box">
                <strong>%s</strong>
            </div>
            <div class="timestamp">
                Generated: %s
            </div>
            <div class="message">
                %s
            </div>
        </div>
        <div class="footer">
            <div class="logo">🛡️ Glocker</div>
            <div>Automated Security Monitoring System</div>
            <div style="margin-top: 10px; font-size: 12px;">
                This is an automated message. Please do not reply to this email.
            </div>
        </div>
    </div>
</body>
</html>`,
		subject,                                // title
		alertColor,                             // gradient start
		adjustColorBrightness(alertColor, -20), // gradient end (darker)
		adjustColorOpacity(alertColor, 0.1),    // alert box background
		alertColor,                             // alert box border
		alertColor,                             // pre border
		alertIcon,                              // header icon
		subject,                                // alert box title
		time.Now().Format("2006-01-02 15:04:05"), // timestamp
		htmlBody,                               // message content
	)
}

// adjustColorBrightness adjusts the brightness of a hex color.
// Negative percent values darken the color.
func adjustColorBrightness(hexColor string, percent int) string {
	if percent < 0 {
		// Darken the color
		switch hexColor {
		case "#d32f2f":
			return "#b71c1c"
		case "#f57c00":
			return "#e65100"
		case "#1976d2":
			return "#1565c0"
		case "#388e3c":
			return "#2e7d32"
		default:
			return "#333333"
		}
	}
	return hexColor
}

// adjustColorOpacity converts a hex color to rgba with the specified opacity.
func adjustColorOpacity(hexColor string, opacity float64) string {
	switch hexColor {
	case "#d32f2f":
		return fmt.Sprintf("rgba(211, 47, 47, %.1f)", opacity)
	case "#f57c00":
		return fmt.Sprintf("rgba(245, 124, 0, %.1f)", opacity)
	case "#1976d2":
		return fmt.Sprintf("rgba(25, 118, 210, %.1f)", opacity)
	case "#388e3c":
		return fmt.Sprintf("rgba(56, 142, 60, %.1f)", opacity)
	default:
		return fmt.Sprintf("rgba(51, 51, 51, %.1f)", opacity)
	}
}
