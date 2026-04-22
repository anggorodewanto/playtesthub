import { describe, expect, it } from 'vitest';
import { parseRoute, type Route } from '../src/lib/router';

describe('parseRoute', () => {
  const cases: Array<[string, Route]> = [
    ['', { name: 'not-found' }],
    ['#/', { name: 'not-found' }],
    ['#/playtest/demo', { name: 'landing', slug: 'demo' }],
    ['#/playtest/demo/', { name: 'landing', slug: 'demo' }],
    ['#/playtest/demo/signup', { name: 'signup', slug: 'demo' }],
    ['#/playtest/demo/pending', { name: 'pending', slug: 'demo' }],
    [
      '#/callback?code=abc&state=xyz',
      { name: 'callback', params: { code: 'abc', state: 'xyz' } },
    ],
    ['#/callback', { name: 'callback', params: {} }],
    ['#/totally-unknown', { name: 'not-found' }],
    ['#/playtest/', { name: 'not-found' }],
  ];

  it.each(cases)('%s → %o', (input, expected) => {
    expect(parseRoute(input)).toEqual(expected);
  });

  it('URL-decodes the slug', () => {
    expect(parseRoute('#/playtest/a%2Fb/signup')).toEqual({ name: 'signup', slug: 'a/b' });
  });
});
