import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { api } from "../lib/api";
import type { Day, SupplementDay, Workout } from "../lib/types";
import { SupplementsCard } from "../components/Supplements";
import { linkTarget } from "../components/DayView";

vi.mock("../lib/api", () => ({
  api: {
    takeSupplement: vi.fn(),
    untakeSupplement: vi.fn(),
    saveSupplement: vi.fn(),
    uploadSupplementImage: vi.fn(),
    photoObjectURL: vi.fn(),
  },
}));

const creatine: SupplementDay = {
  id: 2,
  name: "Creatine",
  product: "Nutratology Creatine Monohydrate",
  dose: "1 scoop (5 g)",
  taken_with: "cold water",
  product_url: "",
  notes: "",
  started_on: "2026-10-02",
  active: true,
  position: 1,
  taken: null,
  streak: 1,
  days_taken: 9,
  days_since: 11,
  missed: ["2026-10-10"],
};

function day(supplements: SupplementDay[]): Day {
  return { date: "2026-10-12", is_today: true, supplements } as unknown as Day;
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.takeSupplement).mockResolvedValue({ supplement_id: 2, date: "2026-10-12", taken_with: "cold water", notes: "" });
  vi.mocked(api.untakeSupplement).mockResolvedValue({ ok: true });
});

test("one tap marks a supplement taken; tapping again undoes it", async () => {
  const changed = vi.fn();
  const { rerender } = render(<SupplementsCard day={day([creatine])} onChanged={changed} />);
  expect(screen.getByText("0/1 taken today")).toBeInTheDocument();
  expect(screen.getByText(/9\/11 days/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Mark Creatine taken" }));
  await waitFor(() => expect(api.takeSupplement).toHaveBeenCalledWith(2, "2026-10-12", ""));
  expect(changed).toHaveBeenCalled();

  const taken = { ...creatine, taken: { supplement_id: 2, date: "2026-10-12", taken_with: "cold water", notes: "" } };
  rerender(<SupplementsCard day={day([taken])} onChanged={changed} />);
  expect(screen.getByText(/With cold water/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "almond milk" }));
  await waitFor(() => expect(api.takeSupplement).toHaveBeenCalledWith(2, "2026-10-12", "almond milk"));
  fireEvent.click(screen.getByRole("button", { name: /Creatine taken; tap to undo/ }));
  await waitFor(() => expect(api.untakeSupplement).toHaveBeenCalledWith(2, "2026-10-12"));
});

test("an imported recording near a logged session suggests linking", () => {
  const own = { id: 24, source: "manual", sources: ["manual"], started_at: "2026-10-12T10:30:00Z", activity: "Weight training with trainer", exercises: [{}] } as unknown as Workout;
  const strava = { id: 23, source: "75hard", sources: ["75hard"], started_at: "2026-10-12T10:34:47Z", activity: "Morning Workout", exercises: [] } as unknown as Workout;
  const walk = { id: 25, source: "75hard", sources: ["75hard"], started_at: "2026-10-12T22:00:00Z", activity: "Evening Walk", exercises: [] } as unknown as Workout;
  expect(linkTarget(strava, [own, strava, walk])?.id).toBe(24);
  expect(linkTarget(walk, [own, strava, walk])).toBeNull();
  expect(linkTarget(own, [own, strava, walk])).toBeNull();
});
