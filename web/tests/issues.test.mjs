import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createServer } from "vite";

const server = await createServer({
  configFile: false,
  root: fileURLToPath(new URL("..", import.meta.url)),
  resolve: { alias: { "@": fileURLToPath(new URL("../src", import.meta.url)) } },
  server: { middlewareMode: true, hmr: false },
  appType: "custom",
});
after(() => server.close());
const { Route } = await server.ssrLoadModule("/src/routes/monitor.tsx");
const { useServers } = await server.ssrLoadModule("/src/hooks/use-servers.ts");
const { mergeStockPlans } = await server.ssrLoadModule("/src/lib/stock-plans.ts");
const { Route: ServersRoute } = await server.ssrLoadModule("/src/routes/servers.tsx");
const { hasStockWithOption } = await server.ssrLoadModule("/src/hooks/use-availability.ts");
const { comparePrices } = await server.ssrLoadModule("/src/lib/money.ts");

test("default account's configured subsidiary selects the price catalog", () => {
  const client = new QueryClient();
  client.setQueryData(["accounts", "list"], [{ id: "us", endpoint: "ovh-us", zone: "US", isDefault: true }]);
  client.setQueryData(["servers", "list", { showApiServers: true, accountId: "" }], []);
  try {
    renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(ServersRoute.options.component)));
    assert.ok(client.getQueryState(["ovh-catalog", "eco", "US"]));
    assert.equal(client.getQueryState(["ovh-catalog", "eco", "IE"]), undefined);
  } finally {
    client.clear();
  }
});

test("stock option indicators recognize display aliases without mixing datacenters", () => {
  for (const [display, api] of [["mum", "ynm"], ["tor", "ca-east-tor-a"], ["par-a", "eu-west-par-a"]]) {
    const variants = [{ fqn: "plan.ram-32g", datacenters: [{ datacenter: api, availability: "1H-low" }] }];
    assert.equal(hasStockWithOption(variants, {}, "memory", "ram-32g", [display]), true);
    assert.equal(hasStockWithOption(variants, {}, "memory", "ram-32g", ["par-b"]), false);
  }
});

test("unknown prices stay last in either direction and zero is a valid price", () => {
  const values = [undefined, 50, 0, NaN, 20];
  assert.deepEqual([...values].sort((a, b) => comparePrices(a, b, "asc")).filter(Number.isFinite), [0, 20, 50]);
  assert.deepEqual([...values].sort((a, b) => comparePrices(a, b, "desc")).filter(Number.isFinite), [50, 20, 0]);
  for (const direction of ["asc", "desc"]) {
    assert.ok(comparePrices(undefined, 50, direction) > 0);
    assert.ok(comparePrices(0, undefined, direction) < 0);
    assert.equal(comparePrices(undefined, NaN, direction), 0);
  }
});

for (const catalogState of ["ready", "pending", "error"]) {
  test(`server list includes 27skle01-v1 from stock with sales catalog ${catalogState}`, () => {
    const client = new QueryClient({ defaultOptions: { queries: { retryOnMount: false } } });
    client.setQueryData(["accounts", "list"], [
      { id: "eu", endpoint: "ovh-eu", zone: "IE", isDefault: true },
    ]);
    const key = ["servers", "list", { showApiServers: true, accountId: "" }];
    if (catalogState === "ready") client.setQueryData(key, []);
    if (catalogState === "error") {
      client.getQueryCache().build(client, { queryKey: key }).setState({ status: "error", error: new Error("catalog unavailable") });
    }
    client.setQueryData(["availability", "all", "EU"], [{
      planCode: "27skle01-v1", server: "27skle01", memory: "ram-16g-ddr3-noecc-1333",
      storage: "softraid-2x480ssd", fqn: "27skle01-v1.ram-16g-ddr3-noecc-1333.softraid-2x480ssd",
      datacenters: [{ datacenter: "gra", availability: "1H-high" }],
    }]);
    try {
      const html = renderToStaticMarkup(createElement(QueryClientProvider, { client },
        createElement(ServersRoute.options.component)));
      assert.ok(html.includes("27skle01-v1"));
      assert.match(html, /预上架|Prerelease/);
      assert.ok(html.includes("16 GB"));
      if (catalogState === "error") assert.ok(html.includes("catalog unavailable"));
    } finally {
      client.clear();
    }
  });
}

test("an unknown model's diagnostic does not crash the monitor page or hide deletion", () => {
  const client = new QueryClient();
  const planCode = "nonexistent-model";
  const error = "EU 站点没有该型号的任何可用性记录";
  client.setQueryData(["monitor", "list"], [{
    planCode,
    datacenters: [],
    notifyAvailable: true,
    notifyUnavailable: false,
    lastStatus: {},
    createdAt: "2026-10-19T00:00:00Z",
  }]);
  client.setQueryData(["monitor", "status"], {
    running: true,
    subscriptions_count: 1,
    check_interval: 60,
    known_servers_count: 0,
    // The actual object shape returned by monitor.Status(), not a string.
    region_issues: [{
      planCode, serverName: "", accountId: "", region: "EU", subsidiary: "IE",
      checkedAt: "2026-10-19T00:00:00Z", error,
    }],
  });
  try {
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client },
      createElement(Route.options.component)));
    assert.ok(html.includes(planCode));
    assert.ok(html.includes(error));
    assert.ok(html.includes("@ EU (IE)"));
    assert.match(html, /aria-label="(?:删除|Delete)"/);
  } finally {
    client.clear();
  }
});

for (const region_issues of [[], undefined]) {
  test(`monitor page renders without diagnostics (${JSON.stringify(region_issues)})`, () => {
    const client = new QueryClient();
    client.setQueryData(["monitor", "list"], []);
    client.setQueryData(["monitor", "status"], {
      running: true, subscriptions_count: 0, check_interval: 60,
      known_servers_count: 0, region_issues,
    });
    try {
      const html = renderToStaticMarkup(createElement(QueryClientProvider, { client },
        createElement(Route.options.component)));
      assert.ok(!html.includes("border-warning/40 bg-warning/5"));
    } finally {
      client.clear();
    }
  });
}

test("task account override scopes the catalog cache to that account", () => {
  const client = new QueryClient();
  client.setQueryData(["accounts", "list"], [
    { id: "eu-main", endpoint: "ovh-eu", zone: "IE", isDefault: true },
    { id: "eu-second", endpoint: "ovh-eu", zone: "IE" },
    { id: "us", endpoint: "ovh-us", zone: "US" },
  ]);
  function CatalogProbe({ accountId }) {
    useServers(true, accountId);
    return null;
  }
  try {
    for (const accountId of ["eu-second", "us"]) {
      renderToStaticMarkup(createElement(QueryClientProvider, { client },
        createElement(CatalogProbe, { accountId })));
      assert.ok(client.getQueryState(["servers", "list", { showApiServers: true, accountId }]));
    }
    assert.equal(client.getQueryState(["servers", "list", { showApiServers: true, accountId: "" }]), undefined);
  } finally {
    client.clear();
  }
});

test("stock-only Eco models become searchable prereleases without replacing listed plans", () => {
  const listed = { planCode: "24sk202", name: "KS-2", availableOptions: [] };
  const stock = (planCode, memory = "ram-32g-ddr4-ecc-2666") => ({
    planCode, server: planCode.replace(/-v1$/, ""), memory,
    storage: "softraid-2x480ssd", fqn: `${planCode}.${memory}.softraid-2x480ssd`,
    datacenters: [{ datacenter: "gra", availability: "1H-low" }],
  });
  const merged = mergeStockPlans([listed], [
    stock("24sk202"), stock("27skle03-v1"), stock("27skle03-v1", "ram-64g-ddr4-ecc-2666"),
    stock("27sysstorle01-v1"), stock("25scale01-v1"),
  ]);
  assert.equal(merged[0], listed);
  assert.deepEqual(merged.map((p) => p.planCode), ["24sk202", "27skle03-v1", "27sysstorle01-v1"]);
  const found = merged.find((p) => `${p.planCode} ${p.name}`.toLowerCase().includes("27SKLE03".toLowerCase()));
  assert.equal(found.prerelease, true);
  assert.ok(found.memory.includes("32") && found.memory.includes("64"));
  assert.deepEqual(found.availableOptions.map(({ value, family }) => [value, family]), [
    ["ram-32g-ddr4-ecc-2666", "memory"],
    ["ram-64g-ddr4-ecc-2666", "memory"],
    ["softraid-2x480ssd", "storage"],
  ]);
  assert.deepEqual(mergeStockPlans([listed]), [listed]);
});


test("coupon is trimmed and sent to every datacenter and quantity task", async () => {
  const { useCreateQueueItem } = await server.ssrLoadModule("/src/hooks/use-queue.ts");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const client = new QueryClient();
  const adapter = api.defaults.adapter;
  const bodies = [];
  api.defaults.adapter = async (config) => {
    assert.equal(config.url, "/queue");
    bodies.push(JSON.parse(config.data));
    return { data: {}, status: 200, statusText: "OK", headers: {}, config };
  };
  let mutation;
  function Probe() { mutation = useCreateQueueItem(); return null; }
  try {
    renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
    const result = await mutation.mutateAsync({ account_id: "eu", planCode: "24sk202", datacenters: ["gra", "rbx"], quantity: 2, options: ["ram-64g"], preorder: true, coupon: " Save10 " });
    assert.equal(result.success, 4);
    assert.deepEqual(bodies.map((b) => b.datacenter), ["gra", "gra", "rbx", "rbx"]);
    for (const body of bodies) {
      assert.equal(body.coupon, "Save10");
      assert.equal(body.autoPay, false);
      assert.equal(body.preorder, true);
      assert.deepEqual(body.options, ["ram-64g"]);
    }
    await mutation.mutateAsync({ account_id: "eu", planCode: "24sk202", datacenters: ["gra"], coupon: "  " });
    assert.equal(bodies.at(-1).coupon, undefined);
  } finally {
    api.defaults.adapter = adapter;
    client.clear();
  }
});


test("successful history keeps coupon fallback visible in desktop and mobile views", async () => {
  const { Route: HistoryRoute } = await server.ssrLoadModule("/src/routes/history.tsx");
  const client = new QueryClient();
  const warning = "优惠码未生效，本次未使用优惠码";
  client.setQueryData(["history"], [{ id: "order", accountId: "eu", planCode: "24sk202", datacenter: "gra", status: "success", purchaseTime: "2026-10-23T00:00:00Z", couponWarning: warning }]);
  client.setQueryData(["accounts", "list"], []);
  try {
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(HistoryRoute.options.component)));
    assert.equal(html.split(warning).length - 1, 2);
  } finally {
    client.clear();
  }
});

test("task edits update one queue item and monitor edits retain exact empty configuration identity", async () => {
  const { useUpdateQueueItem } = await server.ssrLoadModule("/src/hooks/use-queue.ts");
  const { useUpdateMonitorSubscription, usePauseMonitorSubscription } = await server.ssrLoadModule("/src/hooks/use-monitor.ts");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const client = new QueryClient();
  const adapter = api.defaults.adapter;
  const calls = [];
  api.defaults.adapter = async (config) => {
    calls.push({ method: config.method, url: config.url, params: config.params, body: JSON.parse(config.data) });
    return { data: {}, status: 200, statusText: "OK", headers: {}, config };
  };
  let queue, monitor, pause;
  function Probe() { queue = useUpdateQueueItem(); monitor = useUpdateMonitorSubscription(); pause = usePauseMonitorSubscription(); return null; }
  try {
    renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
    await queue.mutateAsync({ id: "task", datacenter: "rbx", options: ["ram-64g", "custom-addon"], retryInterval: 17, autoPay: false, coupon: "Save10" });
    await monitor.mutateAsync({ planCode: "plan", optionsKey: "", notifyAvailable: false, options: ["ram-32g"] });
    await pause.mutateAsync({ planCode: "plan", optionsKey: "", paused: true });
    assert.equal(calls.length, 3);
    assert.equal(calls[0].method, "put");
    assert.equal(calls[0].url, "/queue/task");
    assert.deepEqual(calls[0].body.options, ["ram-64g", "custom-addon"]);
    assert.equal(calls[1].params.optionsKey, "");
    assert.equal(calls[1].body.notifyAvailable, false);
    assert.equal(calls[1].body.optionsKey, undefined);
    assert.equal(calls[2].body.optionsKey, "");
    assert.equal(calls[2].body.paused, true);
  } finally { api.defaults.adapter = adapter; client.clear(); }
});

test("webhook tests cache actual delivery status on success and rejection without sending another probe", async () => {
  const { useTestNotification } = await server.ssrLoadModule("/src/hooks/use-notify-channels.ts");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const { AxiosError } = await import("axios");
  const client = new QueryClient();
  const adapter = api.defaults.adapter;
  const key = ["notify", "channels", true];
  let mutation, requestCount = 0, reject = false;
  function Probe() { mutation = useTestNotification(); return null; }
  api.defaults.adapter = async (config) => {
    requestCount++;
    assert.equal(config.method, "post");
    assert.equal(config.url, "/monitor/test-notification");
    const channels = [{ name: "Webhook", configured: true, ok: !reject, detail: reject ? "Webhook 返回错误 (code 19002): msg_type need" : "" }];
    const response = { data: { channels, delivered: reject ? 0 : 1, message: "test result" }, status: reject ? 500 : 200, statusText: "fixture", headers: {}, config };
    if (reject) throw new AxiosError("rejected", "ERR_BAD_RESPONSE", config, undefined, response);
    return response;
  };
  try {
    renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
    await mutation.mutateAsync();
    assert.equal(requestCount, 1);
    assert.equal(client.getQueryData(key).anyAvailable, true);
    assert.equal(client.getQueryState(key).isInvalidated, false);
    reject = true;
    await assert.rejects(mutation.mutateAsync());
    const result = client.getQueryData(key);
    assert.equal(requestCount, 2);
    assert.equal(result.verified, true);
    assert.equal(result.anyAvailable, false);
    assert.equal(result.channels[0].ok, false);
    assert.match(result.channels[0].detail, /19002/);
    assert.equal(client.getQueryState(key).isInvalidated, false);
  } finally { api.defaults.adapter = adapter; client.clear(); }
});

// Payment is read-only until the user explicitly submits the mutation.
test("payment preflight stays read-only and pins the account through submission", async () => {
  const { useOrderPaymentInfo, usePayAccountOrder } = await server.ssrLoadModule("/src/hooks/use-account.ts");
  const { OrderPaymentButton } = await server.ssrLoadModule("/src/components/account/OrderPaymentButton.tsx");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const client = new QueryClient();
  const original = api.defaults.adapter;
  const calls = [];
  api.defaults.adapter = async (config) => {
    calls.push(config);
    return { data: { accountId: "original", orderId: 123, status: "notPaid", methods: [] }, status: 200, statusText: "OK", headers: {}, config };
  };
  let pay;
  function Probe() { useOrderPaymentInfo("original", 123, false); pay = usePayAccountOrder("original", 123); return createElement(OrderPaymentButton, { accountId: "original", orderId: 123 }); }
  try {
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
    assert.match(html, /发起支付|Pay order/);
    assert.equal(calls.length, 0);
    const query = client.getQueryCache().find({ queryKey: ["account", "order-payment", "original", 123] });
    assert.equal(query.options.enabled, false);
    await query.options.queryFn();
    assert.equal(calls[0].method, "get");
    assert.equal(calls[0].params.account, "original");
    await pay.mutateAsync({ accountId: "original", paymentMethodId: 7, paymentType: "CREDIT_CARD", amount: 39.98, currency: "EUR", confirm: true });
    assert.equal(calls.length, 2);
    assert.equal(calls[1].method, "post");
    assert.equal(calls[1].params.account, "original");
    assert.deepEqual(JSON.parse(calls[1].data), { paymentMethodId: 7, paymentType: "CREDIT_CARD", amount: 39.98, currency: "EUR", confirm: true });
  } finally { api.defaults.adapter = original; client.clear(); }
});

test("payment mutation does not retry ambiguous failures", async () => {
  const { usePayAccountOrder } = await server.ssrLoadModule("/src/hooks/use-account.ts");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const client = new QueryClient({ defaultOptions: { mutations: { retry: 3 } } });
  const original = api.defaults.adapter;
  let calls = 0, pay;
  api.defaults.adapter = async () => { calls++; throw new Error("ambiguous result"); };
  function Probe() { pay = usePayAccountOrder("account", 123); return null; }
  try {
    renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
    await assert.rejects(pay.mutateAsync({ accountId: "account", paymentMethodId: 7, paymentType: "CREDIT_CARD", amount: 39.98, currency: "EUR", confirm: true }), /ambiguous/);
    assert.equal(calls, 1);
  } finally { api.defaults.adapter = original; client.clear(); }
});

test("monitor displays saved last restock time and a truthful empty state", () => {
  for (const lastRestockAt of ["2026-10-29T10:15:00Z", undefined]) {
    const client = new QueryClient();
    client.setQueryData(["monitor", "list"], [{ planCode: "24skstor01-v1", datacenters: ["gra"], notifyAvailable: true, notifyUnavailable: false, lastStatus: {}, createdAt: "2026-10-01T00:00:00Z", lastRestockAt }]);
    client.setQueryData(["monitor", "status"], { running: false, subscriptions_count: 1, check_interval: 5, known_servers_count: 0 });
    client.setQueryData(["accounts", "list"], []);
    try {
      const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Route.options.component)));
      assert.match(html, lastRestockAt ? /最近检测到补货|Last observed restock/ : /暂无历史补货记录|No restock observed/);
      assert.match(html, /OVH/);
    } finally { client.clear(); }
  }
});

// 关联暂停改变了队列;保存失败也必须刷新真实内存状态,不能保留仍在抢购的旧显示。
test("monitor pause refreshes queue and monitoring after success or persistence failure", async () => {
  const { usePauseMonitorSubscription } = await server.ssrLoadModule("/src/hooks/use-monitor.ts");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const original = api.defaults.adapter;
  for (const fail of [false, true]) {
    const client = new QueryClient();
    const keys = [["monitor", "list"], ["monitor", "status"], ["queue", "list"]];
    keys.forEach((key) => client.setQueryData(key, []));
    api.defaults.adapter = async (config) => {
      if (fail) throw new Error("pause persistence failure");
      return { data: { paused: true, pausedTasks: 2 }, status: 200, statusText: "OK", headers: {}, config };
    };
    let pause;
    function Probe() { pause = usePauseMonitorSubscription(); return null; }
    try {
      renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
      const pending = pause.mutateAsync({ planCode: "plan", optionsKey: "", paused: true });
      if (fail) await assert.rejects(pending, /persistence/); else await pending;
      for (const key of keys) assert.equal(client.getQueryState(key).isInvalidated, true);
    } finally { api.defaults.adapter = original; client.clear(); }
  }
});

// 新增标签页采用当前账户的工单列表,页面挂载不能发出真实工单操作。
test("support list renders account-scoped tickets without submitting", async () => {
  const { SupportTickets } = await server.ssrLoadModule("/src/components/support/SupportTickets.tsx");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const client = new QueryClient();
  client.setQueryData(["support", "list", "", 0, ""], { accountId: "eu", accountName: "EU fixture", items: [{ ticketId: 123, ticketNumber: 987654, subject: "Disk issue", state: "open", category: "incident", product: "dedicated" }], page: 0, hasMore: false, failedCount: 0 });
  const original = api.defaults.adapter;
  let calls = 0;
  api.defaults.adapter = async () => { calls++; throw new Error("unexpected request"); };
  try {
    const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(SupportTickets)));
    assert.match(html, /客服工单|Support tickets/);
    assert.match(html, /新建工单|New ticket/);
    assert.match(html, /987654/);
    assert.match(html, /Disk issue/);
    assert.equal(calls, 0);
  } finally { api.defaults.adapter = original; client.clear(); }
});

// 类别、产品、子类别和服务名称均传给后端,操作账户在窗口打开时即固定。
test("support mutations preserve selected classifications and the pinned account", async () => {
  const { useCreateSupportTicket, useSupportAction } = await server.ssrLoadModule("/src/hooks/use-support.ts");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const client = new QueryClient();
  const original = api.defaults.adapter;
  const calls = [];
  api.defaults.adapter = async (config) => { calls.push(config); return { data: { accountId: "eu", result: { ticketId: 123, ticketNumber: 987654 } }, status: 200, statusText: "OK", headers: {}, config }; };
  let create, action;
  function Probe() { create = useCreateSupportTicket("eu"); action = useSupportAction("eu", 123); return null; }
  try {
    renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
    await create.mutateAsync({ subject: "Disk issue", body: "Line 1\n\nLine 2", category: "incident", product: "dedicated", subcategory: "down", serviceName: "ns-fixture" });
    const body = JSON.parse(calls[0].data);
    assert.equal(body.category, "incident");
    assert.equal(body.product, "dedicated");
    assert.equal(body.subcategory, "down");
    assert.equal(body.serviceName, "ns-fixture");
    assert.equal(body.body, "Line 1\n\nLine 2");
    await action.mutateAsync({ action: "reply", body: "Follow-up" });
    await action.mutateAsync({ action: "close" });
    await action.mutateAsync({ action: "reopen", body: "Still unresolved" });
    for (const call of calls) assert.equal(call.params.account, "eu");
    assert.deepEqual(calls.slice(1).map((call) => call.url), Array(3).fill("/ovh/support/tickets/123"));
    assert.equal(calls.length, 4);
  } finally { api.defaults.adapter = original; client.clear(); }
});

// 真实对外通信失败时不自动重试,避免重复创建或回复。
test("support create and reply do not retry ambiguous errors", async () => {
  const { useCreateSupportTicket, useSupportAction } = await server.ssrLoadModule("/src/hooks/use-support.ts");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const client = new QueryClient({ defaultOptions: { mutations: { retry: 3 } } });
  const original = api.defaults.adapter;
  let calls = 0, create, action;
  api.defaults.adapter = async () => { calls++; throw new Error("unknown outcome"); };
  function Probe() { create = useCreateSupportTicket("eu"); action = useSupportAction("eu", 123); return null; }
  try {
    renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
    await assert.rejects(create.mutateAsync({ subject: "x", body: "x", category: "assistance", product: "vps" }), /unknown/);
    assert.equal(calls, 1);
    await assert.rejects(action.mutateAsync({ action: "reply", body: "x" }), /unknown/);
    assert.equal(calls, 2);
  } finally { api.defaults.adapter = original; client.clear(); }
});

// 显式空账户代表默认账户,不能在请求拦截时被后来的全局账户切换覆盖。
test("explicit default account remains pinned when another account is active", async () => {
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const original = api.defaults.adapter;
  const previousWindow = globalThis.window;
  const accounts = [];
  globalThis.window = { localStorage: { getItem: (key) => key === "ovh_active_server_control_account_id" ? "us" : null } };
  api.defaults.adapter = async (config) => { accounts.push(config.params.account); return { data: {}, status: 200, statusText: "OK", headers: {}, config }; };
  try {
    await api.get("/ovh/support/tickets", { params: { account: "" } });
    await api.get("/ovh/support/tickets", { params: { account: "eu" } });
    await api.get("/ovh/support/tickets");
    assert.deepEqual(accounts, ["", "eu", "us"]);
  } finally { api.defaults.adapter = original; if (previousWindow === undefined) delete globalThis.window; else globalThis.window = previousWindow; }
});

// 搜索、账户和页码均进入查询键,不同筛选不能复用旧列表缓存。
test("support subject search keeps filter and account scope before pagination", async () => {
  const { useSupportTickets } = await server.ssrLoadModule("/src/hooks/use-support.ts");
  const { qk } = await server.ssrLoadModule("/src/lib/query.ts");
  const { api } = await server.ssrLoadModule("/src/lib/api.ts");
  const client = new QueryClient();
  const original = api.defaults.adapter;
  const calls = [];
  api.defaults.adapter = async (config) => { calls.push(config); return { data: { accountId: "us", items: [] }, status: 200, statusText: "OK", headers: {}, config }; };
  let list;
  function Probe() { list = useSupportTickets("us", 2, "open", "Disk issue"); return null; }
  try {
    renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(Probe)));
    await list.refetch();
    assert.deepEqual(calls[0].params, { account: "us", page: 2, status: "open", subject: "Disk issue" });
    assert.notDeepEqual(qk.support.list("us", 2, "open", "Disk issue"), qk.support.list("us", 2, "open", "Other issue"));
    assert.notDeepEqual(qk.support.list("eu", 2, "open", "Disk issue"), qk.support.list("us", 2, "open", "Disk issue"));
  } finally { api.defaults.adapter = original; client.clear(); }
});

// 聊天正文保持原文和换行,上游消息中的 HTML 只能作为文字显示。
test("support conversation keeps plain text for both senders", async () => {
  const { TicketMessage } = await server.ssrLoadModule("/src/components/support/TicketMessage.tsx");
  for (const from of ["support", "customer"]) {
    const html = renderToStaticMarkup(createElement(TicketMessage, { message: { messageId: 1, from, creationDate: "2026-10-30T01:00:00Z", body: "Please investigate.\n<script>alert('x')</script>" } }));
    assert.match(html, /Please investigate\.\n/);
    assert.match(html, /&lt;script&gt;/);
    assert.doesNotMatch(html, /<script>/);
    assert.match(html, from === "customer" ? /我方|You/ : /OVH 支持团队|OVH support/);
  }
});

// 覆盖所有支持的语言、工单静态文案、官方枚举和后端错误码,防止新增提示漏接。
test("support translations cover every app language and backend error code", async () => {
  const { readFile, readdir } = await import("node:fs/promises");
  const { default: i18n, SUPPORTED_LANGUAGES } = await server.ssrLoadModule("/src/i18n/index.ts");
  const go = await readFile(new URL("../../server/internal/handlers/support.go", import.meta.url), "utf8");
  const keys = new Set([...go.matchAll(/"(SUPPORT_\w+)"/g)].map((m) => `api.${m[1]}`));
  const components = new URL("../src/components/support/", import.meta.url);
  for (const file of await readdir(components)) {
    const source = await readFile(new URL(file, components), "utf8");
    for (const match of source.matchAll(/\bt\(\s*"((?:support|common)\.[^"]+)"/g)) keys.add(match[1]);
  }
  for (const [name, group] of [["supportCategories", "categories"], ["supportProducts", "products"], ["supportSubcategories", "subcategories"]]) {
    const choices = go.match(new RegExp(`var ${name} = \\[\\]string\\{([^}]+)\\}`))[1];
    for (const value of choices.matchAll(/"([^"]+)"/g)) keys.add(`support.${group}.${value[1]}`);
  }
  for (const lang of SUPPORTED_LANGUAGES) {
    for (const key of keys) {
      assert.equal(i18n.exists(key, { lng: lang, fallbackLng: false }), true, `${lang}: missing ${key}`);
      const text = i18n.t(key, { lng: lang });
      assert.ok(text && text !== key, `${lang}: empty ${key}`);
      if (lang === "en") assert.doesNotMatch(text, /\p{Script=Han}/u, `${key} leaks Chinese`);
    }
  }
  // 英文是工单内容偏好,界面语言切换不会把填写示例翻译成中文。
  for (const field of ["subjectPlaceholder", "bodyPlaceholder", "replyPlaceholder", "reopenPlaceholder"]) {
    assert.equal(i18n.t(`support.${field}`, { lng: "zh" }), i18n.t(`support.${field}`, { lng: "en" }));
  }
});

// 错误翻译保留上游原文,且列表中的单条错误跟随切换而不是缓存译文。
test("support errors and partial rows update with language while keeping OVH details", async () => {
  const { default: i18n } = await server.ssrLoadModule("/src/i18n/index.ts");
  const { apiMessage, bodyMessage } = await server.ssrLoadModule("/src/lib/api-error.ts");
  const { SupportTickets } = await server.ssrLoadModule("/src/components/support/SupportTickets.tsx");
  const originalLanguage = i18n.language;
  const client = new QueryClient();
  const failure = { error: "中文默认错误", code: "SUPPORT_REQUEST_FAILED", params: { detail: "OVH 403 Fixture rejection (query: fixture-123)" } };
  client.setQueryData(["support", "list", "", 0, ""], { accountId: "eu", accountName: "Fixture", items: [{ ticketId: 124, ...failure }], page: 0, hasMore: false, failedCount: 1 });
  try {
    for (const lang of ["en", "zh"]) {
      await i18n.changeLanguage(lang);
      const message = apiMessage({ response: { data: { error: "fallback", code: "SUPPORT_SUBMISSION_UNCONFIRMED", params: failure.params } } });
      assert.match(message, lang === "en" ? /before sending again/ : /避免重复发送/);
      assert.ok(message.includes(failure.params.detail));
      assert.equal(apiMessage({ response: { data: { error: "Vendor original", code: "FUTURE_CODE" } } }), "Vendor original");
      assert.equal(bodyMessage({ message: "Legacy message" }), "Legacy message");
      const html = renderToStaticMarkup(createElement(QueryClientProvider, { client }, createElement(SupportTickets)));
      assert.ok(html.includes(failure.params.detail));
      assert.doesNotMatch(html, /中文默认错误/);
      assert.match(html, lang === "en" ? /ticket request failed/ : /工单请求失败/);
    }
  } finally { await i18n.changeLanguage(originalLanguage); client.clear(); }
});

// OVH 工单账户的凭据失效是业务错误,不能清除控制台会话或触发登录覆盖层。
test("OVH support 401 does not expire the application session", async () => {
  const { api, onAuthFailure } = await server.ssrLoadModule("/src/lib/api.ts");
  const original = api.defaults.adapter;
  let expired = 0;
  const unregister = onAuthFailure(() => { expired++; });
  const error = Object.assign(new Error("OVH credential rejection"), { response: { status: 401, data: { success: false, code: "SUPPORT_REQUEST_FAILED", error: "OVH credential rejection", params: { detail: "OVH 401" } } } });
  api.defaults.adapter = async () => { throw error; };
  try {
    await assert.rejects(api.get("/ovh/support/tickets", { params: { account: "eu" } }), /credential rejection/);
    assert.equal(expired, 0);
  } finally { unregister(); api.defaults.adapter = original; }
});
