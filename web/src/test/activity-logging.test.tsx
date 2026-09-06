import { useState } from "react";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, expect, test, vi } from "vitest";
import { ActivityGrid } from "../components/ActivityGrid";
import { Layout } from "../components/Layout";
import { MeditationSheet, WorkoutSheet } from "../components/sheets";
import { api } from "../lib/api";
import type { Day } from "../lib/types";

vi.mock("../state/AuthContext", () => ({
  useAuth: () => ({
    user: { id: 1, name: "Test", water_unit: "gal" },
    refresh: vi.fn().mockResolvedValue(undefined),
    logout: vi.fn(),
  }),
}));
vi.mock("../lib/api", () => ({
  api: {
    addWater: vi.fn(),
    deleteWater: vi.fn(),
    updateProfile: vi.fn(),
    createWorkout: vi.fn(),
    createMeditation: vi.fn(),
  },
}));
const emptyDay = {
  date: "2026-09-05",
  is_today: true,
  metrics: { water_ml: null },
  goals: { water_ml: null },
  water: [],
  meals: [],
  workouts: [],
  meditations: [],
  journal: [],
  photos: [],
  totals: { workout_minutes: 0, meditation_minutes: 0, kcal: 0 },
} as unknown as Day;
const withDrink: Day = {
  ...emptyDay,
  metrics: { ...emptyDay.metrics, water_ml: 946 },
  water: [{ id: 7, amount_ml: 946.352946, created_at: "2026-09-05 12:00:00" }],
};
function Grid() {
  const [day, setDay] = useState(emptyDay);
  return (
    <ActivityGrid
      day={day}
      onLog={() => {}}
      onChanged={(next) => {
        if (next) setDay(next);
      }}
    />
  );
}
beforeEach(() => vi.clearAllMocks());

test("one-tap water retries retain their identity and an undo updates the displayed total", async () => {
  vi.mocked(api.addWater)
    .mockRejectedValueOnce(new Error("Connection lost. Try again."))
    .mockResolvedValueOnce(withDrink);
  vi.mocked(api.deleteWater).mockResolvedValue(emptyDay);
  render(<Grid />);
  fireEvent.click(screen.getByRole("button", { name: "Add ¼ gal of water" }));
  await screen.findByText("Connection lost. Try again.");
  const original = vi.mocked(api.addWater).mock.calls[0];
  fireEvent.click(screen.getByRole("button", { name: "Add ¼ gal of water" }));
  await screen.findByRole("status");
  expect(api.addWater).toHaveBeenNthCalledWith(2, ...original);
  expect(original.slice(0, 3)).toEqual([emptyDay.date, 0.25, "gal"]);
  expect(screen.getByRole("status")).toHaveTextContent("0.25 gal logged");
  fireEvent.click(screen.getByRole("button", { name: "Undo last drink" }));
  await waitFor(() =>
    expect(api.deleteWater).toHaveBeenCalledWith(emptyDay.date, 7),
  );
  await screen.findByText("Drink removed from your total.");
});

test("water unit changes send the selected unit and keep form focus while typing", async () => {
  vi.mocked(api.updateProfile).mockResolvedValue({} as never);
  vi.mocked(api.addWater).mockResolvedValue({
    ...withDrink,
    metrics: { ...withDrink.metrics, water_ml: 237 },
  });
  render(<Grid />);
  fireEvent.click(screen.getByRole("button", { name: "Log water" }));
  const dialog = screen.getByRole("dialog", { name: "Log water" });
  expect(dialog).toHaveAttribute("aria-modal", "true");
  fireEvent.change(within(dialog).getByLabelText("Display water in"), {
    target: { value: "oz" },
  });
  await waitFor(() =>
    expect(api.updateProfile).toHaveBeenCalledWith({ water_unit: "oz" }),
  );
  const amount = within(dialog).getByLabelText("Custom amount (oz)");
  amount.focus();
  fireEvent.change(amount, { target: { value: "8" } });
  expect(document.activeElement).toBe(amount);
  fireEvent.click(within(dialog).getByRole("button", { name: "Add water" }));
  await waitFor(() =>
    expect(api.addWater).toHaveBeenCalledWith(
      emptyDay.date,
      8,
      "oz",
      expect.any(String),
    ),
  );
});

test("exercise and meditation accept short sessions without challenge-duration rules", async () => {
  vi.mocked(api.createWorkout).mockResolvedValue({} as never);
  vi.mocked(api.createMeditation).mockResolvedValue({} as never);
  const saved = vi.fn();
  const view = render(
    <WorkoutSheet
      open
      onClose={() => {}}
      date={emptyDay.date}
      onSaved={saved}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "10 min" }));
  fireEvent.click(screen.getByRole("button", { name: "Save exercise" }));
  await waitFor(() =>
    expect(api.createWorkout).toHaveBeenCalledWith(
      expect.objectContaining({
        date: emptyDay.date,
        minutes: 10,
        kind: "walk",
      }),
    ),
  );
  view.unmount();
  render(
    <MeditationSheet
      open
      onClose={() => {}}
      date={emptyDay.date}
      onSaved={saved}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "5 min" }));
  fireEvent.click(screen.getByRole("button", { name: "Save meditation" }));
  await waitFor(() =>
    expect(api.createMeditation).toHaveBeenCalledWith(
      expect.objectContaining({ date: emptyDay.date, minutes: 5 }),
    ),
  );
});

test("the mobile More menu makes every secondary page accessible", () => {
  render(
    <MemoryRouter initialEntries={["/app"]}>
      <Layout />
    </MemoryRouter>,
  );
  const nav = screen.getByRole("navigation", { name: "Mobile navigation" });
  expect(within(nav).getByRole("link", { name: "Calendar" })).toHaveAttribute(
    "href",
    "/app/history",
  );
  fireEvent.click(within(nav).getByRole("button", { name: "More navigation" }));
  const dialog = screen.getByRole("dialog", { name: "Your space" });
  for (const name of [
    "Recipes",
    "Trends",
    "Blood work",
    "Reflections",
    "Settings",
  ])
    expect(within(dialog).getByRole("link", { name })).toBeVisible();
});
