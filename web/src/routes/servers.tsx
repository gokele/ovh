import { createFileRoute } from "@tanstack/react-router";
import {
  Server, RefreshCw, Search, Bell, ShoppingCart, Cpu, MemoryStick, HardDrive, Wifi,
  Filter, MapPin, User, Globe } from "lucide-react";
import { useMemo, useState } from "react";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Chip } from "@/components/common/Chip";
import { StatusDot } from "@/components/common/StatusDot";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { useServers, useAddToMonitor, type ServerPlan } from "@/hooks/use-servers";
import { useAccountInfo } from "@/hooks/use-account";
import { useCreateQueueItem } from "@/hooks/use-queue";
import { useCacheInfo } from "@/hooks/use-settings";
import { useDefaultAccount } from "@/hooks/use-accounts";
import { useActiveAccount } from "@/hooks/use-active-account";
import { useEffect } from "react";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
import {
  useAvailability,
  buildAvailabilityMap,
  buildVariantIndex,
  variantsForPlan,
  variantDcStatus,
  hasStockWithOption,
  useOvhCatalog,
  buildCatalogIndex,
  computePriceFromOptions,
  formatPrice,
  type AvailabilityItem,
  type CatalogIndex,
  type PriceInfo,
} from "@/hooks/use-availability";
import { groupOptions, type OptionGroupKey } from "@/lib/option-groups";
import { OptionGroupSection } from "@/components/common/OptionGroupSection";
import { lookupDcStatus, datacentersForPlan } from "@/lib/datacenters";
import { OVH_SUBSIDIARIES } from "@/lib/ovh-subsidiaries";
import { formatMoney, CURRENCY_UNKNOWN_HINT } from "@/lib/money";
import { useAccounts, findAccountByID } from "@/hooks/use-accounts";
import { endpointRegion, regionLabel } from "@/lib/ovh-regions";

/** 服务器列表：卡片网格 + 详情弹窗 */
export const Route = createFileRoute("/servers")({
  component: ServersPage,
});



/**
 * 没有价格时显示什么。
 *
 * 以前一律显示"价格加载中" —— 但目录早就加载完了,只是这个 planCode 不在当前子公司的
 * 目录里(三区目录互不相通,机型代码都不一样)。用户盯着一个永远转不完的"加载中",
 * 完全不知道发生了什么。现在把两种情况分开说。
 */
function PriceFallback({ loading, subsidiary }: { loading: boolean; subsidiary: string }) {
  if (loading) {
    return <span className="text-muted-foreground font-normal">— · 价格加载中</span>;
  }
  return (
    <span
      className="text-muted-foreground font-normal"
      title={`${subsidiary} 的目录里没有这个机型的报价。OVH 各子公司目录独立,机型代码也不同。`}
    >
      — · {subsidiary} 无报价
    </span>
  );
}

function ServersPage() {
  const q = useServers();
  // 单次拉取 OVH 公开可用性接口（一条请求拿到所有 planCode × 所有 DC 的状态）
  const availQ = useAvailability();
  const availMap = useMemo(() => buildAvailabilityMap(availQ.data), [availQ.data]);
  // FQN 级索引,抢购对话框按当前选配实时算 DC 可用 + option 绿红点
  const variantIndex = useMemo(() => buildVariantIndex(availQ.data), [availQ.data]);

  // OVH 账户信息：拿 ovhSubsidiary 作为默认价格地区
  const account = useAccountInfo();
  const accountSub = account.data?.info.ovhSubsidiary;

  // 计价子公司必须跟**机型列表是按哪个子公司拉的**保持一致。
  // 后端 /api/servers 用的是账户配置里的 zone(catalog.SubsidiaryOfAccount),
  // 所以这里也用 zone —— 不能用 OVH /me 返回的 ovhSubsidiary:
  // 两者不一致时(账户 zone 填错,后端会回 X-Subsidiary-Mismatch 警告),
  // 列表里是 A 子公司的 planCode、价格却去查 B 子公司的目录,
  // 查不到就一直显示"价格加载中",而用户根本看不出是配置错了。
  const pageAccounts = useAccounts();
  const [activeAccountId] = useActiveAccount();
  const activeAcc = findAccountByID(pageAccounts.data, activeAccountId);
  const subsidiary = (activeAcc?.zone || accountSub || "IE").toUpperCase();

  // 单次拉取所选 subsidiary 的目录算价格（base plan + addon family 月费累加）
  const catalogQ = useOvhCatalog(subsidiary);
  const catalogIdx = useMemo(() => buildCatalogIndex(catalogQ.data), [catalogQ.data]);
  // 卡片显示价格用每台服务器的 catalog defaultOptions 算,跟详情对话框打开时的初始价格一致。
  // 旧的 buildPriceMap 走 FQN 维度前缀匹配,跟 catalog 默认值可能挑到不同 addon → 卡片价跟弹窗价对不上。
  const priceMap = useMemo(() => {
    const out: Record<string, PriceInfo> = {};
    for (const srv of q.data || []) {
      const defaults = (srv.defaultOptions || []).map((o) => o.value).filter(Boolean);
      const p = computePriceFromOptions(srv.planCode, defaults, catalogIdx);
      if (p) out[srv.planCode] = p;
    }
    return out;
  }, [q.data, catalogIdx]);

  const [search, setSearch] = useState("");
  const [onlyAvailable, setOnlyAvailable] = useState(false);
  const [detailPlanCode, setDetailPlanCode] = useState<string | null>(null);

  const list = q.data || [];
  const filtered = useMemo(() => {
    const s = search.trim().toLowerCase();
    let out = list;
    if (s) {
      out = out.filter((srv) =>
        `${srv.planCode} ${srv.name} ${srv.cpu} ${srv.memory} ${srv.storage}`.toLowerCase().includes(s)
      );
    }
    if (onlyAvailable) {
      out = out.filter((srv) => {
        const map = availMap[srv.planCode];
        if (map) {
          // 实时数据：任一 DC 可用即视为可用
          return Object.values(map).some((v) => v && v !== "unavailable" && v !== "unknown");
        }
        // 实时还没到：用目录里的静态字段兜底
        return srv.datacenters.some((dc) => dc.availability && dc.availability !== "unavailable" && dc.availability !== "unknown");
      });
    }
    return out;
  }, [list, search, onlyAvailable, availMap]);

  const detailServer = detailPlanCode ? list.find((s) => s.planCode === detailPlanCode) || null : null;

  return (
    <div className="space-y-6">
      <PageHeader
        icon={Server}
        title="服务器列表"
        description="目录、价格、可用性全部走访问触发的缓存，2 小时内复用"
        action={
          <div className="flex items-center gap-2">
            <CacheBadge />
            <Button
              variant="outline"
              onClick={() => {
                // 一键刷三件套：目录强刷（清后端缓存）、catalog（价格）refetch、可用性 refetch
                q.forceRefresh();
                catalogQ.refetch();
                availQ.refetch();
              }}
              // 只看手动刷新状态：q.isRefreshing 是 forceRefresh 期间的 mutation pending；
              // *Q.isRefetching 是 refetch 后的状态。不引入 isFetching/isLoading，
              // 这样首次加载的菊花不会显示在这个按钮上，避免误导。
              disabled={q.isRefreshing || catalogQ.isRefetching || availQ.isRefetching}
            >
              <RefreshCw
                className={`w-4 h-4 ${
                  q.isRefreshing || catalogQ.isRefetching || availQ.isRefetching
                    ? "animate-spin"
                    : ""
                }`}
              />
              刷新
            </Button>
          </div>
        }
      />

      {/* 工具条 */}
      <Card>
        <CardContent className="p-4 flex flex-col sm:flex-row sm:items-center gap-3">
          <div className="relative flex-1 min-w-0">
            <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
            <Input
              placeholder="搜索 planCode / 型号 / CPU / 内存..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9 rounded-full"
            />
          </div>
          <Button
            variant={onlyAvailable ? "default" : "outline"}
            size="sm"
            className="rounded-full"
            onClick={() => setOnlyAvailable((v) => !v)}
          >
            <Filter className="w-3.5 h-3.5" />
            仅显示可用
          </Button>
          {/* 价格地区不再单独选:它以前只换价格、不换机型列表,而下拉里写着「US · 美国」,
              看上去像是切到了美区目录 —— 实际列表还是欧区的 planCode(24sk602 vs 美区的
              24sk602-v1-us),照着它下单必然被拒。现在币种直接跟当前账户的子公司走。 */}
          <span className="inline-flex items-center gap-1.5 h-9 px-3 rounded-full border border-border text-[12px] text-muted-foreground">
            <Globe className="w-3.5 h-3.5" />
            价格按 <b className="text-foreground font-semibold">{subsidiary}</b> 结算
          </span>
          <span className="text-[12px] text-muted-foreground whitespace-nowrap">
            {q.isPending ? "加载中..." : `共 ${filtered.length} 款`}
          </span>
        </CardContent>
      </Card>

      {/* 后端 /me 响应上的 X-Subsidiary-Mismatch:账户里配的 zone 跟 OVH 认的 ovhSubsidiary 不一致。
          这一页的机型集合、价格、库存全部由子公司决定,配错了看到的整页都是别的区的东西,
          而且要到下单那一刻才会失败,所以在这里就得说清楚。 */}
      {account.data?.subsidiaryMismatch && (
        <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[12px]">
          <MapPin className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
          <div>
            <p className="font-semibold">账户子公司配置与 OVH 实际归属不一致</p>
            <p className="text-muted-foreground mt-0.5">
              OVH 认这个账户属于 <code className="font-mono">{accountSub || "—"}</code>，
              与设置页里填的 zone 不同。目录、价格、币种、库存、下单 region 全按子公司走，
              请到「设置 → OVH 账户」改成 {accountSub || "OVH 返回的那个子公司"} 后再下单。
            </p>
          </div>
        </div>
      )}

      {/* 网格 */}
      {q.isPending ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-[260px] rounded-2xl" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <Card>
          <EmptyState
            icon={Server}
            title="未找到服务器"
            description={list.length === 0 ? "API 未返回服务器，检查 API 设置" : "没有匹配的搜索结果"}
          />
        </Card>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
          {filtered.map((srv) => (
            <ServerCard
              key={srv.planCode}
              server={srv}
              realtimeDcMap={availMap[srv.planCode]}
              price={priceMap[srv.planCode]}
              priceLoading={catalogQ.isPending}
              subsidiary={subsidiary}
              onView={() => setDetailPlanCode(srv.planCode)}
            />
          ))}
        </div>
      )}

      {/* 详情弹窗 */}
      <Dialog open={!!detailServer} onOpenChange={(v) => !v && setDetailPlanCode(null)}>
        <DialogContent className="w-[95vw] sm:w-full sm:max-w-3xl max-h-[90vh] overflow-hidden flex flex-col">
          {detailServer ? (
            <DetailContent
              server={detailServer}
              realtimeDcMap={availMap[detailServer.planCode]}
              variants={variantIndex[detailServer.planCode]}
              defaultPrice={priceMap[detailServer.planCode]}
              priceLoading={catalogQ.isPending}
              catalogIdx={catalogIdx}
              subsidiary={subsidiary}
              onClose={() => setDetailPlanCode(null)}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** 服务器卡片 */
function ServerCard({
  server,
  realtimeDcMap,
  price,
  priceLoading,
  subsidiary,
  onView,
}: {
  server: ServerPlan;
  realtimeDcMap?: Record<string, string>;
  price?: PriceInfo;
  /** 目录还在拉 → 显示"加载中";已拉完仍无价 → 显示"该子公司无报价" */
  priceLoading: boolean;
  subsidiary: string;
  onView: () => void;
}) {
  const addMon = useAddToMonitor();

  // 静态可用性兜底（首次渲染、实时还没回来时也有数据）
  const staticDcMap = useMemo(() => {
    const m: Record<string, string> = {};
    for (const d of server.datacenters || []) {
      m[d.datacenter.toLowerCase()] = d.availability;
    }
    return m;
  }, [server.datacenters]);

  // 实时覆盖静态：页面级单次 OVH 接口拿到的状态优先生效
  const dcMap = useMemo(() => ({ ...staticDcMap, ...(realtimeDcMap || {}) }), [staticDcMap, realtimeDcMap]);

  // 只有两态：明确可用 → 绿；其它一律视为缺货（红）
  // 只列这台机器在当前账户站点真正可选的机房 —— 欧区机型不该出现 HIL/VIN。
  // dcMap 已经是"静态目录 + 实时可用性"合并后的该机型机房集合。
  const planDCs = useMemo(
    () => datacentersForPlan({ [server.planCode]: dcMap }, server.planCode),
    [dcMap, server.planCode]
  );
  const dcStatuses = planDCs.map((dc) => {
    const status = lookupDcStatus(dcMap, dc);
    const isOk = !!status && status !== "unavailable" && status !== "unknown";
    return { dc, isOk };
  });
  const total = dcStatuses.length;
  const okCount = dcStatuses.filter((s) => s.isOk).length;

  const tone = okCount > 0 ? "success" : "danger";
  const statusText = okCount > 0 ? `${okCount}/${total} 可用` : "暂时缺货";

  return (
    <Card className="overflow-hidden transition-colors hover:bg-secondary/30">
      <CardContent className="p-5 flex flex-col gap-4">
        {/* 头部：planCode + 状态 chip */}
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <h3 className="font-mono text-[15px] font-semibold truncate">{server.planCode}</h3>
            <p className="text-[12px] text-muted-foreground truncate mt-0.5">{server.name}</p>
            <div className="text-[13px] font-semibold mt-1 tabular-nums">
              {price ? (
                formatPrice(price)
              ) : (
                <PriceFallback loading={priceLoading} subsidiary={subsidiary} />
              )}
            </div>
          </div>
          <Chip tone={tone as any}>
            {okCount > 0 ? (
              <StatusDot tone="success" pulse size="xs" />
            ) : (
              <StatusDot tone="danger" size="xs" />
            )}
            {statusText}
          </Chip>
        </div>

        {/* 规格 2x2 */}
        <div className="grid grid-cols-2 gap-2 text-[12px]">
          <SpecRow icon={<Cpu className="w-3.5 h-3.5" />} text={server.cpu} />
          <SpecRow icon={<MemoryStick className="w-3.5 h-3.5" />} text={server.memory} />
          <SpecRow icon={<HardDrive className="w-3.5 h-3.5" />} text={server.storage} />
          <SpecRow icon={<Wifi className="w-3.5 h-3.5" />} text={server.bandwidth} />
        </div>

        {/* DC 点阵：12 个标准 OVH DC，只两态 — 绿色有货 / 红色缺货 */}
        <div className="flex flex-wrap items-center gap-1.5 py-1">
          {dcStatuses.map(({ dc, isOk }) => (
            <span
              key={dc.code}
              title={`${dc.name} · ${dc.region}`}
              className="inline-flex items-center gap-1 px-1.5 h-5 rounded-full border border-border text-[10px] font-mono"
            >
              <StatusDot tone={isOk ? "success" : "danger"} size="xs" pulse={isOk} />
              {dc.code.toUpperCase()}
            </span>
          ))}
        </div>

        {/* 操作按钮 */}
        <div className="flex items-center gap-2 pt-1">
          <Button
            variant="outline"
            size="sm"
            className="flex-1"
            disabled={addMon.isPending}
            onClick={() =>
              addMon.mutate({
                planCode: server.planCode,
                datacenters: planDCs.map((dc) => dc.code),
                serverName: server.name,
              })
            }
          >
            <Bell className="w-3.5 h-3.5" />
            监控
          </Button>
          <Button size="sm" className="flex-1" onClick={onView}>
            <ShoppingCart className="w-3.5 h-3.5" />
            抢购
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

/** 单行规格（icon + 文本） */
function SpecRow({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <div className="flex items-center gap-1.5 min-w-0 text-foreground/80">
      <span className="text-muted-foreground flex-shrink-0">{icon}</span>
      <span className="truncate" title={text}>{text}</span>
    </div>
  );
}

/** 详情弹窗内容 */
function DetailContent({
  server,
  realtimeDcMap,
  variants,
  defaultPrice,
  priceLoading,
  catalogIdx,
  subsidiary,
  onClose,
}: {
  server: ServerPlan;
  realtimeDcMap?: Record<string, string>;
  /** 此 planCode 在 OVH availability 接口里的所有 FQN 变体 */
  variants?: AvailabilityItem[];
  /** 用默认配置算出的代表价，作为用户尚未变动时的兜底显示 */
  defaultPrice?: PriceInfo;
  priceLoading: boolean;
  /** 目录索引：用户切配置时实时算价用 */
  catalogIdx: CatalogIndex;
  /** 仅用于价格展示的 subsidiary（顶部下拉决定）。实际下单 subsidiary 由后端 cfg.Zone 决定，在设置页改 */
  subsidiary: string;
  onClose: () => void;
}) {
  const addMon = useAddToMonitor();
  const create = useCreateQueueItem();
  const defaultAcc = useDefaultAccount();
  const { data: accounts } = useAccounts();

  // 下单账户 = 左侧菜单栏选的那个全局账户,这里不再单独选。
  // 拿不到时退回默认账户,避免刚装好还没选就点不了下单。
  const [globalAccountId] = useActiveAccount();
  const accountId = globalAccountId || defaultAcc?.id || "";
  const activeAccount = findAccountByID(accounts, accountId);

  // 这里选的下单账户可能跟页面顶部的活跃账户不是同一个区。
  // 库存必须按"实际下单的那个账户"的站点查:EU/US/CA 三站库存互不相通
  // (实测 US 站点有 423 个 planCode,只有 134 个与 EU 重合,还多 vin/hil 两个机房),
  // 拿页面级(活跃账户)的库存去点红绿点,用户会照着别区的货去下单。
  const orderEndpoint = findAccountByID(accounts, accountId)?.endpoint || "";
  const orderAvail = useAvailability(orderEndpoint || undefined);
  // 下单账户那一份还没到手时,先用页面级传进来的变体兜底显示(不空窗)
  const orderVariants = useMemo(
    () => (orderEndpoint ? variantsForPlan(orderAvail.data, server.planCode) ?? variants : variants),
    [orderEndpoint, orderAvail.data, server.planCode, variants]
  );
  // plan 级机房状态同样改用下单账户那一份(空 picks = 聚合所有变体,与页面卡片口径一致)
  const orderDcMap = useMemo(
    () => (orderVariants ? variantDcStatus(orderVariants, []) : realtimeDcMap),
    [orderVariants, realtimeDcMap]
  );
  const [selectedDCs, setSelectedDCs] = useState<string[]>([]);
  const [quantity, setQuantity] = useState("1");
  const [retryInterval, setRetryInterval] = useState("60");
  const toggleDC = (code: string) =>
    setSelectedDCs((prev) => (prev.includes(code) ? prev.filter((c) => c !== code) : [...prev, code]));
  const qty = Math.max(1, Number(quantity) || 1);
  const totalTasks = selectedDCs.length * qty;
  // 静态可用性兜底：实时还没返回时也能看到目录里的初始数据
  const staticDcMap = useMemo(() => {
    const m: Record<string, string> = {};
    for (const d of server.datacenters || []) m[d.datacenter.toLowerCase()] = d.availability;
    return m;
  }, [server.datacenters]);
  // 实时覆盖静态:plan 级聚合,无 variants 数据时兜底
  const aggregateDcMap = useMemo(() => ({ ...staticDcMap, ...(orderDcMap || {}) }), [staticDcMap, orderDcMap]);

  // 按组拆分可选配置 + 默认值集合
  const grouped = useMemo(() => groupOptions(server.availableOptions), [server.availableOptions]);
  const defaultValueSet = useMemo(
    () => new Set((server.defaultOptions || []).map((o) => o.value)),
    [server.defaultOptions]
  );

  // 各组的当前选中值（按 group key 索引）。默认从 catalog 的 defaultOptions 里取该组里命中的那个 value。
  // 用户切配置后,每个 option chip / 每个 DC 的红绿点会实时反映"这套组合是否有 DC 有货",
  // 用户看到红就自己换 —— 不替用户自动改默认值。
  const initialPicked = useMemo(() => {
    const out: Partial<Record<OptionGroupKey, string>> = {};
    (Object.keys(grouped) as OptionGroupKey[]).forEach((g) => {
      const list = grouped[g];
      if (list.length === 0) return;
      const def = list.find((o) => defaultValueSet.has(o.value));
      if (def) out[g] = def.value;
    });
    return out;
  }, [grouped, defaultValueSet]);
  const [picked, setPicked] = useState<Partial<Record<OptionGroupKey, string>>>(initialPicked);

  // DC 红绿:看"该 DC 在任何 FQN 里有货否",跟卡片外面口径一致。
  // 用户看 DC 红绿决定去哪个机房,看 option chip 红绿决定换什么配置。
  // 当前完整选配 vs 实际可下单的精确校验放到提交按钮那一步处理(待加)。
  const dcMap = aggregateDcMap;
  // 该机型在当前账户站点真正可选的机房。写死 16 个会让欧区机型列出 HIL/VIN 这种
  // 永远买不到的机房,美区账户同样会看到一堆自己订不了的欧洲机房。
  const dialogDCs = useMemo(
    () => datacentersForPlan({ [server.planCode]: dcMap }, server.planCode),
    [dcMap, server.planCode]
  );

  const total = dialogDCs.length;
  const ok = dialogDCs.filter((dc) => {
    const status = lookupDcStatus(dcMap, dc);
    return !!status && status !== "unavailable" && status !== "unknown";
  }).length;
  const ratio = total > 0 ? ok / total : 0;

  // option chip 上的有货预判。
  // OVH availability FQN 只包含 planCode.memory.storage[.systemStorage] 三段,
  // 带宽 / vRack / CPU / other 这些 addon 不在 FQN 里 → 它们的库存跟主机解耦,
  // 主机有货就总能加购,这些组固定绿,不参与 FQN 匹配。
  const optionHasStock = (groupKey: OptionGroupKey, value: string): boolean => {
    if (groupKey === "bandwidth" || groupKey === "vrack" || groupKey === "cpu" || groupKey === "other") {
      return true;
    }
    return hasStockWithOption(orderVariants, picked as Record<string, string>, groupKey, value);
  };

  // 用户选中的所有 option value（非默认值才计入，让 Queue 表单只填差异化部分；
  // 但保险起见全量传过去，让后端忽略相同默认值即可）
  const selectedValues = useMemo(
    () => (Object.values(picked).filter(Boolean) as string[]),
    [picked]
  );

  // 价格用的 subsidiary(顶部下拉)属于哪个站点 vs 下单账户属于哪个站点
  const priceRegion = endpointRegion(
    OVH_SUBSIDIARIES.find((s) => s.code === subsidiary)?.endpoint || "ovh-eu"
  );
  const orderRegion = endpointRegion(orderEndpoint);
  const priceRegionMismatch = !!orderEndpoint && priceRegion !== orderRegion;

  // 跟随选配实时算价：base plan + 选中的各 addon 月费
  const price = useMemo(() => {
    if (selectedValues.length === 0) return defaultPrice;
    return computePriceFromOptions(server.planCode, selectedValues, catalogIdx) || defaultPrice;
  }, [server.planCode, selectedValues, catalogIdx, defaultPrice]);

  return (
    <>
      <DialogHeader>
        <div className="flex items-start justify-between gap-3 pr-6">
          <div className="min-w-0">
            <DialogTitle className="font-mono text-xl truncate">{server.planCode}</DialogTitle>
            <DialogDescription className="truncate mt-0.5">{server.name}</DialogDescription>
          </div>
          {ok > 0 ? (
            <Chip tone="success"><StatusDot tone="success" pulse size="xs" />当前可用</Chip>
          ) : (
            <Chip tone="danger"><StatusDot tone="danger" size="xs" />暂时缺货</Chip>
          )}
        </div>
      </DialogHeader>

      <div className="overflow-y-auto -mx-6 px-6 space-y-6 flex-1">
        {/* 价格 Hero（随下方配置实时变化） */}
        <div className="border border-border rounded-2xl p-4 bg-secondary/30 flex items-end justify-between gap-3 flex-wrap">
          <div>
            <div className="text-[11px] text-muted-foreground">
              月费 · {subsidiary}
              <span className="ml-2 text-[10px]">
                {selectedValues.length > 0 ? "（随当前选配）" : "（默认配置）"}
              </span>
            </div>
            <div className="text-2xl font-bold tabular-nums mt-0.5">
              {price ? (
                formatPrice(price)
              ) : (
                <span className="text-base">
                  <PriceFallback loading={priceLoading} subsidiary={subsidiary} />
                </span>
              )}
            </div>
          </div>
          {price && (
            <div className="text-right text-[11px] text-muted-foreground space-y-0.5 tabular-nums">
              {price.installPrice > 0 && (
                <div>安装费 {formatMoney(price.installPrice, price.currency)}（一次性）</div>
              )}
              {/* 币种取目录 locale.currencyCode 原值;拿不到就明说未知,不写死 EUR */}
              <div title={price.currency ? undefined : CURRENCY_UNKNOWN_HINT}>
                币种 {price.currency || "未知"}
              </div>
            </div>
          )}
        </div>

        {/* 规格 4 卡 */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2.5 sm:gap-3">
          <SpecCard icon={<Cpu className="w-4 h-4" />} label="CPU" value={server.cpu} />
          <SpecCard icon={<MemoryStick className="w-4 h-4" />} label="内存" value={server.memory} />
          <SpecCard icon={<HardDrive className="w-4 h-4" />} label="硬盘" value={server.storage} />
          <SpecCard icon={<Wifi className="w-4 h-4" />} label="带宽" value={server.bandwidth} />
        </div>

        {/* 硬件配置选择 */}
        {(["cpu", "memory", "systemStorage", "storage", "bandwidth", "vrack", "other"] as OptionGroupKey[])
          .filter((g) => grouped[g].length > 0)
          .map((g) => (
            <OptionGroupSection
              key={g}
              groupKey={g}
              options={grouped[g]}
              picked={picked[g] || ""}
              defaultValueSet={defaultValueSet}
              hasStock={variants && variants.length > 0 ? (value) => optionHasStock(g, value) : undefined}
              onPick={(value) => setPicked((p) => ({ ...p, [g]: value }))}
            />
          ))}

        {/* DC 多选（点击切换） + 全选/反选 */}
        <div>
          <div className="flex items-center justify-between mb-2.5 gap-2 flex-wrap">
            <h3 className="text-[13px] font-semibold flex items-center gap-1.5">
              <MapPin className="w-3.5 h-3.5 text-muted-foreground" />
              数据中心 · 选 {selectedDCs.length} / {dialogDCs.length}
            </h3>
            <div className="flex items-center gap-2">
              <span className="text-[11px] text-muted-foreground">
                {`${ok}/${total} 可用 · ${Math.round(ratio * 100)}%`}
              </span>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-[11px]"
                onClick={() => {
                  // 全选可用的；都满了就清空
                  const okCodes = dialogDCs
                    .filter((dc) => {
                      const s = lookupDcStatus(dcMap, dc);
                      return !!s && s !== "unavailable" && s !== "unknown";
                    })
                    .map((dc) => dc.code);
                  setSelectedDCs(selectedDCs.length === okCodes.length ? [] : okCodes);
                }}
                title="一键选中所有可用 DC，再点一次清空"
              >
                {selectedDCs.length > 0 ? "清空" : "选可用"}
              </Button>
            </div>
          </div>
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-1.5 sm:gap-2">
            {dialogDCs.map((dc) => {
              const status = lookupDcStatus(dcMap, dc);
              const isOk = !!status && status !== "unavailable" && status !== "unknown";
              const isSelected = selectedDCs.includes(dc.code);
              return (
                <button
                  key={dc.code}
                  type="button"
                  onClick={() => toggleDC(dc.code)}
                  className={
                    "text-left border rounded-xl px-3 py-2 flex items-center justify-between transition-colors " +
                    (isSelected
                      ? "border-foreground bg-foreground text-background"
                      : "border-border hover:bg-secondary/50")
                  }
                >
                  <div className="min-w-0">
                    <div className="text-[12px] font-bold font-mono">{dc.code.toUpperCase()}</div>
                    <div className={"text-[10px] truncate " + (isSelected ? "text-background/70" : "text-muted-foreground")}>
                      {dc.region} · {dc.name}
                    </div>
                  </div>
                  <StatusDot tone={isOk ? "success" : "danger"} size="sm" pulse={isOk && !isSelected} />
                </button>
              );
            })}
          </div>
        </div>

        {/* 抢购参数：账户 / 数量 / 重试间隔 */}
        <div className="border-t border-border pt-4">
          <h3 className="text-[13px] font-semibold mb-2.5 flex items-center gap-1.5">
            <ShoppingCart className="w-3.5 h-3.5 text-muted-foreground" />
            抢购参数
          </h3>
          <div className="space-y-3">
            <div>
              <label className="block text-[11px] text-muted-foreground mb-1">OVH 账户 *</label>
              {/* 账户只在左侧菜单栏切,这里只显示当前是谁 —— 两个地方各切一次
                  正是"欧区机型配美区账户"那类必然失败组合的来源 */}
              <div className="flex items-center gap-2 px-3 py-2 rounded-xl border border-border bg-secondary/30">
                <User className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                <span className="text-[13px] font-medium">{activeAccount?.name || "未选择账户"}</span>
                {activeAccount && (
                  <span className="text-[11px] text-muted-foreground">
                    {activeAccount.zone} · {regionLabel(endpointRegion(activeAccount.endpoint))}
                  </span>
                )}
                <span className="ml-auto text-[10px] text-muted-foreground">在左侧菜单切换</span>
              </div>
              {orderEndpoint && (
                <p className="text-[11px] text-muted-foreground mt-1">
                  机房与配置的红绿点按该账户所在站点（{regionLabel(endpointRegion(orderEndpoint))}）实时查询
                  {orderAvail.isFetching && " · 同步中…"}
                </p>
              )}
              {/* 下单前唯一能看出"区配错了"的地方:价格按上方选的 subsidiary 显示,
                  下单却走账户所属站点。两者不同区时价格/库存都对不上,必须显式警告。 */}
              {priceRegionMismatch && (
                <p className="text-[11px] text-warning mt-1">
                  ⚠ 价格按 {subsidiary}（{regionLabel(priceRegion)}）显示，而下单账户在
                  {regionLabel(orderRegion)}站点 —— 三区的目录、价格、库存互不相通，实际扣款以账户所属站点为准。
                </p>
              )}
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label className="block text-[11px] text-muted-foreground mb-1">每个数据中心数量</label>
                <Input
                  type="number"
                  min={1}
                  value={quantity}
                  onChange={(e) => setQuantity(e.target.value)}
                />
              </div>
              <div>
                <label className="block text-[11px] text-muted-foreground mb-1">重试间隔（秒）</label>
                <Input
                  type="number"
                  min={10}
                  value={retryInterval}
                  onChange={(e) => setRetryInterval(e.target.value)}
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      <DialogFooter className="border-t border-border pt-4 -mx-6 px-6">
        <div className="mr-auto text-[12px] text-muted-foreground">
          {selectedDCs.length > 0
            ? `将创建 ${totalTasks} 个任务（${selectedDCs.length} DC × ${qty}）${selectedValues.length > 0 ? ` · ${selectedValues.length} 项选配` : ""}`
            : "请选数据中心"}
          {selectedDCs.length > 0 && (
            // 下单 checkout 带 waiveRetractationPeriod:true —— 替用户放弃了
            // 欧区 14 天法定撤销权(抢购要立即开通,这是常规做法),但必须让用户知情
            <span className="block text-[11px] mt-0.5">
              下单成功后需自行付款；下单即放弃 14 天撤销期（立即开通）
            </span>
          )}
        </div>
        <Button variant="outline" onClick={onClose} disabled={create.isPending}>
          关闭
        </Button>
        <Button
          variant="outline"
          disabled={addMon.isPending || create.isPending}
          onClick={() =>
            addMon.mutate({
              planCode: server.planCode,
              datacenters: dialogDCs.map((dc) => dc.code),
              serverName: server.name,
            })
          }
        >
          <Bell className="w-4 h-4" />
          加入监控
        </Button>
        <Button
          disabled={selectedDCs.length === 0 || create.isPending}
          onClick={async () => {
            if (selectedDCs.length === 0) {
              toast.error("请至少选择一个数据中心");
              return;
            }
            if (!accountId) {
              toast.error("请选择 OVH 账户");
              return;
            }
            const result = await create.mutateAsync({
              account_id: accountId,
              planCode: server.planCode,
              datacenters: selectedDCs,
              quantity: qty,
              retryInterval: Number(retryInterval) || 60,
              options: selectedValues,
            });
            if (result.success > 0) {
              toast.success(`已创建 ${result.success}/${result.total} 个抢购任务`);
              onClose();
            }
            if (result.failed > 0) {
              toast.error(`${result.failed} 个任务创建失败`);
            }
          }}
        >
          {create.isPending ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin" />
              创建中…
            </>
          ) : (
            <>
              <ShoppingCart className="w-4 h-4" />
              {selectedDCs.length > 0 ? `创建 ${totalTasks} 个任务` : "创建抢购任务"}
            </>
          )}
        </Button>
      </DialogFooter>
    </>
  );
}


function SpecCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="border border-border rounded-xl px-3.5 py-3 flex items-center gap-3 min-w-0">
      <div className="w-9 h-9 rounded-lg bg-secondary flex items-center justify-center text-foreground flex-shrink-0">
        {icon}
      </div>
      <div className="min-w-0">
        <div className="text-[11px] text-muted-foreground">{label}</div>
        <div className="text-[13px] font-semibold truncate" title={value}>{value}</div>
      </div>
    </div>
  );
}

/** 服务器目录缓存状态徽章：基于 /api/cache/info 显示当前数据是几分钟前的缓存还是已过期 */
function CacheBadge() {
  const info = useCacheInfo();
  const backend = info.data?.backend;
  if (!backend || !backend.hasCachedData) {
    return <span className="text-[11px] text-muted-foreground">尚未加载</span>;
  }
  const ageSec = backend.cacheAge ?? 0;
  const valid = !!backend.cacheValid;

  let text: string;
  if (ageSec < 60) {
    text = `${ageSec} 秒前`;
  } else if (ageSec < 3600) {
    text = `${Math.floor(ageSec / 60)} 分钟前`;
  } else {
    const h = Math.floor(ageSec / 3600);
    const m = Math.floor((ageSec % 3600) / 60);
    text = m > 0 ? `${h} 小时 ${m} 分钟前` : `${h} 小时前`;
  }

  return (
    <span
      className={`inline-flex items-center gap-1 px-2 py-1 rounded-md text-[11px] border ${
        valid
          ? "border-border text-muted-foreground bg-muted/40"
          : "border-amber-500/30 text-amber-700 dark:text-amber-300 bg-amber-50/60 dark:bg-amber-950/30"
      }`}
      title={
        valid
          ? "数据来自缓存，过期后再次访问才会重新调 OVH"
          : "缓存已过期，下次访问或点刷新会调 OVH 拉新数据"
      }
    >
      {valid ? "缓存" : "缓存已过期"} · {text}
    </span>
  );
}
