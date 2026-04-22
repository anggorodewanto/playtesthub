<script lang="ts">
  import type { Config } from '../lib/config';
  import { ApiError, submitSignup, type Platform } from '../lib/api';
  import { PLATFORM_OPTIONS } from '../lib/platforms';
  import { getAccessToken } from '../lib/auth';
  import { navigate } from '../lib/router';

  let { config, slug }: { config: Config; slug: string } = $props();

  let selected = $state<Set<Platform>>(new Set());
  let submitting = $state(false);
  let errorMessage = $state<string | null>(null);

  // If the user arrives without a token (direct link), bounce them back
  // to the landing so they can run the Discord login flow.
  const hasToken = getAccessToken() !== null;
  if (!hasToken && typeof window !== 'undefined') {
    navigate(`/playtest/${slug}`);
  }

  function togglePlatform(p: Platform) {
    const next = new Set(selected);
    if (next.has(p)) next.delete(p);
    else next.add(p);
    selected = next;
  }

  async function handleSubmit(event: Event) {
    event.preventDefault();
    if (selected.size === 0) {
      errorMessage = 'Select at least one platform.';
      return;
    }
    errorMessage = null;
    submitting = true;
    try {
      await submitSignup(config, slug, { platforms: Array.from(selected) });
      navigate(`/playtest/${slug}/pending`);
    } catch (err) {
      if (err instanceof ApiError) {
        errorMessage =
          err.status === 401
            ? 'Your session has expired — please sign in again.'
            : 'Could not submit signup — please try again.';
      } else {
        errorMessage = 'Could not submit signup — please try again.';
      }
    } finally {
      submitting = false;
    }
  }
</script>

<main class="mx-auto max-w-lg p-6 md:p-10">
  <h1 class="text-2xl font-semibold">Sign up for {slug}</h1>
  <p class="mt-2 text-sm text-slate-600">
    Tell us which platforms you own so the studio can match testers to their targets.
  </p>
  <form class="mt-6 space-y-4" onsubmit={handleSubmit}>
    <fieldset>
      <legend class="font-medium">Platforms you own</legend>
      <div class="mt-2 space-y-2">
        {#each PLATFORM_OPTIONS as opt (opt.value)}
          <label class="flex items-center gap-2">
            <input
              type="checkbox"
              name="platforms"
              value={opt.value}
              checked={selected.has(opt.value)}
              onchange={() => togglePlatform(opt.value)}
            />
            <span>{opt.label}</span>
          </label>
        {/each}
      </div>
    </fieldset>
    {#if errorMessage}
      <p data-testid="signup-error" class="text-sm text-red-700" role="alert">{errorMessage}</p>
    {/if}
    <button
      type="submit"
      disabled={submitting}
      class="rounded bg-slate-900 px-4 py-2 text-white hover:bg-slate-700 disabled:opacity-50"
    >
      {submitting ? 'Submitting…' : 'Submit application'}
    </button>
  </form>
</main>
