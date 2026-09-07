/**
 * dsh-account-login — Host 侧 server 半。
 *
 * 在 dsh 进程内提供两个同源 API（浏览器 UI 直接 fetch，无 CORS）：
 *   POST /api/account-login/exchange  {mode: register|login, email, password}
 *   POST /api/account-login/sign-out
 * 成功后把 24h 会话凭据原子写入桌面壳监听的本机会话桥（DSH_ACCOUNT_BRIDGE_DIR
 * / ~/.dsh-desktop/account-bridge/session.json），由桌面壳采用并启动 Relay。
 * 密码只存在于请求体与账号 server 之间，绝不落盘。
 */

import { createHmac } from "node:crypto";
import { readFileSync } from "node:fs";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { join } from "node:path";
import z from "@deepseek-ai/schemastery";

export const name = "account-login";
export const inject = ["webServer"];

export const Config = z.object({});

const MAX_BODY_BYTES = 32 * 1024;

function isLoopbackOrigin(origin) {
  try {
    const host = new URL(origin).hostname;
    return host === "127.0.0.1" || host === "localhost" || host === "[::1]" || host === "::1";
  } catch {
    return false;
  }
}

function accountServerURL() {
  // 单一事实来源为桌面壳注入的 DSH_ACCOUNT_SERVER_URL；未注入时回退到与桌面壳
  // account_adapters.go 相同的缺省账号 server，避免独立运行/未注入时误报“未配置”。
  const configured = process.env.DSH_ACCOUNT_SERVER_URL;
  if (typeof configured === "string" && configured !== "") return configured.replace(/\/$/, "");
  return "https://relay.codegoround.com";
}

function accountBridgeDir() {
  const configured = process.env.DSH_ACCOUNT_BRIDGE_DIR;
  if (typeof configured === "string" && configured !== "") return configured;
  const home = process.env.HOME || process.env.USERPROFILE || ".";
  return join(home, ".dsh-desktop", "account-bridge");
}

function accountSessionFile() {
  return join(accountBridgeDir(), "session.json");
}

async function writeSession(session, secret) {
  await mkdir(accountBridgeDir(), { recursive: true, mode: 0o700 });
  const target = accountSessionFile();
  const payload = JSON.stringify(session);
  // 两行格式：payload + HMAC-SHA256(payload)，桌面壳据此认证写方（每次启动随机密钥）。
  // 未注入 secret（独立/未接线运行）时只写 payload 行：桌面壳 verifyBridgeFile 因缺
  // 签名会忽略，从而既不会误采纳，也不会让登录/注册因“密钥未配置”抛错变成 500。
  const mac = secret ? createHmac("sha256", secret).update(payload).digest("hex") : null;
  const content = mac === null ? payload : payload + "\n" + mac;
  const temporary = target + ".tmp";
  await writeFile(temporary, content, { mode: 0o600 });
  await rename(temporary, target); // 同目录 rename 原子替换，避免撕裂读
}

async function readBody(req, send) {
  let body = "";
  for await (const chunk of req) {
    body += chunk;
    if (body.length > MAX_BODY_BYTES) {
      send(413, { ok: false, code: "body_too_large" });
      return null;
    }
  }
  let payload;
  try {
    payload = JSON.parse(body);
  } catch {
    send(400, { ok: false, code: "invalid_json" });
    return null;
  }
  return payload;
}

function isLocalRequest(req, send) {
  if (req.method !== "POST") {
    send(405, { ok: false, code: "method_not_allowed" });
    return false;
  }
  const contentType = String(req.headers["content-type"] ?? "");
  if (!contentType.toLowerCase().startsWith("application/json")) {
    send(415, { ok: false, code: "content_type" });
    return false;
  }
  const origin = req.headers.origin;
  if (typeof origin === "string" && origin !== "" && !isLoopbackOrigin(origin)) {
    send(403, { ok: false, code: "forbidden_origin" });
    return false;
  }
  return true;
}

export function apply(ctx, config) {
  void config;
  ctx.webServer.register({
    kind: "exact",
    path: "/api/account-login/exchange",
    handler: async (req, res) => {
      const send = (status, body) => {
        res.writeHead(status, { "content-type": "application/json", "cache-control": "no-store" });
        res.end(JSON.stringify(body));
      };
      try {
        if (!isLocalRequest(req, send)) return;
        const payload = await readBody(req, send);
        if (!payload) return;
        const mode = payload.mode === "register" ? "register" : "login";
        const email = typeof payload.email === "string" ? payload.email.trim().toLowerCase() : "";
        const password = typeof payload.password === "string" ? payload.password : "";
        if (!email.includes("@") || password.length < 8 || password.length > 128) {
          return send(400, { ok: false, code: "invalid_credentials" });
        }
        const serverURL = accountServerURL();
        const secret = process.env.DSH_ACCOUNT_BRIDGE_SECRET || "";
        if (!serverURL) {
          return send(503, { ok: false, code: "account_server_not_configured" });
        }
        let response;
        try {
          response = await fetch(serverURL + "/v1/account/" + (mode === "register" ? "register" : "login"), {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ email, password }),
          });
        } catch {
          return send(502, { ok: false, code: "network" });
        }
        if (response.status === 409) return send(409, { ok: false, code: "account_exists" });
        if (response.status === 401) return send(401, { ok: false, code: "invalid_credentials" });
        if (!response.ok) return send(502, { ok: false, code: "server_error" });
        let credential;
        try {
          credential = await response.json();
        } catch {
          return send(502, { ok: false, code: "server_error" });
        }
        if (!credential || !credential.accountID || !credential.accessToken) {
          return send(502, { ok: false, code: "server_error" });
        }
        await writeSession(
          {
            accountID: credential.accountID,
            accessToken: credential.accessToken,
            expiresAt: credential.expiresAt ?? null,
            signedOut: false,
          },
          secret
        );
        ctx.logger?.info ? ctx.logger.info("[account-login] session written for " + credential.accountID) : null;
        return send(200, { ok: true, accountID: credential.accountID });
      } catch (error) {
        ctx.logger?.error ? ctx.logger.error("[account-login] exchange failed: " + String(error)) : null;
        send(500, { ok: false, code: "internal" });
      }
    },
  });
  ctx.webServer.register({
    kind: "exact",
    path: "/api/account-login/status",
    handler: async (req, res) => {
      const send = (status, body) => {
        res.writeHead(status, { "content-type": "application/json", "cache-control": "no-store" });
        res.end(JSON.stringify(body));
      };
      try {
        if (req.method !== "GET") return send(405, { ok: false, code: "method_not_allowed" });
        let raw;
        try {
          raw = readFileSync(join(accountBridgeDir(), "status.json"), "utf8");
        } catch {
          return send(200, { ok: true, status: null });
        }
        let status;
        try {
          status = JSON.parse(raw);
        } catch {
          return send(200, { ok: true, status: null });
        }
        return send(200, { ok: true, status });
      } catch {
        send(500, { ok: false });
      }
    },
  });
  ctx.webServer.register({
    kind: "exact",
    path: "/api/account-login/sign-out",
    handler: async (req, res) => {
      const send = (status, body) => {
        res.writeHead(status, { "content-type": "application/json", "cache-control": "no-store" });
        res.end(JSON.stringify(body));
      };
      try {
        if (!isLocalRequest(req, send)) return;
        await readBody(req, send);
        await writeSession({ accountID: "", accessToken: "", expiresAt: null, signedOut: true }, process.env.DSH_ACCOUNT_BRIDGE_SECRET || "");
        return send(200, { ok: true });
      } catch {
        send(500, { ok: false, code: "internal" });
      }
    },
  });
}
