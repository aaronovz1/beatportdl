package beatport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	beatportBaseUrl   = "https://api.beatport.com/v4"
	beatsourceBaseUrl = "https://api.beatsource.com/v4"
)

type Beatport struct {
	store   Store
	client  *http.Client
	headers map[string]string
	auth    *Auth
}

type FetcherError struct {
	Detail *string `json:"detail,omitempty"`
	Error  *string `json:"error,omitempty"`
}

type APIError struct {
	StatusCode int
	Detail     string
	Endpoint   string
	Store      Store
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
				"requested quality %q may not be available for this store or subscription tier, or the track may be unavailable in your region",
				quality,
			)
		}
		if e.StatusCode == http.StatusForbidden {
			return "download access was denied; check your active subscription tier, selected store, and territorial availability"
		}
	}

	if e.StatusCode == http.StatusForbidden {
		if strings.Contains(detail, "territor") || strings.Contains(detail, "region") || strings.Contains(detail, "country") {
			return "access was denied for this resource; check territorial availability for your account"
		}
		if strings.Contains(detail, "subscription") || strings.Contains(detail, "plan") {
			return "access was denied; check that your subscription tier includes this operation and quality"
		}
		return "access was denied; check login state, subscription tier, selected store, and territorial availability"
	}

	if e.StatusCode == http.StatusBadRequest {
		if strings.Contains(detail, "quality") {
			return "the requested quality was rejected; check the configured quality against your subscription tier and selected store"
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

func New(store Store, proxyUrl string, auth *Auth) *Beatport {
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
		store: store,
		auth:  auth,
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

func (b *Beatport) fetch(method, endpoint string, payload interface{}, contentType string) (*http.Response, error) {
	var body bytes.Buffer

	if endpoint != tokenEndpoint && endpoint != authEndpoint && endpoint != loginEndpoint {
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

	var baseUrl string
	switch b.store {
	default:
		baseUrl = beatportBaseUrl
	case StoreBeatsource:
		baseUrl = beatsourceBaseUrl
	}

	req, err := http.NewRequest(method, baseUrl+endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for key, value := range b.headers {
		req.Header.Add(key, value)
	}

	if payload != nil {
		req.Header.Set("Content-Type", contentType)
	}

	if b.auth.tokenPair != nil {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", b.auth.tokenPair.AccessToken))
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		if resp.StatusCode == http.StatusUnauthorized && endpoint != tokenEndpoint && endpoint != authEndpoint && endpoint != loginEndpoint {
			b.auth.Invalidate()
			return b.fetch(method, endpoint, payload, contentType)
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
				Store:      b.store,
			}
		}
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
			Store:      b.store,
		}
	}

	return resp, nil
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
