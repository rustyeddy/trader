<script lang="ts">
  import { createEventDispatcher } from 'svelte';

  export let open = false;
  export let title = 'Confirm';
  export let message = 'Are you sure?';
  export let confirmLabel = 'Confirm';
  export let danger = false;

  const dispatch = createEventDispatcher<{ confirm: void; cancel: void }>();

  function confirm() { open = false; dispatch('confirm'); }
  function cancel()  { open = false; dispatch('cancel'); }

  function onKeydown(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === 'Escape') cancel();
    if (e.key === 'Enter')  confirm();
  }
</script>

<svelte:window on:keydown={onKeydown} />

{#if open}
  <!-- Backdrop -->
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div
    class="fixed inset-0 z-40 bg-black/60 flex items-center justify-center"
    on:click|self={cancel}
  >
    <div class="z-50 bg-surface-raised border border-surface-border rounded-lg p-6 w-full max-w-sm shadow-xl">
      <h3 class="text-base font-semibold text-slate-100 mb-2">{title}</h3>
      <p class="text-sm text-slate-400 mb-6">{message}</p>
      <div class="flex justify-end gap-3">
        <button
          on:click={cancel}
          class="px-4 py-1.5 rounded text-sm text-slate-400 hover:text-slate-100
                 border border-surface-border hover:border-slate-500 transition-colors"
        >
          Cancel
        </button>
        <button
          on:click={confirm}
          class="px-4 py-1.5 rounded text-sm font-semibold transition-colors
                 {danger
                   ? 'bg-loss/80 hover:bg-loss text-white'
                   : 'bg-accent text-slate-900 hover:bg-accent-dim'}"
        >
          {confirmLabel}
        </button>
      </div>
    </div>
  </div>
{/if}
