/**
 * App 配对管理面板(设置页一节):
 * 生成码(二维码 + 8 位大码 + 倒计时)、设备列表(在线时间/吊销)。
 * 手机丢了在这里吊销 —— 换密钥会让所有设备掉线,单独吊销不会。
 */
import { useEffect, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { Smartphone, Trash2, Plus, Clock } from "lucide-react";
import { Section } from "@/routes/settings-sections";
import { useAppDevices, useCreatePairingCode, useRevokeAppDevice } from "@/hooks/use-app-devices";
import { Button } from "@/components/ui/button";

/** 剩余秒数文案:过期后直接显示"已过期",不显示负数 */
function useCountdown(expiresAt?: string): string {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!expiresAt) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [expiresAt]);
  if (!expiresAt) return "";
  const left = Math.floor((new Date(expiresAt).getTime() - now) / 1000);
  if (left <= 0) return "已过期";
  return `剩 ${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}`;
}

export function AppPairingSection() {
  const devices = useAppDevices();
  const create = useCreatePairingCode();
  const revoke = useRevokeAppDevice();
  const countdown = useCountdown(create.data?.expiresAt);

  const code = create.data?.code;
  const expired = countdown === "已过期";

  return (
    <Section title="App 配对">
      <p className="text-[11px] text-muted-foreground leading-relaxed">
        手机 App 扫码或手填 8 位码连接本控制台。每台设备独立令牌(存手机钥匙串),
        <b>丢了手机在这里单独吊销</b>,不影响其他设备、不用换访问密钥。配对码 2 分钟内有效、一码一机。
      </p>

      {/* 生成区:码 + 二维码并排;过期后提示重新生成而不是静默留着死码 */}
      <div className="flex gap-4 items-start">
        {code && !expired ? (
          <>
            <div className="p-2.5 bg-white rounded-xl border border-border flex-shrink-0">
              <QRCodeSVG value={create.data!.pairUrl} size={104} />
            </div>
            <div className="min-w-0 flex-1 space-y-2">
              <p className="font-mono text-xl font-bold tracking-[0.3em] select-all">{code}</p>
              <p className="text-[11px] text-muted-foreground flex items-center gap-1">
                <Clock className="w-3 h-3" />
                {countdown} · 手填也行:App 端输入框直接打这 8 位
              </p>
              <p className="text-[10px] text-muted-foreground">
                App 配对地址用当前访问地址生成;若 App 与本页不在同一网络会连不上
              </p>
            </div>
          </>
        ) : (
          <div className="flex-1">
            <Button size="sm" onClick={() => create.mutate()} disabled={create.isPending}>
              <Plus className="w-3.5 h-3.5" />
              {create.isPending ? "生成中…" : code ? "重新生成(旧码已作废)" : "生成配对码"}
            </Button>
            {code && expired && (
              <p className="text-[11px] text-muted-foreground mt-2">上一个码已过期,点上面重新生成</p>
            )}
          </div>
        )}
      </div>
      {code && !expired && (
        <Button size="sm" variant="outline" onClick={() => create.mutate()} disabled={create.isPending}>
          重新生成
        </Button>
      )}

      {/* 设备列表 */}
      <div className="space-y-2">
        {devices.isPending ? (
          <p className="text-[11px] text-muted-foreground">设备列表读取中…</p>
        ) : devices.isError ? (
          <p className="text-[11px] text-destructive">设备列表读取失败,刷新重试</p>
        ) : (devices.data || []).length === 0 ? (
          <p className="text-[11px] text-muted-foreground">还没有配对过的设备</p>
        ) : (
          (devices.data || []).map((d) => (
            <div key={d.id} className="flex items-center gap-3 rounded-xl border border-border px-3.5 py-2.5">
              <Smartphone className="w-4 h-4 text-muted-foreground flex-shrink-0" />
              <div className="flex-1 min-w-0">
                <p className="text-[13px] font-medium truncate">
                  {d.name}
                  {d.revoked && <span className="ml-2 text-[10px] text-muted-foreground">(已吊销)</span>}
                </p>
                <p className="text-[10.5px] text-muted-foreground">
                  配对于 {new Date(d.createdAt).toLocaleString("zh-CN")}
                  {d.lastUsedAt ? ` · 最近使用 ${new Date(d.lastUsedAt).toLocaleString("zh-CN")}` : " · 未使用过"}
                </p>
              </div>
              {!d.revoked && (
                <Button
                  size="sm"
                  variant="outline"
                  className="text-destructive border-destructive/40 hover:bg-destructive/10"
                  disabled={revoke.isPending}
                  onClick={() => {
                    if (confirm(`吊销「${d.name}」?该设备下一次请求立即失败,需要重新配对。`)) revoke.mutate(d.id);
                  }}
                >
                  <Trash2 className="w-3.5 h-3.5" />
                  吊销
                </Button>
              )}
            </div>
          ))
        )}
      </div>
    </Section>
  );
}
