/** Small formatting helpers shared across screens. */

export function todayISO(): string {
  const d = new Date();
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function addDays(date: string, n: number): string {
  const [y, m, d] = date.split("-").map(Number);
  const dt = new Date(Date.UTC(y, m - 1, d + n));
  return `${dt.getUTCFullYear()}-${pad(dt.getUTCMonth() + 1)}-${pad(dt.getUTCDate())}`;
}

function pad(n: number) {
  return n < 10 ? `0${n}` : String(n);
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
];

/** "Tue 3 Sep" */
export function prettyDate(date: string, withYear = false): string {
  const [y, m, d] = date.split("-").map(Number);
  const dt = new Date(Date.UTC(y, m - 1, d));
  const base = `${WEEKDAYS[dt.getUTCDay()]} ${d} ${MONTHS[m - 1]}`;
  return withYear ? `${base} ${y}` : base;
}

export function weekday(date: string): string {
  const [y, m, d] = date.split("-").map(Number);
  return WEEKDAYS[new Date(Date.UTC(y, m - 1, d)).getUTCDay()];
}

export function relativeDay(date: string): string {
  const t = todayISO();
  if (date === t) return "Today";
  if (date === addDays(t, -1)) return "Yesterday";
  if (date === addDays(t, 1)) return "Tomorrow";
  return prettyDate(date);
}

export function kcal(n: number): string {
  return `${Math.round(n)}`;
}

export function grams(n: number): string {
  return `${Math.round(n)}g`;
}

export function weightDisplay(
  kg: number | null | undefined,
  unit: "kg" | "lb",
): string {
  if (kg == null) return "—";
  return unit === "lb"
    ? `${(kg * 2.20462).toFixed(1)} lb`
    : `${kg.toFixed(1)} kg`;
}

export function toKg(value: number, unit: "kg" | "lb"): number {
  return unit === "lb" ? value / 2.20462 : value;
}

export function fromKg(kg: number, unit: "kg" | "lb"): number {
  return unit === "lb" ? kg * 2.20462 : kg;
}

export function minutes(n: number): string {
  if (n < 60) return `${n} min`;
  const h = Math.floor(n / 60);
  const m = n % 60;
  return m ? `${h}h ${m}m` : `${h}h`;
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function timeAgo(iso: string | null | undefined): string {
  if (!iso) return "never";
  const t = new Date(
    iso.includes("T") ? iso : iso.replace(" ", "T") + "Z",
  ).getTime();
  if (Number.isNaN(t)) return iso;
  const s = Math.round((Date.now() - t) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  return `${Math.round(s / 86400)} d ago`;
}

export function capitalize(s: string): string {
  return s ? s[0].toUpperCase() + s.slice(1) : s;
}
