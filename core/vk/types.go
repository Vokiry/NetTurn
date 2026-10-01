package vk

import (
	"time"

	"github.com/Vokiry/NetTurn/core/vk/captcha"
)

// AppCredentials официальные идентификаторы приложений VK для анонимного входа.
type AppCredentials struct {
	ClientID     string
	ClientSecret string
	Description  string
}

// DefaultAppCredentialsList пул официальных приложений VK для ротации при лимитах.
var DefaultAppCredentialsList = []AppCredentials{
	{ClientID: "6287487", ClientSecret: "QbYic1K3lEV5kTGiqlq2", Description: "VK Web App"},
	{ClientID: "7879029", ClientSecret: "aR5NKGmm03GYrCiNKsaw", Description: "VK Mobile Web"},
}

// TurnCredentials содержит параметры доступа к медиа-релею OKCDN.
type TurnCredentials struct {
	ServerAddr string    `json:"server_addr"` // host:port релея (например: 91.231.x.x:3478)
	Username   string    `json:"username"`
	Password   string    `json:"password"`
	ExpiresAt  time.Time `json:"expires_at"`
	Link       string    `json:"link"`
}

// IsExpired проверяет, истек ли срок действия креденшелов с учетом защитного интервала в 60 секунд.
func (c *TurnCredentials) IsExpired() bool {
	return time.Now().Add(60 * time.Second).After(c.ExpiresAt)
}

// CaptchaChallenge алиас структуры задачи капчи.
type CaptchaChallenge = captcha.Challenge

// BrowserProfile алиас профиля браузера.
type BrowserProfile = captcha.BrowserProfile

// DefaultBrowserProfile возвращает актуальный отпечаток десктопного Chrome.
func DefaultBrowserProfile() BrowserProfile {
	return captcha.DefaultBrowserProfile()
}
