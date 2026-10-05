import { ExternalLink } from "lucide-react";
import { useTranslation } from "react-i18next";
import { apiBaseUrlForEndpoint } from "@/lib/ovh-regions";

/**
 * 「这三个值去哪拿」的分步引导。
 *
 * 为什么值得单独做一块:APP KEY / APP SECRET / CONSUMER KEY 是整个程序唯一
 * 必须手工准备的东西,而它有两个新手必踩的坑,OVH 的申请页都不会提示:
 *
 *  1. **权限规则(Rights)留空或只给 GET** —— 之后所有下单/改配置一律 403,
 *     而 OVH 返回的原文是 "This call has not been granted",完全看不出要去改什么。
 *  2. **站点选错** —— 三站 token 互不通用,拿欧区 token 配美区账户永远登不进去。
 *
 * 官方文档(OVHcloud API first steps)原文:"The Rights field allows you to restrict
 * the use of the application to certain APIs. In order to allow all OVHcloud APIs
 * for an HTTP method, put an asterisk (*) into the field",三段凭据分别叫 AK / AS / CK。
 * https://docs.ovhcloud.com/en/guides/manage-and-operate/api/first-steps
 *
 * 注:createToken 页能否用 URL 参数预填权限,官方文档没有写,所以这里不那么做 ——
 * 只给链接 + 明确告诉用户要手动加哪四条。
 */
export function OvhTokenGuide({ endpoint }: { endpoint: string }) {
  const { t } = useTranslation();
  const site = apiBaseUrlForEndpoint(endpoint);
  const host = site.replace("https://", "");
  return (
    <div className="rounded-xl border border-border bg-muted/40 px-3.5 py-3 space-y-2">
      <p className="text-[12px] font-medium">{t("commons.tokenGuide.title")}</p>
      <ol className="text-[11px] text-muted-foreground leading-relaxed space-y-1.5 list-decimal pl-4">
        <li>
          {t("commons.tokenGuide.step1Open")}
          <a
            href={`${site}/createToken/`}
            target="_blank"
            rel="noreferrer"
            className="underline mx-1 inline-flex items-center gap-0.5 text-foreground"
          >
            {host}/createToken
            <ExternalLink className="w-3 h-3" />
          </a>
          {t("commons.tokenGuide.step1Login")}
          <span className="block">
            {t("commons.tokenGuide.step1Note")}
          </span>
        </li>
        <li>
          <b className="text-foreground">{t("commons.tokenGuide.step2Bold")}</b>
          {t("commons.tokenGuide.step2Tail")}
          <div className="mt-1 font-mono text-[11px] bg-background border border-border rounded-lg px-2 py-1.5 leading-relaxed">
            GET&nbsp;&nbsp;&nbsp;&nbsp;/*
            <br />
            POST&nbsp;&nbsp;&nbsp;/*
            <br />
            PUT&nbsp;&nbsp;&nbsp;&nbsp;/*
            <br />
            DELETE&nbsp;/*
          </div>
          <span className="block mt-1">
            {t("commons.tokenGuide.step2RightsPre")}
            <code className="px-1 bg-background rounded">not been granted</code>
            {t("commons.tokenGuide.step2RightsMid")}
            <b className="text-foreground">{t("commons.tokenGuide.step2ValidityBold")}</b>
            {t("commons.tokenGuide.step2ValidityTail")}
          </span>
        </li>
        <li>
          {t("commons.tokenGuide.step3Pre")}
          <b className="text-foreground">Application Key</b>
          {t("commons.tokenGuide.step3Map1")}
          <b className="text-foreground">Application Secret</b>
          {t("commons.tokenGuide.step3Map2")}
          <b className="text-foreground">Consumer Key</b>
          {t("commons.tokenGuide.step3Map3")}
          <span className="block">{t("commons.tokenGuide.step3Note")}</span>
        </li>
      </ol>
      <p className="text-[10px] text-muted-foreground">
        {t("commons.tokenGuide.storageNote")}
      </p>
    </div>
  );
}
