// Package hard75 is a read-only client for the 75hard API
// (github.com/biswas-dev/75hard), used to pull a person's record into lifeai.
//
// The connection is one way: lifeai reads, and never writes, 75hard. Calls are
// paced to stay under the 75hard host's per-minute rate limit, and a 429 is
// waited out rather than treated as a failure.
package hard75

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one 75hard instance as one user.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	// Pace is the minimum gap between requests. The 75hard nginx allows 100
	// requests a minute with a burst of 30; 650ms keeps a long pull under it.
	Pace time.Duration
	last time.Time
}

// New builds a client. baseURL is the origin, e.g. https://75hard.biswas.me.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Pace:    650 * time.Millisecond,
	}
}

// ErrUnauthorized means the token was rejected.
var ErrUnauthorized = errors.New("75hard: token rejected")

// User is the connected account.
type User struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

// Program is one attempt at the challenge.
type Program struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	StartDate  string `json:"start_date"`
	LengthDays int    `json:"length_days"`
	Status     string `json:"status"`
	CurrentDay int    `json:"current_day"`
}

// DaySummary is one row of a program's calendar.
type DaySummary struct {
	DayNumber  int    `json:"day_number"`
	Date       string `json:"date"`
	Status     string `json:"status"`
	TasksDone  int    `json:"tasks_done"`
	PhotoCount int    `json:"photo_count"`
}

// Photo is a stored image.
type Photo struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Pose      string `json:"pose"`
	DayNumber *int   `json:"day_number"`
	Caption   string `json:"caption"`
	TakenAt   string `json:"taken_at"`
	URL       string `json:"url"`
}

// MealItem is one component of a meal.
type MealItem struct {
	Name       string   `json:"name"`
	Qty        float64  `json:"qty"`
	Unit       string   `json:"unit"`
	Kcal       float64  `json:"kcal"`
	ProteinG   float64  `json:"protein_g"`
	CarbsG     float64  `json:"carbs_g"`
	FatG       float64  `json:"fat_g"`
	Confidence *float64 `json:"confidence"`
}

// Meal is one eating occasion.
type Meal struct {
	ID             int64      `json:"id"`
	PhotoID        *int64     `json:"photo_id"`
	Name           string     `json:"name"`
	Slot           string     `json:"slot"`
	Kcal           float64    `json:"kcal"`
	ProteinG       float64    `json:"protein_g"`
	CarbsG         float64    `json:"carbs_g"`
	FatG           float64    `json:"fat_g"`
	Source         string     `json:"source"`
	Notes          string     `json:"notes"`
	EatenAt        string     `json:"eaten_at"`
	Items          []MealItem `json:"items"`
	EstimateStatus string     `json:"estimate_status"`
}

// Workout is a training session.
type Workout struct {
	ID        int64    `json:"id"`
	Kind      string   `json:"kind"`
	Activity  string   `json:"activity"`
	Minutes   int      `json:"minutes"`
	Kcal      *float64 `json:"kcal"`
	Notes     string   `json:"notes"`
	StartedAt string   `json:"started_at"`
}

// Meditation is a sitting.
type Meditation struct {
	ID         int64  `json:"id"`
	Minutes    int    `json:"minutes"`
	Style      string `json:"style"`
	Notes      string `json:"notes"`
	Reflection string `json:"reflection"`
	StartedAt  string `json:"started_at"`
}

// Day is everything logged on one day of a program.
type Day struct {
	ProgramID   int64        `json:"program_id"`
	DayNumber   int          `json:"day_number"`
	Date        string       `json:"date"`
	Status      string       `json:"status"`
	Note        string       `json:"note"`
	WeightKg    *float64     `json:"weight_kg"`
	RestingHR   *int         `json:"resting_hr"`
	Photos      []Photo      `json:"photos"`
	Meals       []Meal       `json:"meals"`
	Workouts    []Workout    `json:"workouts"`
	Meditations []Meditation `json:"meditations"`
	Entries     []Entry      `json:"entries"`
}

// Entry preserves the challenge's daily habit readings and checkboxes.
type Entry struct {
	Key   string   `json:"key"`
	Title string   `json:"title"`
	Unit  string   `json:"unit"`
	Value *float64 `json:"value"`
	Note  string   `json:"note"`
	Done  bool     `json:"done"`
}

// JournalEntry is a typed or scanned page.
type JournalEntry struct {
	ID         int64  `json:"id"`
	DayNumber  *int   `json:"day_number"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Body       string `json:"body"`
	ParsedText string `json:"parsed_text"`
	CreatedAt  string `json:"created_at"`
}

// Me verifies the token and returns the account it belongs to.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	if err := c.getJSON(ctx, "/api/me", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Programs lists every attempt, newest first.
func (c *Client) Programs(ctx context.Context) ([]Program, error) {
	var out []Program
	err := c.getJSON(ctx, "/api/programs", nil, &out)
	return out, err
}

// Days lists the calendar of a program.
func (c *Client) Days(ctx context.Context, programID int64) ([]DaySummary, error) {
	var out []DaySummary
	err := c.getJSON(ctx, "/api/programs/"+strconv.FormatInt(programID, 10)+"/days", nil, &out)
	return out, err
}

// Day fetches one day in full.
func (c *Client) Day(ctx context.Context, programID int64, dayNumber int) (*Day, error) {
	var out Day
	err := c.getJSON(ctx, fmt.Sprintf("/api/programs/%d/days/%d", programID, dayNumber), nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Photos lists photos of a kind, newest first.
func (c *Client) Photos(ctx context.Context, kind string, limit int) ([]Photo, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out []Photo
	err := c.getJSON(ctx, "/api/photos", q, &out)
	return out, err
}

// PhotoBytes downloads the full-size stored image.
func (c *Client) PhotoBytes(ctx context.Context, id int64) ([]byte, string, error) {
	resp, err := c.do(ctx, "/api/photos/"+strconv.FormatInt(id, 10)+"/file", nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20))
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// Journal lists every entry.
func (c *Client) Journal(ctx context.Context) ([]JournalEntry, error) {
	var out []JournalEntry
	err := c.getJSON(ctx, "/api/journal", nil, &out)
	return out, err
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, v any) error {
	resp, err := c.do(ctx, path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("75hard: %s: bad JSON: %w", path, err)
	}
	return nil
}

// do performs a paced GET, waiting out a 429 up to three times.
func (c *Client) do(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	for attempt := 0; attempt < 4; attempt++ {
		if wait := c.Pace - time.Since(c.last); wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/json, image/*")
		req.Header.Set("User-Agent", "lifeai-sync/1.0")
		c.last = time.Now()
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("75hard: %s: %w", path, err)
		}
		switch {
		case resp.StatusCode == http.StatusOK:
			return resp, nil
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			resp.Body.Close()
			return nil, ErrUnauthorized
		case resp.StatusCode == http.StatusTooManyRequests:
			resp.Body.Close()
			delay := 30 * time.Second
			if ra, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && ra > 0 && ra <= 120 {
				delay = time.Duration(ra) * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			continue
		default:
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return nil, fmt.Errorf("75hard: %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
		}
	}
	return nil, fmt.Errorf("75hard: %s: rate limited repeatedly", path)
}
