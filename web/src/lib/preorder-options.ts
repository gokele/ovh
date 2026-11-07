export function preorderOptions(options: string[], cpuCodes: string[] = []): string[] {
  const cpus = new Set(cpuCodes.map((value) => value.toLowerCase()));
  return options.filter((value) => !cpus.has(value.toLowerCase()) && !/^(?:cpu|processor)(?:-|$)/i.test(value.trim()));
}

export function retainPreorderPlan(previous: string, planCode: string, prerelease: boolean): string {
  return prerelease || previous === planCode ? planCode : "";
}

// This is form feedback only. The backend resolves official product keys and checks the cart.
export function preorderVariantCoversAll(fqn: string, options: string[]): boolean {
  const parts = fqn.toLowerCase().split(".").slice(1);
  return options.every((option) => {
    const wanted = option.trim().toLowerCase().replace(/-(?:\d{2}[a-z0-9]+-v\d+(?:-[a-z]+)?|rise|sys|sk)$/, "");
    if (!wanted.startsWith("ram-") && !wanted.startsWith("gpu-") && !/^(?:hybridsoft|soft|hard|no)?raid-/.test(wanted)) return true;
    return parts.some((part) => part === wanted);
  });
}
export function hasPreorderHardware(options: string[]): boolean {
  return options.some((code) => /^ram-/i.test(code)) && options.some((code) => /^(?:hybridsoft|soft|hard|no)?raid-/i.test(code));
}
