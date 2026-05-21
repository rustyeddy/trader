/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{html,js,svelte,ts}'],
  theme: {
    extend: {
      colors: {
        // Trader brand palette — dark dashboard aesthetic.
        surface: {
          DEFAULT: '#0f172a',  // slate-900
          raised: '#1e293b',   // slate-800
          border: '#334155',   // slate-700
        },
        accent: {
          DEFAULT: '#38bdf8',  // sky-400
          dim: '#0ea5e9',      // sky-500
        },
        profit: '#4ade80',     // green-400
        loss:   '#f87171',     // red-400
      },
    },
  },
  plugins: [],
};
