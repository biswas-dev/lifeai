import type { MarkerPoint, MarkerSeries } from "./types";

export interface BloodResult {
  code: string;
  name: string;
  unit: string;
  value: number | null;
  value_text?: string;
  date?: string;
  flag: string;
  ref_low?: number | null;
  ref_high?: number | null;
  ref_text?: string;
}

export function resultFlag(
  result: Pick<BloodResult, "flag" | "value" | "ref_low" | "ref_high">,
): string {
  const flag = result.flag.trim().toLowerCase();
  const normalized =
    flag === "h"
      ? "high"
      : flag === "l"
        ? "low"
        : flag === "a"
          ? "abnormal"
          : flag;
  if (isFlagged(normalized)) return normalized;
  if (result.value != null) {
    if (result.ref_high != null && result.value > result.ref_high)
      return "high";
    if (result.ref_low != null && result.value < result.ref_low) return "low";
    if (!normalized && (result.ref_low != null || result.ref_high != null))
      return "normal";
  }
  return normalized;
}

export function isFlagged(flag: string): boolean {
  return ["high", "low", "abnormal", "positive"].includes(flag);
}

export function flagLabel(flag: string): string {
  const label = flag.replace(/_/g, " ");
  return label ? label[0].toUpperCase() + label.slice(1) : "No range";
}

export function resultValue(result: BloodResult): string {
  return result.value == null
    ? result.value_text || "No numeric result"
    : `${result.value} ${result.unit}`.trim();
}

export function referenceRange(
  result: Pick<BloodResult, "ref_low" | "ref_high" | "ref_text" | "unit">,
): string {
  if (result.ref_text) return result.ref_text;
  const { ref_low: low, ref_high: high, unit } = result;
  if (low != null && high != null) return `${low} – ${high} ${unit}`.trim();
  if (high != null) return `Upper limit ${high} ${unit}`.trim();
  if (low != null) return `Lower limit ${low} ${unit}`.trim();
  return "Not provided by the lab";
}

export function seriesResult(
  series: MarkerSeries,
  point = series.points[series.points.length - 1],
): BloodResult {
  // Older API responses only include the latest range. Never apply it to an older reading.
  const latest = point === series.points[series.points.length - 1];
  return {
    code: series.code,
    name: series.name,
    unit: series.unit,
    value: point?.value ?? null,
    date: point?.date,
    flag: point?.flag || (latest ? series.flag : ""),
    ref_low:
      point?.ref_low !== undefined
        ? point.ref_low
        : latest
          ? series.ref_low
          : null,
    ref_high:
      point?.ref_high !== undefined
        ? point.ref_high
        : latest
          ? series.ref_high
          : null,
    ref_text:
      point?.ref_text !== undefined
        ? point.ref_text
        : latest
          ? series.ref_text
          : "",
  };
}

export function orderedPoints(points: MarkerPoint[]): MarkerPoint[] {
  return [...points].sort(
    (a, b) => a.date.localeCompare(b.date) || a.report_id - b.report_id,
  );
}

export function flagExplanation(result: BloodResult): string {
  const flag = resultFlag(result);
  const boundary =
    flag === "high" ? result.ref_high : flag === "low" ? result.ref_low : null;
  if (
    result.value != null &&
    boundary != null &&
    (flag === "high" ? result.value > boundary : result.value < boundary)
  ) {
    const difference = Number(Math.abs(result.value - boundary).toPrecision(6));
    const unit = result.unit === "%" ? "percentage points" : result.unit;
    return `Your result is ${difference}${unit ? ` ${unit}` : ""} ${flag === "high" ? "above" : "below"} the lab’s ${flag === "high" ? "upper" : "lower"} limit of ${boundary}${result.unit ? ` ${result.unit}` : ""}.`;
  }
  if (isFlagged(flag)) {
    return `The lab marked this result as ${flag}. ${result.ref_low == null && result.ref_high == null ? "No numeric reference range is available to calculate how far it is outside the range." : "The lab’s flag is retained even when the numeric range alone does not explain it."} Check the original report for comments and context.`;
  }
  return flag === "normal"
    ? "This result is within the recorded lab range or was marked normal by the lab. Your personal target may differ."
    : "There is no numeric range or abnormal flag to interpret for this result. Check the original report for the lab’s comments.";
}

// General education, reviewed against MedlinePlus on 2026-09-05. These do not diagnose a result.
export const markerEducation: Record<
  string,
  { about: string; high: string; source: string }
> = {
  hba1c: {
    about:
      "HbA1c reflects your average blood sugar over roughly the past two to three months.",
    high: "A high HbA1c can indicate persistently elevated blood sugar. Your clinician can assess this alongside your history and may repeat the test or use another glucose test to confirm what it means.",
    source: "https://medlineplus.gov/lab-tests/hemoglobin-a1c-hba1c-test/",
  },
  alt: {
    about:
      "ALT is an enzyme found mainly in the liver. More ALT can enter the blood when liver cells are injured.",
    high: "A high ALT can signal liver irritation or injury, but it does not identify the cause or measure the amount of damage. Medicines, supplements and intense exercise can also affect the result. Your clinician can review it alongside other liver tests.",
    source: "https://medlineplus.gov/lab-tests/alt-blood-test/",
  },
};
