import { describe, expect, it } from 'vitest';
import { parseConfig, ConfigError } from '../src/lib/config';

describe('parseConfig', () => {
  const valid = {
    grpcGatewayUrl: 'https://api.example.com/playtesthub',
    iamBaseUrl: 'https://iam.example.com',
    discordClientId: 'abc123',
  };

  it('accepts a well-formed config', () => {
    expect(parseConfig(JSON.stringify(valid))).toEqual(valid);
  });

  it('rejects invalid JSON', () => {
    expect(() => parseConfig('{not json')).toThrow(ConfigError);
    expect(() => parseConfig('{not json')).toThrow(/not valid JSON/);
  });

  it('rejects non-object root', () => {
    expect(() => parseConfig('null')).toThrow(ConfigError);
    expect(() => parseConfig('"string"')).toThrow(ConfigError);
    expect(() => parseConfig('[]')).toThrow(ConfigError);
  });

  it.each(['grpcGatewayUrl', 'iamBaseUrl', 'discordClientId'])(
    'rejects missing key %s',
    (key) => {
      const partial = { ...valid } as Record<string, unknown>;
      delete partial[key];
      expect(() => parseConfig(JSON.stringify(partial))).toThrow(
        new RegExp(`missing required key: ${key}`),
      );
    },
  );

  it('rejects grpcGatewayUrl that is not a URL', () => {
    expect(() => parseConfig(JSON.stringify({ ...valid, grpcGatewayUrl: 'not a url' }))).toThrow(
      /grpcGatewayUrl/,
    );
  });

  it('rejects iamBaseUrl that is not a URL', () => {
    expect(() => parseConfig(JSON.stringify({ ...valid, iamBaseUrl: '::::' }))).toThrow(
      /iamBaseUrl/,
    );
  });

  it('rejects empty discordClientId', () => {
    expect(() => parseConfig(JSON.stringify({ ...valid, discordClientId: '' }))).toThrow(
      /discordClientId/,
    );
  });

  it('rejects non-string field types', () => {
    expect(() => parseConfig(JSON.stringify({ ...valid, discordClientId: 42 }))).toThrow(
      ConfigError,
    );
    expect(() => parseConfig(JSON.stringify({ ...valid, grpcGatewayUrl: null }))).toThrow(
      ConfigError,
    );
  });
});
