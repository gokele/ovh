import { useEffect, useState, type ReactNode } from "react";
import { KeyRound, Loader2, ShieldAlert } from "lucide-react";
import axios from "axios";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { getApiSecretKey, setApiSecretKey, clearApiSecretKey, onAuthFailure } from "@/lib/api";

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
  const [state, setState] = useState<AuthState>("checking");
  const [errMsg, setErrMsg] = useState<string>("");

  // 进入应用后订阅会话失效。
  //
  // 只在 authed 状态下订阅:needs-auth 时用户正在登录覆盖层里,
  // 而覆盖层的验证走裸 axios 不经过拦截器,不会有 401 打进来。
  useEffect(() => {
    if (state !== "authed") return;
    return onAuthFailure(() => {
      setErrMsg("登录状态已失效，请重新输入 API 密钥");
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
    verifyKey(stored)
      .then((ok) => {
        if (ok) setState("authed");
        else {
          clearApiSecretKey();
          setErrMsg("已保存的 API 密钥失效，请重新输入");
          setState("needs-auth");
        }
      })
      .catch(() => {
        // 网络错误：放行进入应用，让单个请求的拦截器 / 业务层处理报错
        setState("authed");
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
  const [error, setError] = useState<string>(initialError || "");

  const submit = async () => {
    const trimmed = key.trim();
    if (!trimmed) {
      setError("请输入 API 密钥");
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      const ok = await verifyKey(trimmed);
      if (ok) {
        onSuccess(trimmed);
      } else {
        setError("密钥无效（服务端返回 401）");
      }
    } catch (e: any) {
      setError(e?.message || "验证失败，请检查网络或后端服务");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[100] bg-background/95 backdrop-blur-sm flex items-center justify-center px-4">
      <div className="w-full max-w-md border border-border rounded-2xl bg-background p-7 space-y-5">
        <div className="flex items-center gap-2.5">
          <div className="w-10 h-10 rounded-xl bg-secondary flex items-center justify-center">
            <ShieldAlert className="w-5 h-5" />
          </div>
          <div>
            <h2 className="text-lg font-semibold leading-tight">需要 API 密钥</h2>
            <p className="text-[12px] text-muted-foreground mt-0.5">访问后端前请先验证身份</p>
          </div>
        </div>

        <div className="space-y-2">
          <label className="text-[12px] font-medium block">API 密钥</label>
          <div className="relative">
            <KeyRound className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
            <Input
              type="password"
              autoFocus
              autoComplete="off"
              spellCheck={false}
              placeholder="后端配置文件 / 环境变量里的 X-API-Key"
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

        <Button onClick={submit} disabled={submitting || !key.trim()} className="w-full">
          {submitting ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin mr-1.5" />
              验证中…
            </>
          ) : (
            "验证并进入"
          )}
        </Button>

        <p className="text-[10px] text-muted-foreground leading-relaxed">
          密钥保存在浏览器 localStorage，不会上传服务端。换设备或清缓存后需重新输入。
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
async function verifyKey(key: string): Promise<boolean> {
  try {
    const res = await axios.get("/api/stats", {
      headers: { "X-API-Key": key },
      timeout: 10000,
      // 别让 axios 把 401 当成 reject，自己判 status
      validateStatus: () => true,
    });
    if (res.status === 200) return true;
    if (res.status === 401) return false;
    // 其它非 2xx 当成网络异常抛出
    throw new Error(`后端返回 ${res.status}`);
  } catch (e: any) {
    if (e?.response?.status === 401) return false;
    throw e;
  }
}
