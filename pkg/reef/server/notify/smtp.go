package notify

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SMTPNotifier sends alerts via email using SMTP.
type SMTPNotifier struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       []string
}

// NewSMTPNotifier creates a new SMTPNotifier.
func NewSMTPNotifier(host string, port int, username, password, from string, to []string) *SMTPNotifier {
	return &SMTPNotifier{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		to:       to,
	}
}

func (n *SMTPNotifier) Name() string { return "smtp" }

func (n *SMTPNotifier) Notify(ctx context.Context, alert Alert) error {
	subject := fmt.Sprintf("[Reef Alert] %s — Task %s", alert.Event, alert.TaskID)

	var body strings.Builder
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	body.WriteString(fmt.Sprintf("From: %s\r\n", n.from))
	body.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(n.to, ",")))
	body.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	body.WriteString("\r\n")
	body.WriteString("<html><body>")
	body.WriteString(fmt.Sprintf("<h2 style=\"color: #d9534f;\">🚨 Reef Alert: %s</h2>", alert.Event))
	body.WriteString("<table border=\"1\" cellpadding=\"8\" cellspacing=\"0\" style=\"border-collapse: collapse;\">")
	body.WriteString(fmt.Sprintf("<tr><td><b>Task ID</b></td><td>%s</td></tr>", alert.TaskID))
	body.WriteString(fmt.Sprintf("<tr><td><b>Status</b></td><td>%s</td></tr>", alert.Status))
	body.WriteString(fmt.Sprintf("<tr><td><b>Role</b></td><td>%s</td></tr>", alert.RequiredRole))
	body.WriteString(fmt.Sprintf("<tr><td><b>Instruction</b></td><td>%s</td></tr>", alert.Instruction))
	body.WriteString(fmt.Sprintf("<tr><td><b>Escalations</b></td><td>%d/%d</td></tr>", alert.EscalationCount, alert.MaxEscalations))
	if alert.Error != nil {
		body.WriteString(fmt.Sprintf("<tr><td><b>Error</b></td><td><pre>%s: %s</pre></td></tr>", alert.Error.Type, alert.Error.Message))
	}
	body.WriteString(fmt.Sprintf("<tr><td><b>Time</b></td><td>%s</td></tr>", alert.Timestamp.Format("2006-01-02 15:04:05")))
	body.WriteString("</table></body></html>")

	addr := net.JoinHostPort(n.host, fmt.Sprintf("%d", n.port))
	auth := smtp.PlainAuth("", n.username, n.password, n.host)

	return smtp.SendMail(addr, auth, n.from, n.to, []byte(body.String()))
}
