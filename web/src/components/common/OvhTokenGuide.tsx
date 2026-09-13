import { ExternalLink } from "lucide-react";
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
  const site = apiBaseUrlForEndpoint(endpoint);
  const host = site.replace("https://", "");
  return (
    <div className="rounded-xl border border-border bg-muted/40 px-3.5 py-3 space-y-2">
      <p className="text-[12px] font-medium">还没有这三个值?按这三步拿:</p>
      <ol className="text-[11px] text-muted-foreground leading-relaxed space-y-1.5 list-decimal pl-4">
        <li>
          打开
          <a
            href={`${site}/createToken/`}
            target="_blank"
            rel="noreferrer"
            className="underline mx-1 inline-flex items-center gap-0.5 text-foreground"
          >
            {host}/createToken
            <ExternalLink className="w-3 h-3" />
          </a>
          用 OVH 账号登录。
          <span className="block">
            这个地址跟着上面选的子公司走 —— 三个站点的 token 互不通用,在别的站点申请的登不进去。
          </span>
        </li>
        <li>
          <b className="text-foreground">Rights(权限)填四条</b>,这一步最容易漏:
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
            只给 GET 的话能看不能买:下单、改配置、重装全部会失败,OVH 只回一句
            <code className="px-1 bg-background rounded">not been granted</code>。
            「有效期 / Validity」建议选<b className="text-foreground">不限(Unlimited)</b>,
            选了期限到期后要重来一遍。
          </span>
        </li>
        <li>
          提交后页面会显示三个值:
          <b className="text-foreground">Application Key</b> → APP KEY、
          <b className="text-foreground">Application Secret</b> → APP SECRET、
          <b className="text-foreground">Consumer Key</b> → CONSUMER KEY。
          <span className="block">页面关掉就看不到了,先复制过来再关。</span>
        </li>
      </ol>
      <p className="text-[10px] text-muted-foreground">
        这三个值加密保存在本机的 SQLite 里,不会上传到任何地方;界面和日志里也只会显示前几位。
      </p>
    </div>
  );
}
