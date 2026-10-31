import { Minus, Plus, RotateCcw } from 'lucide-react'
import { useApp } from '../store'

export function FontSizeControl({ bare }: { bare?: boolean }) {
  const { fontScale, increaseFontScale, decreaseFontScale, resetFontScale } = useApp()
  const percent = Math.round(fontScale * 100)
  return (
    <div className={`text-size-control ${bare ? 'bare' : ''}`} title="Text size (⌘+ / ⌘− / ⌘0)">
      {!bare && (
        <div className="text-size-copy">
          <span className="text-size-label">Text size</span>
          <span className="text-size-hint">Use ⌘+ and ⌘− from anywhere.</span>
        </div>
      )}
      <div className="text-size-buttons">
        <button type="button" className="text-size-btn" onClick={decreaseFontScale} disabled={fontScale <= 0.85} aria-label="Decrease text size">
          <Minus size={13} />
        </button>
        <button type="button" className="text-size-value" onClick={resetFontScale} title="Reset text size">
          {percent}%
        </button>
        <button type="button" className="text-size-btn" onClick={increaseFontScale} disabled={fontScale >= 1.6} aria-label="Increase text size">
          <Plus size={13} />
        </button>
      </div>
      {!bare && (
        <button type="button" className="text-size-reset" onClick={resetFontScale} disabled={fontScale === 1} aria-label="Reset text size">
          <RotateCcw size={12} /> Reset
        </button>
      )}
    </div>
  )
}
