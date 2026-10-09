import type { Meal, NutritionPlan, PlanMeal, PlanOption } from "./types";

export const WEEKDAY_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

export function weekdayOf(date: string) {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d)).getUTCDay();
}

/** The option planned for a weekday: pinned to that day, else unpinned, else the first. */
export function optionFor(meal: PlanMeal, weekday: number): PlanOption {
  return meal.options.find((o) => o.days?.includes(weekday)) || meal.options.find((o) => !o.days?.length) || meal.options[0];
}

export const planKey = (meal: PlanMeal, option: PlanOption) => `${meal.key}:${option.key}`;

/** The logged meal fulfilling a planned one, if any. */
export function loggedFor(meals: Meal[], meal: PlanMeal) {
  return meals.find((m) => m.plan_key === meal.key || m.plan_key?.startsWith(`${meal.key}:`));
}

export function plannedTotals(plan: NutritionPlan, weekday: number) {
  return plan.meals.reduce(
    (t, m) => {
      const o = optionFor(m, weekday);
      return { kcal: t.kcal + o.kcal, protein_g: t.protein_g + o.protein_g, carbs_g: t.carbs_g + o.carbs_g, fat_g: t.fat_g + o.fat_g };
    },
    { kcal: 0, protein_g: 0, carbs_g: 0, fat_g: 0 },
  );
}

function minutesOf(clock: string) {
  const [h, m] = clock.split(":").map(Number);
  return h * 60 + m;
}

export function clockLabel(clock: string) {
  const [h, m] = clock.split(":").map(Number);
  const suffix = h >= 12 ? "PM" : "AM";
  const hour = h % 12 || 12;
  return m ? `${hour}:${String(m).padStart(2, "0")} ${suffix}` : `${hour} ${suffix}`;
}

function duration(mins: number) {
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return h ? (m ? `${h}h ${m}m` : `${h}h`) : `${m}m`;
}

export interface WindowStatus {
  state: "eating" | "fasting";
  headline: string;
  detail: string;
  /** Fraction of the current phase elapsed, 0-1. */
  progress: number;
}

/** Where now falls in the eating window, in the device's local time. */
export function windowStatus(plan: NutritionPlan, now: Date): WindowStatus | null {
  if (!plan.window_start || !plan.window_end) return null;
  const start = minutesOf(plan.window_start);
  const end = minutesOf(plan.window_end);
  const t = now.getHours() * 60 + now.getMinutes();
  const eatLen = (end - start + 1440) % 1440;
  const fastLen = 1440 - eatLen;
  const intoEating = (t - start + 1440) % 1440;
  if (intoEating < eatLen) {
    const left = eatLen - intoEating;
    return {
      state: "eating",
      headline: "Eating window open",
      detail: `Closes at ${clockLabel(plan.window_end)} · ${duration(left)} left`,
      progress: intoEating / eatLen,
    };
  }
  const intoFast = (t - end + 1440) % 1440;
  return {
    state: "fasting",
    headline: `Fasting · ${duration(intoFast)} of ${duration(fastLen)}`,
    detail: `Eating window opens at ${clockLabel(plan.window_start)} · in ${duration(fastLen - intoFast)}`,
    progress: intoFast / fastLen,
  };
}

export const MEAL_TIME_LABEL = (m: PlanMeal) => (m.end_time ? `${clockLabel(m.time)}–${clockLabel(m.end_time)}` : clockLabel(m.time));
