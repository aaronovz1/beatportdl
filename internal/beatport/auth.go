package beatport

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"
)

const (
	clientId      = "ryZ8LuyQVPqbK2mBX2Hwt4qSMtnWuTYSqBPO92yQ"
	tokenEndpoint = "/auth/o/token/"
	authEndpoint  = "/auth/o/authorize/?client_id=" + clientId + "&response_type=code"
	loginEndpoint = "/auth/login/"
)

var (
	ErrInvalidAuthorizationCode = errors.New("invalid authorization code")
	ErrInvalidSessionCookie     = errors.New("invalid session cookie")
	ErrLoginIDMismatch          = errors.New("login id does not match")
)

type Auth struct {
	username    string
	password    string
	tokenPair   *tokenPair
	cacheFile   string
	mutex       sync.RWMutex
	refreshOnce sync.Once
	refreshGate chan struct{}
	lastRenewal *authRenewalResult
}

type authRenewalResult struct {
	err      error
	canceled bool
}

type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	LoginID      string `json:"login_id"`
	IssuedAt     int64  `json:"issued_at"`
}

func NewAuth(username, password, cacheFile string) *Auth {
	return &Auth{
		username:  username,
		password:  password,
		cacheFile: cacheFile,
	}
}

func (a *Auth) LoadCache() error {
	if err := a.lockRefresh(context.Background()); err != nil {
		return err
	}
	defer a.unlockRefresh()

	data, err := os.ReadFile(a.cacheFile)
	if err != nil {
		return fmt.Errorf("failed to read token file: %w", err)
	}

	var loadedToken tokenPair
	if err := json.Unmarshal(data, &loadedToken); err != nil {
		return fmt.Errorf("failed to unmarshal token data: %w", err)
	}

	if loadedToken.LoginID != a.loginId() {
		return ErrLoginIDMismatch
	}
	if loadedToken.AccessToken == "" {
		return errors.New("cached token has no access token")
	}

	a.mutex.Lock()
	a.tokenPair = &loadedToken
	a.mutex.Unlock()

	return nil
}

func (a *Auth) WriteCache() error {
	if err := a.lockRefresh(context.Background()); err != nil {
		return err
	}
	defer a.unlockRefresh()
	return a.writeCache(a.snapshot())
}

func (a *Auth) writeCache(token *tokenPair) error {
	if token == nil {
		return errors.New("no token to cache")
	}
	data, err := json.MarshalIndent(token, "", " ")
	if err != nil {
		return fmt.Errorf("failed to marshal tokenPair: %w", err)
	}

	err = os.MkdirAll(path.Dir(a.cacheFile), 0700)
	if err != nil {
		return fmt.Errorf("could not create folder for cache file: %w", err)
	}

	if err := os.WriteFile(a.cacheFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write token to cache: %w", err)
	}

	return nil
}

func (a *Auth) Check(inst *Beatport) error {
	if a == nil {
		return errors.New("missing authentication")
	}
	if err := inst.requestContext().Err(); err != nil {
		return err
	}
	previous := a.renewalSnapshot()
	if !tokenExpired(a.snapshot()) {
		return nil
	}

	if err := a.lockRefresh(inst.requestContext()); err != nil {
		return err
	}
	defer a.unlockRefresh()
	if err := inst.requestContext().Err(); err != nil {
		return err
	}
	if !tokenExpired(a.snapshot()) {
		return inst.requestContext().Err()
	}
	if result := a.sharedRenewalResult(previous); result != nil {
		return result.err
	}
	return a.renew(inst)
}

// Token pairs are immutable after publication, so readers can use a snapshot
// without holding the token mutex across nested authentication requests.
func (a *Auth) snapshot() *tokenPair {
	if a == nil {
		return nil
	}
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.tokenPair
}

func tokenExpired(token *tokenPair) bool {
	return token == nil || token.AccessToken == "" || time.Now().Unix()+300 >= token.IssuedAt+token.ExpiresIn
}

func (a *Auth) renewalSnapshot() *authRenewalResult {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.lastRenewal
}

func (a *Auth) sharedRenewalResult(previous *authRenewalResult) *authRenewalResult {
	current := a.renewalSnapshot()
	if current != nil && current != previous && current.err != nil && !current.canceled {
		return current
	}
	return nil
}

func (a *Auth) recoverUnauthorized(inst *Beatport, rejected *tokenPair) error {
	previous := a.renewalSnapshot()
	if err := a.lockRefresh(inst.requestContext()); err != nil {
		return err
	}
	defer a.unlockRefresh()
	if err := inst.requestContext().Err(); err != nil {
		return err
	}
	if current := a.snapshot(); current != rejected && !tokenExpired(current) {
		return nil
	}
	if result := a.sharedRenewalResult(previous); result != nil {
		return result.err
	}
	return a.renew(inst)
}

// The gate serializes recovery and cache writes without making canceled clients
// wait for another client's network requests. It also supports zero-value Auth.
func (a *Auth) lockRefresh(ctx context.Context) error {
	a.refreshOnce.Do(func() { a.refreshGate = make(chan struct{}, 1) })
	select {
	case a.refreshGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			a.unlockRefresh()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Auth) unlockRefresh() {
	<-a.refreshGate
}

// Callers hold the refresh gate, never the token mutex, across network recovery.
func (a *Auth) renew(inst *Beatport) (err error) {
	// Record each flight so waiters can share failures, not another caller's cancellation.
	defer func() {
		result := &authRenewalResult{err: err, canceled: inst.requestContext().Err() != nil}
		a.mutex.Lock()
		a.lastRenewal = result
		a.mutex.Unlock()
	}()
	if err := inst.requestContext().Err(); err != nil {
		return err
	}
	if token := a.snapshot(); token != nil && token.RefreshToken != "" {
		fmt.Println("Refreshing token")
		if _, err := a.refresh(inst); err == nil {
			return nil
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
	if err := a.init(inst); err != nil {
		return fmt.Errorf("invalid token and authorization error: %w", err)
	}
	return nil
}

func (a *Auth) Invalidate() {
	if a == nil {
		return
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.tokenPair != nil {
		token := *a.tokenPair
		token.IssuedAt = 0
		a.tokenPair = &token
	}
}

func (a *Auth) Init(inst *Beatport) error {
	if err := a.lockRefresh(inst.requestContext()); err != nil {
		return err
	}
	defer a.unlockRefresh()
	return a.init(inst)
}

func (a *Auth) init(inst *Beatport) error {
	if err := inst.requestContext().Err(); err != nil {
		return err
	}
	fmt.Println("Logging in")
	sessionId, err := a.login(inst)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	authorizationCode, err := a.authorize(inst, sessionId)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}
	if err := a.issue(inst, authorizationCode); err != nil {
		return fmt.Errorf("issue token: %w", err)
	}
	return nil
}

func (a *Auth) refresh(inst *Beatport) (*tokenPair, error) {
	token := a.snapshot()
	if token == nil || token.RefreshToken == "" {
		return nil, errors.New("no refresh token")
	}
	payload := map[string]string{
		"client_id":     clientId,
		"refresh_token": token.RefreshToken,
		"grant_type":    "refresh_token",
	}

	res, err := inst.fetch("POST", tokenEndpoint, payload, "application/x-www-form-urlencoded")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	response := &tokenPair{}
	if err = json.NewDecoder(res.Body).Decode(response); err != nil {
		return nil, err
	}
	if response.AccessToken == "" {
		return nil, errors.New("token response has no access token")
	}
	response.IssuedAt = time.Now().Unix()
	response.LoginID = token.LoginID
	a.mutex.Lock()
	a.tokenPair = response
	a.mutex.Unlock()
	if err = a.writeCache(response); err != nil {
		return nil, err
	}

	return response, nil
}

func (a *Auth) issue(inst *Beatport, code string) error {
	payload := map[string]string{
		"client_id": clientId,
	}

	if code != "" {
		payload["grant_type"] = "authorization_code"
		payload["code"] = code
	} else {
		payload["grant_type"] = "password"
		payload["username"] = a.username
		payload["password"] = a.password
	}

	res, err := inst.fetch("POST", tokenEndpoint, payload, "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	response := &tokenPair{}
	if err = json.NewDecoder(res.Body).Decode(response); err != nil {
		return err
	}
	if response.AccessToken == "" {
		return errors.New("token response has no access token")
	}
	response.IssuedAt = time.Now().Unix()
	response.LoginID = a.loginId()
	a.mutex.Lock()
	a.tokenPair = response
	a.mutex.Unlock()
	err = a.writeCache(response)
	if err != nil {
		return err
	}

	return nil
}

func (a *Auth) authorize(inst *Beatport, sessionId string) (string, error) {
	headers := http.Header{}
	headers.Set("Cookie", fmt.Sprintf("sessionid=%s", sessionId))
	res, err := inst.fetchWithHeaders("GET", authEndpoint, nil, "", headers)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	redirectUrl := res.Header.Get("Location")
	parsedUrl, err := url.Parse(redirectUrl)
	if err != nil {
		return "", err
	}

	query, _ := url.ParseQuery(parsedUrl.RawQuery)
	code := query.Get("code")
	if code != "" {
		return code, nil
	}

	return "", ErrInvalidAuthorizationCode
}

func (a *Auth) login(inst *Beatport) (string, error) {
	payload := map[string]string{
		"username": a.username,
		"password": a.password,
	}

	res, err := inst.fetch("POST", loginEndpoint, payload, "application/json")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	cookies := res.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "sessionid" {
			return cookie.Value, nil
		}
	}
	return "", ErrInvalidSessionCookie
}

func (a *Auth) loginId() string {
	hash := fnv.New64a()
	data := fmt.Sprintf("%s:%s", a.username, a.password)
	hash.Write([]byte(data))
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes)
}
