# The host is the server; the cloud account handles identity, routing, push, and device registry

Users want Codex-style remote control. Codex's phone control connects to the user's running host, which acts as the server: sessions, files, and credentials live on the host, and the phone is a remote view. The cloud account does not host the session state being controlled — it provides identity, relay routing, push notifications, and the device registry. We adopt the same model for dsh: the desktop shell stays the only place dsh runs and the single source of truth; the phone connects to the online host (LAN direct or via relay); "offline" means the host is unreachable, not that there is a cloud copy to browse.

_Status: accepted_
