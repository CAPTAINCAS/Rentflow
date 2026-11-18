"use client";
import { useState } from "react";
import { api } from "@/lib/api";

export default function ActivatePage() {
  const [token, setToken] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [status, setStatus] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [message, setMessage] = useState("");

  // read token from query on mount
  if (typeof window !== "undefined" && !token) {
    const t = new URLSearchParams(window.location.search).get("token");
    if (t) setToken(t);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (password !== confirm) {
      setStatus("error");
      setMessage("Passwords do not match");
      return;
    }
    setStatus("loading");
    try {
      await api("/auth/activate", { method: "POST", body: JSON.stringify({ token, password }) });
      setStatus("success");
      setMessage("Account activated. You can now log in.");
    } catch (err: any) {
      setStatus("error");
      setMessage(err?.message || "Activation failed");
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <form onSubmit={submit} className="w-full max-w-md space-y-4">
        <h1 className="text-2xl font-bold">Activate your account</h1>
        {status === "success" && <p className="text-green-600">{message}</p>}
        {status === "error" && <p className="text-red-600">{message}</p>}
        <input type="hidden" value={token} />
        <input
          type="password"
          placeholder="New password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="w-full border p-2 rounded"
          required
        />
        <input
          type="password"
          placeholder="Confirm password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          className="w-full border p-2 rounded"
          required
        />
        <button
          type="submit"
          disabled={status === "loading"}
          className="w-full bg-blue-600 text-white p-2 rounded"
        >
          {status === "loading" ? "Activating..." : "Activate"}
        </button>
      </form>
    </div>
  );
}
