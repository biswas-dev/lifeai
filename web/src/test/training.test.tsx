import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, expect, test, vi } from "vitest";
import { api } from "../lib/api";
import type { Exercise, ExerciseCatalog, NutritionPlan } from "../lib/types";
import { formatSets } from "../lib/training";
import { optionFor, windowStatus } from "../lib/nutrition";
import { MuscleMap } from "../components/MuscleMap";
import { TrainingSession } from "../routes/TrainingSession";

vi.mock("../lib/api", () => ({
  api: {
    exercises: vi.fn(),
    exerciseHistory: vi.fn(),
    workouts: vi.fn(),
    workout: vi.fn(),
    stravaStatus: vi.fn(),
    saveWorkout: vi.fn(),
    pushWorkoutToStrava: vi.fn(),
  },
}));

const goblet: Exercise = {
  slug: "goblet-squat",
  name: "Dumbbell goblet squat",
  aliases: ["goblet squats"],
  category: "legs",
  equipment: "dumbbell",
  mode: "weight_reps",
  load: "one dumbbell",
  primary: ["quads", "glutes"],
  secondary: ["adductors"],
  images: ["/exercises/goblet-squat-0.jpg", "/exercises/goblet-squat-1.jpg"],
  summary: "A squat holding one dumbbell.",
  steps: ["Hold the dumbbell at your chest.", "Sit down between your heels."],
  cues: ["Elbows inside the knees"],
  mistakes: ["Knees caving in"],
};
const catalog: ExerciseCatalog = { categories: ["legs"], muscles: ["quads", "glutes", "adductors"], exercises: [goblet] };

// Newer Node versions define their own localStorage global, which can shadow
// jsdom's; give these tests a plain in-memory one.
function memoryStorage(): Storage {
  const data = new Map<string, string>();
  return {
    get length() {
      return data.size;
    },
    clear: () => data.clear(),
    getItem: (k) => data.get(k) ?? null,
    key: (i) => [...data.keys()][i] ?? null,
    removeItem: (k) => void data.delete(k),
    setItem: (k, v) => void data.set(k, String(v)),
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("localStorage", memoryStorage());
  vi.mocked(api.exercises).mockResolvedValue(catalog);
  vi.mocked(api.exerciseHistory).mockResolvedValue([
    { workout_id: 3, date: "2026-10-05", name: goblet.name, notes: "", sets: [{ reps: 10, weight: 16, unit: "lb", seconds: null }] },
  ]);
  vi.mocked(api.workouts).mockResolvedValue([]);
  vi.mocked(api.stravaStatus).mockResolvedValue({ configured: true, connected: true, can_upload: true, username: "me", athlete_id: 1, last_sync_at: null, last_error: "", imported: 0 });
});

test("formatSets folds identical consecutive sets", () => {
  expect(
    formatSets([
      { reps: 10, weight: 17.5, unit: "lb", seconds: null },
      { reps: 10, weight: 20, unit: "lb", seconds: null },
      { reps: 10, weight: 20, unit: "lb", seconds: null },
      { reps: 12, weight: null, unit: "lb", seconds: null },
      { reps: null, weight: null, unit: "lb", seconds: 30 },
    ]),
  ).toBe("17.5 lb × 10, 2× 20 lb × 10, 12 reps, 30 s");
});

const plan: NutritionPlan = {
  name: "1-7 PM",
  window_start: "13:00",
  window_end: "19:00",
  targets: { kcal: 1800 },
  meals: [
    {
      key: "dinner",
      time: "18:15",
      title: "Dinner",
      slot: "dinner",
      options: [
        { key: "salmon", title: "Salmon", items: [], kcal: 720, protein_g: 49, carbs_g: 67, fat_g: 30, days: [1, 4] },
        { key: "avocado", title: "Chicken + avocado", items: [], kcal: 735, protein_g: 46, carbs_g: 75, fat_g: 29, days: [2, 5, 0] },
        { key: "tofu", title: "Tofu", items: [], kcal: 730, protein_g: 49, carbs_g: 72, fat_g: 30, days: [3, 6] },
      ],
    },
  ],
};

test("the eating window reports fasting and eating phases", () => {
  const at = (h: number, m = 0) => new Date(2026, 9, 9, h, m);
  const morning = windowStatus(plan, at(9, 30))!;
  expect(morning.state).toBe("fasting");
  expect(morning.headline).toBe("Fasting · 14h 30m of 18h");
  expect(morning.detail).toContain("in 3h 30m");
  const afternoon = windowStatus(plan, at(17, 15))!;
  expect(afternoon.state).toBe("eating");
  expect(afternoon.detail).toBe("Closes at 7 PM · 1h 45m left");
  expect(windowStatus({ ...plan, window_start: undefined, window_end: undefined }, at(12))).toBeNull();
});

test("the dinner rotation follows the weekday", () => {
  expect(optionFor(plan.meals[0], 1).key).toBe("salmon");
  expect(optionFor(plan.meals[0], 0).key).toBe("avocado");
  expect(optionFor(plan.meals[0], 6).key).toBe("tofu");
});

test("the muscle map names what an exercise trains", () => {
  render(<MuscleMap primary={["quads", "glutes"]} secondary={["adductors"]} />);
  expect(screen.getByRole("img", { name: "Muscles worked. Primary: Quads, Glutes. Also: Inner thighs." })).toBeInTheDocument();
});

test("a strength session is built from the catalog and saved with its sets", async () => {
  const saved = { id: 42, date: "2026-10-09", kind: "strength", activity: "Trainer session", minutes: 60, kcal: null, distance_km: null, avg_hr: null, notes: "", started_at: null, source: "manual", exercises: [] };
  vi.mocked(api.saveWorkout).mockResolvedValue(saved);
  vi.mocked(api.workout).mockResolvedValue(saved);
  render(
    <MemoryRouter initialEntries={["/app/training/new?date=2026-10-09"]}>
      <Routes>
        <Route path="/app/training/:id" element={<TrainingSession />} />
      </Routes>
    </MemoryRouter>,
  );
  fireEvent.change(await screen.findByLabelText("Title"), { target: { value: "Trainer session" } });
  fireEvent.click(await screen.findByRole("button", { name: "+ Add exercise" }));
  const picker = await screen.findByRole("dialog", { name: "Add an exercise" });
  fireEvent.click(within(picker).getByRole("button", { name: /^Dumbbell goblet squat/ }));

  expect(await screen.findByText(/Last time .*16 lb × 10/)).toBeInTheDocument();
  expect(screen.getByText("Dumbbell")).toBeInTheDocument(); // one-dumbbell load label
  fireEvent.change(screen.getByLabelText("Set 1 weight (lb)"), { target: { value: "16" } });
  fireEvent.click(screen.getByRole("button", { name: "+ Add set" }));
  fireEvent.change(screen.getByLabelText("Set 2 weight (lb)"), { target: { value: "22" } });
  fireEvent.click(screen.getByRole("button", { name: "Save session" }));

  await waitFor(() => expect(api.saveWorkout).toHaveBeenCalled());
  const [id, body] = vi.mocked(api.saveWorkout).mock.calls[0];
  expect(id).toBeNull();
  // Saving moves to the session's own page, where it can go to Strava.
  expect(await screen.findByRole("button", { name: "Post to Strava" })).toBeInTheDocument();
  expect(body).toMatchObject({
    date: "2026-10-09",
    kind: "strength",
    activity: "Trainer session",
    minutes: 60,
    exercises: [
      {
        slug: "goblet-squat",
        name: "Dumbbell goblet squat",
        sets: [
          { reps: 10, weight: 16, unit: "lb" },
          { reps: 10, weight: 22, unit: "lb" },
        ],
      },
    ],
  });
});

test("an unsaved session survives a reload", async () => {
  localStorage.setItem(
    "lifeai_session_draft",
    JSON.stringify({ date: "2026-10-09", start: "", minutes: 45, activity: "Half done", kind: "strength", notes: "", exercises: [{ key: "k", slug: "goblet-squat", name: "Dumbbell goblet squat", notes: "", sets: [{ reps: 8, weight: 22, unit: "lb", seconds: null }] }] }),
  );
  render(
    <MemoryRouter initialEntries={["/app/training/new"]}>
      <Routes>
        <Route path="/app/training/:id" element={<TrainingSession />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(await screen.findByText("Restored your unsaved session.")).toBeInTheDocument();
  expect(screen.getByLabelText("Set 1 weight (lb)")).toHaveValue(22);
});
