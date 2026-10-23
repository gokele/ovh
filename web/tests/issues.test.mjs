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
