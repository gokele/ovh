import { useEffect, useState, type ReactNode } from "react";
import { KeyRound, Loader2, ShieldAlert } from "lucide-react";
import axios from "axios";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { getApiSecretKey, setApiSecretKey, clearApiSecretKey, onAuthFailure } from "@/lib/api";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";
import { LanguageToggle } from "@/components/layout/LanguageToggle";
import { ThemeToggle } from "@/components/layout/ThemeToggle";

type AuthState = "checking" | "needs-auth" | "authed";

/**
 * 全局鉴权拦截：
 * - 首次挂载 → 探测 /api/stats（受保护端点，便宜），200 即视为通过
 * - 没 key 或 401 → 进入 needs-auth 状态，整屏覆盖登录界面
 *   - 用户在浏览器上看不到任何应用内容，也点不到任何路由 / 按钮（fixed inset-0 + 高 z-index）
 *   - 输入 key → 临时塞到 localStorage → 再次探测 /api/stats，通过才放行
 * - 验证用裸 axios，不走带拦截器的 `api` 客户端：避免循环弹 toast
 * - 进入应用后仍订阅 401：密钥会中途失效（服务端换了 key、别的标签页清了它），
 *   那时用户会卡在一屏永远不再更新的旧数据上。收到 401 就重新弹登录覆盖层。
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const [state, setState] = useState<AuthState>("checking");
  const [errMsg, setErrMsg] = useState<string>("");

  // 进入应用后订阅会话失效。
  //
  // 只在 authed 状态下订阅:needs-auth 时用户正在登录覆盖层里,
  // 而覆盖层的验证走裸 axios 不经过拦截器,不会有 401 打进来。
  useEffect(() => {
    if (state !== "authed") return;
    return onAuthFailure(() => {
      // 事件可能晚于本次渲染很久才触发,直接走 i18n.t 免得闭包里拿到旧语言
      setErrMsg(i18n.t("commons.auth.sessionExpired"));
      setState("needs-auth");
    });
  }, [state]);

  // 启动时检查一次
  useEffect(() => {
    const stored = getApiSecretKey();
    if (!stored) {
      setState("needs-auth");
      return;
    }
    verifyKey(stored).then((r) => {
      if (r.ok) {
        setState("authed");
        return;
      }
      if (r.reason === "unreachable") {
        // 后端没起来/网络问题:放行进入应用,让各请求自己报错 ——
        // 卡在这一层的话用户连界面都看不到,更不知道发生了什么。
        setState("authed");
        return;
      }
      if (r.reason === "rate-limited") {
        // 限流不能放行:放进去只会让每个请求都 429,而且不断续上限流窗口。
        // 也不清除已保存的密钥 —— 它可能本来就是对的,只是被别人试错连累了。
        setErrMsg(r.message);
        setState("needs-auth");
        return;
      }
      clearApiSecretKey();
      setErrMsg(t("commons.auth.storedKeyInvalid"));
      setState("needs-auth");
    });
  }, []);

  if (state === "checking") {
    return (
      <div className="fixed inset-0 z-[100] bg-background flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (state === "needs-auth") {
    return (
      <LoginOverlay
        initialError={errMsg}
        onSuccess={(key) => {
          setApiSecretKey(key);
          setErrMsg("");
          setState("authed");
        }}
      />
    );
  }

  return <>{children}</>;
}

/** 全屏登录覆盖：fixed inset-0 + 顶层 z-index，盖住所有内容，点任何地方都点不到下面 */
function LoginOverlay({
  initialError,
  onSuccess,
}: {
  initialError?: string;
  onSuccess: (key: string) => void;
}) {
  const [key, setKey] = useState("");
  const [submitting, setSubmitting] = useState(false);
  // 限流倒计时。被挡住时把提交按钮也锁上 —— 用户一直点只会让窗口不断续上,
  // 而他看到的还是同一句错误,会以为是密钥的问题。
  const [cooldown, setCooldown] = useState(0);
  useEffect(() => {
    if (cooldown <= 0) return;
    const t = setInterval(() => setCooldown((n) => (n <= 1 ? 0 : n - 1)), 1000);
    return () => clearInterval(t);
  }, [cooldown]);
  const [error, setError] = useState<string>(initialError || "");
  const { t } = useTranslation();

  const submit = async () => {
    const trimmed = key.trim();
    if (!trimmed) {
      setError(t("commons.auth.emptyKey"));
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      const r = await verifyKey(trimmed);
      if (r.ok) {
        onSuccess(trimmed);
      } else {
        setError(r.message);
        // 被限流时把按钮也锁住:不锁的话用户会一直点,而每次点都在续限流窗口
        if (r.reason === "rate-limited" && r.retryAfter) setCooldown(r.retryAfter);
      }
    } catch (e: any) {
      setError(e?.message || t("commons.auth.verifyFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[100] bg-background/95 backdrop-blur-sm flex items-center justify-center px-4">
      <div className="w-full max-w-md border border-border rounded-2xl bg-background p-7 space-y-5 relative">
        {/* 登录页也要能切语言/主题:英文浏览器环境的中文用户不该被锁在外面 */}
        <div className="absolute top-4 right-4 flex items-center gap-1">
          <LanguageToggle />
          <ThemeToggle />
        </div>
        <div className="flex items-center gap-2.5">
          <div className="w-10 h-10 rounded-xl bg-secondary flex items-center justify-center">
            <ShieldAlert className="w-5 h-5" />
          </div>
          <div>
            <h2 className="text-lg font-semibold leading-tight">{t("commons.auth.title")}</h2>
            <p className="text-[12px] text-muted-foreground mt-0.5">{t("commons.auth.subtitle")}</p>
          </div>
        </div>

        <div className="space-y-2">
          <label className="text-[12px] font-medium block">{t("commons.auth.keyLabel")}</label>
          <div className="relative">
            <KeyRound className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
            <Input
              type="password"
              autoFocus
              autoComplete="off"
              spellCheck={false}
              placeholder={t("commons.auth.keyPlaceholder")}
              value={key}
              onChange={(e) => setKey(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !submitting) submit();
              }}
              className="pl-9 font-mono text-[13px]"
            />
          </div>
          {error && <p className="text-[11px] text-destructive">{error}</p>}
        </div>

        <Button
          onClick={submit}
          disabled={submitting || !key.trim() || cooldown > 0}
          className="w-full"
        >
          {submitting ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin mr-1.5" />
              {t("commons.auth.verifying")}
            </>
          ) : cooldown > 0 ? (
            t("commons.auth.waitSeconds", { count: cooldown })
          ) : (
            t("commons.auth.submit")
          )}
        </Button>

        <p className="text-[10px] text-muted-foreground leading-relaxed">
          {t("commons.auth.storageNote")}
        </p>
      </div>
    </div>
  );
}

/**
 * 通过裸 axios 探测 /api/stats（受保护端点）：
 * - 200 → key 有效
 * - 401 → key 无效（返回 false）
 * - 其它（网络 / 5xx） → 抛错让调用方决定
 */
/**
 * 校验密钥。
 *
 * 返回结构化结果而不是 boolean:密钥错、被限流、后端连不上是三件不同的事,
 * 给用户的下一步也完全不同(改密钥 / 等一会儿 / 看后端是否在跑)。
 * 以前统一压成 true/false,429 会走到"其它状态码"分支抛出「后端返回 429」——
 * 一个裸状态码,用户只会继续重试,而重试正是让限流窗口一直续上的原因。
 */
type VerifyResult =
  | { ok: true }
  | { ok: false; reason: "bad-key" | "rate-limited" | "unreachable"; message: string; retryAfter?: number };

async function verifyKey(key: string): Promise<VerifyResult> {
  let res;
  try {
    res = await axios.get("/api/stats", {
      headers: { "X-API-Key": key },
      timeout: 10000,
      // 别让 axios 把 401/429 当成 reject，自己判 status
      validateStatus: () => true,
    });
  } catch (e: any) {
    return {
      ok: false,
      reason: "unreachable",
      message: i18n.t("commons.auth.unreachable", { reason: e?.message || i18n.t("commons.auth.networkError") }),
    };
  }
  if (res.status === 200) return { ok: true };
  if (res.status === 401) {
    return {
      ok: false,
      reason: "bad-key",
      message: i18n.t("commons.auth.badKey"),
    };
  }
  if (res.status === 429) {
    // 后端已经给了带"等多久"的中文说明,原样用它,别自己另编一句
    const secs = Number(res.data?.retryAfter) || Number(res.headers?.["retry-after"]) || 60;
    return {
      ok: false,
      reason: "rate-limited",
      retryAfter: secs,
      message: res.data?.message || i18n.t("commons.auth.rateLimited", { secs }),
    };
  }
  return {
    ok: false,
    reason: "unreachable",
    message: i18n.t("commons.auth.unexpectedStatus", { status: res.status }),
  };
}