import { Box, Button, CircularProgress, Stack, TextField, Typography } from '@mui/material';
import SaveIcon from '@mui/icons-material/Save';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';

// DelayFields = kontrol "Jeda Kirim" yang dipakai bersama oleh Broadcast & Jadwal:
// jeda acak antar pesan + jeda istirahat berkala. Validasi tetap di parent (lewat `error`).
//
// onSave (opsional) memunculkan tombol "Simpan sebagai default". Setelan ini disimpan
// PER AKUN, bukan per nomor CS — satu orang bisa memegang beberapa nomor dan ritme
// kirimnya mengikuti orangnya. Tanpa onSave, komponen tampil seperti semula.
export default function DelayFields({
  minDelay, maxDelay, restEvery, restDuration,
  setMinDelay, setMaxDelay, setRestEvery, setRestDuration,
  error, onEditDelay,
  onSave, saving = false, savedAsDefault = false,
}: {
  minDelay: number;
  maxDelay: number;
  restEvery: number;
  restDuration: number;
  setMinDelay: (n: number) => void;
  setMaxDelay: (n: number) => void;
  setRestEvery: (n: number) => void;
  setRestDuration: (n: number) => void;
  error?: string;
  onEditDelay?: () => void;
  onSave?: () => void;
  saving?: boolean;
  savedAsDefault?: boolean;
}) {
  return (
    <Box>
      <Typography variant="subtitle2" sx={{ fontWeight: 700 }}>Jeda antar pesan</Typography>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
        Tunggu beberapa detik (acak) sebelum mengirim ke nomor berikutnya.
      </Typography>
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}>
        <TextField type="number" size="small" label="Minimal (detik)" value={minDelay}
          onChange={e => { setMinDelay(Number(e.target.value)); onEditDelay?.(); }}
          error={!!error}
          sx={{ width: { xs: '100%', sm: 160 } }} />
        <TextField type="number" size="small" label="Maksimal (detik)" value={maxDelay}
          onChange={e => { setMaxDelay(Number(e.target.value)); onEditDelay?.(); }}
          error={!!error}
          helperText={error || ' '}
          sx={{ width: { xs: '100%', sm: 160 } }} />
      </Stack>

      <Typography variant="subtitle2" sx={{ fontWeight: 700, mt: 1.5 }}>Jeda istirahat</Typography>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
        Berhenti sejenak setelah mengirim sejumlah pesan, lalu lanjut otomatis. Isi 0 untuk mematikan.
      </Typography>
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}>
        <TextField type="number" size="small" label="Berhenti setiap (pesan)" value={restEvery}
          onChange={e => setRestEvery(Math.max(0, Number(e.target.value)))}
          helperText={restEvery <= 0 ? 'Mati' : ' '}
          sx={{ width: { xs: '100%', sm: 190 } }} />
        <TextField type="number" size="small" label="Lama berhenti (detik)" value={restDuration}
          onChange={e => setRestDuration(Math.max(0, Number(e.target.value)))}
          disabled={restEvery <= 0}
          helperText=" "
          sx={{ width: { xs: '100%', sm: 190 } }} />
      </Stack>

      {onSave && (
        <Stack
          direction={{ xs: 'column', sm: 'row' }}
          spacing={1}
          sx={{ alignItems: { xs: 'stretch', sm: 'center' }, mt: 0.5 }}
        >
          <Button
            size="small"
            variant="outlined"
            onClick={onSave}
            // Jeda yang tidak masuk akal jangan sampai tersimpan jadi default —
            // kesalahannya akan terbawa ke semua blast berikutnya.
            disabled={saving || !!error}
            startIcon={saving ? <CircularProgress size={14} color="inherit" /> : <SaveIcon fontSize="small" />}
          >
            {saving ? 'Menyimpan…' : 'Simpan sebagai default'}
          </Button>
          {savedAsDefault && !saving && (
            <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
              <CheckCircleIcon color="success" sx={{ fontSize: 16 }} />
              <Typography variant="caption" color="text.secondary">
                Setelan ini sudah jadi default kamu
              </Typography>
            </Stack>
          )}
        </Stack>
      )}

      {onSave && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.75 }}>
          Tersimpan di akunmu, jadi ikut terpakai di semua nomor yang kamu pegang — di tab Blast maupun Jadwal Blast.
        </Typography>
      )}
    </Box>
  );
}
