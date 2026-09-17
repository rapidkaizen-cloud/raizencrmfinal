import { useState, useRef, useCallback } from 'react';
import { Box, ToggleButton, ToggleButtonGroup, TextField, Typography } from '@mui/material';
import FormatBoldIcon from '@mui/icons-material/FormatBold';
import FormatItalicIcon from '@mui/icons-material/FormatItalic';
import StrikethroughSIcon from '@mui/icons-material/StrikethroughS';
import CodeIcon from '@mui/icons-material/Code';

const FORMATS = [
  { key: 'bold', icon: <FormatBoldIcon fontSize="small" />, label: 'Bold', wrapper: '*' },
  { key: 'italic', icon: <FormatItalicIcon fontSize="small" />, label: 'Italic', wrapper: '_' },
  { key: 'strike', icon: <StrikethroughSIcon fontSize="small" />, label: 'Coret', wrapper: '~' },
  { key: 'mono', icon: <CodeIcon fontSize="small" />, label: 'Monospace', wrapper: '```' },
];

/** Tipe data drag untuk chip variabel pesan; nilainya teks yang disisipkan, mis. "{nama}". */
export const VAR_DRAG_TYPE = 'application/x-wa-var';

const MIRROR_STYLES = [
  'fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'letterSpacing', 'lineHeight', 'textTransform', 'wordSpacing',
  'textIndent', 'tabSize', 'boxSizing', 'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft',
  'borderTopWidth', 'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth',
] as const;

// indexFromPoint = indeks karakter textarea di bawah titik layar (x, y).
// Jalur utama: document.caretPositionFromPoint (mengenali textarea di Chrome 128+/Firefox).
// Cadangan: div cermin transparan dengan tipografi & scroll yang sama, lalu caretRangeFromPoint.
function indexFromPoint(el: HTMLTextAreaElement, x: number, y: number): number {
  const doc = document as Document & {
    caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null;
    caretRangeFromPoint?: (x: number, y: number) => Range | null;
  };
  const pos = doc.caretPositionFromPoint?.(x, y);
  if (pos && pos.offsetNode === el) return Math.min(pos.offset, el.value.length);
  if (!doc.caretRangeFromPoint) return el.value.length;

  const cs = getComputedStyle(el);
  const rect = el.getBoundingClientRect();
  const mirror = document.createElement('div');
  for (const p of MIRROR_STYLES) mirror.style[p] = cs[p];
  Object.assign(mirror.style, {
    position: 'fixed', left: `${rect.left}px`, top: `${rect.top}px`, width: `${rect.width}px`, height: `${rect.height}px`,
    overflow: 'hidden', whiteSpace: 'pre-wrap', overflowWrap: 'break-word', borderStyle: 'solid', borderColor: 'transparent',
    opacity: '0', zIndex: '2147483647',
  });
  mirror.textContent = el.value + '​';
  document.body.appendChild(mirror);
  mirror.scrollTop = el.scrollTop;
  const range = doc.caretRangeFromPoint(x, y);
  mirror.remove();
  return range && mirror.contains(range.startContainer) ? Math.min(range.startOffset, el.value.length) : el.value.length;
}

interface Props {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  rows?: number;
  error?: boolean;
  helperText?: string;
}

export default function WhatsAppEditor({ value, onChange, placeholder, rows = 4, error, helperText }: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [preview, setPreview] = useState(false);

  const updateCursor = (el: HTMLTextAreaElement, selStart: number, selEnd: number) => {
    // Pastikan fokus dulu, lalu set selection range.
    // Pakai requestAnimationFrame hanya untuk memastikan browser sudah selesai layout.
    el.focus();
    requestAnimationFrame(() => el.setSelectionRange(selStart, selEnd));
  };

  const insertFormat = useCallback((wrapper: string) => {
    const el = textareaRef.current;
    if (!el) return;
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const wlen = wrapper.length;
    const hasSelection = start !== end;

    // Cek apakah teks yang dipilih / sekitar kursor sudah dibungkus wrapper.
    // Kalau iya → UNTOGGLE (hapus wrapper), kalau tidak → tambahkan.
    const before = value.substring(start - wlen, start);
    const after = value.substring(end, end + wlen);
    const selected = value.substring(start, end);
    const alreadyWrapped = before === wrapper && after === wrapper;

    let newText: string;
    let newStart: number;
    let newEnd: number;

    if (alreadyWrapped) {
      // Hapus wrapper di kiri & kanan
      newText = value.substring(0, start - wlen) + selected + value.substring(end + wlen);
      newStart = start - wlen;
      newEnd = end - wlen;
    } else {
      // Tambahkan wrapper
      const inner = hasSelection ? selected : 'teks';
      newText = value.substring(0, start) + wrapper + inner + wrapper + value.substring(end);
      newStart = start + wlen;
      newEnd = newStart + inner.length;
    }

    // 1. Update DOM langsung (imperative)
    el.value = newText;

    // 2. Set selection
    updateCursor(el, newStart, newEnd);

    // 3. Sync ke React
    onChange(newText);
  }, [value, onChange]);

  const renderPreview = (text: string) => {
    let html = text
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/\n/g, '<br>');
    html = html.replace(/```(.+?)```/g, '<code style="background:#edf2ed;padding:1px 4px;border-radius:3px;font-family:monospace">$1</code>');
    html = html.replace(/\*(.+?)\*/g, '<b>$1</b>');
    html = html.replace(/_(.+?)_/g, '<i>$1</i>');
    html = html.replace(/~(.+?)~/g, '<s>$1</s>');
    return html;
  };

  // Chip variabel yang diseret (VAR_DRAG_TYPE) ditangani sendiri, tidak lewat drop bawaan textarea
  // (perilakunya beda antar browser dan sering menolak drop). Saat diseret di atas textarea, caret
  // mengikuti kursor; saat dilepas, variabel disisipkan di posisi huruf itu. Di area editor lain
  // (toolbar, tepi, pratinjau) variabel ditambahkan di akhir. Seret teks biasa tetap perilaku browser.
  const isVarDrag = (e: React.DragEvent) => e.dataTransfer.types.includes(VAR_DRAG_TYPE);
  const dropIndex = (e: React.DragEvent) => {
    const el = textareaRef.current;
    return el && e.target === el ? indexFromPoint(el, e.clientX, e.clientY) : value.length;
  };

  return (
    <Box
      onDragOver={e => {
        if (!isVarDrag(e)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = 'copy';
        const el = textareaRef.current;
        if (el && e.target === el) {
          const idx = dropIndex(e);
          if (document.activeElement !== el) el.focus();
          if (el.selectionStart !== idx || el.selectionEnd !== idx) el.setSelectionRange(idx, idx);
        }
      }}
      onDrop={e => {
        if (!isVarDrag(e)) return;
        e.preventDefault();
        const text = e.dataTransfer.getData(VAR_DRAG_TYPE);
        if (!text) return;
        const idx = dropIndex(e);
        const el = textareaRef.current;
        const next = value.slice(0, idx) + text + value.slice(idx);
        if (el) { el.value = next; updateCursor(el, idx + text.length, idx + text.length); }
        onChange(next);
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
        <ToggleButtonGroup size="small" exclusive={false}>
          {FORMATS.map(f => (
            <ToggleButton
              key={f.key}
              value={f.key}
              aria-label={f.label}
              onMouseDown={e => e.preventDefault()} // cegah textarea kehilangan fokus
              onClick={() => insertFormat(f.wrapper)}
              sx={{ px: 1, minWidth: 36 }}
            >
              {f.icon}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
        <Typography
          variant="caption"
          color="primary"
          onClick={() => setPreview(!preview)}
          sx={{ cursor: 'pointer', userSelect: 'none', fontWeight: 600 }}
        >
          {preview ? 'Edit' : 'Pratinjau'}
        </Typography>
      </Box>
      {preview ? (
        <Box
          sx={{
            minHeight: rows * 24 + 16,
            maxHeight: 360,
            overflowY: 'auto',
            p: 1.5,
            border: '1px solid',
            borderColor: error ? 'error.main' : 'divider',
            borderRadius: 1,
            fontSize: '0.9rem',
            lineHeight: 1.6,
            whiteSpace: 'pre-wrap',
          }}
          dangerouslySetInnerHTML={{ __html: renderPreview(value) || '<span style="color:#aaa">Ketik pesan…</span>' }}
        />
      ) : (
        <TextField
          fullWidth
          multiline
          rows={rows}
          size="small"
          value={value}
          onChange={e => onChange(e.target.value)}
          placeholder={placeholder || 'Tulis pesan broadcast…'}
          error={error}
          helperText={helperText}
          inputRef={textareaRef}
        />
      )}
    </Box>
  );
}
