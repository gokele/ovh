/**
 * App 配对管理面板(设置页一节):
 * 生成码(二维码 + 8 位大码 + 倒计时)、设备列表(在线时间/吊销)。
 * 手机丢了在这里吊销 —— 换密钥会让所有设备掉线,单独吊销不会。
 */
import { useEffect, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { Smartphone, Trash2, Plus, Clock } from "lucide-react";
import { useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";
import { Section } from "@/routes/settings-sections";
import { useAppDevices, useCreatePairingCode, useRevokeAppDevice } from "@/hooks/use-app-devices";
import { Button } from "@/components/ui/button";

/** 剩余秒数文案:过期后直接显示"已过期",不显示负数 */
function useCountdown(expiresAt?: string): { expired: boolean; label: string } {
  const { t } = useTranslation();
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!expiresAt) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [expiresAt]);
  if (!expiresAt) return { expired: false, label: "" };
  const left = Math.floor((new Date(expiresAt).getTime() - now) / 1000);
  if (left <= 0) return { expired: true, label: t("settings.app.expired") };
  return {
    expired: false,
    label: t("settings.app.countdown", {
      min: Math.floor(left / 60),
      sec: String(left % 60).padStart(2, "0"),
    }),
  };
}

export function AppPairingSection() {
  const { t } = useTranslation();
  const devices = useAppDevices();
  const create = useCreatePairingCode();
  const revoke = useRevokeAppDevice();
  const { expired, label: countdown } = useCountdown(create.data?.expiresAt);

  const code = create.data?.code;

  return (
    <Section title={t("settings.sections.app")}>
      <p className="text-[11px] text-muted-foreground leading-relaxed">
        {t("settings.app.introPre")}<b>{t("settings.app.introBold")}</b>{t("settings.app.introPost")}
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
                {countdown}{t("settings.app.manualFill")}
              </p>
              <p className="text-[10px] text-muted-foreground">
                {t("settings.app.pairUrlNote")}
              </p>
            </div>
          </>
        ) : (
          <div className="flex-1">
            <Button size="sm" onClick={() => create.mutate()} disabled={create.isPending}>
              <Plus className="w-3.5 h-3.5" />
              {create.isPending ? t("settings.app.generating") : code ? t("settings.app.regenerate") : t("settings.app.generate")}
            </Button>
            {code && expired && (
              <p className="text-[11px] text-muted-foreground mt-2">{t("settings.app.codeExpiredHint")}</p>
            )}
          </div>
        )}
      </div>
      {code && !expired && (
        <Button size="sm" variant="outline" onClick={() => create.mutate()} disabled={create.isPending}>
          {t("settings.app.regenerateSimple")}
        </Button>
      )}

      {/* 设备列表 */}
      <div className="space-y-2">
        {devices.isPending ? (
          <p className="text-[11px] text-muted-foreground">{t("settings.app.devicesLoading")}</p>
        ) : devices.isError ? (
          <p className="text-[11px] text-destructive">{t("settings.app.devicesFailed")}</p>
        ) : (devices.data || []).length === 0 ? (
          <p className="text-[11px] text-muted-foreground">{t("settings.app.noDevices")}</p>
        ) : (
          (devices.data || []).map((d) => (
            <div key={d.id} className="flex items-center gap-3 rounded-xl border border-border px-3.5 py-2.5">
              <Smartphone className="w-4 h-4 text-muted-foreground flex-shrink-0" />
              <div className="flex-1 min-w-0">
                <p className="text-[13px] font-medium truncate">
                  {d.name}
                  {d.revoked && <span className="ml-2 text-[10px] text-muted-foreground">{t("settings.app.revokedSuffix")}</span>}
                </p>
                <p className="text-[10.5px] text-muted-foreground">
                  {t("settings.app.pairedAt", { date: fmtDateTime(d.createdAt) })}
                  {d.lastUsedAt ? t("settings.app.lastUsedAt", { date: fmtDateTime(d.lastUsedAt) }) : t("settings.app.neverUsed")}
                </p>
              </div>
              {!d.revoked && (
                <Button
                  size="sm"
                  variant="outline"
                  className="text-destructive border-destructive/40 hover:bg-destructive/10"
                  disabled={revoke.isPending}
                  onClick={() => {
                    if (confirm(t("settings.app.revokeConfirm", { name: d.name }))) revoke.mutate(d.id);
                  }}
                >
                  <Trash2 className="w-3.5 h-3.5" />
                  {t("settings.app.revoke")}
                </Button>
              )}
            </div>
          ))
        )}
      </div>
    </Section>
  );
}
