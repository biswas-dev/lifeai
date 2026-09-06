import type { WaterUnit } from "./types";

export const waterUnits: {
  value: WaterUnit;
  label: string;
  short: string;
  ml: number;
  quick: number[];
}[] = [
  {
    value: "gal",
    label: "US gallons",
    short: "gal",
    ml: 3785.411784,
    quick: [0.125, 0.25, 0.5],
  },
  {
    value: "oz",
    label: "US fluid ounces",
    short: "oz",
    ml: 29.5735295625,
    quick: [8, 16, 24],
  },
  {
    value: "ml",
    label: "Millilitres",
    short: "ml",
    ml: 1,
    quick: [250, 500, 750],
  },
  { value: "l", label: "Litres", short: "L", ml: 1000, quick: [0.25, 0.5, 1] },
];
export const waterConfig = (unit: WaterUnit) =>
  waterUnits.find((item) => item.value === unit) || waterUnits[0];
export function waterAmount(ml: number | null | undefined, unit: WaterUnit) {
  const value = (ml || 0) / waterConfig(unit).ml;
  return value.toLocaleString(undefined, {
    maximumFractionDigits: unit === "ml" ? 0 : unit === "oz" ? 1 : 2,
  });
}
export const waterDisplay = (ml: number | null | undefined, unit: WaterUnit) =>
  `${waterAmount(ml, unit)} ${waterConfig(unit).short}`;
export function drinkLabel(amount: number, unit: WaterUnit) {
  if (unit === "gal")
    return `${amount === 0.125 ? "⅛" : amount === 0.25 ? "¼" : amount === 0.5 ? "½" : amount} gal`;
  return `${amount} ${waterConfig(unit).short}`;
}
