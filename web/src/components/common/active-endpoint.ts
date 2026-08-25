import { useAccounts, useDefaultAccount, findAccountByID } from "@/hooks/use-accounts";
import { useActiveServerControlAccount } from "@/hooks/use-active-account";
import { endpointRegion, isUsEndpoint, type OvhRegion } from "@/lib/ovh-regions";

/**
 * 当前「服务器控制 / VPS 控制」视角所用的 OVH 账户及其 endpoint。
 *
 * 为什么组件需要知道它：OVHcloud US 的官方 schema 里压根没有备份FTP、也没有 NIC 联系人
 * 变更这两套端点。后端已经在入口挡下来了（返回 notAvailable / 400 中文提示），但按钮如果
 * 照常可点，用户仍会走一次注定失败的请求，然后拿到一个看起来像故障的错误。所以入口这一层
 * 也要按 endpoint 提前禁用并写明原因。
 *
 * 大区判定统一走 lib/ovh-regions（对齐后端 ovh.EndpointRegion），不要在组件里比
 * `endpoint === "ovh-us"`：endpoint 是自由字符串，还有 kimsufi-* / soyoustart-* 别名。
 *
 * activeId 为空表示「用默认账户」，这跟 lib/api 的请求拦截逻辑一致。
 */
export function useActiveAccountEndpoint(): {
  endpoint: string;
  zone: string;
  /** 账户所属大区 EU / US / CA，按 endpoint 推（与后端 ovh.EndpointRegion 同一套规则） */
  region: OvhRegion;
  /** 账户列表还没加载完时为 false —— 此时不要据此禁用任何入口，否则会闪一下假提示 */
  ready: boolean;
  /** 账户列表正在首次加载。区分「还没查到」和「查到了但一个账户都没有」 */
  loading: boolean;
  /** true = 美区账户，US 特有的能力缺失都以它为准 */
  isUS: boolean;
} {
  const [activeId] = useActiveServerControlAccount();
  const { data: accounts, isPending } = useAccounts();
  const fallback = useDefaultAccount();
  const acc = findAccountByID(accounts, activeId) || fallback;
  const endpoint = acc?.endpoint || "";
  return {
    endpoint,
    zone: acc?.zone || "",
    region: endpointRegion(endpoint),
    ready: !isPending && !!acc,
    loading: isPending,
    isUS: isUsEndpoint(endpoint),
  };
}
