"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { api, asRecord } from "@/lib/api";
import { Field } from "@/components/ui";

type Mode = "login" | "register" | "forgot" | "reset";
const schemas: Record<Mode, z.ZodTypeAny> = {
  login: z.object({ email: z.string().email("Enter a valid email address"), password: z.string().min(1, "Enter your password") }),
  register: z.object({ name: z.string().trim().min(2, "Enter your name"), email: z.string().email("Enter a valid email address"), password: z.string().min(12, "Use at least 12 characters").max(128, "Use at most 128 characters"), confirm: z.string() }).refine(d => d.password === d.confirm, { message: "Passwords do not match", path: ["confirm"] }),
  forgot: z.object({ email: z.string().email("Enter a valid email address") }),
  reset: z.object({ token: z.string().min(1, "Enter the reset token"), password: z.string().min(12, "Use at least 12 characters").max(128, "Use at most 128 characters"), confirm: z.string() }).refine(d => d.password === d.confirm, { message: "Passwords do not match", path: ["confirm"] }),
};
const config: Record<Mode, { title: string; subtitle: string; endpoint: string; submit: string; fields: { name: string; label: string; type: string; autocomplete?: string; hint?: string }[]; success: string; link?: { href: string; label: string } }> = {
  login: { title: "Sign in", subtitle: "Continue to your live property and payment records.", endpoint: "/auth/login", submit: "Sign in", fields: [{ name: "email", label: "Email address", type: "email", autocomplete: "email" }, { name: "password", label: "Password", type: "password", autocomplete: "current-password" }], success: "You’re signed in.", link: { href: "/forgot-password", label: "Forgot your password?" } },
  register: { title: "Create your account", subtitle: "Set up a RentFlow account for your property operations.", endpoint: "/auth/register", submit: "Create account", fields: [{ name: "name", label: "Full name", type: "text", autocomplete: "name" }, { name: "email", label: "Email address", type: "email", autocomplete: "email" }, { name: "password", label: "Password", type: "password", autocomplete: "new-password", hint: "12–128 characters." }, { name: "confirm", label: "Confirm password", type: "password", autocomplete: "new-password" }], success: "Account created." },
  forgot: { title: "Reset your password", subtitle: "Request a password reset using the email on your account.", endpoint: "/auth/forgot-password", submit: "Send reset request", fields: [{ name: "email", label: "Email address", type: "email", autocomplete: "email" }], success: "If the account is eligible, reset instructions have been requested." },
  reset: { title: "Choose a new password", subtitle: "Enter the reset token and a new password.", endpoint: "/auth/reset-password", submit: "Reset password", fields: [{ name: "token", label: "Reset token", type: "text", autocomplete: "one-time-code" }, { name: "password", label: "New password", type: "password", autocomplete: "new-password", hint: "12–128 characters." }, { name: "confirm", label: "Confirm password", type: "password", autocomplete: "new-password" }], success: "Password reset completed." },
};
export function AuthForm({ mode }: { mode: Mode }) {
  const router = useRouter(); const qc = useQueryClient(); const item = config[mode]; const [notice, setNotice] = useState("");
  const form = useForm<Record<string, string>>({ resolver: zodResolver(schemas[mode]), defaultValues: Object.fromEntries(item.fields.map(field => [field.name, ""])) });
  const { setValue } = form;
  useEffect(() => {
    if (mode !== "reset") return;
    const token = new URLSearchParams(window.location.search).get("token");
    if (token) setValue("token", token, { shouldValidate: false });
  }, [mode, setValue]);
  const mutation = useMutation({
    mutationFn: (values: Record<string, string>) => api(item.endpoint, { method: "POST", body: JSON.stringify(values) }),
    onSuccess: async () => {
      setNotice(item.success);
      if (mode === "login" || mode === "register") {
        const response = await api<unknown>("/auth/me"); const record = asRecord(response); const user = asRecord(record?.user) ?? asRecord(record?.account) ?? record ?? {};
        qc.setQueryData(["session"], user); const role = String(user.role ?? "").toLowerCase(); router.replace(role === "tenant" ? "/tenant" : "/dashboard"); router.refresh();
      } else if (mode === "reset") router.replace("/login");
    },
  });
  return <main className="auth-layout"><Link href="/" className="brand" aria-label="RentFlow"><svg viewBox="0 0 38 38" aria-hidden="true"><path d="M4 17 19 6l15 11M9 16v17h20V16M14 22h10M14 27h8M25 30l2 3 2-3" /></svg><span>rent<span>flow</span></span></Link><section className="auth-copy"><p className="eyebrow">ACCOUNT ACCESS</p><h1>{item.title}</h1><p>{item.subtitle}</p><div className="auth-card">{mutation.isError && <div className="notice notice-error auth-error" role="alert">{(mutation.error as Error).message}</div>}{notice && <div className="notice notice-success auth-error" role="status">{notice}</div>}<form onSubmit={form.handleSubmit(values => mutation.mutate(values))}>{item.fields.map(field => <Field key={field.name} label={field.label} hint={field.hint} error={form.formState.errors[field.name]?.message as string | undefined}><input type={field.type} autoComplete={field.autocomplete} {...form.register(field.name)} /></Field>)}<button className="button button-primary" style={{ width: "100%", marginTop: 5 }} disabled={mutation.isPending}>{mutation.isPending ? "Please wait…" : item.submit}</button></form>{item.link && <p className="auth-foot"><Link className="text-link" href={item.link.href}>{item.link.label}</Link></p>}</div><p className="auth-foot">{mode === "login" ? <>No account yet? <Link className="text-link" href="/register">Create one</Link></> : <>Already have an account? <Link className="text-link" href="/login">Sign in</Link></>}</p></section></main>;
}
