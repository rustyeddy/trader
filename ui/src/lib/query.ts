// Shared TanStack Query client. Import and pass to QueryClientProvider
// in the root layout.
import { QueryClient } from '@tanstack/svelte-query';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 4_000,
      refetchInterval: 5_000,
      retry: 2,
    },
  },
});
