import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";
import { zoneStyle } from "@/lib/zone-color";
import i18n from "@/i18n";
import { apiMessage } from "@/lib/api-error";

export interface OVHAccount {
  id: string;
  name: string;
  endpoint: string;
  zone: string;
  appKey: string;
  appSecret: string;
  consumerKey: string;
  iam: string;
  isDefault: boolean;
  createdAt: string;

  // ── 出站配置 ──
  // proxyUrl 是**打过码的**(密码换成 ***,主机端口保留)。只能用来显示,
  // 绝不能当成真值再提交回去 —— 那会把 "***" 当成密码存进库,代理从此连不上,
  // 而后端配了代理就不会退回直连,表现是这个账户在补货那一刻一单都下不出去。
  proxyUrl?: string;
  /** 出站指纹配置名,空 = default */
  fingerprint?: string;
}

export interface AccountInput {
  name: string;
  zone: string;
  endpoint?: string; // 可空, 后端按 zone 推
  appKey: string;
  appSecret: string;
  consumerKey: string;
  iam?: string;
  setDefault?: boolean;

  // 代理 / 指纹是**指针语义**,跟上面几个字段的"空 = 保持原值"不一样:
  //   字段不出现(undefined) = 不改
  //   ""                    = 清掉(改回直连 / 默认指纹)
  //   非空                  = 设成这个
  // 所以「输入框留空」必须翻译成"不传这个 key",不能翻译成空串 ——
  // 空串会把用户配好的代理悄悄清掉,而清掉代理的表现是一切正常、隔离却没了。
  // 反过来,清代理也只有发空串这一条路,界面上必须给一个明确的清除动作。
  // (注意别发 null:Go 那边 *string 收到 null 也是 nil,等于"不改"。)
  proxyUrl?: string;
  fingerprint?: string;
}

const ACCOUNTS_KEY = ["accounts", "list"] as const;

/**
 * 创建 / 更新 / 验证账户时后端带回来的子公司错配说明(空 = 没问题)。
 *
 * 后端(handlers.SubsidiaryMismatchNote)拿账户里存的 zone 跟 OVH /me 返回的 ovhSubsidiary 比:
 * zone 决定目录站点、价格币种和下单 region,ovhSubsidiary 才是 OVH 认的归属。
 * 两者不同区时凭据依然 valid=true,所有请求却都会打到错误的站点 —— 这段话是用户在
 * 真正下单失败之前唯一能看到的信号,必须原样显示,不能只吞成一句"验证通过"。
 */
export interface AccountVerifyResult {
  valid: boolean;
  subsidiaryWarning?: string;
}

/** 全部账户列表(默认账户排首位) */
export function useAccounts() {
  return useQuery({
    queryKey: ACCOUNTS_KEY,
    queryFn: async () => {
      const res = await api.get<{ accounts: OVHAccount[] }>("/accounts");
      return res.data.accounts || [];
    },
    staleTime: 5 * 60_000,
  });
}

/** 默认账户(取列表中 isDefault, 没有就第一个) */
export function useDefaultAccount(): OVHAccount | null {
  const q = useAccounts();
  const list = q.data || [];
  return list.find((a) => a.isDefault) || list[0] || null;
}

/** 创建账户。后端会自动调 /me 验证,返回 valid 字段 */
export function useCreateAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: AccountInput) => {
      const res = await api.post<{ account: OVHAccount } & AccountVerifyResult>("/accounts", input);
      return res.data;
    },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ACCOUNTS_KEY });
      // 新账户的出站配置要出现在代理健康面板里
      qc.invalidateQueries({ queryKey: qk.accounts.proxyStatus() });
      if (data.valid) {
        toast.success(i18n.t("hooksMsg.account.created", { name: data.account.name }));
      } else {
        toast.warning(i18n.t("hooksMsg.account.savedVerifyFailed"));
      }
      // 子公司填错不会让 valid 变 false,但会让目录/价格/下单全部走错站点,单独长时间提示
      if (data.subsidiaryWarning) {
        toast.warning(data.subsidiaryWarning, { duration: 15000 });
      }
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/** 更新账户 */
export function useUpdateAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, input }: { id: string; input: Partial<AccountInput> }) => {
      const res = await api.put<{ account: OVHAccount } & AccountVerifyResult>(`/accounts/${id}`, input);
      return res.data;
    },
    onSuccess: (data, vars) => {
      qc.invalidateQueries({ queryKey: ACCOUNTS_KEY });
      qc.invalidateQueries({ queryKey: qk.accounts.proxyStatus() });
      // 出站配置可能刚改过,上一次测到的出口 IP 就不再对应现在生效的配置了。
      // 留着它等于拿旧 IP 冒充新配置的结果 —— 而这个 IP 正是用户用来判断
      // "隔离到底生没生效"的唯一依据,宁可空着让他重测一次。
      qc.removeQueries({ queryKey: qk.accounts.proxyTest(vars.id) });
      // 链路检测同理:那份延迟数字是旧代理跑出来的,留着会让用户拿旧链路的成绩
      // 给新代理背书 —— 而他改代理的目的往往正是嫌慢。
      qc.removeQueries({ queryKey: qk.accounts.proxyCheck(vars.id) });
      toast.success(i18n.t("hooksMsg.account.updated"));
      if (!data.valid) {
        toast.warning(i18n.t("hooksMsg.account.savedVerifyFailed"));
      }
      if (data.subsidiaryWarning) {
        toast.warning(data.subsidiaryWarning, { duration: 15000 });
      }
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/** 删除账户(级联删除关联的 queue/history/sniper 记录) */
export function useDeleteAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => (await api.delete(`/accounts/${id}`)).data,
    onSuccess: (_d, id) => {
      qc.invalidateQueries({ queryKey: ACCOUNTS_KEY });
      qc.invalidateQueries({ queryKey: qk.accounts.proxyStatus() });
      qc.removeQueries({ queryKey: qk.accounts.proxyTest(id) });
      qc.removeQueries({ queryKey: qk.accounts.proxyCheck(id) });
      // 关联数据也变了,顺手 invalidate
      qc.invalidateQueries({ queryKey: ["queue"] });
      qc.invalidateQueries({ queryKey: ["history"] });
      toast.success(i18n.t("hooksMsg.account.deleted"));
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/** 把指定账户标为默认 */
export function useSetDefaultAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => (await api.post(`/accounts/${id}/set-default`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ACCOUNTS_KEY });
      toast.success(i18n.t("hooksMsg.account.setDefault"));
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/** 重新验证账户凭据(调 OVH /me) */
export function useVerifyAccount() {
  return useMutation({
    mutationFn: async (id: string) =>
      (await api.post<AccountVerifyResult>(`/accounts/${id}/verify`)).data,
    onSuccess: (data) => {
      if (data.valid) {
        toast.success(i18n.t("hooksMsg.account.verifyOk"));
      } else {
        toast.error(i18n.t("hooksMsg.account.verifyFailed"));
      }
      // 凭据有效 ≠ 区配对了。这条警告比"验证通过"重要得多,单独弹且停久一点
      if (data.subsidiaryWarning) {
        toast.warning(data.subsidiaryWarning, { duration: 15000 });
      }
    },
  });
}

/** 按 ID 查账户(从 useAccounts 缓存里找,不发请求) */
export function findAccountByID(accounts: OVHAccount[] | undefined, id: string): OVHAccount | undefined {
  if (!accounts || !id) return undefined;
  return accounts.find((a) => a.id === id);
}

/** zone 颜色映射,用于账户 chip 区分。
 *
 * 委托给 zone-color(按 API endpoint 分组:EUs蓝 / US 琥珀 / CA 绿),
 * 不再自己按国家另配一套 —— 之前这里按国家给 US=红、CA=绿、亚太=橙,
 * 和顶栏切换器的 endpoint 配色是同一概念的两种颜色,
 * 同一个账户在两处显示不一样,看着像两个东西。 */
export function accountChipColor(zone: string): string {
  return zoneStyle(zone).badge;
}

// ─── 出站代理 / 指纹 ────────────────────────────────────────────────────────

/**
 * proxy-status 里单个账户的出站配置与健康状况。
 *
 * tripped 比字面严重得多:代理连续失败到阈值后,后端会把这个账户**进行中的抢购任务
 * 全部暂停**、把订阅的自动下单关掉,而且代理修好之后也不会自动恢复。
 * 界面上不显眼地标出来,用户看到的现象只是"这个账户一直抢不到"。
 */
export interface AccountProxyStatus {
  id: string;
  name: string;
  zone: string;
  usingProxy: boolean;
  /** 打过码的代理地址;没配代理时为空 */
  proxy: string;
  fingerprint: string;
  /** 因为代理连续故障被停了 */
  tripped: boolean;
  /** 连续失败次数(窗口内) */
  fails: number;
  trippedAt?: string;
  lastFailAt?: string;
}

export interface ProxyStatusResult {
  success: boolean;
  /** 可选的指纹配置名。下拉选项只认这份清单,前端不写死 */
  profiles: string[];
  accounts: AccountProxyStatus[];
}

/**
 * 各账户的代理健康(30 秒一轮)。
 *
 * 顺带带回 profiles —— 指纹下拉的选项来源。写死一份迟早跟后端对不上:
 * 用户选了个后端不认的名字,后端会退回 default,而界面上还显示着他选的那个。
 */
export function useProxyStatus() {
  return useQuery<ProxyStatusResult>({
    queryKey: qk.accounts.proxyStatus(),
    queryFn: async () => (await api.get<ProxyStatusResult>("/accounts/proxy-status")).data,
    refetchInterval: 30_000,
  });
}

/** 出口 IP 测到了 */
export interface ProxyTestSuccess {
  success: true;
  egressIP: string;
  usingProxy: boolean;
  /** 打过码的代理地址,直连时为空 */
  proxy: string;
  fingerprint: string;
  /** 例如指纹名不认识、已按 default 处理 */
  warning?: string;
}

/** 没测到。失败时没有 IP,只有"经由哪条路失败的" —— 代理失败和直连失败的处理完全不同 */
export interface ProxyTestFailure {
  success: false;
  error: string;
  /** "直连" 或 "代理 xxx" */
  via: string;
  usingProxy: boolean;
  fingerprint: string;
}

export type ProxyTestResult = ProxyTestSuccess | ProxyTestFailure;

/** 缓存里存的那一份:多记一个测试时刻,免得几小时前的 IP 被当成刚测出来的 */
export type ProxyTestRecord =
  | (ProxyTestSuccess & { testedAt: number })
  | (ProxyTestFailure & { testedAt: number });

/**
 * 用账户**已保存**的出站配置查一次真实出口 IP。
 *
 * 这是用户唯一能确认隔离真的生效的手段:配置界面上看不出代理有没有生效,
 * 写错的代理、把流量透明转发回本机的代理,长相一模一样。
 * 唯一可靠的判据是把几个账户的出口 IP 摆在一起 —— 相同就说明没生效。
 *
 * 注意后端对"测试失败"也回 200(body 里 success=false),所以落进 mutation 的成功分支
 * 只代表请求通了。业务成败一律看 data.success,两者不能在界面上混成一件事。
 */
export function useProxyTest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) =>
      (await api.post<ProxyTestResult>(`/accounts/${id}/proxy-test`)).data,
    onSuccess: (data, id) => {
      const rec: ProxyTestRecord = { ...data, testedAt: Date.now() };
      qc.setQueryData(qk.accounts.proxyTest(id), rec);
      if (data.success) {
        toast.success(i18n.t("hooksMsg.account.egressOk", { ip: data.egressIP, via: data.usingProxy ? i18n.t("hooksMsg.account.viaProxy") : i18n.t("hooksMsg.account.viaDirect") }));
        if (data.warning) toast.warning(data.warning, { duration: 10000 });
      } else {
        // 配了代理就不会退回直连:这条失败等于该账户此刻一单都下不出去
        toast.error(i18n.t("hooksMsg.account.egressFailed", { via: data.via, error: data.error }));
      }
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/**
 * 读某个账户最近一次的出口测试结果。
 *
 * 这条 query 永远不会自己发请求(enabled: false),它只是 useProxyTest 写进缓存那份的读端 ——
 * 测试在编辑框里点,结果要同时显示在账户列表上,两处必须是同一份数据。
 */
export function useLastProxyTest(accountId: string) {
  return useQuery<ProxyTestRecord | null>({
    queryKey: qk.accounts.proxyTest(accountId),
    queryFn: async () => null,
    enabled: false,
    staleTime: Infinity,
  });
}

/**
 * 一次读多个账户最近一次的出口测试结果(同样不发请求)。
 *
 * 用来把几个账户的出口 IP 摆在一起看 —— 撞在同一个 IP 上就说明这几个账户
 * 在 OVH 眼里仍是同一个来源,限流照样互相拖累,配代理等于白配。
 */
export function useLastProxyTests(accountIds: string[]) {
  return useQueries({
    queries: accountIds.map((id) => ({
      queryKey: qk.accounts.proxyTest(id),
      queryFn: async () => null as ProxyTestRecord | null,
      enabled: false,
      staleTime: Infinity,
    })),
  });
}

// ─── 出站链路检测 ──────────────────────────────────────────────────────────

/**
 * 一个探测目标的检测结果。
 *
 * ok 的判据是**拿到了任何 HTTP 响应**,不是拿到 200(后端 netfp.ProbeTarget 就是这么判的):
 * 404 / 302 只说明那个路径不存在或要跳转,链路本身是通的。
 *
 * 反过来,ok 为 true 时 error 也可能非空 —— 那是 3 次采样里失败的那几次。
 * 既不是"通了"也不是"不通",是这条链路会偶发抽风,必须单独说出来。
 */
export interface ProxyProbeTarget {
  name: string;
  url: string;
  ok: boolean;
  /** 拿到的 HTTP 状态码 */
  status?: number;
  /** 3 次采样的最小 / 平均耗时(毫秒) */
  minMs?: number;
  avgMs?: number;
  error?: string;
}

/** 检测跑完了。注意这不代表每个目标都通 —— 每个目标的成败各看自己的 ok */
export interface ProxyCheckSuccess {
  success: true;
  accountId: string;
  accountName: string;
  /** 这个账户真正会打的那个大区(EU / US / CA),后端按 endpoint 推 */
  region: string;
  usingProxy: boolean;
  /** 打过码的代理地址,直连时为空 */
  proxy: string;
  fingerprint: string;
  egressIP?: string;
  /** 查不到出口 IP 的原因。它和 egressIP 只会出现一个 */
  egressError?: string;
  /** 后端测完的时刻(RFC3339) */
  checkedAt: string;
  /** 例如指纹名不认识、已按 default 处理 */
  warning?: string;
  targets: ProxyProbeTarget[];
}

/** 检测没做成(账户不存在之类)。后端这种情况也回 200,只是 success=false */
export interface ProxyCheckFailure {
  success: false;
  error: string;
}

export type ProxyCheckResult = ProxyCheckSuccess | ProxyCheckFailure;

/** 缓存里存的那一份:多记一个收到的时刻,免得几小时前的结果被当成刚测出来的 */
export type ProxyCheckRecord =
  | (ProxyCheckSuccess & { receivedAt: number })
  | (ProxyCheckFailure & { receivedAt: number });

/** 这份结果是什么时候的:后端给的检测时刻优先,没有(检测本身失败)就用收到的时刻 */
export function proxyCheckTime(rec: ProxyCheckRecord): Date {
  if (rec.success && rec.checkedAt) {
    const d = new Date(rec.checkedAt);
    if (!isNaN(d.getTime())) return d;
  }
  return new Date(rec.receivedAt);
}

/** 延迟档位。抢购对延迟直接敏感,数字必须配一句结论 —— 只给数字用户没法判断该不该换代理 */
export type LatencyLevel = "fast" | "slow" | "bad";

/**
 * 按最小耗时分档。
 *
 * 用 min 不用 avg:min 是这条链路的**最好情况**。连最好情况都超过 800ms,
 * 补货那一刻就没什么指望了 —— 别人 200ms 打完三个来回,这边第一个请求还在路上。
 */
export function gradeLatency(minMs: number): { level: LatencyLevel; note: string } {
  if (minMs < 300) {
    return { level: "fast", note: i18n.t("hooksMsg.account.latFast") };
  }
  if (minMs <= 800) {
    return { level: "slow", note: i18n.t("hooksMsg.account.latSlow") };
  }
  return {
    level: "bad",
    note: i18n.t("hooksMsg.account.latVerySlow"),
  };
}

/**
 * 抖动:平均比最小大一倍以上。
 *
 * 这条比平均值本身更要紧 —— 抖动大的链路平时看着挺快,会不定时地慢一拍,
 * 而那一拍就决定抢不抢得到。
 */
export function isJittery(t: ProxyProbeTarget): boolean {
  return !!(t.ok && t.minMs && t.avgMs && t.avgMs > t.minMs * 2);
}

/**
 * 一次检测里最慢的那条(只看通了的)。
 * 一条都没通就是 undefined —— 那是"不通",跟"慢"是两回事,不能混成一个结论。
 */
export function worstMinMs(targets: ProxyProbeTarget[]): number | undefined {
  const xs = targets
    .filter((t) => t.ok && typeof t.minMs === "number")
    .map((t) => t.minMs as number);
  return xs.length ? Math.max(...xs) : undefined;
}

/**
 * 给一个账户做一次出站链路体检:出口 IP + 到 OVH 各目标的连通性与延迟。
 *
 * 跟「测试出口 IP」不是一回事:那个只回答"我从哪个 IP 出去",
 * 这个回答"从这个账户打到 OVH 要多久" —— 抢购的输赢就在这几百毫秒上。
 * 每个目标真打 3 次,所以要几秒,界面上必须看得出在跑。
 *
 * 同样注意后端对"检测没做成"也回 200(body 里 success=false):
 * 落进 mutation 的成功分支只代表请求通了,业务成败一律看 data.success。
 */
export function useProxyCheck() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) =>
      (await api.post<ProxyCheckResult>(`/accounts/${id}/proxy-check`)).data,
    onSuccess: (data, id) => {
      const rec: ProxyCheckRecord = { ...data, receivedAt: Date.now() };
      qc.setQueryData(qk.accounts.proxyCheck(id), rec);
      if (!data.success) {
        toast.error(i18n.t("hooksMsg.account.probeFailed", { error: data.error }));
        return;
      }
      if (data.warning) toast.warning(data.warning, { duration: 10000 });
      const down = data.targets.filter((t) => !t.ok);
      if (down.length > 0) {
        // 目标不通 ≠ 慢,这是"此刻下不了单",优先级高于任何延迟结论
        toast.error(i18n.t("hooksMsg.account.probeDown", { down: down.length, total: data.targets.length }));
        return;
      }
      const worst = worstMinMs(data.targets);
      if (worst === undefined) {
        toast.warning(i18n.t("hooksMsg.account.probeNoLatency"));
        return;
      }
      const g = gradeLatency(worst);
      const head = i18n.t("hooksMsg.account.probeDone", { worst });
      if (g.level === "bad") toast.error(`${head} —— ${g.note}`, { duration: 10000 });
      else if (g.level === "slow") toast.warning(`${head} —— ${g.note}`, { duration: 8000 });
      else toast.success(head);
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/**
 * 读某个账户最近一次的链路检测结果。
 *
 * 同 useLastProxyTest:永远不自己发请求(enabled: false),只是 useProxyCheck 写进缓存那份的读端。
 * 检测要几秒、还会真的去打 OVH,所以关掉弹窗再打开显示的是上次那份 + 那次的时间,不自动重测。
 */
export function useLastProxyCheck(accountId: string) {
  return useQuery<ProxyCheckRecord | null>({
    queryKey: qk.accounts.proxyCheck(accountId),
    queryFn: async () => null,
    enabled: false,
    staleTime: Infinity,
  });
}