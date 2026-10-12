package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestSupplementsDailyLoggingAndBackfill(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "supplements@example.com")
	code, sp := c.do("POST", "/api/supplements", map[string]any{"name": "Creatine", "product": "Nutratology Creatine Monohydrate", "dose": "1 scoop (5 g)", "taken_with": "cold water", "started_on": "2026-10-02"})
	if code != 201 || sp["taken_with"] != "cold water" || sp["active"] != true {
		t.Fatalf("create: %d %v", code, sp)
	}
	base := fmt.Sprintf("/api/supplements/%d", int64(sp["id"].(float64)))
	if code, _ := c.do("POST", "/api/supplements", map[string]any{"product": "no name"}); code != 400 {
		t.Fatalf("nameless supplement: %d", code)
	}
	code, res := c.do("POST", base+"/backfill", map[string]any{"to": "2026-10-11", "skip": []string{"2026-10-10"}})
	if code != 200 || res["logged"] != float64(9) {
		t.Fatalf("backfill: %d %v", code, res)
	}
	// One tap logs today with the usual drink; another way can be given.
	if code, l := c.do("PUT", base+"/days/2026-10-12", nil); code != 200 || l["taken_with"] != "cold water" {
		t.Fatalf("take: %d %v", code, l)
	}
	_, day := c.do("GET", "/api/days/2026-10-12", nil)
	list := day["supplements"].([]any)
	if len(list) != 1 {
		t.Fatalf("day supplements: %v", day["supplements"])
	}
	d := list[0].(map[string]any)
	if d["taken"] == nil || d["streak"] != float64(2) || d["days_taken"] != float64(10) || d["days_since"] != float64(11) || fmt.Sprint(d["missed"]) != "[2026-10-10]" {
		t.Fatalf("consistency: %v", d)
	}
	if code, _ := c.do("DELETE", base+"/days/2026-10-12", nil); code != 200 {
		t.Fatal("untake")
	}
	_, day = c.do("GET", "/api/days/2026-10-12", nil)
	if d := day["supplements"].([]any)[0].(map[string]any); d["taken"] != nil || d["streak"] != float64(1) {
		t.Fatalf("after untake: %v", d)
	}
	// Not shown before it was started.
	if _, day = c.do("GET", "/api/days/2026-09-30", nil); len(day["supplements"].([]any)) != 0 {
		t.Fatal("shown before started_on")
	}
	// Renaming keeps the history.
	if code, up := c.do("PATCH", base, map[string]any{"product": "Nutratology Creatine Monohydrate, micronized"}); code != 200 || up["dose"] != "1 scoop (5 g)" {
		t.Fatalf("patch: %d %v", code, up)
	}
}

func TestSupplementImage(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "supplement-photo@example.com")
	_, sp := c.do("POST", "/api/supplements", map[string]any{"name": "Greens"})
	path := fmt.Sprintf("/api/supplements/%d/image", int64(sp["id"].(float64)))
	put := func(body []byte) (int, map[string]any) {
		req := httptest.NewRequest("PUT", path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+c.token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, _ := put([]byte("<svg onload=alert(1)>")); code != 400 {
		t.Fatalf("non-image accepted: %d", code)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	code, up := put(buf.Bytes())
	if code != 200 || up["image_url"] == nil {
		t.Fatalf("upload: %d %v", code, up)
	}
	req := httptest.NewRequest("GET", up["image_url"].(string), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), buf.Bytes()) {
		t.Fatalf("serve: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	other := signup(t, h, "someone-else@example.com")
	req = httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+other.token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("another account read the photo: %d", rec.Code)
	}
}

func TestMCPSupplementTools(t *testing.T) {
	s, h := newTestServer(t)
	r := chi.NewRouter()
	r.Mount("/", h)
	r.HandleFunc("/mcp", s.HandleMCP)
	c := signup(t, r, "mcp-supplements@example.com")
	_, tok := c.do("POST", "/api/tokens", map[string]any{"name": "agent", "scopes": []string{"read", "write"}})
	call := func(name string, args any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		_, res := mcpCall(t, r, tok["secret"].(string), string(b))
		result := res["result"].(map[string]any)
		if result["isError"] == true {
			t.Fatalf("%s: %v", name, result)
		}
		return result["structuredContent"].(map[string]any)
	}
	call("save_supplement", map[string]any{"name": "Magic Scoop", "product": "Magic Scoop All-In-One Greens, Strawberry Lemonade", "taken_with": "cold water", "started_on": "2026-09-30"})
	if got := call("backfill_supplement", map[string]any{"name": "magic scoop", "to": "2026-10-11", "skip": []string{"2026-10-10"}}); got["logged"] != float64(11) {
		t.Fatalf("backfill: %v", got)
	}
	if got := call("log_supplement", map[string]any{"name": "greens", "date": "2026-10-12"}); got["taken"] != true {
		t.Fatalf("log: %v", got)
	}
	got := call("list_supplements", map[string]any{"date": "2026-10-12"})
	sp := got["supplements"].([]any)[0].(map[string]any)
	if sp["streak"] != float64(2) || sp["taken"].(map[string]any)["taken_with"] != "cold water" {
		t.Fatalf("list: %v", sp)
	}
}
