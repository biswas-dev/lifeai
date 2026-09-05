package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	goapi "github.com/anchoo2kewl/go-api"
	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/biswas-dev/lifeai/api/internal/blood"
	"github.com/biswas-dev/lifeai/api/internal/dates"
	"github.com/biswas-dev/lifeai/api/internal/version"
)

// MCP over streamable HTTP, served from the same binary at /mcp.
//
// Every tool reads or writes the caller's own record and none of them calls
// a model: the point is to hand an agent the numbers so it can do the
// analysis itself, rather than paying for the same analysis twice. The
// transport is the stateless subset of the spec — one JSON-RPC request per
// POST, one JSON response — which is what Claude Code, Cursor and the rest
// speak when pointed at a URL.

const mcpProtocolVersion = "2025-03-26"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	write       bool
	run         func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error)
}

// HandleMCP is the endpoint.
func (s *Server) HandleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// No server-initiated stream; the spec allows a 405 here.
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodPost:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID, scopes, ok := s.mcpAuth(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	var reqs []rpcRequest
	batch := body[0] == '['
	if batch {
		if err := json.Unmarshal(body, &reqs); err != nil {
			writeRPC(w, []rpcResponse{{JSONRPC: "2.0", Error: &rpcError{-32700, "parse error"}}}, false)
			return
		}
	} else {
		var one rpcRequest
		if err := json.Unmarshal(body, &one); err != nil {
			writeRPC(w, []rpcResponse{{JSONRPC: "2.0", Error: &rpcError{-32700, "parse error"}}}, false)
			return
		}
		reqs = []rpcRequest{one}
	}

	var out []rpcResponse
	for _, req := range reqs {
		resp, respond := s.mcpDispatch(r.Context(), userID, scopes, req)
		if respond {
			out = append(out, resp)
		}
	}
	if len(out) == 0 {
		// Notifications only.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeRPC(w, out, batch)
}

func writeRPC(w http.ResponseWriter, out []rpcResponse, batch bool) {
	w.Header().Set("Content-Type", "application/json")
	if batch {
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	_ = json.NewEncoder(w).Encode(out[0])
}

// mcpAuth accepts a session JWT or an API token. A read token may call the
// read tools; the write tools check scopes at call time.
func (s *Server) mcpAuth(w http.ResponseWriter, r *http.Request) (int64, goapi.Scopes, bool) {
	header := r.Header.Get("Authorization")
	scheme, credential, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		w.Header().Set("WWW-Authenticate", `Bearer realm="lifeai"`)
		http.Error(w, `{"error":"authorization required: send Authorization: Bearer <lifeai API token>"}`, http.StatusUnauthorized)
		return 0, nil, false
	}
	if TokenScheme.Issued(credential) {
		userID, record, err := s.tokenAuthenticator().Authenticate(r.Context(), credential, http.MethodGet)
		if err != nil {
			http.Error(w, `{"error":"`+goapi.PublicMessage(err)+`"}`, goapi.StatusFor(err))
			return 0, nil, false
		}
		return userID, record.Scopes, true
	}
	claims, err := auth.ValidateToken(credential, s.cfg.JWTSecret)
	if err != nil {
		http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
		return 0, nil, false
	}
	var active bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT deleted_at IS NULL FROM users WHERE id = ?`, claims.UserID).Scan(&active); err != nil || !active {
		http.Error(w, `{"error":"account unavailable"}`, http.StatusUnauthorized)
		return 0, nil, false
	}
	return claims.UserID, goapi.Scopes{"read", "write"}, true
}

func (s *Server) mcpDispatch(ctx context.Context, userID int64, scopes goapi.Scopes, req rpcRequest) (rpcResponse, bool) {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "lifeai", "version": version.Version},
			"instructions": "lifeai holds one person's food, training, body metrics, blood work, recipes and journal. " +
				"Start with get_health_summary for the computed picture, then drill into days, stats, blood markers or recipes. " +
				"Dates are YYYY-MM-DD in the person's own timezone.",
		}
	case "notifications/initialized", "notifications/cancelled":
		return resp, false
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		tools := make([]map[string]any, 0, len(mcpTools))
		for _, t := range mcpTools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema})
		}
		resp.Result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{-32602, "invalid params"}
			break
		}
		tool, ok := mcpToolByName[p.Name]
		if !ok {
			resp.Error = &rpcError{-32602, "unknown tool: " + p.Name}
			break
		}
		if tool.write && !scopes.Has("write") {
			resp.Result = toolError("this API token is read-only; create one with the write scope to use " + p.Name)
			break
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		callCtx := context.WithValue(ctx, UserIDKey, userID)
		result, err := tool.run(callCtx, s, userID, p.Arguments)
		if err != nil {
			s.log.Warn("mcp tool failed", zap.String("tool", p.Name), zap.Error(err))
			resp.Result = toolError(err.Error())
			break
		}
		b, _ := json.Marshal(result)
		resp.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
	default:
		if isNotification {
			return resp, false
		}
		resp.Error = &rpcError{-32601, "method not found: " + req.Method}
	}
	return resp, !isNotification
}

func toolError(msg string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": msg}}, "isError": true}
}

// ---- argument helpers ----

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func argFloat(args map[string]any, key string) (float64, bool) {
	switch v := args[key].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

func argInt(args map[string]any, key string) (int, bool) {
	f, ok := argFloat(args, key)
	return int(f), ok
}

func argBool(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return isTrue(v)
	}
	return false
}

func argStrings(args map[string]any, key string) []string {
	var out []string
	switch v := args[key].(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.Split(v, "\n") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func schema(props map[string]any, required ...string) map[string]any {
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func (s *Server) mcpDate(ctx context.Context, args map[string]any) (string, error) {
	d := argString(args, "date")
	if d == "" || d == "today" {
		return s.today(ctx), nil
	}
	if !dates.Valid(d) {
		return "", errors.New("date must be YYYY-MM-DD")
	}
	return d, nil
}

// ---- tools ----

var mcpTools []mcpTool
var mcpToolByName = map[string]mcpTool{}

func init() {
	mcpTools = []mcpTool{
		{
			Name:        "get_health_summary",
			Description: "The computed picture: profile with BMI, goals, latest blood markers (watch list and anything out of range), 30- and 90-day training and nutrition stats, today, and plain-language signals. Start here.",
			InputSchema: schema(map[string]any{}),
			run: func(ctx context.Context, s *Server, userID int64, _ map[string]any) (any, error) {
				return s.healthSummary(ctx, userID)
			},
		},
		{
			Name:        "get_profile",
			Description: "Name, email, timezone, date of birth, sex, height and unit preference.",
			InputSchema: schema(map[string]any{}),
			run: func(ctx context.Context, s *Server, userID int64, _ map[string]any) (any, error) {
				return s.getUser(ctx, userID)
			},
		},
		{
			Name:        "get_goals",
			Description: "Daily calorie and macro targets, target weight, steps, water, sleep, weekly training minutes, and the person's own notes on what they are working toward.",
			InputSchema: schema(map[string]any{}),
			run: func(ctx context.Context, s *Server, userID int64, _ map[string]any) (any, error) {
				return s.goals(ctx, userID)
			},
		},
		{
			Name:        "set_goals",
			Description: "Replace the targets. Omit a field to clear it.",
			InputSchema: schema(map[string]any{
				"daily_kcal": prop("integer", "kcal per day"), "protein_g": prop("integer", ""), "carbs_g": prop("integer", ""), "fat_g": prop("integer", ""),
				"target_weight_kg": prop("number", ""), "steps": prop("integer", ""), "water_ml": prop("integer", ""), "sleep_hours": prop("number", ""),
				"workout_minutes": prop("integer", "per week"), "notes": prop("string", "free text the coach reads"),
			}),
			write: true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				b, _ := json.Marshal(args)
				var g Goals
				if err := json.Unmarshal(b, &g); err != nil {
					return nil, err
				}
				_, err := s.db.ExecContext(ctx, `
					INSERT INTO goals (user_id, daily_kcal, protein_g, carbs_g, fat_g, target_weight_kg, steps, water_ml, sleep_hours, workout_minutes, notes, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
					ON CONFLICT(user_id) DO UPDATE SET daily_kcal = excluded.daily_kcal, protein_g = excluded.protein_g, carbs_g = excluded.carbs_g, fat_g = excluded.fat_g,
						target_weight_kg = excluded.target_weight_kg, steps = excluded.steps, water_ml = excluded.water_ml, sleep_hours = excluded.sleep_hours,
						workout_minutes = excluded.workout_minutes, notes = excluded.notes, updated_at = CURRENT_TIMESTAMP`,
					userID, nullInt(g.DailyKcal), nullInt(g.ProteinG), nullInt(g.CarbsG), nullInt(g.FatG), nullFloat(g.TargetWeightKg), nullInt(g.Steps), nullInt(g.WaterMl), nullFloat(g.SleepHours), nullInt(g.WorkoutMinutes), strings.TrimSpace(g.Notes))
				if err != nil {
					return nil, err
				}
				return s.goals(ctx, userID)
			},
		},
		{
			Name:        "get_day",
			Description: "Everything logged on one date: body metrics, meals with items, workouts, meditation, journal, photos, totals against goals.",
			InputSchema: schema(map[string]any{"date": prop("string", "YYYY-MM-DD, or 'today' (default)")}),
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				d, err := s.mcpDate(ctx, args)
				if err != nil {
					return nil, err
				}
				return s.loadDay(ctx, userID, d)
			},
		},
		{
			Name:        "list_days",
			Description: "One summary row per date in a range (kcal, protein, weight, training minutes, steps, sleep, mood). Default: the last 30 days.",
			InputSchema: schema(map[string]any{"from": prop("string", "YYYY-MM-DD"), "to": prop("string", "YYYY-MM-DD")}),
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				to := argString(args, "to")
				if to == "" {
					to = s.today(ctx)
				}
				from := argString(args, "from")
				if from == "" {
					from = dates.AddDays(to, -29)
				}
				if !dates.Valid(from) || !dates.Valid(to) {
					return nil, errors.New("from and to must be YYYY-MM-DD")
				}
				if dates.DaysBetween(from, to) > 400 {
					return nil, errors.New("range too large (400 days max)")
				}
				return s.daySummaries(ctx, userID, from, to)
			},
		},
		{
			Name:        "get_stats",
			Description: "Trends over a window: weight, body fat, resting HR, sleep, steps, kcal, protein, training series with averages, streak and calorie adherence.",
			InputSchema: schema(map[string]any{"days": prop("integer", "window length, 7 to 730 (default 90)")}),
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				days, ok := argInt(args, "days")
				if !ok || days < 7 || days > 730 {
					days = 90
				}
				return s.stats(ctx, userID, days)
			},
		},
		{
			Name:        "log_metrics",
			Description: "Record body metrics for a date. Only the fields given change.",
			InputSchema: schema(map[string]any{
				"date": prop("string", "YYYY-MM-DD, default today"), "weight_kg": prop("number", ""), "body_fat_pct": prop("number", ""), "resting_hr": prop("integer", ""),
				"sleep_hours": prop("number", ""), "steps": prop("integer", ""), "water_ml": prop("integer", ""), "mood": prop("integer", "1-5"), "energy": prop("integer", "1-5"), "note": prop("string", ""),
			}),
			write: true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				d, err := s.mcpDate(ctx, args)
				if err != nil {
					return nil, err
				}
				if err := s.ensureDay(ctx, userID, d); err != nil {
					return nil, err
				}
				var touched []string
				for _, col := range []string{"weight_kg", "body_fat_pct", "resting_hr", "sleep_hours", "steps", "water_ml", "mood", "energy"} {
					if v, ok := argFloat(args, col); ok {
						if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE days SET %s = ?, source = 'manual', updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND on_date = ?`, col), v, userID, d); err != nil {
							return nil, err
						}
						touched = append(touched, col)
					}
				}
				if n, ok := args["note"].(string); ok {
					if _, err := s.db.ExecContext(ctx, `UPDATE days SET note = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND on_date = ?`, strings.TrimSpace(n), userID, d); err != nil {
						return nil, err
					}
				}
				if len(touched) > 0 {
					_ = s.markManual(ctx, userID, d, touched)
				}
				return s.loadDay(ctx, userID, d)
			},
		},
		{
			Name:        "log_meal",
			Description: "Log a meal with its calories and macros.",
			InputSchema: schema(map[string]any{
				"date": prop("string", "YYYY-MM-DD, default today"), "name": prop("string", ""), "slot": prop("string", "breakfast|lunch|dinner|snack"),
				"kcal": prop("number", ""), "protein_g": prop("number", ""), "carbs_g": prop("number", ""), "fat_g": prop("number", ""), "notes": prop("string", ""),
			}, "name", "kcal"),
			write: true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				d, err := s.mcpDate(ctx, args)
				if err != nil {
					return nil, err
				}
				slot := strings.ToLower(argString(args, "slot"))
				if !validSlot(slot) {
					slot = slotForTime(time.Now().In(s.userLocation(ctx)))
				}
				kcal, _ := argFloat(args, "kcal")
				p, _ := argFloat(args, "protein_g")
				c, _ := argFloat(args, "carbs_g")
				f, _ := argFloat(args, "fat_g")
				if err := s.ensureDay(ctx, userID, d); err != nil {
					return nil, err
				}
				res, err := s.db.ExecContext(ctx, `INSERT INTO meals (user_id, on_date, name, slot, kcal, protein_g, carbs_g, fat_g, source, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'manual', ?)`,
					userID, d, argString(args, "name"), slot, kcal, p, c, f, argString(args, "notes"))
				if err != nil {
					return nil, err
				}
				id, _ := res.LastInsertId()
				return s.mealByID(ctx, userID, id)
			},
		},
		{
			Name:        "log_workout",
			Description: "Log a training session.",
			InputSchema: schema(map[string]any{
				"date": prop("string", "YYYY-MM-DD, default today"), "kind": prop("string", strings.Join(WorkoutKinds, "|")), "activity": prop("string", "free text"),
				"minutes": prop("integer", ""), "kcal": prop("number", ""), "distance_km": prop("number", ""), "avg_hr": prop("integer", ""), "notes": prop("string", ""),
			}, "minutes"),
			write: true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				d, err := s.mcpDate(ctx, args)
				if err != nil {
					return nil, err
				}
				mins, ok := argInt(args, "minutes")
				if !ok || mins <= 0 {
					return nil, errors.New("minutes must be positive")
				}
				kind := strings.ToLower(argString(args, "kind"))
				if !validWorkoutKind(kind) {
					kind = "other"
				}
				var kcal, dist any
				if v, ok := argFloat(args, "kcal"); ok {
					kcal = v
				}
				if v, ok := argFloat(args, "distance_km"); ok {
					dist = v
				}
				var hr any
				if v, ok := argInt(args, "avg_hr"); ok {
					hr = v
				}
				if err := s.ensureDay(ctx, userID, d); err != nil {
					return nil, err
				}
				res, err := s.db.ExecContext(ctx, `INSERT INTO workouts (user_id, on_date, kind, activity, minutes, kcal, distance_km, avg_hr, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					userID, d, kind, argString(args, "activity"), mins, kcal, dist, hr, argString(args, "notes"))
				if err != nil {
					return nil, err
				}
				id, _ := res.LastInsertId()
				return s.workoutByID(ctx, userID, id)
			},
		},
		{
			Name:        "log_meditation",
			Description: "Log a meditation sitting.",
			InputSchema: schema(map[string]any{"date": prop("string", ""), "minutes": prop("integer", ""), "style": prop("string", strings.Join(MeditationStyles, "|")), "notes": prop("string", "")}, "minutes"),
			write:       true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				d, err := s.mcpDate(ctx, args)
				if err != nil {
					return nil, err
				}
				mins, ok := argInt(args, "minutes")
				if !ok || mins <= 0 {
					return nil, errors.New("minutes must be positive")
				}
				if err := s.ensureDay(ctx, userID, d); err != nil {
					return nil, err
				}
				style := strings.ToLower(argString(args, "style"))
				if style == "" {
					style = "guided"
				}
				_, err = s.db.ExecContext(ctx, `INSERT INTO meditations (user_id, on_date, minutes, style, notes, started_at) VALUES (?, ?, ?, ?, ?, ?)`, userID, d, mins, style, argString(args, "notes"), time.Now().UTC())
				if err != nil {
					return nil, err
				}
				return map[string]any{"ok": true, "date": d, "minutes": mins}, nil
			},
		},
		{
			Name:        "add_journal",
			Description: "Write a journal entry against a date.",
			InputSchema: schema(map[string]any{"date": prop("string", ""), "title": prop("string", ""), "body": prop("string", "")}, "body"),
			write:       true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				d, err := s.mcpDate(ctx, args)
				if err != nil {
					return nil, err
				}
				body := argString(args, "body")
				if body == "" {
					return nil, errors.New("body is required")
				}
				if err := s.ensureDay(ctx, userID, d); err != nil {
					return nil, err
				}
				res, err := s.db.ExecContext(ctx, `INSERT INTO journal_entries (user_id, on_date, title, body) VALUES (?, ?, ?, ?)`, userID, d, argString(args, "title"), body)
				if err != nil {
					return nil, err
				}
				id, _ := res.LastInsertId()
				return scanJournal(s.db.QueryRowContext(ctx, `SELECT `+journalColumns+` FROM journal_entries WHERE id = ?`, id))
			},
		},
		{
			Name:        "list_journal",
			Description: "Journal entries, newest first, optionally searched.",
			InputSchema: schema(map[string]any{"q": prop("string", "search text"), "limit": prop("integer", "default 50")}),
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				limit, ok := argInt(args, "limit")
				if !ok || limit <= 0 || limit > 500 {
					limit = 50
				}
				q := strings.ToLower(argString(args, "q"))
				query := `SELECT ` + journalColumns + ` FROM journal_entries WHERE user_id = ?`
				a := []any{userID}
				if q != "" {
					query += ` AND (lower(title) LIKE ? OR lower(body) LIKE ?)`
					a = append(a, "%"+q+"%", "%"+q+"%")
				}
				query += ` ORDER BY on_date DESC, id DESC LIMIT ?`
				a = append(a, limit)
				rows, err := s.db.QueryContext(ctx, query, a...)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				out := []JournalEntry{}
				for rows.Next() {
					if e, err := scanJournal(rows); err == nil {
						out = append(out, e)
					}
				}
				return out, nil
			},
		},
		{
			Name:        "list_recipes",
			Description: "The recipe library with per-serving macros. Filter by search text, tag or favourites.",
			InputSchema: schema(map[string]any{"q": prop("string", ""), "tag": prop("string", ""), "favourite": prop("boolean", "")}),
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				query := `SELECT ` + recipeColumns + ` FROM recipes WHERE user_id = ?`
				a := []any{userID}
				if q := strings.ToLower(argString(args, "q")); q != "" {
					query += ` AND (lower(name) LIKE ? OR lower(summary) LIKE ? OR lower(ingredients_json) LIKE ? OR tags LIKE ?)`
					like := "%" + q + "%"
					a = append(a, like, like, like, like)
				}
				if tag := strings.ToLower(argString(args, "tag")); tag != "" {
					query += ` AND (',' || tags || ',') LIKE ?`
					a = append(a, "%,"+tag+",%")
				}
				if argBool(args, "favourite") {
					query += ` AND favourite = 1`
				}
				query += ` ORDER BY favourite DESC, updated_at DESC LIMIT 300`
				rows, err := s.db.QueryContext(ctx, query, a...)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				out := []Recipe{}
				for rows.Next() {
					if rc, err := scanRecipe(rows); err == nil {
						out = append(out, rc)
					}
				}
				return out, nil
			},
		},
		{
			Name:        "get_recipe",
			Description: "One recipe in full: ingredients, steps, macros.",
			InputSchema: schema(map[string]any{"id": prop("integer", "")}, "id"),
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				id, ok := argInt(args, "id")
				if !ok {
					return nil, errors.New("id is required")
				}
				return s.recipeByID(ctx, userID, int64(id))
			},
		},
		{
			Name:        "add_recipe",
			Description: "Add a recipe to the library. Macros are per serving.",
			InputSchema: schema(map[string]any{
				"name": prop("string", ""), "summary": prop("string", ""), "minutes": prop("integer", ""), "servings": prop("integer", ""),
				"kcal_per_serving": prop("number", ""), "protein_g": prop("number", ""), "carbs_g": prop("number", ""), "fat_g": prop("number", ""),
				"ingredients": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "steps": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}, "name", "ingredients"),
			write: true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				req := recipeRequest{Name: argString(args, "name"), Summary: argString(args, "summary"), Ingredients: argStrings(args, "ingredients"), Steps: argStrings(args, "steps"), Tags: argStrings(args, "tags"), Source: "import"}
				req.Minutes, _ = argInt(args, "minutes")
				req.Servings, _ = argInt(args, "servings")
				req.KcalPerServing, _ = argFloat(args, "kcal_per_serving")
				req.ProteinG, _ = argFloat(args, "protein_g")
				req.CarbsG, _ = argFloat(args, "carbs_g")
				req.FatG, _ = argFloat(args, "fat_g")
				if msg, _ := req.validate(); msg != "" {
					return nil, errors.New(msg)
				}
				ing, _ := json.Marshal(req.Ingredients)
				steps, _ := json.Marshal(req.Steps)
				res, err := s.db.ExecContext(ctx, `
					INSERT INTO recipes (user_id, name, summary, minutes, servings, kcal_per_serving, protein_g, carbs_g, fat_g, ingredients_json, steps_json, tags, source)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'import')`,
					userID, req.Name, req.Summary, req.Minutes, req.Servings, req.KcalPerServing, req.ProteinG, req.CarbsG, req.FatG, string(ing), string(steps), strings.Join(req.Tags, ","))
				if err != nil {
					return nil, err
				}
				id, _ := res.LastInsertId()
				return s.recipeByID(ctx, userID, id)
			},
		},
		{
			Name:        "cook_recipe",
			Description: "Log a meal from a recipe, scaled by servings eaten.",
			InputSchema: schema(map[string]any{"id": prop("integer", ""), "date": prop("string", ""), "slot": prop("string", ""), "servings": prop("number", "default 1")}, "id"),
			write:       true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				id, ok := argInt(args, "id")
				if !ok {
					return nil, errors.New("id is required")
				}
				rc, err := s.recipeByID(ctx, userID, int64(id))
				if err != nil {
					return nil, errors.New("recipe not found")
				}
				d, err := s.mcpDate(ctx, args)
				if err != nil {
					return nil, err
				}
				servings, ok := argFloat(args, "servings")
				if !ok || servings <= 0 {
					servings = 1
				}
				slot := strings.ToLower(argString(args, "slot"))
				if !validSlot(slot) {
					slot = slotForTime(time.Now().In(s.userLocation(ctx)))
				}
				if err := s.ensureDay(ctx, userID, d); err != nil {
					return nil, err
				}
				res, err := s.db.ExecContext(ctx, `INSERT INTO meals (user_id, on_date, photo_id, recipe_id, name, slot, kcal, protein_g, carbs_g, fat_g, source) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'recipe')`,
					userID, d, rc.PhotoID, rc.ID, rc.Name, slot, rc.KcalPerServing*servings, rc.ProteinG*servings, rc.CarbsG*servings, rc.FatG*servings)
				if err != nil {
					return nil, err
				}
				_, _ = s.db.ExecContext(ctx, `UPDATE recipes SET times_cooked = times_cooked + 1, last_cooked_at = CURRENT_TIMESTAMP WHERE id = ?`, rc.ID)
				mid, _ := res.LastInsertId()
				return s.mealByID(ctx, userID, mid)
			},
		},
		{
			Name:        "list_blood_reports",
			Description: "Lab reports newest first, with counts of normal and abnormal markers.",
			InputSchema: schema(map[string]any{}),
			run: func(ctx context.Context, s *Server, userID int64, _ map[string]any) (any, error) {
				rows, err := s.db.QueryContext(ctx, `SELECT id, taken_on, lab, ordered_by, notes, parse_status FROM blood_reports WHERE user_id = ? ORDER BY taken_on DESC`, userID)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				out := []map[string]any{}
				for rows.Next() {
					var id int64
					var takenOn, lab, by, notes, status string
					if rows.Scan(&id, &takenOn, &lab, &by, &notes, &status) == nil {
						out = append(out, map[string]any{"id": id, "taken_on": takenOn, "lab": lab, "ordered_by": by, "notes": notes, "parse_status": status, "counts": s.markerCounts(ctx, id)})
					}
				}
				return out, nil
			},
		},
		{
			Name:        "get_blood_report",
			Description: "One report with every marker: value, unit, reference range, flag.",
			InputSchema: schema(map[string]any{"id": prop("integer", "")}, "id"),
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				id, ok := argInt(args, "id")
				if !ok {
					return nil, errors.New("id is required")
				}
				return s.bloodReportByID(ctx, userID, int64(id))
			},
		},
		{
			Name:        "get_blood_markers",
			Description: "Every marker across every report as a series, with reference ranges and the direction that counts as improvement. Filter by code (e.g. hba1c, ldl, alt) or category (sugar, lipids, liver, kidney, thyroid, blood).",
			InputSchema: schema(map[string]any{"code": prop("string", ""), "category": prop("string", "")}),
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				series, err := s.markerSeries(ctx, userID)
				if err != nil {
					return nil, err
				}
				code, cat := strings.ToLower(argString(args, "code")), strings.ToLower(argString(args, "category"))
				if code == "" && cat == "" {
					return series, nil
				}
				out := []MarkerSeries{}
				for _, ms := range series {
					if (code != "" && ms.Code == code) || (cat != "" && ms.Category == cat) {
						out = append(out, ms)
					}
				}
				return out, nil
			},
		},
		{
			Name:        "add_blood_report",
			Description: "Record a lab report by hand. Each marker needs a name and value; units and ranges are optional. Names are matched to canonical codes automatically.",
			InputSchema: schema(map[string]any{
				"taken_on": prop("string", "YYYY-MM-DD"), "lab": prop("string", ""), "notes": prop("string", ""),
				"markers": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
					"name": prop("string", ""), "value": prop("number", ""), "unit": prop("string", ""), "ref_low": prop("number", ""), "ref_high": prop("number", ""), "flag": prop("string", "normal|high|low"),
				}, "required": []string{"name", "value"}}},
			}, "taken_on", "markers"),
			write: true,
			run: func(ctx context.Context, s *Server, userID int64, args map[string]any) (any, error) {
				takenOn := argString(args, "taken_on")
				if !dates.Valid(takenOn) {
					return nil, errors.New("taken_on must be YYYY-MM-DD")
				}
				raw, _ := json.Marshal(args["markers"])
				var payload []bloodMarkerPayload
				if err := json.Unmarshal(raw, &payload); err != nil || len(payload) == 0 {
					return nil, errors.New("markers must be a list of {name, value, unit, ref_low, ref_high}")
				}
				id, err := s.insertBloodReport(ctx, userID, takenOn, argString(args, "lab"), "", argString(args, "notes"), "", "", 0, "", "manual", toMarkers(payload))
				if err != nil {
					return nil, err
				}
				return s.bloodReportByID(ctx, userID, id)
			},
		},
		{
			Name:        "list_marker_definitions",
			Description: "The canonical marker codes lifeai knows, with categories, so marker queries and manual entries use the right names.",
			InputSchema: schema(map[string]any{}),
			run: func(_ context.Context, _ *Server, _ int64, _ map[string]any) (any, error) {
				out := make([]map[string]any, 0, len(blood.Definitions))
				for _, d := range blood.Definitions {
					out = append(out, map[string]any{"code": d.Code, "name": d.Name, "category": d.Category, "lower_is_better": d.LowerIsBetter, "watch": d.Watch})
				}
				return out, nil
			},
		},
	}
	for _, t := range mcpTools {
		mcpToolByName[t.Name] = t
	}
}
