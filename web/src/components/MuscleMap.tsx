import type { ReactNode } from "react";
import type { Muscle } from "../lib/types";
import { muscleLabels } from "../lib/training";

type Shape = { muscle: Muscle; el: ReactNode };

// Both figures are drawn on a 120 × 260 grid centred on x = 60. Shapes are
// deliberately simple — a guide to where a muscle is, not an anatomy chart.
const mirror = (el: (x: (v: number) => number) => ReactNode) => (
  <>
    {el((v) => v)}
    {el((v) => 120 - v)}
  </>
);

function silhouette() {
  return (
    <g className="fill-ink-850">
      <ellipse cx={60} cy={20} rx={11} ry={13} />
      <rect x={54} y={30} width={12} height={10} rx={3} />
      <path d="M36 42 Q60 35 84 42 L90 60 Q87 92 81 112 L39 112 Q33 92 30 60 Z" />
      <path d="M38 108 L82 108 L86 134 L60 142 L34 134 Z" />
      {mirror((x) => (
        <>
          <ellipse cx={x(28)} cy={68} rx={7.5} ry={19} transform={`rotate(${x(8) === 8 ? 8 : -8} ${x(28)} 68)`} />
          <ellipse cx={x(22)} cy={101} rx={6.5} ry={17} transform={`rotate(${x(10) === 10 ? 10 : -10} ${x(22)} 101)`} />
          <circle cx={x(18)} cy={123} r={5} />
          <ellipse cx={x(47)} cy={162} rx={12} ry={29} />
          <ellipse cx={x(46)} cy={214} rx={8.5} ry={25} />
          <ellipse cx={x(45)} cy={245} rx={8} ry={4} />
        </>
      ))}
    </g>
  );
}

const rot = (x: (v: number) => number, deg: number, cx: number, cy: number) =>
  `rotate(${x(0) === 0 ? deg : -deg} ${x(cx)} ${cy})`;

const front: Shape[] = [
  { muscle: "traps", el: mirror((x) => <path d={`M${x(53)} 38 L${x(40)} 44 L${x(53)} 45 Z`} />) },
  { muscle: "side-delts", el: mirror((x) => <ellipse cx={x(29)} cy={49} rx={5} ry={8} />) },
  { muscle: "front-delts", el: mirror((x) => <ellipse cx={x(37)} cy={50} rx={5.5} ry={8} />) },
  { muscle: "chest", el: mirror((x) => <path d={`M${x(42)} 54 Q${x(50)} 48 ${x(59)} 52 L${x(59)} 68 Q${x(48)} 72 ${x(42)} 66 Z`} />) },
  { muscle: "biceps", el: mirror((x) => <ellipse cx={x(28)} cy={71} rx={5} ry={11} transform={rot(x, 8, 28, 71)} />) },
  { muscle: "forearms", el: mirror((x) => <ellipse cx={x(22)} cy={100} rx={5} ry={14} transform={rot(x, 10, 22, 100)} />) },
  { muscle: "abs", el: <rect x={52} y={72} width={16} height={36} rx={5} /> },
  { muscle: "obliques", el: mirror((x) => <path d={`M${x(42)} 72 L${x(50.5)} 74 L${x(50.5)} 106 L${x(44)} 104 Q${x(40)} 90 ${x(42)} 72 Z`} />) },
  { muscle: "hip-flexors", el: mirror((x) => <path d={`M${x(44)} 112 L${x(54)} 114 L${x(57)} 127 L${x(48)} 124 Z`} />) },
  { muscle: "quads", el: mirror((x) => <ellipse cx={x(45.5)} cy={161} rx={9} ry={24} />) },
  { muscle: "adductors", el: mirror((x) => <path d={`M${x(55)} 132 L${x(59)} 137 L${x(58)} 168 L${x(53.5)} 158 Z`} />) },
  { muscle: "calves", el: mirror((x) => <ellipse cx={x(46)} cy={212} rx={6} ry={18} />) },
];

const back: Shape[] = [
  { muscle: "traps", el: <path d="M60 34 L75 44 L66 64 L60 71 L54 64 L45 44 Z" /> },
  { muscle: "side-delts", el: mirror((x) => <ellipse cx={x(29)} cy={49} rx={5} ry={8} />) },
  { muscle: "rear-delts", el: mirror((x) => <ellipse cx={x(37)} cy={50} rx={5.5} ry={8} />) },
  { muscle: "upper-back", el: mirror((x) => <path d={`M${x(44)} 52 L${x(52.5)} 61 L${x(52.5)} 77 L${x(44)} 71 Z`} />) },
  { muscle: "lats", el: mirror((x) => <path d={`M${x(39)} 62 L${x(43)} 74 L${x(53)} 81 L${x(54)} 101 Q${x(45)} 97 ${x(39)} 84 Z`} />) },
  { muscle: "lower-back", el: mirror((x) => <rect x={x(0) === 0 ? 52 : 61} y={82} width={7} height={26} rx={3} />) },
  { muscle: "triceps", el: mirror((x) => <ellipse cx={x(28)} cy={70} rx={5} ry={11} transform={rot(x, 8, 28, 70)} />) },
  { muscle: "forearms", el: mirror((x) => <ellipse cx={x(22)} cy={100} rx={5} ry={14} transform={rot(x, 10, 22, 100)} />) },
  { muscle: "glutes", el: mirror((x) => <ellipse cx={x(50)} cy={124} rx={10} ry={11} />) },
  { muscle: "hamstrings", el: mirror((x) => <ellipse cx={x(46.5)} cy={164} rx={9} ry={22} />) },
  { muscle: "calves", el: mirror((x) => <ellipse cx={x(46)} cy={207} rx={7} ry={18} />) },
];

/** A front and back body map highlighting the muscles an exercise trains. */
export function MuscleMap({
  primary,
  secondary = [],
  selected,
  onSelect,
  className = "",
}: {
  primary: Muscle[];
  secondary?: Muscle[];
  selected?: Muscle | "";
  onSelect?: (m: Muscle) => void;
  className?: string;
}) {
  const fill = (m: Muscle) =>
    primary.includes(m) || selected === m
      ? "fill-vital-500"
      : secondary.includes(m)
        ? "fill-[#a8c9b6]"
        : "fill-ink-800";
  const label = onSelect
    ? "Body map. Choose a muscle to filter exercises."
    : `Muscles worked. Primary: ${primary.map((m) => muscleLabels[m]).join(", ") || "none"}.${secondary.length ? ` Also: ${secondary.map((m) => muscleLabels[m]).join(", ")}.` : ""}`;
  const figure = (shapes: Shape[], dx: number, title: string) => (
    <g transform={`translate(${dx} 0)`}>
      {silhouette()}
      {shapes.map((s, i) => (
        <g
          key={`${s.muscle}-${i}`}
          className={`${fill(s.muscle)} transition-colors ${onSelect ? "cursor-pointer hover:fill-vital-300" : ""}`}
          stroke="white"
          strokeWidth={0.8}
          onClick={onSelect ? () => onSelect(s.muscle) : undefined}
          role={onSelect ? "button" : undefined}
          tabIndex={onSelect ? 0 : undefined}
          aria-label={onSelect ? muscleLabels[s.muscle] : undefined}
          onKeyDown={
            onSelect
              ? (e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onSelect(s.muscle);
                  }
                }
              : undefined
          }
        >
          <title>{muscleLabels[s.muscle]}</title>
          {s.el}
        </g>
      ))}
      <text x={60} y={258} textAnchor="middle" className="fill-ink-500 text-[9px]">
        {title}
      </text>
    </g>
  );
  return (
    <svg
      viewBox="0 0 250 262"
      role={onSelect ? "group" : "img"}
      aria-label={label}
      className={className}
    >
      {figure(front, 0, "Front")}
      {figure(back, 130, "Back")}
    </svg>
  );
}

export function MuscleLegend({
  primary,
  secondary,
}: {
  primary: Muscle[];
  secondary: Muscle[];
}) {
  return (
    <div className="space-y-2 text-xs">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="mr-1 inline-block h-2.5 w-2.5 rounded-sm bg-vital-500" />
        <span className="text-ink-500">Main</span>
        {primary.map((m) => (
          <span key={m} className="rounded-full bg-vital-500/10 px-2 py-0.5 font-medium text-vital-500">
            {muscleLabels[m]}
          </span>
        ))}
      </div>
      {secondary.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="mr-1 inline-block h-2.5 w-2.5 rounded-sm bg-[#a8c9b6]" />
          <span className="text-ink-500">Also</span>
          {secondary.map((m) => (
            <span key={m} className="rounded-full bg-ink-850 px-2 py-0.5 text-ink-400">
              {muscleLabels[m]}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
