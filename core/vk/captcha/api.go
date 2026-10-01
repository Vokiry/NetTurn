package captcha

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNoSessionToken   = errors.New("captcha: missing session_token in challenge")
	ErrNoRedirectURI    = errors.New("captcha: missing redirect_uri in challenge")
	ErrBootstrapFailed  = errors.New("captcha: failed to extract PoW bootstrap data from HTML")
	ErrCaptchaCheckFail = errors.New("captcha: captchaNotRobot.check returned non-OK status")
)

// BootstrapData содержит параметры PoW, извлеченные со страницы капчи.
type BootstrapData struct {
	PowInput   string
	Difficulty int
	Settings   map[string]any
}

var (
	// Современный формат VK 2025/2026: }('pow_input', difficulty, 'pow_timeout'
	reModernPow = regexp.MustCompile(`\}\(\s*['"]([A-Za-z0-9_-]+)['"]\s*,\s*(\d+)\s*,\s*['"]pow_timeout['"]`)
	// Устаревший формат в JSON
	reLegacyPowInput   = regexp.MustCompile(`["']?pow[_-]?[iI]nput["']?\s*:\s*["']([^"']+)["']`)
	reLegacyDifficulty = regexp.MustCompile(`["']?difficulty["']?\s*:\s*(\d+)`)
)

// ParseBootstrap извлекает powInput и difficulty из HTML страницы id.vk.ru/not_robot_captcha.
func ParseBootstrap(html string) (*BootstrapData, error) {
	// Сначала проверяем современный формат
	if m := reModernPow.FindStringSubmatch(html); len(m) >= 3 {
		diff, _ := strconv.Atoi(m[2])
		return &BootstrapData{
			PowInput:   m[1],
			Difficulty: diff,
		}, nil
	}

	// Фолбек на легаси формат
	inputMatches := reLegacyPowInput.FindStringSubmatch(html)
	if len(inputMatches) < 2 {
		return nil, fmt.Errorf("%w: pow_input not found", ErrBootstrapFailed)
	}

	diff := 3
	diffMatches := reLegacyDifficulty.FindStringSubmatch(html)
	if len(diffMatches) >= 2 {
		if d, err := strconv.Atoi(diffMatches[1]); err == nil && d > 0 {
			diff = d
		}
	}

	return &BootstrapData{
		PowInput:   inputMatches[1],
		Difficulty: diff,
	}, nil
}

// GenerateFakeCursor создает синтетическую траекторию движения мыши для прохождения эвристики капчи.
func GenerateFakeCursor() string {
	startX := 400 + rand.Intn(400)
	startY := 200 + rand.Intn(300)
	startTime := time.Now().UnixMilli() - int64(rand.Intn(1500)+800)

	points := make([]string, 0, 20)
	numPoints := 12 + rand.Intn(8)

	for i := 0; i < numPoints; i++ {
		startX += rand.Intn(13) - 5
		startY += rand.Intn(11) - 2
		startTime += int64(rand.Intn(35) + 15)
		points = append(points, fmt.Sprintf(`{"x":%d,"y":%d,"t":%d}`, startX, startY, startTime))
	}

	return "[" + strings.Join(points, ",") + "]"
}

// GenerateBrowserFP генерирует правдоподобный отпечаток браузера для капчи.
func GenerateBrowserFP(profile BrowserProfile) string {
	data := profile.UserAgent + profile.SecChUa + "1920x1080x24" + strconv.FormatInt(time.Now().UnixNano(), 10)
	sum := md5.Sum([]byte(data))
	return hex.EncodeToString(sum[:])
}

// SolveCaptchaLevel1 реализует полный автономный пайплайн решения VK Smart Captcha через PoW в Go.
func SolveCaptchaLevel1(ctx context.Context, client *http.Client, challenge *Challenge, profile BrowserProfile) (string, error) {
	if challenge.SessionToken == "" {
		return "", ErrNoSessionToken
	}
	if challenge.RedirectURI == "" {
		return "", ErrNoRedirectURI
	}

	// 1. Загрузка страницы капчи
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, challenge.RedirectURI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", profile.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("captcha: fetch bootstrap request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("captcha: read bootstrap body failed: %w", err)
	}

	bootstrap, err := ParseBootstrap(string(bodyBytes))
	if err != nil {
		return "", err
	}

	// 2. Решение PoW
	powRes, err := SolvePoW(ctx, bootstrap.PowInput, bootstrap.Difficulty)
	if err != nil {
		return "", fmt.Errorf("captcha: PoW computation failed: %w", err)
	}

	// Формируем payload формата v2.base64(json)
	powPayload := map[string]any{
		"hash":        powRes.Hash,
		"nonce":       powRes.Nonce,
		"error":       "",
		"duration_ms": 15,
		"telemetry":   map[string]any{},
		"tel_hash":    "",
	}
	powPayloadBytes, _ := json.Marshal(powPayload)
	v2Hash := "v2." + base64.StdEncoding.EncodeToString(powPayloadBytes)

	// 3. Вызовы API captchaNotRobot
	callVKMethod := func(method string, form neturl.Values) (map[string]any, error) {
		apiURL := "https://api.vk.ru/method/" + method + "?v=5.131"
		mReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		mReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mReq.Header.Set("User-Agent", profile.UserAgent)
		mReq.Header.Set("Origin", "https://id.vk.ru")
		mReq.Header.Set("Referer", "https://id.vk.ru/")
		mReq.Header.Set("Sec-Fetch-Site", "same-site")
		mReq.Header.Set("Sec-Fetch-Mode", "cors")

		mResp, err := client.Do(mReq)
		if err != nil {
			return nil, err
		}
		defer mResp.Body.Close()

		respBody, err := io.ReadAll(mResp.Body)
		if err != nil {
			return nil, err
		}

		var parsed map[string]any
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, fmt.Errorf("invalid json response from %s: %w", method, err)
		}
		return parsed, nil
	}

	baseParams := neturl.Values{}
	baseParams.Set("session_token", challenge.SessionToken)
	baseParams.Set("domain", "vk.com")
	baseParams.Set("adFp", "")
	baseParams.Set("access_token", "")

	// 3.1. settings
	if _, err := callVKMethod("captchaNotRobot.settings", baseParams); err != nil {
		return "", fmt.Errorf("captcha settings failed: %w", err)
	}

	time.Sleep(150 * time.Millisecond)

	// 3.2. componentDone
	browserFP := GenerateBrowserFP(profile)
	compParams := neturl.Values{}
	for k, v := range baseParams {
		compParams[k] = v
	}
	compParams.Set("browser_fp", browserFP)
	compParams.Set("device", `{"screen_x":1920,"screen_y":1080,"screen_scale":1}`)

	if _, err := callVKMethod("captchaNotRobot.componentDone", compParams); err != nil {
		return "", fmt.Errorf("captcha componentDone failed: %w", err)
	}

	time.Sleep(150 * time.Millisecond)

	// 3.3. check
	cursorJSON := GenerateFakeCursor()
	debugInfo := fmt.Sprintf("%x", md5.Sum([]byte(profile.UserAgent+strconv.FormatInt(time.Now().UnixNano(), 10))))

	checkParams := neturl.Values{}
	for k, v := range baseParams {
		checkParams[k] = v
	}
	checkParams.Set("accelerometer", "[]")
	checkParams.Set("gyroscope", "[]")
	checkParams.Set("motion", "[]")
	checkParams.Set("cursor", cursorJSON)
	checkParams.Set("taps", "[]")
	checkParams.Set("connectionRtt", "[45,45,45,45,45]")
	checkParams.Set("connectionDownlink", "[10.0,10.0,10.0,10.0]")
	checkParams.Set("browser_fp", browserFP)
	checkParams.Set("hash", v2Hash)
	checkParams.Set("answer", base64.StdEncoding.EncodeToString([]byte("{}")))
	checkParams.Set("debug_info", debugInfo)

	checkResp, err := callVKMethod("captchaNotRobot.check", checkParams)
	if err != nil {
		return "", fmt.Errorf("captcha check call failed: %w", err)
	}

	respObj, ok := checkResp["response"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("%w: response object missing: %v", ErrCaptchaCheckFail, checkResp)
	}

	status, _ := respObj["status"].(string)
	if status != "OK" {
		return "", fmt.Errorf("%w: status=%s", ErrCaptchaCheckFail, status)
	}

	successToken, _ := respObj["success_token"].(string)
	if successToken == "" {
		return "", fmt.Errorf("%w: missing success_token in response", ErrCaptchaCheckFail)
	}

	time.Sleep(100 * time.Millisecond)

	// 3.4. endSession (fire and forget)
	_, _ = callVKMethod("captchaNotRobot.endSession", baseParams)

	return successToken, nil
}
