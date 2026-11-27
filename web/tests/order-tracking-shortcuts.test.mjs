import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createElement as h } from "react";
import { renderToStaticMarkup as render } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createServer } from "vite";

const vite = await createServer({ configFile: false, root: fileURLToPath(new URL("..", import.meta.url)),
  resolve: { alias: { "@": fileURLToPath(new URL("../src", import.meta.url)) } },
  server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
after(() => vite.close());
const { OrderTrackingButton } = await vite.ssrLoadModule("/src/components/account/OrderTrackingButton.tsx");
const { OrderTimeline } = await vite.ssrLoadModule("/src/components/account/OrderTimeline.tsx");
const { useOrderTracking } = await vite.ssrLoadModule("/src/hooks/use-account.ts");
const { api } = await vite.ssrLoadModule("/src/lib/api.ts");
const { default: i18n } = await vite.ssrLoadModule("/src/i18n/index.ts");

// Closed rows must work without mounting a tracking query, even without a QueryClient.
test("a history badge remains readable when it cannot identify one order", () => {
  for (const [accountId, orderId] of [["", "1"], [" a", "1"], ["a", undefined], ["a", "01"], ["a", "1/2"],
    ["a", "9223372036854775808"], ["a", Number.MAX_SAFE_INTEGER + 1]]) {
    assert.equal(render(h(OrderTrackingButton, { accountId, orderId }, h("span", null, "Delivered"))), "<span>Delivered</span>");
    assert.equal(render(h(OrderTrackingButton, { accountId, orderId })), "");
  }
  const trigger = render(h(OrderTrackingButton, { accountId: "buyer", orderId: "9223372036854775807" }, "Delivered"));
  assert.match(trigger, /<button/); assert.match(trigger, /Delivered/); assert.match(trigger, /9223372036854775807/);
});

test("tracking GET uses the row's owner and exact decimal ID; equal numbers on two accounts are independent", async () => {
  const client = new QueryClient();
  const previous = api.defaults.adapter;
  const seen = [];
  api.defaults.adapter = async (config) => {
    seen.push([config.method, config.url, config.params.account]);
    return { data: { accountId: config.params.account, status: "delivering", steps: [], details: [] }, status: 200, statusText: "OK", headers: {}, config };
  };
  let query;
  function Probe({ accountId, id }) { query = useOrderTracking(accountId, id, true); return null; }
  try {
    const id = "9223372036854775807";
    for (const accountId of ["buyer-a", "buyer-b"]) {
      render(h(QueryClientProvider, { client }, h(Probe, { accountId, id })));
      assert.equal((await query.refetch()).data.accountId, accountId);
    }
    assert.deepEqual(seen, ["buyer-a", "buyer-b"].map(a => ["get", `/ovh/account/orders/${id}/tracking`, a]));
    assert.equal(client.getQueryCache().getAll().length, 2);
  } finally { api.defaults.adapter = previous; client.clear(); }
});

for (const lang of ["en", "zh"]) {
  test(`timeline distinguishes an unavailable read from an empty history and shows newest first (${lang})`, async () => {
    await i18n.changeLanguage(lang);
    const data = { accountId: "buyer", orderId: 1, status: "delivering", details: [], steps: [] };
    assert.ok(render(h(OrderTimeline, { data })).includes(i18n.t("orderTrack.noTimeline")));
    const failed = render(h(OrderTimeline, { data: { ...data, followUpError: "fixture denied" } }));
    assert.ok(failed.includes("fixture denied")); assert.ok(!failed.includes(i18n.t("orderTrack.noTimeline")));
    const event = (date, description) => ({ date, description, label: "DELIVERY" });
    const html = render(h(OrderTimeline, { data: { ...data, steps: [
      { step: "VALIDATED", status: "DONE", history: [event("2026-01-01T02:00:00Z", "older-event")] },
      { step: "DELIVERING", status: "DOING", history: [event("2026-01-01T03:00:00Z", "later-event")] },
    ] } }));
    assert.ok(html.includes("older-event") && html.includes("later-event"));
    assert.ok(html.indexOf("later-event") < html.indexOf("older-event"));
  });
}
