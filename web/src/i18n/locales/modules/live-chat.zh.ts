export const liveChatZh = { liveChat: {
  "title": "在线客服",
  "description": "通过当前 OVH 账户的一次性认证，在独立的官方窗口打开在线客服。聊天内容由 OVH 处理。",
  "account": "当前聊天账户",
  "switchHint": "使用侧栏或顶部的账户选择器切换账户。切换账户或离开此标签页会关闭旧窗口并取消未完成的认证。",
  "product": "咨询产品",
  "dedicated": "独立服务器",
  "expectation": "官方聊天使用英语。是否可接通人工取决于 OVH 大区、支持等级及客服工作时间；请在官方窗口核对当前客户身份。",
  "preparing": "正在准备认证…",
  "preparingAccount": "正在为账户 {{name}} 准备 OVH 官方聊天认证…",
  "opened": "已为 {{name}} 打开官方窗口，API 客户编号为 {{code}}。连接状态和客服回复请查看官方窗口。",
  "open": "打开官方聊天",
  "reopen": "重新打开官方聊天",
  "close": "关闭聊天窗口",
  "help": "OVH 帮助中心",
  "permissions": "API 需要 GET /me 和 POST /auth/token 权限。若跳转帮助中心，请核对官网当前登录的账户。",
  "errors": {
    "CHAT_POPUP_BLOCKED": "浏览器阻止了弹出窗口，请允许本站弹窗后重新打开。尚未申请聊天令牌。",
    "CHAT_INVALID_RESPONSE": "聊天认证响应与当前账户或官方地址不一致，已关闭窗口。请重试或使用官网帮助中心。"
  }
} };

export const liveChatApiZh = {
  "CHAT_INVALID_PRODUCT": "请选择独立服务器或 VPS 客服。",
  "CHAT_IDENTITY_MISSING": "OVH 未返回客户编号，未申请聊天令牌。请核对账户及 GET /me 权限。",
  "CHAT_TOKEN_INVALID": "OVH 未返回有效聊天令牌。请重试或使用官网帮助中心。",
  "CHAT_REQUEST_FAILED": "无法准备官方聊天认证，请检查 GET /me、POST /auth/token 权限及账户状态。状态：{{detail}}"
};
