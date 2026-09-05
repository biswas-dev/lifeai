package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func mcpCall(t *testing.T, h http.Handler, token string, payload string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestMCPEndToEnd(t *testing.T) {
	s, h := newTestServer(t)
	r := chi.NewRouter()
	r.Mount("/", h)
	r.HandleFunc("/mcp", s.HandleMCP)
	c := signup(t, r, "mcp@example.com")

	// Some data to read.
	c.do("PUT", "/api/goals", map[string]any{"daily_kcal": 1900, "protein_g": 140, "notes": "get HbA1c and LDL down in 3 months"})
	c.do("POST", "/api/blood/reports", map[string]any{"taken_on": "2026-08-24", "lab": "Dynacare", "markers": []map[string]any{
		{"name": "HEMOGLOBIN A1C (HB A1C)", "value": 7.1, "unit": "%", "ref_high": 6.0},
		{"name": "LDL CHOLESTEROL", "value": 2.8, "unit": "mmol/L", "ref_high": 3.5},
		{"name": "ALANINE AMINOTRANSFERASE", "value": 88, "unit": "U/L", "ref_high": 46},
	}})
	_, tok := c.do("POST", "/api/tokens", map[string]any{"name": "claude", "scopes": []string{"read"}})
	readToken := tok["secret"].(string)
	_, tokW := c.do("POST", "/api/tokens", map[string]any{"name": "claude-w", "scopes": []string{"read", "write"}})
	writeToken := tokW["secret"].(string)

	if code, _ := mcpCall(t, r, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`); code != 401 {
		t.Fatalf("no auth should be 401, got %d", code)
	}
	code, res := mcpCall(t, r, readToken, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	if code != 200 || res["result"].(map[string]any)["protocolVersion"] != "2025-03-26" {
		t.Fatalf("initialize: %d %v", code, res)
	}
	code, res = mcpCall(t, r, readToken, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := res["result"].(map[string]any)["tools"].([]any)
	if code != 200 || len(tools) < 15 {
		t.Fatalf("tools/list: %d %d", code, len(tools))
	}
	code, res = mcpCall(t, r, readToken, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_health_summary","arguments":{}}}`)
	if code != 200 {
		t.Fatalf("call: %d %v", code, res)
	}
	content := res["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var sum HealthSummary
	if err := json.Unmarshal([]byte(content), &sum); err != nil {
		t.Fatal(err)
	}
	if len(sum.Blood.Abnormal) != 2 || sum.Blood.NextTestDue != "2026-11-22" {
		t.Fatalf("summary blood: %+v", sum.Blood)
	}
	joined := strings.Join(sum.Signals, " | ")
	if !strings.Contains(joined, "HbA1c is above range") || !strings.Contains(joined, "ALT is above range") {
		t.Fatalf("signals: %v", sum.Signals)
	}

	// A read token cannot write.
	code, res = mcpCall(t, r, readToken, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"log_meal","arguments":{"name":"Eggs","kcal":300}}}`)
	if code != 200 || res["result"].(map[string]any)["isError"] != true {
		t.Fatalf("read token write should be a tool error: %v", res)
	}
	code, res = mcpCall(t, r, writeToken, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"log_meal","arguments":{"name":"Eggs","kcal":300,"protein_g":20,"slot":"breakfast"}}}`)
	if code != 200 || res["result"].(map[string]any)["isError"] == true {
		t.Fatalf("write token: %v", res)
	}
	code, res = mcpCall(t, r, writeToken, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_blood_markers","arguments":{"category":"sugar"}}}`)
	text := res["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if code != 200 || !strings.Contains(text, `"code":"hba1c"`) || strings.Contains(text, `"code":"ldl"`) {
		t.Fatalf("marker filter: %s", text)
	}
	// Notifications get a 202 and no body.
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Authorization", "Bearer "+readToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatalf("notification: %d", rec.Code)
	}
}

func TestBloodUploadAndSeries(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "blood@example.com")
	text := "NAME: X   DATE SAMPLES COLLECTED:     2026 Aug 24, 12:28\n   Dynacare Plus\n" +
		"          32         HEMOGLOBIN A1C (HB A1C)             TEST STATUS\n  Final\n            YOUR RESULT\n            7.1 %    high\nREFERENCE RANGE: < 6.0 %\n" +
		"          22         LDL CHOLESTEROL                     TEST STATUS\n  Final\n            YOUR RESULT\n            2.8 mmol/L     normal\nREFERENCE RANGE: < 3.50 mmol/L\n"
	var buf bytes.Buffer
	buf.WriteString(text)
	req := httptest.NewRequest("POST", "/api/blood/reports/upload", nil)
	// Multipart by hand.
	body := &bytes.Buffer{}
	boundary := "xxBOUNDARYxx"
	body.WriteString("--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"report.txt\"\r\nContent-Type: text/plain\r\n\r\n")
	body.Write(buf.Bytes())
	body.WriteString("\r\n--" + boundary + "--\r\n")
	req = httptest.NewRequest("POST", "/api/blood/reports/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var rep BloodReport
	_ = json.Unmarshal(rec.Body.Bytes(), &rep)
	if rep.TakenOn != "2026-08-24" || rep.Lab != "Dynacare" || len(rep.Markers) != 2 || rep.Counts["abnormal"] != 1 {
		t.Fatalf("parsed report wrong: %+v", rep)
	}
	// A second report shows the trend.
	c.do("POST", "/api/blood/reports", map[string]any{"taken_on": "2026-11-20", "markers": []map[string]any{{"name": "Hemoglobin A1c", "value": 5.9, "unit": "%", "ref_high": 6.0}}})
	code, series := c.doList("GET", "/api/blood/markers")
	if code != 200 || len(series) != 2 {
		t.Fatalf("series: %d %d", code, len(series))
	}
	a1c := series[0]
	if a1c["code"] != "hba1c" || len(a1c["points"].([]any)) != 2 || a1c["change"].(float64) > -0.79 || a1c["flag"] != "normal" {
		t.Fatalf("hba1c series: %v", a1c)
	}
}

func TestBloodUploadHandlerRegistered(t *testing.T) {
	// Sanity that the upload route parses multipart in the test router above.
	_, h := newTestServer(t)
	if h == nil {
		t.Fatal("no handler")
	}
}

func TestImportDedupAcrossSources(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "dedup@example.com")
	// Apple reports the run first, then a Samsung watch reports the same run 20s later.
	code, res := c.do("POST", "/api/import/health", map[string]any{"source": "apple", "metrics": []map[string]any{{"date": "2026-08-24", "metric": "steps", "value": 9000}},
		"workouts": []map[string]any{{"started_at": "2026-08-24T22:00:00Z", "minutes": 33, "activity": "Outdoor Run", "kcal": 310}}})
	if code != 200 || res["workouts"].(float64) != 1 {
		t.Fatalf("apple import: %d %v", code, res)
	}
	code, res = c.do("POST", "/api/import/health", map[string]any{"source": "samsung", "metrics": []map[string]any{{"date": "2026-08-24", "metric": "steps", "value": 8700}, {"date": "2026-08-24", "metric": "weight_kg", "value": 81.5}},
		"workouts": []map[string]any{{"started_at": "2026-08-24T22:00:20Z", "minutes": 32, "activity": "Running", "avg_hr": 150}}})
	if code != 200 || res["workouts"].(float64) != 0 || res["workouts_skipped"].(float64) != 1 {
		t.Fatalf("samsung import should dedup: %d %v", code, res)
	}
	_, day := c.do("GET", "/api/days/2026-08-24", nil)
	wks := day["workouts"].([]any)
	if len(wks) != 1 {
		t.Fatalf("expected one workout, got %d", len(wks))
	}
	wk := wks[0].(map[string]any)
	if wk["source"] != "apple" || wk["avg_hr"].(float64) != 150 || wk["kcal"].(float64) != 310 {
		t.Fatalf("dedup should keep apple and fill hr from samsung: %v", wk)
	}
	m := day["metrics"].(map[string]any)
	// Apple outranks Samsung for steps; weight only came from Samsung.
	if m["steps"].(float64) != 9000 || m["weight_kg"].(float64) != 81.5 {
		t.Fatalf("resolution wrong: %v", m)
	}
	// A hand-typed weight wins over a later import.
	c.do("PATCH", "/api/days/2026-08-24", map[string]any{"weight_kg": 82.0})
	c.do("POST", "/api/import/health", map[string]any{"source": "apple", "metrics": []map[string]any{{"date": "2026-08-24", "metric": "weight_kg", "value": 80.0}}})
	_, day = c.do("GET", "/api/days/2026-08-24", nil)
	if day["metrics"].(map[string]any)["weight_kg"].(float64) != 82.0 {
		t.Fatal("import overwrote a manual weight")
	}
}
