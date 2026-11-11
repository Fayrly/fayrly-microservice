package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Message is a transactional email.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Mailer delivers transactional email. Enabled reports whether delivery is
// configured; when false the service refuses reset requests instead of silently
// discarding codes.
type Mailer interface {
	Enabled() bool
	Send(ctx context.Context, msg Message) error
}

type MailerConfig struct {
	Mode         string
	ResendAPIKey string
	From         string
	Logger       *slog.Logger
}

// NewMailer picks a delivery backend. MAIL_MODE=log writes messages to the
// structured log (development only); otherwise a Resend API key enables real
// delivery. Anything else leaves delivery disabled.
func NewMailer(cfg MailerConfig) Mailer {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	switch mode {
	case "log":
		return &logMailer{logger: cfg.Logger}
	case "off", "disabled":
		return disabledMailer{}
	}
	if strings.TrimSpace(cfg.ResendAPIKey) != "" && strings.TrimSpace(cfg.From) != "" {
		return &resendMailer{
			apiKey: cfg.ResendAPIKey,
			from:   cfg.From,
			client: &http.Client{Timeout: 10 * time.Second},
		}
	}
	return disabledMailer{}
}

type disabledMailer struct{}

func (disabledMailer) Enabled() bool { return false }

func (disabledMailer) Send(context.Context, Message) error { return ErrMailUnavailable }

type logMailer struct {
	logger *slog.Logger
}

func (m *logMailer) Enabled() bool { return true }

func (m *logMailer) Send(_ context.Context, msg Message) error {
	m.logger.Info("email delivery is in log mode",
		"to", msg.To,
		"subject", msg.Subject,
		"body", msg.Text,
	)
	return nil
}

type resendMailer struct {
	apiKey string
	from   string
	client *http.Client
}

func (m *resendMailer) Enabled() bool { return true }

type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

func (m *resendMailer) Send(ctx context.Context, msg Message) error {
	payload, err := json.Marshal(resendPayload{
		From:    m.from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		HTML:    msg.HTML,
		Text:    msg.Text,
	})
	if err != nil {
		return fmt.Errorf("encode email payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build email request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("send email: provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func passwordChangedMessage(to string, at time.Time) Message {
	changedAt := at.In(time.FixedZone("WIB", 7*60*60)).Format("02 Jan 2006 15:04") + " WIB"
	return Message{
		To:      to,
		Subject: "Kata sandi Fayrly berhasil diubah",
		Text: fmt.Sprintf(`Halo,

Kata sandi akun Fayrly Anda berhasil diubah pada %s.

Semua sesi lain sudah dikeluarkan dari akun Anda.
Jika Anda tidak melakukan perubahan ini, segera lakukan reset kata sandi dan hubungi admin Fayrly.

Salam,
Tim Fayrly
`, changedAt),
		HTML: fmt.Sprintf(`<!doctype html>
<html lang="id">
  <body style="margin:0;padding:24px;background:#f6f9f7;font-family:Arial,Helvetica,sans-serif;color:#0f172a;">
    <div style="max-width:480px;margin:0 auto;background:#ffffff;border:1px solid #e2e8f0;border-radius:16px;padding:32px;">
      <h1 style="margin:0 0 12px;font-size:20px;color:#0f172a;">Kata sandi berhasil diubah</h1>
      <p style="margin:0 0 20px;font-size:15px;line-height:1.6;color:#334155;">
        Kata sandi akun Fayrly Anda berhasil diubah pada %s.
      </p>
      <p style="margin:0 0 8px;font-size:14px;line-height:1.6;color:#475569;">
        Semua sesi lain sudah dikeluarkan dari akun Anda.
      </p>
      <p style="margin:0;font-size:14px;line-height:1.6;color:#475569;">
        Jika Anda tidak melakukan perubahan ini, segera lakukan reset kata sandi dan hubungi admin Fayrly.
      </p>
    </div>
    <p style="max-width:480px;margin:16px auto 0;font-size:12px;color:#94a3b8;text-align:center;">Email otomatis dari Fayrly.</p>
  </body>
</html>`, changedAt),
	}
}

func passwordResetMessage(to, code string, ttl time.Duration) Message {
	minutes := int(ttl.Minutes())
	return Message{
		To:      to,
		Subject: "Kode reset kata sandi Fayrly",
		Text: fmt.Sprintf(`Halo,

Kami menerima permintaan untuk mengatur ulang kata sandi akun Fayrly Anda.

Kode verifikasi: %s

Kode ini berlaku selama %d menit dan hanya dapat digunakan satu kali.
Jika Anda tidak meminta reset kata sandi, abaikan email ini. Kata sandi Anda tidak berubah.

Salam,
Tim Fayrly
`, code, minutes),
		HTML: fmt.Sprintf(`<!doctype html>
<html lang="id">
  <body style="margin:0;padding:24px;background:#f6f9f7;font-family:Arial,Helvetica,sans-serif;color:#0f172a;">
    <div style="max-width:480px;margin:0 auto;background:#ffffff;border:1px solid #e2e8f0;border-radius:16px;padding:32px;">
      <h1 style="margin:0 0 12px;font-size:20px;color:#0f172a;">Reset kata sandi</h1>
      <p style="margin:0 0 20px;font-size:15px;line-height:1.6;color:#334155;">
        Kami menerima permintaan untuk mengatur ulang kata sandi akun Fayrly Anda.
        Masukkan kode berikut pada halaman lupa kata sandi:
      </p>
      <div style="margin:0 0 20px;padding:16px;border-radius:12px;background:#ecfdf5;text-align:center;font-size:32px;letter-spacing:8px;font-weight:700;color:#047857;">%s</div>
      <p style="margin:0 0 8px;font-size:14px;line-height:1.6;color:#475569;">
        Kode berlaku selama %d menit dan hanya dapat digunakan satu kali.
      </p>
      <p style="margin:0;font-size:14px;line-height:1.6;color:#475569;">
        Jika Anda tidak meminta reset kata sandi, abaikan email ini. Kata sandi Anda tidak berubah.
      </p>
    </div>
    <p style="max-width:480px;margin:16px auto 0;font-size:12px;color:#94a3b8;text-align:center;">Email otomatis dari Fayrly.</p>
  </body>
</html>`, code, minutes),
	}
}
