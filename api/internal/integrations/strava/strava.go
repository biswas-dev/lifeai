// Package strava is a small client for the Strava v3 API: the OAuth
// exchange and refresh, and listing an athlete's activities.
package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	AuthURL  = "https://www.strava.com/oauth/authorize"
	TokenURL = "https://www.strava.com/oauth/token"
	APIBase  = "https://www.strava.com/api/v3"
	Scope    = "read,activity:read_all"
)

// Client holds the application credentials.
type Client struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
	TokenURL     string
	HTTP         *http.Client
}

// New builds a client.
func New(clientID, clientSecret string) *Client {
	return &Client{ClientID: clientID, ClientSecret: clientSecret, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Configured reports whether an application is set up.
func (c *Client) Configured() bool {
	return c != nil && strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

func (c *Client) api() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return APIBase
}

func (c *Client) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return TokenURL
}

// AuthorizeURL is where the browser is sent to grant access.
func (c *Client) AuthorizeURL(redirectURI, state string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("approval_prompt", "auto")
	q.Set("scope", Scope)
	q.Set("state", state)
	return AuthURL + "?" + q.Encode()
}

// Token is what the exchange returns.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Scope        string `json:"scope"`
	Athlete      struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"athlete"`
}

// Expired reports whether the access token needs refreshing.
func (t Token) Expired() bool {
	return t.ExpiresAt == 0 || time.Now().Add(time.Minute).Unix() >= t.ExpiresAt
}

// Exchange swaps an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code string) (*Token, error) {
	return c.token(ctx, url.Values{"client_id": {c.ClientID}, "client_secret": {c.ClientSecret}, "code": {code}, "grant_type": {"authorization_code"}})
}

// Refresh renews an access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	return c.token(ctx, url.Values{"client_id": {c.ClientID}, "client_secret": {c.ClientSecret}, "refresh_token": {refreshToken}, "grant_type": {"refresh_token"}})
}

func (c *Client) token(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("strava: token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("strava: token request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tok Token
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("strava: decoding token: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("strava: token response contained no access token")
	}
	return &tok, nil
}

// Activity is one Strava activity summary.
type Activity struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	SportType      string   `json:"sport_type"`
	Trainer        bool     `json:"trainer"`
	MovingTime     int      `json:"moving_time"`
	ElapsedTime    int      `json:"elapsed_time"`
	Distance       float64  `json:"distance"`
	Calories       *float64 `json:"calories"`
	AverageHR      *float64 `json:"average_heartrate"`
	StartDateLocal string   `json:"start_date_local"`
	StartDate      string   `json:"start_date"`
}

// LocalDate is the activity's calendar date where it happened.
func (a Activity) LocalDate() string {
	if len(a.StartDateLocal) >= 10 {
		return a.StartDateLocal[:10]
	}
	if len(a.StartDate) >= 10 {
		return a.StartDate[:10]
	}
	return ""
}

// StartTime is the UTC start.
func (a Activity) StartTime() time.Time {
	if t, err := time.Parse(time.RFC3339, a.StartDate); err == nil {
		return t
	}
	return time.Time{}
}

// Minutes is the session length: moving time, except for stop-start
// sports where elapsed time is the honest figure.
func (a Activity) Minutes() int {
	secs := a.MovingTime
	switch a.SportType {
	case "WeightTraining", "Crossfit", "HighIntensityIntervalTraining", "Workout", "Yoga", "Pilates", "Swim", "RockClimbing":
		if a.ElapsedTime > secs && a.ElapsedTime <= secs*3 {
			secs = a.ElapsedTime
		}
	}
	return secs / 60
}

// Activities lists activities after a time, newest page first.
func (c *Client) Activities(ctx context.Context, accessToken string, after time.Time, page, perPage int) ([]Activity, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(perPage))
	q.Set("page", strconv.Itoa(page))
	if !after.IsZero() {
		q.Set("after", strconv.FormatInt(after.Unix(), 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api()+"/athlete/activities?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("strava: activities: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("strava: activities failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out []Activity
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("strava: decoding activities: %w", err)
	}
	return out, nil
}

// ErrUnauthorized means the athlete revoked access.
var ErrUnauthorized = fmt.Errorf("strava: access revoked")
