package captcha

// Challenge содержит разобранные данные капчи ВКонтакте (error_code 14).
type Challenge struct {
	ErrorCode      int    `json:"error_code"`
	ErrorMsg       string `json:"error_msg"`
	CaptchaSID     string `json:"captcha_sid"`
	CaptchaImg     string `json:"captcha_img"`
	RedirectURI    string `json:"redirect_uri"`
	SessionToken   string `json:"session_token"`
	CaptchaTS      string `json:"captcha_ts"`
	CaptchaAttempt string `json:"captcha_attempt"`
}

// BrowserProfile отпечаток браузера для эмуляции легитимного клиента при решении капчи.
type BrowserProfile struct {
	UserAgent       string
	SecChUa         string
	SecChUaMobile   string
	SecChUaPlatform string
}

// DefaultBrowserProfile возвращает актуальный отпечаток десктопного Chrome.
func DefaultBrowserProfile() BrowserProfile {
	return BrowserProfile{
		UserAgent:       "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="130", "Google Chrome";v="130", "Not?A_Brand";v="99"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Linux"`,
	}
}
