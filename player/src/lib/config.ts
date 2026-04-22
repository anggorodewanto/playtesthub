export type Config = {
  grpcGatewayUrl: string;
  iamBaseUrl: string;
  discordClientId: string;
};

export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigError';
  }
}

const URL_KEYS = ['grpcGatewayUrl', 'iamBaseUrl'] as const;
const REQUIRED_KEYS = [...URL_KEYS, 'discordClientId'] as const;

export function parseConfig(raw: string): Config {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new ConfigError(`config.json is not valid JSON: ${(err as Error).message}`);
  }

  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new ConfigError('config.json must be a JSON object');
  }

  const obj = parsed as Record<string, unknown>;

  for (const key of REQUIRED_KEYS) {
    if (!(key in obj)) {
      throw new ConfigError(`config.json missing required key: ${key}`);
    }
    if (typeof obj[key] !== 'string') {
      throw new ConfigError(`config.json key ${key} must be a string`);
    }
  }

  for (const key of URL_KEYS) {
    const value = obj[key] as string;
    try {
      // eslint-disable-next-line no-new
      new URL(value);
    } catch {
      throw new ConfigError(`config.json ${key} is not a valid URL: ${value}`);
    }
  }

  const discordClientId = obj.discordClientId as string;
  if (discordClientId.length === 0) {
    throw new ConfigError('config.json discordClientId must not be empty');
  }

  return {
    grpcGatewayUrl: obj.grpcGatewayUrl as string,
    iamBaseUrl: obj.iamBaseUrl as string,
    discordClientId,
  };
}

export async function loadConfig(url: string = '/config.json'): Promise<Config> {
  const res = await fetch(url, { cache: 'no-store' });
  if (!res.ok) {
    throw new ConfigError(`config.json fetch failed: ${res.status} ${res.statusText}`);
  }
  const raw = await res.text();
  return parseConfig(raw);
}
