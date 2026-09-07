/**
 * dsh-account-login — dsh 设置页（独立 section）浏览器半。
 * 注册含二次确认密码；登录/注册后在页面内显示持久的已登录状态（账号/主机/Relay），
 * 跨刷新保留（读桌面壳写的 status.json）。
 */

window.__ModuleLoader__.load({
  id: "dsh-account-login",
  factory: (require) => {
    const module = { exports: {} };
    const exports = module.exports;
    Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
    const React = require("react");

    const NS = "accountLogin";
    const zh = {
      "section.title": "Account 登录",
      "section.desc": "邮箱/密码注册或登录后，此 Mac 作为 Host 登记进该账号，手机即可经 Relay 远程操控（当前不做两步验证）。",
      "section.email": "邮箱",
      "section.password": "密码（至少 8 位）",
      "section.confirm": "确认密码",
      "section.register": "注册",
      "section.login": "登录",
      "section.signout": "退出",
      "section.loggedIn": "已登录",
      "section.hostRegistered": "此 Mac 已作为 Host 登记",
      "section.relay": "Relay",
      "section.switchLogin": "用已有账号登录",
      "section.switchRegister": "注册新账号",
      "section.pwMismatch": "两次输入的密码不一致。",
      "section.pwShort": "密码至少 8 位。",
      "section.emailInvalid": "请填写有效邮箱。",
    };
    const en = {
      "section.title": "Account Sign-in",
      "section.desc": "Register or sign in with email/password to register this Mac as a Host under that account; the phone can then drive it over Relay (no 2FA for now).",
      "section.email": "Email",
      "section.password": "Password (min 8 chars)",
      "section.confirm": "Confirm password",
      "section.register": "Register",
      "section.login": "Sign in",
      "section.signout": "Sign out",
      "section.loggedIn": "Signed in",
      "section.hostRegistered": "This Mac is registered as a Host",
      "section.relay": "Relay",
      "section.switchLogin": "Sign in with existing account",
      "section.switchRegister": "Register new account",
      "section.pwMismatch": "Passwords do not match.",
      "section.pwShort": "Password must be at least 8 characters.",
      "section.emailInvalid": "Enter a valid email.",
    };

    const style = {
      page: { padding: 16, display: "grid", gap: 12 },
      title: { fontSize: 16, fontWeight: 600 },
      desc: { fontSize: 13, opacity: 0.75 },
      row: { display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" },
      input: { width: "100%", boxSizing: "border-box", padding: "8px 10px", borderRadius: 8, border: "1px solid var(--dsw-alias-border-l1, rgba(128,128,128,0.4))", background: "transparent", color: "inherit" },
      primary: { padding: "8px 16px", borderRadius: 8, border: "none", background: "var(--dsw-alias-accent-strong, #0a66c2)", color: "#fff", cursor: "pointer" },
      button: { padding: "8px 14px", borderRadius: 8, border: "1px solid var(--dsw-alias-border-l1, rgba(128,128,128,0.4))", background: "transparent", color: "inherit", cursor: "pointer" },
      notice: { fontSize: 12, padding: "8px 10px", borderRadius: 8 },
      card: { border: "1px solid var(--dsw-alias-border-l1, rgba(128,128,128,0.35))", borderRadius: 12, padding: 16, display: "grid", gap: 8 },
      badge: { padding: "2px 8px", borderRadius: 20, fontSize: 12 },
      tab: { padding: "6px 12px", borderRadius: 8, border: "1px solid transparent", background: "transparent", cursor: "pointer" },
      tabActive: { padding: "6px 12px", borderRadius: 8, border: "1px solid var(--dsw-alias-accent-strong, #0a66c2)", background: "var(--dsw-alias-accent-strong, #0a66c2)", color: "#fff", cursor: "pointer" },
    };

    function AccountLoginPage({ t }) {
      const label = (key) => (t && t(key)) || zh[key] || key;
      const [mode, setMode] = React.useState("login");
      const [email, setEmail] = React.useState("");
      const [password, setPassword] = React.useState("");
      const [confirm, setConfirm] = React.useState("");
      const [busy, setBusy] = React.useState(false);
      const [notice, setNotice] = React.useState(null);
      const [status, setStatus] = React.useState(null);
      const [statusLoaded, setStatusLoaded] = React.useState(false);

      const fetchStatus = React.useCallback(async (silent) => {
        try {
          const r = await fetch("/api/account-login/status");
          const b = await r.json().catch(() => ({}));
          setStatus(b && b.status ? b.status : null);
        } catch {
          // ignore
        } finally {
          if (!silent) setStatusLoaded(true);
        }
      }, []);

      React.useEffect(() => {
        fetchStatus(false);
        const id = setInterval(() => fetchStatus(true), 3000);
        return () => clearInterval(id);
      }, [fetchStatus]);

      const exchange = React.useCallback(async (action) => {
        const trimmed = email.trim();
        if (action === "register") {
          if (password.length < 8) { setNotice({ kind: "err", text: label("section.pwShort") }); return; }
          if (password !== confirm) { setNotice({ kind: "err", text: label("section.pwMismatch") }); return; }
        }
        if (!trimmed.includes("@")) { setNotice({ kind: "err", text: label("section.emailInvalid") }); return; }
        setBusy(true);
        setNotice(null);
        try {
          const r = await fetch("/api/account-login/exchange", {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ mode: action, email: trimmed, password }),
          });
          const b = await r.json().catch(() => ({ ok: false, code: "invalid_response" }));
          if (r.ok && b.ok) {
            setNotice({ kind: "ok", text: (action === "register" ? "已注册账号 " : "已登录账号 ") + String(b.accountID || "").slice(0, 8) + "…，桌面端将自动接入。" });
            setPassword("");
            setConfirm("");
            await fetchStatus(false);
          } else if (b.code === "account_exists") {
            setNotice({ kind: "err", text: "该邮箱已注册，请直接登录。" });
          } else if (b.code === "invalid_credentials") {
            setNotice({ kind: "err", text: "邮箱或密码不正确（密码至少 8 位）。" });
          } else if (b.code === "account_server_not_configured") {
            setNotice({ kind: "err", text: "Account server 未配置，请在桌面端设置 DSH_ACCOUNT_SERVER_URL。" });
          } else {
            setNotice({ kind: "err", text: "Account server 请求失败（" + String(b.code || r.status) + "）。" });
          }
        } catch {
          setNotice({ kind: "err", text: "无法连接本地 dsh 服务。" });
        } finally {
          setBusy(false);
        }
      }, [email, password, confirm, label]);

      const signOut = React.useCallback(async () => {
        setBusy(true);
        try {
          await fetch("/api/account-login/sign-out", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" });
          await fetchStatus(false);
          setNotice({ kind: "ok", text: "已退出账号。" });
        } catch {
          setNotice({ kind: "err", text: "退出请求失败。" });
        } finally {
          setBusy(false);
        }
      }, [fetchStatus]);

      const signedIn = status && status.state === "signed-in";
      const relayState = status && status.relayState ? status.relayState : "—";
      const relayOk = relayState === "online";
      const formValid = email.trim().length > 0 && password.length > 0 && (mode !== "register" || (confirm.length > 0 && password === confirm));

      return React.createElement(
        "div",
        { style: style.page, className: NS + "-page" },
        React.createElement("div", { style: style.title }, label("section.title")),
        React.createElement("div", { style: style.desc }, label("section.desc")),
        notice ? React.createElement("div", { style: { ...style.notice, color: notice.kind === "ok" ? "inherit" : "#b3261e", background: notice.kind === "ok" ? "rgba(16,185,129,0.12)" : "rgba(179,38,30,0.10)" } }, notice.text) : null,
        signedIn
          ? React.createElement(
              "div",
              { style: style.card },
              React.createElement("div", { style: { display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" } },
                React.createElement("span", { style: { ...style.badge, background: "rgba(16,185,129,0.15)", color: "#0f9d58" } }, label("section.loggedIn")),
                React.createElement("span", { style: { fontSize: 13, opacity: 0.85 } }, "账号 " + String((status && status.accountID) || "").slice(0, 8) + "…"),
                React.createElement("span", { style: { fontSize: 12, opacity: 0.7 } }, label("section.hostRegistered")),
                React.createElement("span", { style: { fontSize: 12, opacity: 0.7 } }, label("section.relay") + ": " + relayState)
              ),
              React.createElement("button", { style: style.button, disabled: busy, onClick: signOut }, label("section.signout"))
            )
          : React.createElement(
              "div",
              { style: style.card },
              React.createElement(
                "div",
                { style: style.row },
                React.createElement("button", { style: mode === "login" ? style.tabActive : style.tab, onClick: () => { setMode("login"); setNotice(null); } }, label("section.login")),
                React.createElement("button", { style: mode === "register" ? style.tabActive : style.tab, onClick: () => { setMode("register"); setNotice(null); } }, label("section.register"))
              ),
              React.createElement("input", { style: style.input, type: "email", placeholder: label("section.email"), value: email, onChange: (ev) => setEmail(ev.target.value), autoComplete: "email" }),
              React.createElement("input", { style: style.input, type: "password", placeholder: label("section.password"), value: password, onChange: (ev) => setPassword(ev.target.value), autoComplete: "current-password" }),
              mode === "register" ? React.createElement("input", { style: style.input, type: "password", placeholder: label("section.confirm"), value: confirm, onChange: (ev) => setConfirm(ev.target.value), autoComplete: "new-password" }) : null,
              React.createElement("button", { style: style.primary, disabled: busy || !formValid, onClick: () => exchange(mode) }, mode === "register" ? label("section.register") : label("section.login"))
            )
      );
    }

    function apply(ctx) {
      if (!ctx || !ctx.slots) return;
      if (ctx.effect && ctx.locale) {
        ctx.effect(() => ctx.locale.register(NS, { zh, en }), "account-login: dictionaries");
      }
      ctx.slots.inject("settings.section", () =>
        ctx.slots.register({ name: "settings.section", id: "account-login", order: 10, label: "Account 登录", icon: "user", locale: NS, inject: () => ({}) }, AccountLoginPage)
      );
    }

    module.exports = {
      name: "account-login",
      inject: ["slots", "locale"],
      apply,
    };
    return module.exports;
  },
});
