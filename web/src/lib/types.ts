export interface User {
  id: number;
  email: string;
  name: string;
  avatar_url: string;
  timezone: string;
  is_admin: boolean;
  auth_provider: string;
  weight_unit: "kg" | "lb";
  created_at: string;
  dob: string;
  sex: string;
  height_cm: number | null;
  hard75_eligible: boolean;
}

export interface AuthResponse {
  token: string;
  user: User;
}

export interface AuthConfig {
  allow_signup: boolean;
  google: boolean;
  github: boolean;
}

export interface Goals {
  daily_kcal: number | null;
  protein_g: number | null;
  carbs_g: number | null;
  fat_g: number | null;
  target_weight_kg: number | null;
  steps: number | null;
  water_ml: number | null;
  sleep_hours: number | null;
  workout_minutes: number | null;
  notes: string;
}

export interface Metrics {
  weight_kg: number | null;
  body_fat_pct: number | null;
  resting_hr: number | null;
  sleep_hours: number | null;
  steps: number | null;
  water_ml: number | null;
  mood: number | null;
  energy: number | null;
  note: string;
  source: string;
}

export interface Totals {
  kcal: number;
  protein_g: number;
  carbs_g: number;
  fat_g: number;
  workout_minutes: number;
  workout_kcal: number;
  meditation_minutes: number;
  meals: number;
}

export interface MealItem {
  id: number;
  name: string;
  qty: number;
  unit: string;
  kcal: number;
  protein_g: number;
  carbs_g: number;
  fat_g: number;
  confidence: number | null;
}

export type Slot = "breakfast" | "lunch" | "dinner" | "snack";

export interface Meal {
  id: number;
  date: string;
  photo_id: number | null;
  photo_url?: string;
  recipe_id: number | null;
  name: string;
  slot: Slot;
  kcal: number;
  protein_g: number;
  carbs_g: number;
  fat_g: number;
  source: string;
  notes: string;
  eaten_at: string;
  items: MealItem[];
  estimate_status: "" | "pending" | "done" | "failed";
  estimate_error?: string;
}

export interface Workout {
  id: number;
  date: string;
  kind: string;
  activity: string;
  minutes: number;
  kcal: number | null;
  distance_km: number | null;
  avg_hr: number | null;
  notes: string;
  started_at: string | null;
  source: string;
}

export interface Meditation {
  id: number;
  date: string;
  minutes: number;
  style: string;
  notes: string;
  started_at: string | null;
  source: string;
}

export interface JournalEntry {
  id: number;
  date: string;
  title: string;
  body: string;
  source: string;
  created_at: string;
  updated_at: string;
  snippet?: string;
}

export type Pose = "front" | "side" | "back" | "";

export interface Photo {
  id: number;
  date: string;
  kind: "progress" | "food" | "ingredients";
  pose: Pose;
  caption: string;
  width: number;
  height: number;
  bytes: number;
  taken_at: string;
  source: string;
  url: string;
  thumb_url: string;
}

export interface Day {
  date: string;
  is_today: boolean;
  metrics: Metrics;
  meals: Meal[];
  workouts: Workout[];
  meditations: Meditation[];
  journal: JournalEntry[];
  photos: Photo[];
  totals: Totals;
  goals: Goals;
}

export interface DaySummary {
  date: string;
  kcal: number;
  protein_g: number;
  weight_kg: number | null;
  workout_minutes: number;
  meditation_minutes: number;
  meals: number;
  photos: number;
  journal: number;
  steps: number | null;
  sleep_hours: number | null;
  mood: number | null;
}

export interface Recipe {
  id: number;
  name: string;
  summary: string;
  minutes: number;
  servings: number;
  kcal_per_serving: number;
  protein_g: number;
  carbs_g: number;
  fat_g: number;
  ingredients: string[];
  steps: string[];
  tags: string[];
  favourite: boolean;
  photo_id: number | null;
  photo_url?: string;
  source: string;
  times_cooked: number;
  last_cooked_at: string | null;
  created_at: string;
  updated_at: string;
}

export type RecipeDraft = Omit<
  Recipe,
  | "id"
  | "times_cooked"
  | "last_cooked_at"
  | "created_at"
  | "updated_at"
  | "photo_url"
>;

export interface Point {
  date: string;
  value: number;
}

export interface Trend {
  first?: number;
  latest?: number;
  change?: number;
  best?: number;
  average?: number;
  count: number;
}

export interface Stats {
  from: string;
  to: string;
  weight: Point[];
  body_fat: Point[];
  resting_hr: Point[];
  sleep: Point[];
  steps: Point[];
  kcal: Point[];
  protein: Point[];
  training: Point[];
  mood: Point[];
  weight_trend: Trend;
  resting_hr_trend: Trend;
  body_fat_trend: Trend;
  days_logged: number;
  days_in_window: number;
  avg_kcal: number;
  avg_protein: number;
  avg_sleep: number;
  avg_steps: number;
  workout_count: number;
  workout_minutes: number;
  meditation_minutes: number;
  meals_logged: number;
  photos_taken: number;
  journal_entries: number;
  streak: number;
  kcal_adherence?: number;
}

export interface Dashboard {
  today: Day;
  week: DaySummary[];
  stats: Stats;
  recent_recipes: Recipe[];
}

export interface FoodEstimate {
  name: string;
  items: Array<Omit<MealItem, "id" | "confidence"> & { confidence: number }>;
  notes: string;
  kcal: number;
  protein_g: number;
  carbs_g: number;
  fat_g: number;
}

export interface AIRecipe {
  name: string;
  summary: string;
  minutes: number;
  servings: number;
  kcal_per_serving: number;
  protein_g: number;
  carbs_g: number;
  fat_g: number;
  ingredients: string[];
  steps: string[];
  tags: string[];
}

export interface PlanDay {
  day: string;
  training: string;
  nutrition: string;
  note: string;
}

export interface Plan {
  summary: string;
  focus: string;
  days: PlanDay[];
  tips: string[];
}

export interface CoachNote {
  note: string;
  tone: string;
}

export interface AIStatus {
  enabled: boolean;
  providers: string[];
  vision: string[];
  daily_limit: number;
  used_today: number;
  remaining: number;
}

export interface APIToken {
  id: number;
  name: string;
  prefix: string;
  scopes: string[];
  last_used_at?: string;
  expires_at?: string;
  created_at: string;
}

export interface CreatedToken {
  token: APIToken;
  secret: string;
  discovery: Record<string, unknown>;
}

export interface SyncSummary {
  check_ins?: number;
  programs: number;
  days: number;
  photos: number;
  meals: number;
  workouts: number;
  meditations: number;
  journal: number;
  requests: number;
  duration_sec: number;
  finished_at: string;
}

export interface Hard75Status {
  eligible: boolean;
  connected: boolean;
  enabled: boolean;
  base_url: string;
  token_hint: string;
  last_sync_at: string | null;
  last_status: string;
  last_error: string;
  last_summary: SyncSummary | null;
  running: boolean;
  can_store: boolean;
}

export interface BloodMarker {
  id: number;
  code: string;
  name: string;
  category: string;
  value: number | null;
  value_text: string;
  unit: string;
  ref_low: number | null;
  ref_high: number | null;
  ref_text: string;
  flag: string;
  watch: boolean;
}
export interface BloodReport {
  id: number;
  taken_on: string;
  lab: string;
  ordered_by: string;
  notes: string;
  has_file: boolean;
  file_name?: string;
  file_url?: string;
  parse_status: string;
  parse_error?: string;
  markers: BloodMarker[];
  counts: Record<string, number>;
  created_at: string;
}
export interface MarkerSeries {
  code: string;
  name: string;
  category: string;
  unit: string;
  watch: boolean;
  flag: string;
  ref_low: number | null;
  ref_high: number | null;
  points: (Point & { flag: string; report_id: number })[];
  latest?: Point;
  change?: number;
}
export interface HealthSummary {
  generated_at: string;
  signals: string[];
  goals: Goals;
  today: Day;
  window_30d: Stats;
  profile: {
    name: string;
    age?: number;
    sex: string;
    height_cm?: number;
    weight_kg?: number;
    bmi?: number;
    timezone: string;
  };
  blood: {
    reports: number;
    latest_report_date?: string;
    next_test_due?: string;
    watch: MarkerSeries[];
    abnormal: MarkerSeries[];
  };
  window_90d: {
    weight_trend: Trend;
    avg_kcal: number;
    avg_protein: number;
    workout_count: number;
    workout_minutes: number;
    days_logged: number;
  };
}
export interface StravaStatus {
  configured: boolean;
  connected: boolean;
  username: string;
  athlete_id: number;
  last_sync_at: string | null;
  last_error: string;
  imported: number;
}
export interface ImportSummary {
  source: string;
  samples: number;
  workouts: number;
  workouts_skipped: number;
  days_touched: number;
}
export interface ImportRun {
  id: number;
  source: string;
  file_name: string;
  summary: ImportSummary | null;
  error: string;
  created_at: string;
}
