export interface OAuthAttempt {
  provider: string;
  url: string;
  state: string;
}

export function safeOAuthLink(url: string): boolean {
  try {
    const parsed = new URL(url);
    return (
      parsed.protocol === "https:" &&
      Boolean(parsed.hostname) &&
      !parsed.username &&
      !parsed.password
    );
  } catch {
    return false;
  }
}
