import { useState } from 'react';
import { TextField, type TextFieldProps } from '@mui/material';

// TextField select yang menunya dibuka saat klik selesai (mouseup), bukan saat mousedown.
// Select MUI v9 membuka menu di mousedown lalu memilih item di bawah kursor saat mouse dilepas
// >200 ms kemudian, sehingga klik agak pelan terasa "langsung kepilih" (seperti double click).
// Tampilan & pembukaan lewat keyboard (Enter/Space/panah) tetap sama seperti Select biasa.
export default function DropdownField(props: TextFieldProps) {
  const [open, setOpen] = useState(false);
  return (
    <TextField
      {...props}
      select
      slotProps={{
        ...props.slotProps,
        select: {
          open,
          onOpen: () => setOpen(true),
          onClose: () => setOpen(false),
          SelectDisplayProps: {
            onMouseDown: e => { e.preventDefault(); e.currentTarget.focus(); },
            onClick: () => { if (!props.disabled) setOpen(true); },
          },
        },
      }}
    />
  );
}
