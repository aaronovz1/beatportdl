package beatport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	beatportBaseUrl       = "https://api.beatport.com/v4"
	maxGetRequestAttempts = 3
	getRetryBaseDelay     = 250 * time.Millisecond
)

type Beatport struct {
	client  *http.Client
	headers map[string]string
	auth    *Auth
	ctx     context.Context
}

type FetcherError struct {
	Detail *string `json:"detail,omitempty"`
	Error  *string `json:"error,omitempty"`
}

type APIError struct {
	StatusCode int
	Detail     string
	Endpoint   string
}

func (e *APIError) Error() string {
	message := fmt.Sprintf("request failed with status code: %d", e.StatusCode)
	if e.Detail != "" {
		message = fmt.Sprintf("%s - %s", message, e.Detail)
	}
	if hint := e.Hint(); hint != "" {
		message = fmt.Sprintf("%s (hint: %s)", message, hint)
	}
	return message
}

func (e *APIError) Hint() string {
	detail := strings.ToLower(e.Detail)
	endpoint := strings.ToLower(e.Endpoint)

	if endpoint == tokenEndpoint || endpoint == authEndpoint || endpoint == loginEndpoint {
		switch e.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
			return "authentication failed; verify your credentials and remove stale cached credentials if needed"
		}
	}

	if strings.Contains(endpoint, "/download/") || strings.Contains(endpoint, "/stream/") {
		quality := requestedQualityFromEndpoint(e.Endpoint)
		if quality != "" && (e.StatusCode == http.StatusBadRequest || e.StatusCode == http.StatusForbidden) {
			return fmt.Sprintf(
				"requested quality %q may not be available for your Beatport subscription tier, or the track may be unavailable in your region",
				quality,
			)
		}
		if e.StatusCode == http.StatusForbidden {
			return "download access was denied; check your active Beatport subscription tier and territorial availability"
		}
	}

	if e.StatusCode == http.StatusForbidden {
		if strings.Contains(detail, "territor") || strings.Contains(detail, "region") || strings.Contains(detail, "country") {
			return "access was denied for this resource; check territorial availability for your account"
		}
		if strings.Contains(detail, "subscription") || strings.Contains(detail, "plan") {
			return "access was denied; check that your subscription tier includes this operation and quality"
		}
		return "access was denied; check login state, Beatport subscription tier, and territorial availability"
	}

	if e.StatusCode == http.StatusBadRequest {
		if strings.Contains(detail, "quality") {
			return "the requested quality was rejected; check the configured quality against your Beatport subscription tier"
		}
		return "the request was rejected; check the configured quality, URL type, and whether the resource is available to your account"
	}

	return ""
}

func requestedQualityFromEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("quality")
}

type Paginated[T any] struct {
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
	Count    int     `json:"count"`
	Page     string  `json:"page"`
	PerPage  int     `json:"per_page"`
	Results  []T     `json:"results"`
}

func New(proxyUrl string, auth *Auth) *Beatport {
	transport := &http.Transport{}
	if proxyUrl != "" {
		proxyURL, _ := url.Parse(proxyUrl)
		proxy := http.ProxyURL(proxyURL)
		transport.Proxy = proxy
	}
	headers := map[string]string{
		"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"accept-language": "en-US,en;q=0.9",
		"cache-control":   "max-age=0",
		"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	}
	f := Beatport{
		auth: auth,
		ctx:  context.Background(),
		client: &http.Client{
			Timeout:   time.Duration(40) * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		headers: headers,
	}
	return &f
}

// WithContext returns a shallow copy sharing the HTTP client and authentication
// state, with ctx applied to all requests and authentication recovery.
func (b *Beatport) WithContext(ctx context.Context) *Beatport {
	if ctx == nil {
		panic("nil context")
	}
	copy := *b
	copy.ctx = ctx
	return &copy
}

func (b *Beatport) requestContext() context.Context {
	if b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}

func (b *Beatport) fetch(method, endpoint string, payload interface{}, contentType string) (*http.Response, error) {
	return b.fetchWithHeaders(method, endpoint, payload, contentType, nil)
}

func (b *Beatport) fetchWithHeaders(method, endpoint string, payload interface{}, contentType string, headers http.Header) (*http.Response, error) {
	var body bytes.Buffer
	isAuthRequest := endpoint == tokenEndpoint || endpoint == authEndpoint || endpoint == loginEndpoint
	if err := b.requestContext().Err(); err != nil {
		return nil, err
	}

	if !isAuthRequest {
		if err := b.auth.Check(b); err != nil {
			return nil, err
		}
	}

	if payload != nil {
		switch contentType {
		case "application/json":
			if err := json.NewEncoder(&body).Encode(payload); err != nil {
				return nil, fmt.Errorf("failed to encode json payload: %w", err)
			}
		case "application/x-www-form-urlencoded":
			formData, err := encodeFormPayload(payload)
			if err != nil {
				return nil, fmt.Errorf("failed to encode form payload: %w", err)
			}
			body.WriteString(formData.Encode())
		default:
			return nil, fmt.Errorf("unsupported content type: %s", contentType)
		}
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(b.requestContext(), method, beatportBaseUrl+endpoint, bytes.NewReader(body.Bytes()))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		for key, value := range b.headers {
			req.Header.Add(key, value)
		}
		for key, values := range headers {
			req.Header[key] = append([]string(nil), values...)
		}

		if payload != nil {
			req.Header.Set("Content-Type", contentType)
		}

		token := b.auth.snapshot()
		if token != nil {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
		}

		resp, err := b.doRequest(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound {
			return resp, nil
		}
		if resp.StatusCode == http.StatusUnauthorized && !isAuthRequest && attempt == 0 {
			resp.Body.Close()
			if err := b.auth.recoverUnauthorized(b, token); err != nil {
				return nil, err
			}
			continue
		}
		defer resp.Body.Close()
		response := &FetcherError{}
		if err = json.NewDecoder(resp.Body).Decode(response); err == nil {
			detail := ""
			if response.Detail != nil {
				detail = *response.Detail
			} else if response.Error != nil {
				detail = *response.Error
			}
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Detail:     detail,
				Endpoint:   endpoint,
			}
		}
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
		}
	}
}

func (b *Beatport) doRequest(req *http.Request) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		attemptReq := req
		if attempt > 1 {
			attemptReq = req.Clone(req.Context())
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				attemptReq.Body = body
			}
		}

		resp, err := b.client.Do(attemptReq)
		if err == nil {
			return resp, nil
		}
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if req.Method != http.MethodGet || attempt >= maxGetRequestAttempts || !isRetryableRequestError(err) {
			return nil, err
		}

		timer := time.NewTimer(time.Duration(attempt) * getRetryBaseDelay)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

func isRetryableRequestError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func encodeFormPayload(payload interface{}) (url.Values, error) {
	values := url.Values{}

	switch p := payload.(type) {
	case map[string]string:
		for key, value := range p {
			values.Set(key, value)
		}
	case url.Values:
		values = p
	default:
		return nil, errors.New("invalid payload")
	}

	return values, nil
}
