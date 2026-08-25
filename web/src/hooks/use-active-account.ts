import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getActiveAccount, setActiveAccount } from "@/lib/api";

const EVT = "ovh-active-account-changed";

/**
 * 全站唯一的「当前账户」。
 *
 * 只在左侧菜单栏切换,其它任何地方都不再放账户选择器 —— 以前列表页、下单对话框、
 * 服务器控制页各有一个,彼此不同步,于是"用 A 账户浏览、用 B 账户下单"是一键就能
 * 做出来的组合。而 OVH 三个站点的目录互不相通(同一台机器欧区叫 24sk602、
 * 美区叫 24sk602-v1-us),这种组合必然下单失败。
 *
 * 切换时把所有依赖账户的查询全部作废,让它们按新账户重拉。
 */
export function useActiveAccount(): [string, (id: string) => void] {
  const qc = useQueryClient();
  const [accountId, setAccountId] = useState<string>(() => getActiveAccount());

  useEffect(() => {
    // 监听跨组件 / 跨标签页的账户变化
    const onChange = () => setAccountId(getActiveAccount());
    window.addEventListener(EVT, onChange);
    window.addEventListener("storage", onChange);
    return () => {
      window.removeEventListener(EVT, onChange);
      window.removeEventListener("storage", onChange);
    };
  }, []);

  const set = (id: string) => {
    if (id === accountId) return;
    setActiveAccount(id);
    setAccountId(id);
    // 账户变了,这些数据全都不再适用:
    // servers/availability 是按账户所在站点拉的目录与库存,
    // server-control/vps-control/account 是该账户名下的资源,
    // catalog 是按账户子公司计价的。
    for (const key of [
      ["servers"],
      ["availability"],
      ["server-control"],
      ["vps-control"],
      ["account"],
      ["accounts"],
      ["catalog"],
      ["ovh-catalog"],
    ]) {
      qc.invalidateQueries({ queryKey: key });
    }
  };
  return [accountId, set];
}

/** 兼容旧名字，避免一次性改动过大时漏掉调用点 */
export const useActiveServerControlAccount = useActiveAccount;
