/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: {
          DEFAULT: '#0c0c14',
          secondary: '#13131e',
          card: '#16162a',
          hover: '#1c1c30',
        },
        border: {
          DEFAULT: '#242438',
          bright: '#32325a',
          glow: '#4f4f8f',
        },
        accent: {
          green: '#22c55e',
          'green-dim': '#16a34a',
          purple: '#a855f7',
          'purple-dim': '#7c3aed',
          blue: '#3b82f6',
          orange: '#f97316',
          red: '#ef4444',
          cyan: '#06b6d4',
        },
        text: {
          primary: '#e2e8f0',
          secondary: '#94a3b8',
          muted: '#475569',
          code: '#a5f3fc',
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', '"Fira Code"', 'Consolas', 'monospace'],
      },
      animation: {
        'fade-in': 'fadeIn 0.4s ease-out',
        'slide-up': 'slideUp 0.4s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'glow': 'glow 2s ease-in-out infinite alternate',
        'spin-slow': 'spin 8s linear infinite',
        'terminal-blink': 'blink 1.2s step-end infinite',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(12px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        glow: {
          '0%': { boxShadow: '0 0 5px #22c55e33, 0 0 10px #22c55e22' },
          '100%': { boxShadow: '0 0 20px #22c55e66, 0 0 40px #22c55e33' },
        },
        blink: {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0' },
        },
      },
      backgroundImage: {
        'grid-pattern': `
          linear-gradient(rgba(34,197,94,0.03) 1px, transparent 1px),
          linear-gradient(90deg, rgba(34,197,94,0.03) 1px, transparent 1px)
        `,
        'hero-gradient': 'radial-gradient(ellipse 80% 50% at 50% -20%, rgba(34,197,94,0.15), transparent)',
        'card-gradient': 'linear-gradient(135deg, #16162a 0%, #13131e 100%)',
      },
      backgroundSize: {
        'grid': '32px 32px',
      },
    },
  },
  plugins: [],
}
