import express from "express";

const app = express();

app.get("/status", (_req, res) => res.json({ ok: true }));
app.post("/upload", (_req, res) => res.sendStatus(204));

export function remember(token: string) {
  localStorage.setItem("session_token", token);
  sessionStorage.setItem("tab_id", "x");
  Cookies.set("consent", "yes");
}
