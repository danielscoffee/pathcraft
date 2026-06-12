import { defineConfig, presetWind3 } from 'unocss'

export default defineConfig({
  presets: [presetWind3()],
  theme: {
    colors: {
      paper: {
        DEFAULT: '#f5efe2',
        deep: '#ece2cd',
        edge: '#d8cdb8',
      },
      ink: {
        DEFAULT: '#211c14',
        soft: '#4d463a',
        faint: '#80766a',
      },
      accent: {
        DEFAULT: '#c2451e',
        deep: '#9c3415',
      },
      ok: '#2f7d4f',
      err: '#b3261e',
    },
    fontFamily: {
      display: '"Fraunces", Georgia, "Times New Roman", serif',
      sans: '"Archivo", system-ui, sans-serif',
      mono: '"IBM Plex Mono", ui-monospace, SFMono-Regular, monospace',
    },
  },
})
