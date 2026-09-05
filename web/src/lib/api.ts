import type {
  BloodReport,
  BloodMarker,
  MarkerSeries,
  HealthSummary,
  StravaStatus,
  ImportSummary,
  ImportRun,
  AIRecipe,
  AIStatus,
  APIToken,
  AuthConfig,
  AuthResponse,
  CoachNote,
  CreatedToken,
  Dashboard,
  Day,
  DaySummary,
  FoodEstimate,
  Goals,
  Hard75Status,
  JournalEntry,
  Meal,
  Meditation,
  Photo,
  Plan,
  Pose,
  Recipe,
  RecipeDraft,
  Stats,
  User,
  Workout,
} from "./types";

const BASE_URL = import.meta.env.VITE_API_URL || "";
const TOKEN_KEY = "lifeai_token";

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(message: string, status: number, code = "") {
    super(message);
    this.status = status;
    this.code = code;
  }
}

class ApiClient {
  private token: string | null;

  constructor() {
    this.token = safeGet(TOKEN_KEY);
  }

  setToken(token: string | null) {
    this.token = token;
    try {
      if (token) localStorage.setItem(TOKEN_KEY, token);
      else localStorage.removeItem(TOKEN_KEY);
    } catch {
      // Private mode; the session lives in memory only.
    }
  }

  getToken() {
    return this.token;
  }

  private authHeaders(): Record<string, string> {
    return this.token ? { Authorization: `Bearer ${this.token}` } : {};
  }

  private async request<T>(
    path: string,
    options: RequestInit = {},
  ): Promise<T> {
    const headers: Record<string, string> = {
      ...this.authHeaders(),
      ...(options.headers as Record<string, string>),
    };
    if (options.body && !(options.body instanceof FormData)) {
      headers["Content-Type"] = "application/json";
    }
    const res = await fetch(`${BASE_URL}${path}`, { ...options, headers });
    if (res.status === 204) return {} as T;
    const contentType = res.headers.get("content-type") || "";
    if (!contentType.includes("application/json")) {
      if (!res.ok)
        throw new ApiError(`Request failed (${res.status})`, res.status);
      return {} as T;
    }
    const data = await res.json();
    if (!res.ok) {
      throw new ApiError(
        data?.error || `Request failed (${res.status})`,
        res.status,
        data?.code || "",
      );
    }
    return data as T;
  }

  // ---- auth ----
  authConfig() {
    return this.request<AuthConfig>("/api/auth/config");
  }
  signup(body: {
    email: string;
    password: string;
    name?: string;
    timezone?: string;
  }) {
    return this.request<AuthResponse>("/api/auth/signup", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  login(email: string, password: string) {
    return this.request<AuthResponse>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
  }
  forgotPassword(email: string) {
    return this.request<{
      ok: boolean;
      message: string;
      token?: string;
      reset_url?: string;
    }>("/api/auth/forgot-password", {
      method: "POST",
      body: JSON.stringify({ email }),
    });
  }
  resetPassword(token: string, new_password: string) {
    return this.request<AuthResponse>("/api/auth/reset-password", {
      method: "POST",
      body: JSON.stringify({ token, new_password }),
    });
  }
  me() {
    return this.request<User>("/api/me");
  }
  updateProfile(body: {
    name?: string;
    timezone?: string;
    weight_unit?: "kg" | "lb";
    dob?: string;
    sex?: string;
    height_cm?: number;
  }) {
    return this.request<User>("/api/me", {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }
  changePassword(current_password: string, new_password: string) {
    return this.request<{ ok: boolean }>("/api/me/password", {
      method: "POST",
      body: JSON.stringify({ current_password, new_password }),
    });
  }

  // ---- goals and days ----
  goals() {
    return this.request<Goals>("/api/goals");
  }
  saveGoals(body: Goals) {
    return this.request<Goals>("/api/goals", {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }
  dashboard() {
    return this.request<Dashboard>("/api/dashboard");
  }
  today() {
    return this.request<Day>("/api/today");
  }
  day(date: string) {
    return this.request<Day>(`/api/days/${date}`);
  }
  days(from: string, to: string) {
    return this.request<DaySummary[]>(`/api/days?from=${from}&to=${to}`);
  }
  updateDay(date: string, body: Record<string, unknown>) {
    return this.request<Day>(`/api/days/${date}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }
  stats(days = 90) {
    return this.request<Stats>(`/api/stats?days=${days}`);
  }

  // ---- photos ----
  listPhotos(kind?: string, pose?: Pose) {
    const q = new URLSearchParams();
    if (kind) q.set("kind", kind);
    if (pose) q.set("pose", pose);
    const qs = q.toString();
    return this.request<Photo[]>(`/api/photos${qs ? `?${qs}` : ""}`);
  }
  uploadPhoto(
    file: Blob,
    opts: {
      kind?: string;
      date?: string;
      caption?: string;
      pose?: Pose;
      slot?: string;
      autolog?: boolean;
    } = {},
  ) {
    const form = new FormData();
    form.append("file", file, "photo.webp");
    if (opts.kind) form.append("kind", opts.kind);
    if (opts.date) form.append("date", opts.date);
    if (opts.caption) form.append("caption", opts.caption);
    if (opts.slot) form.append("slot", opts.slot);
    if (opts.autolog) form.append("autolog", "1");
    if (opts.pose) form.append("pose", opts.pose);
    return this.request<Photo>("/api/photos", { method: "POST", body: form });
  }
  updatePhoto(
    id: number,
    body: { pose?: Pose; caption?: string; date?: string },
  ) {
    return this.request<Photo>(`/api/photos/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }
  deletePhoto(id: number) {
    return this.request<{ ok: boolean }>(`/api/photos/${id}`, {
      method: "DELETE",
    });
  }
  /** Photos sit behind bearer auth; fetch bytes and hand back an object URL. */
  async photoObjectURL(url: string): Promise<string> {
    const res = await fetch(`${BASE_URL}${url}`, {
      headers: this.authHeaders(),
    });
    if (!res.ok) throw new ApiError("could not load image", res.status);
    return URL.createObjectURL(await res.blob());
  }

  // ---- meals ----
  createMeal(body: Record<string, unknown>) {
    return this.request<Meal>("/api/meals", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  updateMeal(id: number, body: Record<string, unknown>) {
    return this.request<Meal>(`/api/meals/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }
  deleteMeal(id: number) {
    return this.request<{ ok: boolean }>(`/api/meals/${id}`, {
      method: "DELETE",
    });
  }
  retryEstimate(id: number) {
    return this.request<{ status: string }>(`/api/meals/${id}/estimate`, {
      method: "POST",
    });
  }

  // ---- recipes ----
  listRecipes(
    params: {
      q?: string;
      tag?: string;
      favourite?: boolean;
      sort?: string;
    } = {},
  ) {
    const q = new URLSearchParams();
    if (params.q) q.set("q", params.q);
    if (params.tag) q.set("tag", params.tag);
    if (params.favourite) q.set("favourite", "1");
    if (params.sort) q.set("sort", params.sort);
    const qs = q.toString();
    return this.request<Recipe[]>(`/api/recipes${qs ? `?${qs}` : ""}`);
  }
  recipe(id: number) {
    return this.request<Recipe>(`/api/recipes/${id}`);
  }
  createRecipe(body: Partial<RecipeDraft>) {
    return this.request<Recipe>("/api/recipes", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  updateRecipe(id: number, body: Partial<RecipeDraft>) {
    return this.request<Recipe>(`/api/recipes/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }
  deleteRecipe(id: number) {
    return this.request<{ ok: boolean }>(`/api/recipes/${id}`, {
      method: "DELETE",
    });
  }
  cookRecipe(
    id: number,
    body: { date?: string; slot?: string; servings?: number; notes?: string },
  ) {
    return this.request<Meal>(`/api/recipes/${id}/cook`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  // ---- activity ----
  createWorkout(body: Record<string, unknown>) {
    return this.request<Workout>("/api/workouts", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  deleteWorkout(id: number) {
    return this.request<{ ok: boolean }>(`/api/workouts/${id}`, {
      method: "DELETE",
    });
  }
  createMeditation(body: Record<string, unknown>) {
    return this.request<Meditation>("/api/meditations", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  deleteMeditation(id: number) {
    return this.request<{ ok: boolean }>(`/api/meditations/${id}`, {
      method: "DELETE",
    });
  }
  journal(q?: string) {
    return this.request<JournalEntry[]>(
      `/api/journal${q ? `?q=${encodeURIComponent(q)}` : ""}`,
    );
  }
  createJournal(body: { date?: string; title?: string; body: string }) {
    return this.request<JournalEntry>("/api/journal", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  updateJournal(id: number, body: { title?: string; body?: string }) {
    return this.request<JournalEntry>(`/api/journal/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }
  deleteJournal(id: number) {
    return this.request<{ ok: boolean }>(`/api/journal/${id}`, {
      method: "DELETE",
    });
  }

  // ---- AI ----
  aiStatus() {
    return this.request<AIStatus>("/api/ai/status");
  }
  analyzeFood(photo_id: number, hint = "") {
    return this.request<{
      estimate: FoodEstimate;
      cached: boolean;
      provider?: string;
    }>("/api/ai/food", {
      method: "POST",
      body: JSON.stringify({ photo_id, hint }),
    });
  }
  suggestRecipes(body: {
    ingredients?: string[];
    preferences?: string;
    meal_slot?: string;
    photo_id?: number | null;
    date?: string;
  }) {
    return this.request<{
      recipes: AIRecipe[];
      remaining_kcal: number;
      provider?: string;
    }>("/api/ai/recipes", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  importRecipe(text: string) {
    return this.request<{ recipe: AIRecipe; cached: boolean }>(
      "/api/ai/import-recipe",
      { method: "POST", body: JSON.stringify({ text }) },
    );
  }
  buildPlan(force = false) {
    return this.request<{ plan: Plan; cached: boolean }>("/api/ai/plan", {
      method: "POST",
      body: JSON.stringify({ force }),
    });
  }
  coachNote() {
    return this.request<{ note: CoachNote; cached: boolean }>("/api/ai/coach");
  }

  bloodReports() {
    return this.request<BloodReport[]>("/api/blood/reports");
  }
  bloodReport(id: number) {
    return this.request<BloodReport>(`/api/blood/reports/${id}`);
  }
  bloodMarkers() {
    return this.request<MarkerSeries[]>("/api/blood/markers");
  }
  uploadBlood(file: File, taken_on: string) {
    const body = new FormData();
    body.append("file", file);
    if (taken_on) body.append("taken_on", taken_on);
    return this.request<BloodReport>("/api/blood/reports/upload", {
      method: "POST",
      body,
    });
  }
  createBlood(body: {
    taken_on: string;
    lab?: string;
    text?: string;
    markers?: Partial<BloodMarker>[];
  }) {
    return this.request<BloodReport>("/api/blood/reports", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  updateBlood(
    id: number,
    body: {
      taken_on?: string;
      notes?: string;
      lab?: string;
      markers?: BloodMarker[];
    },
  ) {
    return this.request<BloodReport>(`/api/blood/reports/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }
  deleteBlood(id: number) {
    return this.request(`/api/blood/reports/${id}`, { method: "DELETE" });
  }
  healthSummary() {
    return this.request<HealthSummary>("/api/analysis/health");
  }
  stravaStatus() {
    return this.request<StravaStatus>("/api/strava/status");
  }
  stravaConnect() {
    return this.request<{ url: string }>("/api/strava/connect", {
      method: "POST",
    });
  }
  stravaSync() {
    return this.request<ImportSummary>("/api/strava/sync", { method: "POST" });
  }
  stravaDisconnect() {
    return this.request("/api/strava", { method: "DELETE" });
  }
  importHealth(source: "apple" | "samsung", file: File) {
    const body = new FormData();
    body.append("file", file);
    return this.request<ImportSummary>(`/api/import/${source}-health`, {
      method: "POST",
      body,
    });
  }
  importRuns() {
    return this.request<ImportRun[]>("/api/import/runs");
  }

  // ---- tokens and integrations ----
  listTokens() {
    return this.request<APIToken[]>("/api/tokens");
  }
  createToken(body: {
    name: string;
    scopes: string[];
    expires_in_days?: number;
  }) {
    return this.request<CreatedToken>("/api/tokens", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  revokeToken(id: number) {
    return this.request<{ ok: boolean }>(`/api/tokens/${id}`, {
      method: "DELETE",
    });
  }
  hard75Status() {
    return this.request<Hard75Status>("/api/integrations/75hard");
  }
  hard75Connect(body: {
    base_url?: string;
    token?: string;
    enabled?: boolean;
  }) {
    return this.request<
      { status?: Hard75Status; remote_account?: string } & Partial<Hard75Status>
    >("/api/integrations/75hard", {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }
  hard75Disconnect() {
    return this.request<{ ok: boolean }>("/api/integrations/75hard", {
      method: "DELETE",
    });
  }
  hard75Sync() {
    return this.request<{ status: string }>("/api/integrations/75hard/sync", {
      method: "POST",
    });
  }
}

function safeGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export const api = new ApiClient();
