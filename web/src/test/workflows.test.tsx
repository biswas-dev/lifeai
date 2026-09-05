import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi, beforeEach, test, expect } from "vitest";
import { api } from "../lib/api";
import { Coach } from "../routes/Coach";
import { RecipeEditor } from "../routes/Recipes";
import { Blood } from "../routes/Blood";

vi.mock("../lib/api", () => ({
  api: {
    healthSummary: vi.fn(),
    aiStatus: vi.fn(),
    coachNote: vi.fn(),
    buildPlan: vi.fn(),
    createRecipe: vi.fn(),
    bloodReports: vi.fn(),
    bloodMarkers: vi.fn(),
    createBlood: vi.fn(),
  },
}));
beforeEach(() => vi.resetAllMocks());

test("health insights reads data without automatically spending a model call", async () => {
  vi.mocked(api.healthSummary).mockResolvedValue({
    signals: ["Nothing logged in the last 30 days."],
  } as never);
  vi.mocked(api.aiStatus).mockResolvedValue({
    enabled: true,
    remaining: 10,
  } as never);
  vi.mocked(api.coachNote).mockResolvedValue({
    note: { note: "Keep building your record.", tone: "neutral" },
    cached: false,
  });
  render(
    <MemoryRouter>
      <Coach />
    </MemoryRouter>,
  );
  await screen.findByText("Nothing logged in the last 30 days.");
  expect(api.coachNote).not.toHaveBeenCalled();
  expect(api.buildPlan).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Get a daily note" }));
  await screen.findByText("Keep building your record.");
  expect(api.coachNote).toHaveBeenCalledTimes(1);
});

test("manual recipe creation preserves serving amounts and separate ingredient lines", async () => {
  const onSaved = vi.fn();
  vi.mocked(api.createRecipe).mockResolvedValue({ id: 12 } as never);
  render(<RecipeEditor onSaved={onSaved} onClose={() => {}} />);
  fireEvent.change(screen.getByLabelText("Recipe name"), {
    target: { value: "Lentil bowl" },
  });
  fireEvent.change(screen.getByLabelText("Recipe servings"), {
    target: { value: "2" },
  });
  fireEvent.change(screen.getByLabelText("Calories per serving"), {
    target: { value: "420" },
  });
  fireEvent.change(screen.getByLabelText(/Ingredients/), {
    target: { value: "200 g lentils\n1 carrot" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save recipe" }));
  await waitFor(() =>
    expect(api.createRecipe).toHaveBeenCalledWith(
      expect.objectContaining({
        servings: 2,
        kcal_per_serving: 420,
        ingredients: ["200 g lentils", "1 carrot"],
      }),
    ),
  );
  expect(onSaved).toHaveBeenCalledWith({ id: 12 });
});

test("a user can enter a marker when PDF extraction is unavailable", async () => {
  vi.mocked(api.bloodReports).mockResolvedValue([]);
  vi.mocked(api.bloodMarkers).mockResolvedValue([]);
  vi.mocked(api.createBlood).mockResolvedValue({
    id: 3,
    taken_on: "2026-09-01",
    markers: [],
    counts: {},
  } as never);
  render(<Blood />);
  await screen.findByText(/Your first report becomes the baseline/);
  fireEvent.click(screen.getByRole("button", { name: "Enter manually" }));
  fireEvent.change(screen.getByLabelText("Marker name"), {
    target: { value: "HbA1c" },
  });
  fireEvent.change(screen.getByLabelText("Value", { exact: true }), {
    target: { value: "5.3" },
  });
  fireEvent.change(screen.getByLabelText("Unit", { exact: true }), {
    target: { value: "%" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save report" }));
  await waitFor(() =>
    expect(api.createBlood).toHaveBeenCalledWith(
      expect.objectContaining({
        markers: [
          expect.objectContaining({ name: "HbA1c", value: 5.3, unit: "%" }),
        ],
      }),
    ),
  );
});
