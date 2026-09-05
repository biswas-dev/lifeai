import type { Point } from "../lib/types";

/**
 * A small line chart, SVG, no library. Draws the series, a band for a
 * reference range when given, and the last value.
 */
export function Sparkline({
  points,
  height = 64,
  color = "#356b56",
  refLow,
  refHigh,
  target,
  unit = "",
  format = (v: number) => String(Math.round(v * 10) / 10),
}: {
  points: Point[];
  height?: number;
  color?: string;
  refLow?: number | null;
  refHigh?: number | null;
  target?: number | null;
  unit?: string;
  format?: (v: number) => string;
}) {
  if (points.length === 0) {
    return (
      <div
        className="flex items-center justify-center text-xs text-ink-600"
        style={{ height }}
      >
        no data yet
      </div>
    );
  }
  const w = 320;
  const h = height;
  const pad = 6;
  const values = points.map((p) => p.value);
  let lo = Math.min(...values);
  let hi = Math.max(...values);
  if (refLow != null) lo = Math.min(lo, refLow);
  if (refHigh != null) hi = Math.max(hi, refHigh);
  if (target != null) {
    lo = Math.min(lo, target);
    hi = Math.max(hi, target);
  }
  if (hi === lo) {
    hi += 1;
    lo -= 1;
  }
  const span = hi - lo;
  lo -= span * 0.1;
  hi += span * 0.1;
  const x = (i: number) =>
    points.length === 1
      ? w / 2
      : pad + (i / (points.length - 1)) * (w - pad * 2);
  const y = (v: number) => h - pad - ((v - lo) / (hi - lo)) * (h - pad * 2);
  const d = points
    .map(
      (p, i) =>
        `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.value).toFixed(1)}`,
    )
    .join(" ");
  const last = points[points.length - 1];
  return (
    <div className="relative">
      <svg
        viewBox={`0 0 ${w} ${h}`}
        className="h-auto w-full"
        preserveAspectRatio="none"
        aria-hidden
      >
        {(refLow != null || refHigh != null) && (
          <rect
            x={0}
            y={y(refHigh ?? hi)}
            width={w}
            height={Math.max(0, y(refLow ?? lo) - y(refHigh ?? hi))}
            fill={color}
            opacity={0.08}
          />
        )}
        {target != null && (
          <line
            x1={0}
            x2={w}
            y1={y(target)}
            y2={y(target)}
            stroke="#f59e0b"
            strokeDasharray="4 4"
            strokeWidth={1}
            opacity={0.7}
          />
        )}
        <path
          d={d}
          fill="none"
          stroke={color}
          strokeWidth={2}
          strokeLinejoin="round"
          strokeLinecap="round"
        />
        {points.map((p, i) => (
          <circle
            key={p.date + i}
            cx={x(i)}
            cy={y(p.value)}
            r={points.length > 40 ? 1.5 : 2.5}
            fill={color}
          />
        ))}
      </svg>
      <div className="absolute right-0 top-0 rounded-md bg-ink-950/70 px-1.5 py-0.5 text-[11px] tabular-nums text-ink-300">
        {format(last.value)}
        {unit && <span className="text-ink-500"> {unit}</span>}
      </div>
    </div>
  );
}

/** Bars per day, for kcal and training. */
export function Bars({
  points,
  target,
  height = 64,
  color = "#356b56",
}: {
  points: Point[];
  target?: number | null;
  height?: number;
  color?: string;
}) {
  if (points.length === 0) {
    return (
      <div
        className="flex items-center justify-center text-xs text-ink-600"
        style={{ height }}
      >
        no data yet
      </div>
    );
  }
  const w = 320;
  const h = height;
  const max = Math.max(...points.map((p) => p.value), target ?? 0) || 1;
  const bw = Math.max(2, (w - points.length * 2) / points.length);
  return (
    <svg
      viewBox={`0 0 ${w} ${h}`}
      className="h-auto w-full"
      preserveAspectRatio="none"
      aria-hidden
    >
      {points.map((p, i) => {
        const bh = (p.value / max) * (h - 4);
        const over = target != null && p.value > target * 1.05;
        return (
          <rect
            key={p.date + i}
            x={i * (bw + 2)}
            y={h - bh}
            width={bw}
            height={bh}
            rx={1.5}
            fill={over ? "#f43f5e" : color}
            opacity={0.9}
          />
        );
      })}
      {target != null && target > 0 && (
        <line
          x1={0}
          x2={w}
          y1={h - (target / max) * (h - 4)}
          y2={h - (target / max) * (h - 4)}
          stroke="#f59e0b"
          strokeDasharray="4 4"
          strokeWidth={1}
          opacity={0.8}
        />
      )}
    </svg>
  );
}
