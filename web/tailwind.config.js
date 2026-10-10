/** @type {import('tailwindcss').Config} */
// 视觉方向："账本 × 行情板"——暖色新闻纸、墨色正文、朱红印章作为唯一强调色，等宽数字。
// 覆盖 Tailwind 默认色板（slate / emerald / red / amber / sky / violet / white），让全部页面统一换肤。
const ink = {
  50: '#F7F4EE',
  100: '#EFEAE0',
  200: '#E2DBCD',
  300: '#CFC6B4',
  400: '#A39B8B',
  500: '#7A7366',
  600: '#5A544A',
  700: '#3F3A32',
  800: '#2A261F',
  900: '#17140F',
  950: '#0E0C09',
}
export default {
  content: ['./index.html', './src/**/*.{vue,ts}'],
  theme: {
    extend: {
      colors: {
        white: '#FCFAF5',
        paper: { DEFAULT: '#F3EFE6', deep: '#EAE4D7', card: '#FCFAF5' },
        ink,
        slate: ink,
        // 朱红：品牌与强调
        brand: { 50: '#FBEDE6', 100: '#F6D9CB', 200: '#EDB49A', 300: '#E38D68', 400: '#DD6840', 500: '#D9421B', 600: '#C2391A', 700: '#9F2E14', 800: '#7C2410', 900: '#5A1A0B' },
        vermilion: { DEFAULT: '#D9421B', deep: '#9F2E14', tint: '#F7DFD3' },
        // 石青绿：正常、成功
        emerald: { 50: '#E6F1EB', 100: '#CFE4D8', 200: '#A8CDB9', 300: '#7FB49A', 400: '#4F9177', 500: '#2F7A62', 600: '#1E6B57', 700: '#175746', 800: '#12463A', 900: '#0C2F27' },
        // 朱砂：错误、危险（比品牌朱红更深）
        red: { 50: '#F9E7E4', 100: '#F2CBC5', 200: '#E6A49B', 300: '#D77B6F', 400: '#C65347', 500: '#BA3A2E', 600: '#B3261E', 700: '#941E17', 800: '#761812', 900: '#58120D' },
        // 赭黄：警告
        amber: { 50: '#F8F0DD', 100: '#F0DFB8', 200: '#E4C682', 300: '#D6AB52', 400: '#C8932E', 500: '#B7801A', 600: '#A86F10', 700: '#8A5A0C', 800: '#6B460A', 900: '#4D3207' },
        // 钴蓝：信息、进行中
        sky: { 50: '#E8EDF7', 100: '#D3DCEF', 200: '#A9B9DE', 300: '#7F95CC', 400: '#5672B9', 500: '#3657A8', 600: '#2B4DA0', 700: '#24479A', 800: '#1C3878', 900: '#142856' },
        violet: { 50: '#EDEAF0', 100: '#DAD3E0', 700: '#3D2F47', 800: '#2E2336' },
        // 控制台主色（对齐 YourHub Liquid Glass，与市场朱红 brand 分离）
        primary: {
          50: '#E6EEFA',
          100: '#CDDDF5',
          200: '#9BBBF0',
          300: '#6A9AEB',
          400: '#3878E6',
          500: '#0555CE',
          600: '#0446A7',
          700: '#033880',
          800: '#022A5A',
          900: '#011C33',
          950: '#000E1A',
        },
      },
      fontFamily: {
        display: ['"Noto Serif SC"', '"Songti SC"', '"STSong"', 'serif'],
        sans: ['"Schibsted Grotesk"', '"PingFang SC"', '"Hiragino Sans GB"', '"Microsoft YaHei"', 'sans-serif'],
        mono: ['"Spline Sans Mono"', 'ui-monospace', '"SFMono-Regular"', 'Menlo', 'monospace'],
      },
      borderRadius: { DEFAULT: '3px', md: '3px', lg: '4px', '2xl': '1rem' },
      boxShadow: {
        hard: '4px 4px 0 0 #17140F',
        'hard-sm': '2px 2px 0 0 #17140F',
        'hard-red': '3px 3px 0 0 #D9421B',
        sheet: '0 1px 0 0 #E2DBCD, 0 12px 32px -18px rgba(23, 20, 15, 0.35)',
        primary: '0 4px 16px 0 rgba(5, 85, 206, 0.25)',
        glass: '0 8px 32px rgba(0, 0, 0, 0.08)',
      },
      backgroundImage: {
        'mesh-deep':
          'radial-gradient(ellipse 80% 60% at 20% 10%, rgba(5, 85, 206, 0.12) 0%, transparent 60%), radial-gradient(ellipse 60% 50% at 80% 15%, rgba(56, 120, 230, 0.08) 0%, transparent 55%), radial-gradient(ellipse 50% 40% at 50% 80%, rgba(99, 102, 241, 0.06) 0%, transparent 50%), radial-gradient(ellipse 70% 60% at 10% 60%, rgba(5, 85, 206, 0.08) 0%, transparent 55%), radial-gradient(ellipse 40% 30% at 90% 70%, rgba(56, 120, 230, 0.05) 0%, transparent 45%)',
      },
      keyframes: {
        ticker: { from: { transform: 'translateX(0)' }, to: { transform: 'translateX(-50%)' } },
        rise: { from: { opacity: '0', transform: 'translateY(8px)' }, to: { opacity: '1', transform: 'translateY(0)' } },
        stamp: { '0%': { opacity: '0', transform: 'scale(1.6) rotate(-12deg)' }, '60%': { opacity: '1', transform: 'scale(0.94) rotate(-4deg)' }, '100%': { transform: 'scale(1) rotate(-4deg)' } },
        flip: { '0%': { transform: 'rotateX(90deg)', opacity: '0' }, '100%': { transform: 'rotateX(0)', opacity: '1' } },
        blink: { '0%, 100%': { opacity: '1' }, '50%': { opacity: '0.25' } },
      },
      animation: {
        ticker: 'ticker 60s linear infinite',
        rise: 'rise 0.5s cubic-bezier(0.2, 0.7, 0.2, 1) both',
        stamp: 'stamp 0.7s cubic-bezier(0.2, 0.7, 0.2, 1) both',
        flip: 'flip 0.45s cubic-bezier(0.2, 0.7, 0.2, 1) both',
        blink: 'blink 1.6s ease-in-out infinite',
      },
    },
  },
  plugins: [],
}
