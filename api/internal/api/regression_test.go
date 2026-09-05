package api

import (
	"bytes"
	"context"
	"github.com/biswas-dev/lifeai/api/internal/integrations/hard75"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBloodSeriesKeepDifferentUnitsSeparate(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "units@example.com")
	for _, row := range []map[string]any{
		{"taken_on": "2026-06-01", "markers": []map[string]any{{"name": "LDL cholesterol", "value": 2.5, "unit": "mmol/L"}}},
		{"taken_on": "2026-07-01", "markers": []map[string]any{{"name": "LDL cholesterol", "value": 97, "unit": "mg/dL"}}},
	} {
		if code, _ := c.do("POST", "/api/blood/reports", row); code != 201 {
			t.Fatal(code)
		}
	}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM users WHERE email=?`, "units@example.com").Scan(&id); err != nil {
		t.Fatal(err)
	}
	series, err := s.markerSeries(context.Background(), id)
	if err != nil || len(series) != 2 {
		t.Fatalf("separate units: %v %+v", err, series)
	}
	for _, v := range series {
		if len(v.Points) != 1 || v.Change != nil {
			t.Fatalf("incompatible values were compared: %+v", v)
		}
	}
}

func TestHard75DeduplicatesWorkoutAfterHealthImport(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "dedup@example.com")
	c.do("POST", "/api/import/health", map[string]any{"source": "apple", "workouts": []map[string]any{{"started_at": "2026-07-01T18:00:00Z", "minutes": 40, "activity": "Running"}}})
	var id int64
	s.db.QueryRow(`SELECT id FROM users WHERE email=?`, "dedup@example.com").Scan(&id)
	day := &hard75.Day{Date: "2026-07-01", Workouts: []hard75.Workout{{ID: 99, Minutes: 40, Activity: "Running", StartedAt: "2026-07-01T18:00:20Z"}}}
	for i := 0; i < 2; i++ {
		if err := s.syncer.importDay(context.Background(), id, day, map[int64]int64{}, &SyncSummary{}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM workouts WHERE user_id=?`, id).Scan(&n)
	if n != 1 {
		t.Fatalf("same workout counted %d times", n)
	}
}

func TestMCPRejectsDeletedUserSession(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "deleted@example.com")
	s.db.Exec(`UPDATE users SET deleted_at=CURRENT_TIMESTAMP WHERE email=?`, "deleted@example.com")
	code, _ := mcpCall(t, http.HandlerFunc(s.HandleMCP), c.token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != 401 {
		t.Fatalf("deleted session accepted: %d", code)
	}
}

func TestMCPRejectsForeignOrigin(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Origin", "https://unrelated.example")
	res := httptest.NewRecorder()
	s.HandleMCP(res, req)
	if res.Code != 403 {
		t.Fatalf("foreign origin accepted: %d", res.Code)
	}
}

func TestMCPPhotoReadIsOwnerScoped(t *testing.T) {
	s, h := newTestServer(t)
	signup(t, h, "photo-owner@example.com")
	signup(t, h, "photo-other@example.com")
	var owner, other int64
	s.db.QueryRow(`SELECT id FROM users WHERE email=?`, "photo-owner@example.com").Scan(&owner)
	s.db.QueryRow(`SELECT id FROM users WHERE email=?`, "photo-other@example.com").Scan(&other)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 20, 20)), nil); err != nil {
		t.Fatal(err)
	}
	saved, err := s.photos.SaveBytes(buf.Bytes(), owner, "progress", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ensureDay(context.Background(), owner, "2026-07-01"); err != nil {
		t.Fatal(err)
	}
	res, err := s.db.Exec(`INSERT INTO photos(user_id,on_date,kind,rel_path,thumb_path,mime,width,height,bytes,sha256) VALUES(?,?,'progress',?,?,?,?,?,?,?)`, owner, "2026-07-01", saved.RelPath, saved.ThumbPath, saved.Mime, saved.Width, saved.Height, saved.Bytes, saved.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	tool := mcpToolByName["get_photo"]
	if _, err := tool.run(context.Background(), s, other, map[string]any{"id": float64(id)}); err == nil {
		t.Fatal("another user read a private photo")
	}
	result, err := tool.run(context.Background(), s, owner, map[string]any{"id": float64(id)})
	if err != nil {
		t.Fatal(err)
	}
	if img, ok := result.(mcpImageContent); !ok || img.Type != "image" || img.Data == "" {
		t.Fatal("missing image content")
	}
}
