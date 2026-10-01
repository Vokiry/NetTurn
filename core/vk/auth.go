package vk

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	neturl "net/url"
	"strings"
	"time"

	"github.com/Vokiry/NetTurn/core/vk/captcha"
)

var (
	ErrCallJoinFailed   = errors.New("vk: call join failed")
	ErrTURNEmpty        = errors.New("vk: missing or empty turn_server in response")
	ErrAllClientsFailed = errors.New("vk: all app credentials failed")
)

// CleanCallHash извлекает чистый хеш звонка из ссылки любого вида (vk.com/call/join/..., vk.ru/call/join/..., или просто хеш).
func CleanCallHash(rawLink string) string {
	rawLink = strings.TrimSpace(rawLink)
	if idx := strings.Index(rawLink, "join/"); idx != -1 {
		rawLink = rawLink[idx+5:]
	}
	if idx := strings.IndexAny(rawLink, "/?#"); idx != -1 {
		rawLink = rawLink[:idx]
	}
	return rawLink
}

// GenerateRandomName генерирует правдоподобное имя пользователя на кириллице для входа в звонок.
func GenerateRandomName() string {
	firstNames := []string{"Алексей", "Дмитрий", "Сергей", "Андрей", "Михаил", "Максим", "Иван", "Артём", "Никита", "Евгений", "Павел"}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(firstNames))))
	return firstNames[n.Int64()]
}

// FetchTurnCredentials выполняет полный 5-этапный цикл авторизации и возвращает активные TURN-креденшелы.
func FetchTurnCredentials(ctx context.Context, client *HTTPClient, rawLink string) (*TurnCredentials, error) {
	callHash := CleanCallHash(rawLink)
	if callHash == "" {
		return nil, fmt.Errorf("%w: empty call hash", ErrCallJoinFailed)
	}

	var lastErr error
	for _, creds := range DefaultAppCredentialsList {
		if err := client.Throttle(ctx); err != nil {
			return nil, err
		}

		turnCreds, err := fetchWithCredentials(ctx, client, callHash, creds)
		if err == nil && turnCreds != nil {
			turnCreds.Link = callHash
			return turnCreds, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("%w: %v", ErrAllClientsFailed, lastErr)
}

func fetchWithCredentials(ctx context.Context, client *HTTPClient, callHash string, creds AppCredentials) (*TurnCredentials, error) {
	doPost := func(reqURL string, form neturl.Values) (map[string]any, error) {
		req, err := client.Client().PostForm(reqURL, form)
		if err != nil {
			return nil, err
		}
		defer req.Body.Close()

		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}

		var resp map[string]any
		if err := json.Unmarshal(bodyBytes, &resp); err != nil {
			return nil, fmt.Errorf("json parse error: %w (body: %s)", err, string(bodyBytes))
		}
		return resp, nil
	}

	// 1. Токен сообщений (Token 1)
	token1Form := neturl.Values{}
	token1Form.Set("client_id", creds.ClientID)
	token1Form.Set("client_secret", creds.ClientSecret)
	token1Form.Set("token_type", "messages")
	token1Form.Set("version", "1")
	token1Form.Set("app_id", creds.ClientID)

	resp1, err := doPost("https://login.vk.ru/?act=get_anonym_token", token1Form)
	if err != nil {
		return nil, fmt.Errorf("step 1 get_anonym_token: %w", err)
	}
	data1, _ := resp1["data"].(map[string]any)
	token1, _ := data1["access_token"].(string)
	if token1 == "" {
		return nil, fmt.Errorf("step 1: missing access_token in response: %v", resp1)
	}

	// 2. Валидация звонка (CallPreview)
	previewForm := neturl.Values{}
	previewForm.Set("vk_join_link", "https://vk.com/call/join/"+callHash)
	previewForm.Set("fields", "photo_200")
	previewForm.Set("access_token", token1)
	_, _ = doPost("https://api.vk.ru/method/calls.getCallPreview?v=5.275&client_id="+creds.ClientID, previewForm)

	// 3. Токен участника звонка (Token 2 / anonymToken)
	var token2 string
	name := GenerateRandomName()

	anonTokenForm := neturl.Values{}
	anonTokenForm.Set("vk_join_link", "https://vk.com/call/join/"+callHash)
	anonTokenForm.Set("name", name)
	anonTokenForm.Set("access_token", token1)

	for attempt := 0; attempt < 3; attempt++ {
		resp3, err := doPost("https://api.vk.ru/method/calls.getAnonymousToken?v=5.275&client_id="+creds.ClientID, anonTokenForm)
		if err != nil {
			return nil, fmt.Errorf("step 3 getAnonymousToken: %w", err)
		}

		// Проверка на ошибку капчи
		if errObj, ok := resp3["error"].(map[string]any); ok {
			codeFloat, _ := errObj["error_code"].(float64)
			if int(codeFloat) == 14 {
				// Парсим данные капчи
				challenge := parseCaptchaError(errObj)
				if challenge.SessionToken != "" && challenge.RedirectURI != "" {
					successToken, solveErr := captcha.SolveCaptchaLevel1(ctx, client.Client(), challenge, client.Profile())
					if solveErr != nil {
						return nil, fmt.Errorf("step 3 captcha solve failed: %w", solveErr)
					}

					// Добавляем полученный success_token для повторного запроса
					anonTokenForm.Set("success_token", successToken)
					continue
				}
			}
			return nil, fmt.Errorf("step 3 VK API error: %v", errObj)
		}

		responseObj, _ := resp3["response"].(map[string]any)
		token2, _ = responseObj["token"].(string)
		if token2 != "" {
			break
		}
	}

	if token2 == "" {
		return nil, errors.New("step 3: failed to obtain anonymous call token")
	}

	// 4. Сессия в OKCDN (Token 3 / session_key)
	sessionForm := neturl.Values{}
	sessionForm.Set("session_data", `{"version":2,"device_id":"`+generateUUID()+`","client_version":1.1,"client_type":"SDK_JS"}`)
	sessionForm.Set("method", "auth.anonymLogin")
	sessionForm.Set("format", "JSON")
	sessionForm.Set("application_key", "CGMMEJLGDIHBABABA")

	resp4, err := doPost("https://calls.okcdn.ru/fb.do", sessionForm)
	if err != nil {
		return nil, fmt.Errorf("step 4 okcdn auth: %w", err)
	}
	token3, _ := resp4["session_key"].(string)
	if token3 == "" {
		return nil, fmt.Errorf("step 4: missing session_key: %v", resp4)
	}

	// 5. Получение TURN креденшелов
	joinForm := neturl.Values{}
	joinForm.Set("joinLink", callHash)
	joinForm.Set("isVideo", "false")
	joinForm.Set("protocolVersion", "5")
	joinForm.Set("capabilities", "2F7F")
	joinForm.Set("anonymToken", token2)
	joinForm.Set("method", "vchat.joinConversationByLink")
	joinForm.Set("format", "JSON")
	joinForm.Set("application_key", "CGMMEJLGDIHBABABA")
	joinForm.Set("session_key", token3)

	resp5, err := doPost("https://calls.okcdn.ru/fb.do", joinForm)
	if err != nil {
		return nil, fmt.Errorf("step 5 okcdn join: %w", err)
	}

	tsRaw, ok := resp5["turn_server"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %v", ErrTURNEmpty, resp5)
	}

	user, _ := tsRaw["username"].(string)
	pass, _ := tsRaw["credential"].(string)
	urls, _ := tsRaw["urls"].([]any)
	if user == "" || pass == "" || len(urls) == 0 {
		return nil, fmt.Errorf("%w: invalid credentials format", ErrTURNEmpty)
	}

	// Извлекаем чистый адрес host:port
	urlStr, _ := urls[0].(string)
	cleanAddr := strings.Split(urlStr, "?")[0]
	cleanAddr = strings.TrimPrefix(strings.TrimPrefix(cleanAddr, "turn:"), "turns:")

	return &TurnCredentials{
		ServerAddr: cleanAddr,
		Username:   user,
		Password:   pass,
		ExpiresAt:  time.Now().Add(10 * time.Minute),
	}, nil
}

func parseCaptchaError(errData map[string]any) *CaptchaChallenge {
	c := &CaptchaChallenge{
		ErrorCode: 14,
	}
	if msg, ok := errData["error_msg"].(string); ok {
		c.ErrorMsg = msg
	}
	if sid, ok := errData["captcha_sid"].(string); ok {
		c.CaptchaSID = sid
	} else if sidNum, ok := errData["captcha_sid"].(float64); ok {
		c.CaptchaSID = fmt.Sprintf("%.0f", sidNum)
	}
	if uri, ok := errData["redirect_uri"].(string); ok {
		c.RedirectURI = uri
		if parsed, err := neturl.Parse(uri); err == nil {
			c.SessionToken = parsed.Query().Get("session_token")
		}
	}
	if ts, ok := errData["captcha_ts"].(string); ok {
		c.CaptchaTS = ts
	} else if tsNum, ok := errData["captcha_ts"].(float64); ok {
		c.CaptchaTS = fmt.Sprintf("%.0f", tsNum)
	}
	return c
}

func generateUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant RFC 4122
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
