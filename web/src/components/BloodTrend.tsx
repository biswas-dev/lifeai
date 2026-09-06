import type { MarkerSeries } from "../lib/types";
import {
  isFlagged,
  orderedPoints,
  resultFlag,
  resultValue,
  seriesResult,
} from "../lib/blood";

const shortDate = (date: string) =>
  new Date(`${date}T00:00:00Z`).toLocaleDateString("en-CA", {
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  });

/** Calendar-spaced history: a single reading is a baseline, never a fabricated trend. */
export function BloodTrend({ series }: { series: MarkerSeries }) {
  const points = orderedPoints(series.points);
  if (!points.length)
    return <p className="text-sm text-ink-500">No numeric readings yet.</p>;
  const first = points[0],
    last = points[points.length - 1];
  const start = Date.parse(first.date),
    end = Date.parse(last.date);
  const width = 360,
    height = 130,
    left = 45,
    right = 12,
    top = 14,
    bottom = 28;
  const latest = seriesResult({ ...series, points });
  const limits = [latest.ref_low, latest.ref_high].filter(
    (n): n is number => n != null,
  );
  let low = Math.min(...points.map((p) => p.value), ...limits);
  let high = Math.max(...points.map((p) => p.value), ...limits);
  const padding = (high - low || Math.abs(high) * 0.1 || 1) * 0.2;
  low -= padding;
  high += padding;
  const x = (date: string) =>
    end === start
      ? (left + width - right) / 2
      : left +
        ((Date.parse(date) - start) / (end - start)) * (width - left - right);
  const y = (value: number) =>
    top + ((high - value) / (high - low)) * (height - top - bottom);
  const red = "#a93f51",
    green = "#356b56";
  const color = isFlagged(resultFlag(latest)) ? red : green;
  return (
    <figure>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="w-full"
        role="img"
        aria-label={`${series.name}: ${points.length === 1 ? "baseline" : `${points.length} readings over time`}`}
      >
        <title>
          {points
            .map(
              (p) =>
                `${p.date}: ${resultValue(seriesResult({ ...series, points }, p))}`,
            )
            .join("; ")}
        </title>
        {limits.length > 0 && (
          <rect
            x={left}
            y={y(latest.ref_high ?? high)}
            width={width - left - right}
            height={Math.max(
              0,
              y(latest.ref_low ?? low) - y(latest.ref_high ?? high),
            )}
            fill={green}
            opacity={0.07}
          />
        )}
        {[low, (low + high) / 2, high].map((value) => (
          <g key={value}>
            <line
              x1={left}
              x2={width - right}
              y1={y(value)}
              y2={y(value)}
              stroke="#e0e7e2"
            />
            <text
              x={left - 7}
              y={y(value) + 3}
              textAnchor="end"
              fontSize={10}
              fill="#59695f"
            >
              {Number(value.toPrecision(3))}
            </text>
          </g>
        ))}
        {limits.map((limit, i) => (
          <line
            key={i}
            x1={left}
            x2={width - right}
            y1={y(limit)}
            y2={y(limit)}
            stroke={green}
            strokeDasharray="4 4"
            opacity={0.6}
          />
        ))}
        {points.length > 1 && (
          <path
            d={points
              .map((p, i) => `${i ? "L" : "M"}${x(p.date)},${y(p.value)}`)
              .join(" ")}
            fill="none"
            stroke={color}
            strokeWidth={2}
            strokeLinejoin="round"
          />
        )}
        {points.map((p, i) => {
          const result = seriesResult({ ...series, points }, p);
          return (
            <circle
              key={`${p.report_id}-${i}`}
              cx={x(p.date)}
              cy={y(p.value)}
              r={3.5}
              fill={isFlagged(resultFlag(result)) ? red : green}
              stroke="white"
              strokeWidth={1.5}
            >
              <title>
                {p.date}: {resultValue(result)} ·{" "}
                {resultFlag(result) || "No range"}
              </title>
            </circle>
          );
        })}
        {end === start ? (
          <text
            x={x(first.date)}
            y={height - 6}
            textAnchor="middle"
            fontSize={10}
            fill="#59695f"
          >
            {shortDate(first.date)}
          </text>
        ) : (
          <>
            <text x={left} y={height - 6} fontSize={10} fill="#59695f">
              {shortDate(first.date)}
            </text>
            <text
              x={width - right}
              y={height - 6}
              textAnchor="end"
              fontSize={10}
              fill="#59695f"
            >
              {shortDate(last.date)}
            </text>
          </>
        )}
      </svg>
      <figcaption className="mt-1 text-xs leading-relaxed text-ink-500">
        {points.length === 1
          ? "Baseline · Your next result will start the trend line."
          : `${points.length} readings · Spaced by collection date.`}
        {limits.length > 0 && (
          <span className="block">
            Shading shows the latest lab reference range.
          </span>
        )}
      </figcaption>
    </figure>
  );
}
