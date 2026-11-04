export const DEFAULT_ORDER_DURATION = "P1M";
export const RESERVATION_DURATIONS = ["P1M", "P3M", "P6M", "P12M", "P24M", "P36M"];

export function durationMonths(value = DEFAULT_ORDER_DURATION): number {
  const match = /^P([1-9]\d*)(M|Y)$/.exec(value);
  return match ? Number(match[1]) * (match[2] === "Y" ? 12 : 1) : 1;
}

export function supportsDuration(options: string[] | undefined, value: string): boolean {
  return !options?.length || options.some((option) => durationMonths(option) === durationMonths(value));
}
