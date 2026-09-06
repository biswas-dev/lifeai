import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { Blood } from "../routes/Blood";
import { BloodTrend } from "../components/BloodTrend";
import { api } from "../lib/api";
import { resultFlag } from "../lib/blood";
import type { BloodReport, MarkerSeries } from "../lib/types";

vi.mock("../lib/api", () => ({
  api: { bloodReports: vi.fn(), bloodMarkers: vi.fn(), bloodReport: vi.fn() },
}));

function series(
  code: string,
  name: string,
  value: number,
  high: number | null,
  unit: string,
  watch = true,
): MarkerSeries {
  return {
    code,
    name,
    category: "",
    unit,
    watch,
    flag: "",
    ref_low: null,
    ref_high: high,
    points: [
      {
        date: "2026-08-24",
        value,
        report_id: 1,
        flag: "",
        ref_low: null,
        ref_high: high,
        ref_text: high == null ? "" : `< ${high} ${unit}`,
      },
    ],
  };
}
const a1c = series("hba1c", "HbA1c", 7.1, 6, "%");
const alt = series("alt", "ALT", 88, 46, "U/L");
const hemoglobin = series("hemoglobin", "Hemoglobin", 147, 165, "g/L", false);
const report: BloodReport = {
  id: 1,
  taken_on: "2026-08-24",
  lab: "Example lab",
  ordered_by: "",
  notes: "",
  has_file: false,
  parse_status: "manual",
  counts: { total: 3, abnormal: 2 },
  created_at: "",
  markers: [a1c, alt, hemoglobin].map((m, i) => ({
    id: i + 1,
    code: m.code,
    name: m.name,
    category: "",
    value: m.points[0].value,
    value_text: "",
    unit: m.unit,
    ref_low: null,
    ref_high: m.ref_high,
    ref_text: m.points[0].ref_text!,
    flag: "",
    watch: m.watch,
  })),
};

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.bloodReports).mockResolvedValue([report]);
  vi.mocked(api.bloodMarkers).mockResolvedValue([a1c, alt, hemoglobin]);
  vi.mocked(api.bloodReport).mockResolvedValue(report);
});

test("one report shows two red alerts, exact values and honest baselines", async () => {
  render(<Blood />);
  await screen.findByText("2 latest results flagged");
  const card = screen.getByRole("region", { name: "HbA1c trend" });
  expect(within(card).getByText("7.1 %")).toHaveClass("text-rose-400");
  expect(
    within(card)
      .getByRole("img", { name: "HbA1c: baseline" })
      .querySelector("path"),
  ).toBeNull();
  expect(within(card).getByText(/Baseline · Your next result/)).toBeVisible();
  fireEvent.click(
    within(card).getByRole("button", { name: "Explain HbA1c high result" }),
  );
  const dialog = screen.getByRole("dialog", { name: "HbA1c · Result details" });
  expect(within(dialog).getByText(/1.1 percentage points above/)).toBeVisible();
  expect(within(dialog).getByText(/average blood sugar/)).toBeVisible();
  expect(
    within(dialog).getByRole("link", { name: /MedlinePlus/ }),
  ).toHaveAttribute(
    "href",
    "https://medlineplus.gov/lab-tests/hemoglobin-a1c-hba1c-test/",
  );
  fireEvent.keyDown(window, { key: "Escape" });
  expect(screen.queryByRole("dialog")).toBeNull();
  fireEvent.click(
    screen.getByRole("button", { name: "Explain ALT high result" }),
  );
  expect(screen.getByText(/42 U\/L above/)).toBeVisible();
  expect(screen.getByText(/does not identify the cause/)).toBeVisible();
});

test("all markers and search expose numeric markers outside the key set without rounding results", async () => {
  vi.mocked(api.bloodMarkers).mockResolvedValue([
    a1c,
    alt,
    hemoglobin,
    series("ldl", "LDL cholesterol", 3.19, 3.5, "mmol/L"),
  ]);
  render(<Blood />);
  await screen.findByText("3.19 mmol/L");
  expect(screen.queryByRole("heading", { name: "Hemoglobin" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "All markers (4)" }));
  expect(screen.getByRole("heading", { name: "Hemoglobin" })).toBeVisible();
  fireEvent.change(screen.getByRole("searchbox"), {
    target: { value: "hemoglobin" },
  });
  expect(screen.getByRole("heading", { name: "Hemoglobin" })).toBeVisible();
  expect(screen.queryByRole("heading", { name: "ALT" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Flagged (2)" }));
  expect(screen.queryByRole("heading", { name: "Hemoglobin" })).toBeNull();
  expect(screen.getByRole("heading", { name: "ALT" })).toBeVisible();
});

test("multi-year lines use calendar spacing and older readings retain their own lab ranges", async () => {
  const history: MarkerSeries = {
    ...a1c,
    ref_high: 8,
    points: [
      {
        date: "2026-08-24",
        value: 7,
        flag: "normal",
        report_id: 3,
        ref_high: 8,
        ref_low: null,
      },
      {
        date: "2023-08-24",
        value: 7.1,
        flag: "high",
        report_id: 1,
        ref_high: 6,
        ref_low: null,
      },
      {
        date: "2023-09-24",
        value: 6.9,
        flag: "high",
        report_id: 2,
        ref_high: 6,
        ref_low: null,
      },
    ],
  };
  vi.mocked(api.bloodMarkers).mockResolvedValue([history]);
  render(<Blood />);
  const plot = await screen.findByRole("img", {
    name: "HbA1c: 3 readings over time",
  });
  expect(plot.querySelector("path")).not.toBeNull();
  const positions = [...plot.querySelectorAll("circle")].map((c) =>
    Number(c.getAttribute("cx")),
  );
  expect(positions[1] - positions[0]).toBeLessThan(
    (positions[2] - positions[0]) / 10,
  );
  expect(
    screen.getByText(/-0.1 percentage points since first reading/),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "View HbA1c history" }));
  fireEvent.click(screen.getByRole("button", { name: /Thu 24 Aug 2023/ }));
  expect(screen.getByText(/1.1 percentage points above/)).toBeVisible();
  expect(screen.getByText("Lab reference: Upper limit 6 %")).toBeVisible();
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: /Mon 24 Aug 2026/,
    }),
  );
  expect(
    within(screen.getByRole("dialog")).getByText(
      "Lab reference: Upper limit 8 %",
    ),
  ).toBeVisible();
});

test("report alerts explain qualitative abnormal results and closing returns to the report", async () => {
  vi.mocked(api.bloodReport).mockResolvedValue({
    ...report,
    markers: [
      ...report.markers,
      {
        ...report.markers[0],
        code: "smear",
        name: "Smear",
        value: null,
        value_text: "See lab comments",
        flag: "abnormal",
        unit: "",
        ref_low: null,
        ref_high: null,
        ref_text: "",
      },
    ],
  });
  render(<Blood />);
  fireEvent.click(
    await screen.findByRole("button", { name: /Mon 24 Aug 2026 Example lab/ }),
  );
  const reportDialog = await screen.findByRole("dialog", {
    name: "Report · 2026-08-24",
  });
  const altRow = within(reportDialog).getByRole("row", { name: /ALT/ });
  expect(altRow).toHaveClass("text-rose-400");
  fireEvent.click(
    within(reportDialog).getByRole("button", {
      name: "Explain Smear abnormal result",
    }),
  );
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(
    screen.getByText(/The lab marked this result as abnormal/),
  ).toBeVisible();
  expect(
    screen.getByText(/No numeric reference range is available/),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(
    screen.getByRole("dialog", { name: "Report · 2026-08-24" }),
  ).toBeVisible();
});

test("flag classification handles low, positive, missing and zero bounds without inventing abnormality", () => {
  expect(resultFlag({ flag: "", value: -1, ref_low: 0, ref_high: null })).toBe(
    "low",
  );
  expect(resultFlag({ flag: "", value: 0, ref_low: 0, ref_high: null })).toBe(
    "normal",
  );
  expect(
    resultFlag({ flag: "", value: 42, ref_low: null, ref_high: null }),
  ).toBe("");
  expect(resultFlag({ flag: "positive", value: null })).toBe("positive");
  expect(resultFlag({ flag: " H ", value: 42 })).toBe("high");
});

test("same-date readings remain finite and missing ranges do not draw a reference band", () => {
  const sameDate = {
    ...a1c,
    ref_high: null,
    points: [
      { date: "2026-08-24", value: 1, flag: "", report_id: 1 },
      { date: "2026-08-24", value: 2, flag: "", report_id: 2 },
    ],
  };
  render(<BloodTrend series={sameDate} />);
  const plot = screen.getByRole("img");
  expect(plot.querySelector("rect")).toBeNull();
  expect(plot.querySelector("path")?.getAttribute("d")).not.toMatch(
    /NaN|Infinity/,
  );
});
