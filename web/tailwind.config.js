/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // Calm light surfaces, following the AI Agent Lens reference.
        ink: {
          950: "#f7f9f8",
          900: "#ffffff",
          850: "#f1f5f2",
          800: "#e0e7e2",
          700: "#cdd9d1",
          600: "#8c9e92",
          500: "#718077",
          400: "#59695f",
          300: "#3e5146",
          200: "#293e32",
          100: "#1c3025",
        },
        // The accent: a living green, for "life".
        vital: {
          600: "#295542",
          500: "#356b56",
          400: "#356b56",
          300: "#295542",
        },
        // Warm secondary for food and calories.
        ember: {
          500: "#b58438",
          400: "#a37025",
        },
        sky: {
          500: "#5b8caa",
          400: "#427894",
        },
        rose: {
          500: "#be5162",
          400: "#a93f51",
        },
      },
      fontFamily: {
        sans: [
          "Inter",
          "-apple-system",
          "BlinkMacSystemFont",
          "Segoe UI",
          "Roboto",
          "sans-serif",
        ],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
      boxShadow: {
        glow: "0 0 0 1px rgba(25,200,150,0.25), 0 20px 60px -20px rgba(25,200,150,0.35)",
      },
      keyframes: {
        pop: {
          "0%": { transform: "scale(1)" },
          "40%": { transform: "scale(1.12)" },
          "70%": { transform: "scale(0.97)" },
          "100%": { transform: "scale(1)" },
        },
        "slide-up": {
          "0%": { transform: "translateY(12px)", opacity: "0" },
          "100%": { transform: "translateY(0)", opacity: "1" },
        },
        "fade-in": {
          "0%": { opacity: "0" },
          "100%": { opacity: "1" },
        },
      },
      animation: {
        pop: "pop 420ms cubic-bezier(0.34, 1.56, 0.64, 1)",
        "slide-up": "slide-up 280ms cubic-bezier(0.16, 1, 0.3, 1) both",
        "fade-in": "fade-in 200ms ease-out both",
      },
    },
  },
  plugins: [],
};
