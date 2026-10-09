// Package strava is a small client for the Strava v3 API: the OAuth
// exchange and refresh, listing an athlete's activities, and creating or
// updating a manual activity for a session logged elsewhere.
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
	// activity:write lets a strength session logged here be posted to
	// Strava. Connections made before it was requested can still import.
	Scope = "read,activity:read_all,activity:write"
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

// CanWrite reports whether a granted scope allows uploading activities.
func CanWrite(scope string) bool {
	for _, s := range strings.Split(scope, ",") {
		if strings.TrimSpace(s) == "activity:write" {
			return true
		}
	}
	return false
}

// ManualActivity is a session to post without a GPS file.
type ManualActivity struct {
	Name        string
	SportType   string
	StartLocal  time.Time
	ElapsedSecs int
	Description string
	Trainer     bool
}

// ErrForbidden means the token lacks a scope the request needs.
var ErrForbidden = fmt.Errorf("strava: permission missing; reconnect and allow activity uploads")

// CreateActivity posts a manual activity and returns its id.
func (c *Client) CreateActivity(ctx context.Context, accessToken string, a ManualActivity) (int64, error) {
	form := url.Values{}
	form.Set("name", a.Name)
	form.Set("sport_type", a.SportType)
	// Strava reads start_date_local in the athlete's own timezone.
	form.Set("start_date_local", a.StartLocal.Format("2006-01-02T15:04:05"))
	form.Set("elapsed_time", strconv.Itoa(a.ElapsedSecs))
	form.Set("description", a.Description)
	if a.Trainer {
		form.Set("trainer", "1")
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.send(ctx, http.MethodPost, "/activities", accessToken, form, &out); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, fmt.Errorf("strava: create returned no activity id")
	}
	return out.ID, nil
}

// UpdateActivity changes an existing activity's name and description.
func (c *Client) UpdateActivity(ctx context.Context, accessToken string, id int64, name, description string) error {
	form := url.Values{}
	form.Set("name", name)
	form.Set("description", description)
	return c.send(ctx, http.MethodPut, "/activities/"+strconv.FormatInt(id, 10), accessToken, form, nil)
}

func (c *Client) send(ctx context.Context, method, path, accessToken string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.api()+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("strava: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		return ErrForbidden
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("strava: %s %s failed (%d): %s", method, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("strava: decoding response: %w", err)
		}
	}
	return nil
}
