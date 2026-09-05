import { useEffect, type ReactNode } from "react";
import { XIcon } from "./Icons";

/** Page title row. */
export function PageHeader({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle?: string;
  action?: ReactNode;
}) {
  return (
    <div className="mb-5 flex items-start justify-between gap-3">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-ink-100">
          {title}
        </h1>
        {subtitle && <p className="mt-0.5 text-sm text-ink-500">{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}

/** A bottom sheet on phones, a centred dialog on desktop. */
export function Sheet({
  open,
  onClose,
  title,
  children,
  wide,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  wide?: boolean;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 md:items-center"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-label={title}
        className={`max-h-[92dvh] w-full overflow-y-auto rounded-t-3xl border border-ink-800 bg-ink-900 p-5 animate-slide-up md:rounded-2xl ${wide ? "md:max-w-2xl" : "md:max-w-md"}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-ink-100">{title}</h2>
          <button
            className="rounded-lg p-1.5 text-ink-500 hover:bg-ink-800 hover:text-ink-200"
            onClick={onClose}
            aria-label="Close"
          >
            <XIcon size={18} />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

export function ErrorText({ children }: { children: ReactNode }) {
  if (!children) return null;
  return (
    <p className="rounded-xl border border-rose-500/25 bg-rose-500/10 px-3 py-2 text-sm text-rose-400">
      {children}
    </p>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return (
    <div className="card px-4 py-8 text-center text-sm text-ink-500">
      {children}
    </div>
  );
}

export function Spinner({ className = "" }: { className?: string }) {
  return (
    <div
      className={`h-6 w-6 animate-spin rounded-full border-2 border-ink-700 border-t-vital-500 ${className}`}
    />
  );
}

/** A number with a label, for stat rows. */
export function Stat({
  label,
  value,
  sub,
  tone,
}: {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  tone?: "vital" | "ember" | "sky" | "rose";
}) {
  const color =
    tone === "vital"
      ? "text-vital-400"
      : tone === "ember"
        ? "text-ember-400"
        : tone === "sky"
          ? "text-sky-400"
          : tone === "rose"
            ? "text-rose-400"
            : "text-ink-100";
  return (
    <div className="card px-3.5 py-3">
      <div className="text-[11px] font-medium uppercase tracking-wide text-ink-500">
        {label}
      </div>
      <div className={`mt-0.5 text-xl font-semibold tabular-nums ${color}`}>
        {value}
      </div>
      {sub && <div className="mt-0.5 text-xs text-ink-500">{sub}</div>}
    </div>
  );
}

/** Progress bar toward a target. */
export function Bar({
  value,
  target,
  tone = "vital",
}: {
  value: number;
  target?: number | null;
  tone?: "vital" | "ember" | "sky" | "rose";
}) {
  const pct = target && target > 0 ? Math.min(100, (value / target) * 100) : 0;
  const over = target && target > 0 && value > target * 1.05;
  const bg = over
    ? "bg-rose-500"
    : tone === "ember"
      ? "bg-ember-500"
      : tone === "sky"
        ? "bg-sky-500"
        : tone === "rose"
          ? "bg-rose-500"
          : "bg-vital-500";
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-ink-800">
      <div
        className={`h-full rounded-full transition-all ${bg}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

export function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
}) {
  return (
    <label className="block">
      <span className="label">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-ink-500">{hint}</span>}
    </label>
  );
}

export function NumberInput({
  value,
  onChange,
  placeholder,
  step = "any",
  min,
  max,
}: {
  value: number | "";
  onChange: (v: number | "") => void;
  placeholder?: string;
  step?: string;
  min?: number;
  max?: number;
}) {
  return (
    <input
      type="number"
      inputMode="decimal"
      className="field"
      value={value}
      placeholder={placeholder}
      step={step}
      min={min}
      max={max}
      onChange={(e) =>
        onChange(e.target.value === "" ? "" : Number(e.target.value))
      }
    />
  );
}
