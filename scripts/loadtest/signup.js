// scripts/loadtest/signup.js — drives the PRD §6 perf scenario.
//
// Usage:
//   k6 run --summary-export=summary.json \
//          -e TOKENS_FILE=.cache/tokens.json \
//          -e BASE_URL=https://...ext-ns-app \
//          -e SLUG=loadtest-<...> \
//          -e DURATION=10m \
//          -e RATE_PER_MIN=50 \
//          scripts/loadtest/signup.js
//
// The runner script (run.sh) populates these env vars; you usually
// don't invoke k6 directly.

import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { Trend } from 'k6/metrics';
import { SharedArray } from 'k6/data';

// Trend captured per request so the report can read p50/p95/p99 of the
// signup path specifically (the built-in http_req_duration covers every
// HTTP call including pre-flights, but here we only fire one).
const signupLatency = new Trend('signup_latency_ms', true);

const BASE_URL = __ENV.BASE_URL;
const SLUG = __ENV.SLUG;
const TOKENS_FILE = __ENV.TOKENS_FILE || '.cache/tokens.json';
const DURATION = __ENV.DURATION || '10m';
const RATE_PER_MIN = parseInt(__ENV.RATE_PER_MIN || '50', 10);

if (!BASE_URL) throw new Error('BASE_URL env var required');
if (!SLUG) throw new Error('SLUG env var required');

// SharedArray loads once per VM and is read-only across VUs; cheap for
// 500-element token lists.
const tokens = new SharedArray('tokens', function () {
  return JSON.parse(open(TOKENS_FILE));
});

if (tokens.length === 0) throw new Error(`no tokens in ${TOKENS_FILE}`);

// constant-arrival-rate gives an iteration cadence independent of
// response latency — the right shape for "500 over 10 min" since we
// want to exercise the system at the PRD-target arrival pattern, not
// drift slower under load.
//
// preAllocatedVUs is sized so a 3s p95 leaves headroom: at 50/min and
// 3s tail, ~3 VUs in flight is the steady state. We give 20× that to
// avoid k6 dropping iterations under transient spikes.
export const options = {
  // Default summary stats omit p99 — opt in explicitly so the report
  // can show the long tail.
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  scenarios: {
    signup: {
      executor: 'constant-arrival-rate',
      rate: RATE_PER_MIN,
      timeUnit: '1m',
      duration: DURATION,
      preAllocatedVUs: 50,
      maxVUs: 200,
      exec: 'signup',
    },
  },
  thresholds: {
    // PRD §6: p95 signup latency < 3s end-to-end. The harness measures
    // backend signup only — Discord OAuth + browser redirect are
    // excluded; see README.md "What it does NOT measure". This
    // threshold covers the part of the budget the backend owns.
    'signup_latency_ms': ['p(95)<3000'],
    // Sanity gate — anything above 1% errors means the run is broken,
    // not interesting.
    'http_req_failed': ['rate<0.01'],
  },
};

// Each iteration consumes one token, deterministically rotating through
// the pool. iterationInTest is a global monotonic counter across all VUs
// — required so two VUs never alias onto the same idx and turn inserts
// into idempotent replays. Iterations beyond tokens.length wrap and hit
// the replay path; with the default LOADTEST_USERS=500 + 500 iterations
// over 10 min, every iteration is a fresh insert.
export function signup() {
  const idx = exec.scenario.iterationInTest % tokens.length;
  const token = tokens[idx];
  const url = `${BASE_URL}/v1/player/playtests/${SLUG}/signup`;
  const body = JSON.stringify({ platforms: ['PLATFORM_STEAM'] });
  const params = {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    tags: { rpc: 'Signup' },
  };
  const res = http.post(url, body, params);
  signupLatency.add(res.timings.duration);
  check(res, {
    'status is 200': (r) => r.status === 200,
    'has applicant.id': (r) => {
      try {
        return !!JSON.parse(r.body).applicant?.id;
      } catch (_) {
        return false;
      }
    },
  });
}
