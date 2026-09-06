import { useRef, useState } from "react";
import type { MarkerSeries } from "../lib/types";
import {
  type BloodResult,
  flagExplanation,
  flagLabel,
  isFlagged,
  markerEducation,
  orderedPoints,
  referenceRange,
  resultFlag,
  resultValue,
  seriesResult,
} from "../lib/blood";
import { prettyDate } from "../lib/format";
import { Sheet } from "./ui";
import { BloodTrend } from "./BloodTrend";

export function BloodFlag({
  result,
  onClick,
}: {
  result: BloodResult;
  onClick: () => void;
}) {
  const flag = resultFlag(result);
  return isFlagged(flag) ? (
    <button
      type="button"
      onClick={onClick}
      aria-label={`Explain ${result.name} ${flag} result`}
      className="inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-lg px-2 text-xs font-semibold text-rose-400 hover:bg-rose-500/10"
    >
      <span
        aria-hidden="true"
        className="flex h-5 w-5 items-center justify-center rounded-full bg-rose-400 text-sm font-bold text-white"
      >
        !
      </span>
      {flagLabel(flag)}
    </button>
  ) : (
    <span className="text-xs text-ink-500">{flagLabel(flag)}</span>
  );
}

export interface BloodDetailSelection {
  result: BloodResult;
  series?: MarkerSeries;
}

export function BloodDetails({
  selection,
  onClose,
}: {
  selection: BloodDetailSelection;
  onClose: () => void;
}) {
  const [result, setResult] = useState(selection.result);
  const resultPanel = useRef<HTMLElement>(null);
  const flag = resultFlag(result),
    flagged = isFlagged(flag);
  const education = markerEducation[result.code];
  const series = selection.series
    ? { ...selection.series, points: orderedPoints(selection.series.points) }
    : undefined;
  return (
    <Sheet
      open
      wide
      title={`${result.name} · Result details`}
      onClose={onClose}
    >
      <div className="space-y-5">
        <section
          ref={resultPanel}
          tabIndex={-1}
          className={`rounded-xl border p-4 outline-none ${flagged ? "border-rose-500/25 bg-rose-500/5" : "border-ink-800 bg-ink-950"}`}
          aria-live="polite"
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p
              className={`text-2xl font-semibold tabular-nums ${flagged ? "text-rose-400" : "text-ink-100"}`}
            >
              {resultValue(result)}
            </p>
            <span
              className={`text-sm font-medium ${flagged ? "text-rose-400" : "text-ink-400"}`}
            >
              {flagged && "! "}
              {flagLabel(flag)}
            </span>
          </div>
          {result.date && (
            <p className="mt-1 text-xs text-ink-500">
              {prettyDate(result.date, true)}
            </p>
          )}
          <p className="mt-3 text-sm text-ink-400">
            Lab reference: {referenceRange(result)}
          </p>
          <h3 className="mt-4 text-sm font-semibold">
            {flagged ? "Why this is flagged" : "About this result"}
          </h3>
          <p className="mt-1 text-sm leading-relaxed">
            {flagExplanation(result)}
          </p>
        </section>
        {education && (
          <section className="space-y-2 text-sm leading-relaxed">
            <h3 className="font-semibold">What this marker means</h3>
            <p>{education.about}</p>
            {flag === "high" && <p>{education.high}</p>}
            <a
              className="inline-block text-vital-500 underline"
              href={education.source}
              target="_blank"
              rel="noreferrer"
            >
              Read more at MedlinePlus ↗
            </a>
          </section>
        )}
        <p className="text-xs leading-relaxed text-ink-500">
          A flag is not a diagnosis. Review the original report and discuss the
          result, your personal target and any follow-up testing with your
          clinician.
        </p>
        {series && series.points.length > 0 && (
          <section>
            <h3 className="mb-3 font-semibold">
              History · {series.unit || "unit not supplied"}
            </h3>
            <BloodTrend series={series} />
            <p className="mb-2 mt-4 text-xs text-ink-500">
              Select a reading to see its result and reference range.
            </p>
            <div className="space-y-2">
              {[...series.points].reverse().map((point, i) => {
                const reading = seriesResult(series, point),
                  readingFlag = resultFlag(reading);
                return (
                  <button
                    key={`${point.report_id}-${i}`}
                    className={`flex min-h-11 w-full flex-wrap items-center justify-between gap-2 rounded-xl border p-3 text-left text-sm ${isFlagged(readingFlag) ? "border-rose-500/25 text-rose-400" : "border-ink-800 text-ink-300"}`}
                    onClick={() => {
                      setResult(reading);
                      resultPanel.current?.focus();
                    }}
                  >
                    <span>{prettyDate(point.date, true)}</span>
                    <span className="font-medium tabular-nums">
                      {resultValue(reading)} · {isFlagged(readingFlag) && "! "}
                      {flagLabel(readingFlag)}
                    </span>
                  </button>
                );
              })}
            </div>
          </section>
        )}
      </div>
    </Sheet>
  );
}
