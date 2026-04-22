import { describe, expect, it } from 'vitest';
import { createCodeVerifier, deriveCodeChallenge } from '../src/lib/pkce';

describe('pkce', () => {
  it('verifier is 43-128 unreserved chars per RFC 7636', () => {
    const v = createCodeVerifier();
    expect(v.length).toBeGreaterThanOrEqual(43);
    expect(v.length).toBeLessThanOrEqual(128);
    // unreserved = ALPHA / DIGIT / "-" / "." / "_" / "~"
    expect(v).toMatch(/^[A-Za-z0-9\-._~]+$/);
  });

  it('two verifiers differ', () => {
    expect(createCodeVerifier()).not.toBe(createCodeVerifier());
  });

  it('challenge = base64url(sha256(verifier))', async () => {
    // Known vector from RFC 7636 §4.2.
    const verifier = 'dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk';
    const challenge = await deriveCodeChallenge(verifier);
    expect(challenge).toBe('E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM');
  });

  it('challenge is base64url (no =, +, /)', async () => {
    const challenge = await deriveCodeChallenge(createCodeVerifier());
    expect(challenge).toMatch(/^[A-Za-z0-9\-_]+$/);
  });
});
