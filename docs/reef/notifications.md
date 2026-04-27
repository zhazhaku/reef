# Reef Notification Channels

Reef supports multiple notification channels for task escalation alerts.

## Configuration

Add notification channels to your `config.json`:

```json
{
  "channels": {
    "swarm": {
      "enabled": true,
      "mode": "server",
      "ws_addr": ":8080",
      "admin_addr": ":8081",
      "notifications": [
        {
          "type": "webhook",
          "url": "https://your-webhook-endpoint.com/reef"
        },
        {
          "type": "slack",
          "webhook_url": "https://hooks.slack.com/services/T.../B.../..."
        },
        {
          "type": "smtp",
          "smtp_host": "smtp.gmail.com",
          "smtp_port": 587,
          "username": "your@gmail.com",
          "password": "app-password",
          "from": "reef@yourcompany.com",
          "to": ["oncall@yourcompany.com", "team@yourcompany.com"]
        },
        {
          "type": "feishu",
          "hook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/..."
        },
        {
          "type": "wecom",
          "hook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."
        }
      ]
    }
  }
}
```

## Supported Channels

### Webhook (HTTP POST)

Generic HTTP POST with JSON payload.

| Field | Description |
|-------|-------------|
| `url` | Webhook endpoint URL |

### Slack

Sends formatted messages via Slack Incoming Webhook with Block Kit.

| Field | Description |
|-------|-------------|
| `webhook_url` | Slack Incoming Webhook URL |

### SMTP (Email)

Sends HTML email alerts.

| Field | Description |
|-------|-------------|
| `smtp_host` | SMTP server hostname |
| `smtp_port` | SMTP server port (587 for STARTTLS) |
| `username` | SMTP authentication username |
| `password` | SMTP authentication password |
| `from` | Sender email address |
| `to` | Recipient email addresses (array) |

### Feishu (飞书)

Sends interactive card messages via Feishu webhook.

| Field | Description |
|-------|-------------|
| `hook_url` | Feishu bot webhook URL |

### WeCom (企业微信)

Sends Markdown messages via WeCom webhook.

| Field | Description |
|-------|-------------|
| `hook_url` | WeCom bot webhook URL |

## Behavior

- **Fan-out**: All configured channels receive notifications simultaneously
- **Fault isolation**: If one channel fails, others continue to work
- **Async delivery**: Notifications are sent asynchronously (non-blocking)
- **Legacy fallback**: If `notifications` is empty but `webhook_urls` is set, the legacy webhook is used
