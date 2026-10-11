/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: 'class',
  content: [
    "./views/**/*.{templ,go,html}",
    "./cmd/**/*.{templ,go,html}",
    "./internal/**/*.{templ,go,html}"
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['JetBrains Mono', 'Fira Code', 'Menlo', 'Monaco', 'Courier New', 'monospace'],
      },
      colors: {
        // Linear/Vercel zinc dark palette refinement
        canvas: {
          DEFAULT: '#09090b', // zinc-950
          subtle: '#121215',
          elevated: '#18181b', // zinc-900
        },
        surface: {
          DEFAULT: '#18181b', // zinc-900
          border: '#27272a',  // zinc-800
          hover: '#27272a',
          active: '#3f3f46',  // zinc-700
        }
      },
      keyframes: {
        shimmer: {
          '100%': { transform: 'translateX(100%)' }
        },
        pulseSubtle: {
          '0%, 100%': { opacity: 1 },
          '50%': { opacity: 0.4 },
        }
      },
      animation: {
        shimmer: 'shimmer 2s infinite',
        'pulse-subtle': 'pulseSubtle 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      }
    },
  },
  plugins: [],
}
