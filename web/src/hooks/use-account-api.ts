import { useMemo } from "react";
import { createAccountApi } from "@/lib/api";

export function useAccountApi(accountId: string) {
  return useMemo(() => createAccountApi(accountId), [accountId]);
}
