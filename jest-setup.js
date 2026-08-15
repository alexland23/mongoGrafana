// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';

// .config/jest-setup.js stubs HTMLCanvasElement.getContext() as a no-op returning undefined,
// which is enough for chart rendering but not for @grafana/ui's Combobox, which measures label
// width via CanvasRenderingContext2D.measureText() on mount.
HTMLCanvasElement.prototype.getContext = () => ({
  font: '',
  measureText: (text) => ({ width: text.length * 7 }),
});
