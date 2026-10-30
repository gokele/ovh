export const liveChatEn = { liveChat: {
  "title": "Live chat",
  "description": "Use a one-time authentication token for the selected OVH account to open a separate official support window. OVH handles the conversation.",
  "account": "Selected chat account",
  "switchHint": "Switch accounts using the sidebar or top account selector. Switching accounts or leaving this tab closes the old window and cancels pending authentication.",
  "product": "Product",
  "dedicated": "Dedicated server",
  "expectation": "The official chat uses English. Agent availability depends on the OVH region, support level and opening hours. Verify your customer identity in the official window.",
  "preparing": "Preparing authentication…",
  "preparingAccount": "Preparing OVH live chat authentication for account {{name}}…",
  "opened": "Opened the official window for {{name}}. The API customer ID is {{code}}. Check connection status and support replies in that window.",
  "open": "Open official chat",
  "reopen": "Reopen official chat",
  "close": "Close chat window",
  "help": "OVH Help Centre",
  "permissions": "API access requires GET /me and POST /auth/token. When opening the Help Centre, verify the account signed in on that website.",
  "errors": {
    "CHAT_POPUP_BLOCKED": "Your browser blocked the popup. Allow popups for this site and try again. No chat token was requested.",
    "CHAT_INVALID_RESPONSE": "The chat authentication response does not match the selected account or official URL. The window was closed. Retry or use the Help Centre."
  }
} };

export const liveChatApiEn = {
  "CHAT_INVALID_PRODUCT": "Select dedicated server or VPS support.",
  "CHAT_IDENTITY_MISSING": "OVH returned no customer ID. No chat token was requested. Check the account and GET /me access.",
  "CHAT_TOKEN_INVALID": "OVH returned no valid chat token. Retry or use the Help Centre.",
  "CHAT_REQUEST_FAILED": "Could not prepare chat authentication. Check GET /me and POST /auth/token access and the account status. Status: {{detail}}"
};
