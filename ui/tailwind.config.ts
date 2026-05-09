import type { Config } from 'tailwindcss';

const config: Config = {
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'sans-serif'],
      },
      colors: {
        surface: { DEFAULT: '#18181b', raised: '#27272a', overlay: '#3f3f46' },
        brand: { DEFAULT: '#10b981', light: '#34d399', dark: '#059669' },
      },
    },
  },
  plugins: [],
};

export default config;
