import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export function useOrderDurations(accountId: string, planCode: string, product = "eco", enabled = true, subsidiary = "") {
  const [settledPlan, setSettledPlan] = useState(planCode.trim());
  useEffect(() => {
    const timer = setTimeout(() => setSettledPlan(planCode.trim()), 500);
    return () => clearTimeout(timer);
  }, [planCode]);
  const query = useQuery({
    queryKey: ["purchase", "durations", accountId, product, subsidiary, settledPlan],
    queryFn: async ({ signal }) => {
      const { data } = await api.get<{ durations: string[] }>("/purchase/durations", {
        params: { account_id: accountId, planCode: settledPlan, product, subsidiary: subsidiary || undefined }, signal,
      });
      if (!Array.isArray(data.durations)) throw new Error("Invalid purchase period response");
      return data.durations.filter((value) => typeof value === "string" && /^P[1-9]\d*(M|Y)$/.test(value));
    },
    enabled: enabled && !!accountId && !!settledPlan && settledPlan === planCode.trim(),
    staleTime: 5 * 60_000,
    retry: false,
  });
  return { ...query, data: settledPlan === planCode.trim() ? query.data : undefined };
}
