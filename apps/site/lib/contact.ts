// The deployment operator supplies their own publicly visible support mailbox.
export function supportEmail() {
  const email = process.env.NEXT_PUBLIC_SUPPORT_EMAIL?.trim();
  return email && /^[^\s<>@?&#]+@[^\s<>@?&#]+\.[^\s<>@?&#]+$/.test(email)
    ? email
    : undefined;
}
