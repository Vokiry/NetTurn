package vk

import (
	"context"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"time"
)

// HTTPClient кастомизированный HTTP-клиент с браузерными заголовками, куками и защищенным DNS.
type HTTPClient struct {
	client    *http.Client
	profile   BrowserProfile
	mu        sync.Mutex
	lastFetch time.Time
}

type headerRoundTripper struct {
	inner   http.RoundTripper
	profile BrowserProfile
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", rt.profile.UserAgent)
	}
	if req.Header.Get("sec-ch-ua") == "" {
		req.Header.Set("sec-ch-ua", rt.profile.SecChUa)
	}
	if req.Header.Get("sec-ch-ua-mobile") == "" {
		req.Header.Set("sec-ch-ua-mobile", rt.profile.SecChUaMobile)
	}
	if req.Header.Get("sec-ch-ua-platform") == "" {
		req.Header.Set("sec-ch-ua-platform", rt.profile.SecChUaPlatform)
	}
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("DNT", "1")

	return rt.inner.RoundTrip(req)
}

// NewHTTPClient создает новый клиент с кастомным DNS-резолвером и эмуляцией браузера.
func NewHTTPClient(dnsServer string, profile BrowserProfile) *HTTPClient {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	if dnsServer != "" {
		if _, _, err := net.SplitHostPort(dnsServer); err != nil {
			dnsServer = net.JoinHostPort(dnsServer, "53")
		}
		dialer.Resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				conn, err := d.DialContext(ctx, "udp", dnsServer)
				if err == nil {
					return conn, nil
				}
				return net.Dial("udp", "77.88.8.8:53")
			},
		}
	}

	jar, _ := cookiejar.New(nil)

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return dialer.DialContext(ctx, network, addr)
		}

		// Для доменов VK отдаются несколько IP, один из которых (93.186.237.1) часто не отвечает на TLS.
		// Отфильтровываем сбойный IP в пользу рабочего пула (95.213.56.1 и др.).
		ips, err := net.LookupHost(host)
		if err == nil && len(ips) > 1 {
			for _, ip := range ips {
				if ip == "93.186.237.1" {
					continue
				}
				conn, dErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
				if dErr == nil {
					return conn, nil
				}
			}
		}

		return dialer.DialContext(ctx, network, addr)
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   4 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &HTTPClient{
		client: &http.Client{
			Transport: &headerRoundTripper{
				inner:   transport,
				profile: profile,
			},
			Jar:     jar,
			Timeout: 20 * time.Second,
		},
		profile: profile,
	}
}

// Client возвращает лежащий в основе *http.Client.
func (c *HTTPClient) Client() *http.Client {
	return c.client
}

// Profile возвращает отпечаток браузера.
func (c *HTTPClient) Profile() BrowserProfile {
	return c.profile
}

// Throttle ожидает безопасный рандомизированный интервал (1.5 - 3 сек) между обращениями к VK API
// для предотвращения срабатывания лимитов (Error 29 / Rate limit).
func (c *HTTPClient) Throttle(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	minInterval := 1500*time.Millisecond + time.Duration(rand.Intn(1500))*time.Millisecond
	elapsed := time.Since(c.lastFetch)

	if !c.lastFetch.IsZero() && elapsed < minInterval {
		wait := minInterval - elapsed
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	c.lastFetch = time.Now()
	return nil
}
