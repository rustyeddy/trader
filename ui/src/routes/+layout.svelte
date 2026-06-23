<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { QueryClientProvider } from '@tanstack/svelte-query';
  import { queryClient } from '$lib/query';
  import { page } from '$app/stores';
  import { accounts, selectedAccountId, loadAccounts } from '$lib/account';

  const navLinks = [
    { href: '/',           label: 'Dashboard' },
    { href: '/trades',     label: 'Trades' },
    { href: '/backtests',  label: 'Backtests' },
    { href: '/charts',     label: 'Charts' },
    { href: '/replay',     label: 'Replay' },
  ];

  // Populate the account dropdown once on load. Failures (e.g. a backtest-only
  // server with no OANDA token) leave the list empty and the picker hidden.
  onMount(() => {
    loadAccounts().catch(() => { /* no OANDA — picker stays hidden */ });
  });
</script>

<QueryClientProvider client={queryClient}>
  <div class="flex h-screen">
    <!-- Sidebar -->
    <nav class="w-48 bg-surface-raised border-r border-surface-border flex flex-col shrink-0">
      <div class="px-4 py-5 border-b border-surface-border">
        <span class="text-accent font-bold text-lg tracking-wide">Trader</span>
      </div>

      <!-- Account picker — drives every account-scoped view + stream. -->
      {#if $accounts.length > 0}
        <div class="px-3 py-3 border-b border-surface-border">
          <label class="block text-[10px] uppercase tracking-wider text-slate-500 mb-1" for="account-select">
            Account
          </label>
          <select
            id="account-select"
            bind:value={$selectedAccountId}
            class="w-full bg-surface border border-surface-border rounded px-2 py-1.5
                   text-xs font-mono text-slate-200 focus:outline-none focus:border-accent"
          >
            {#each $accounts as a (a.id)}
              <option value={a.id}>{a.id}{a.is_default ? ' (default)' : ''}</option>
            {/each}
          </select>
        </div>
      {/if}
      <ul class="flex-1 py-3 space-y-1 px-2">
        {#each navLinks as link}
          <li>
            <a
              href={link.href}
              class="block px-3 py-2 rounded text-sm transition-colors
                     {$page.url.pathname === link.href
                       ? 'bg-accent/10 text-accent font-medium'
                       : 'text-slate-400 hover:text-slate-100 hover:bg-surface-border'}"
            >
              {link.label}
            </a>
          </li>
        {/each}
      </ul>
      <div class="px-4 py-3 border-t border-surface-border text-xs text-slate-500">
        CLI: <code class="text-slate-400">trader serve</code>
      </div>
    </nav>

    <!-- Main content -->
    <main class="flex-1 overflow-auto p-6">
      <slot />
    </main>
  </div>
</QueryClientProvider>
