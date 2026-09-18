import { Fragment, useState } from 'react';
import { Box, Chip, Collapse, Stack, Table, TableBody, TableCell, TableHead, TableRow, TextField, Typography } from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import type { BroadcastDetailData } from '../../types';

const RCP_COLOR: Record<string, 'success' | 'warning' | 'error' | 'default'> = {
  sent: 'success', failed: 'error', skipped: 'default', pending: 'warning',
};
const RCP_LABEL: Record<string, string> = {
  sent: 'Terkirim', failed: 'Gagal', skipped: 'Dilewati', pending: 'Menunggu',
};

type Filter = 'all' | 'sent' | 'failed' | 'skipped' | 'pending';

// Daftar penerima satu Blast: filter status, cari nomor, dan buka baris untuk melihat pesan
// persis yang diterima. Dipakai detail Blast biasa dan rincian Blast Multiple Number.
export default function RecipientList({ detail }: { detail: BroadcastDetailData }) {
  const [filter, setFilter] = useState<Filter>('all');
  const [search, setSearch] = useState('');
  // Set (bukan satu id) supaya beberapa varian spin bisa dibandingkan berdampingan.
  const [openMsgIds, setOpenMsgIds] = useState<Set<number>>(new Set());
  const toggleMsg = (id: number) => setOpenMsgIds(prev => {
    const next = new Set(prev);
    if (!next.delete(id)) next.add(id);
    return next;
  });

  const recs = detail.recipients;
  const b = detail.broadcast;
  const q = search.replace(/\D/g, '');
  const shown = recs.filter(r => (filter === 'all' || r.status === filter) && (!q || r.number.includes(q)));
  const FILTERS: { k: Filter; label: string }[] = [
    { k: 'all', label: `Semua ${recs.length}` },
    { k: 'sent', label: `Terkirim ${b.sent}` },
    { k: 'failed', label: `Gagal ${b.failed}` },
    { k: 'skipped', label: `Dilewati ${b.skipped}` },
    { k: 'pending', label: `Menunggu ${Math.max(0, b.total - b.sent - b.failed - b.skipped)}` },
  ];
  const rotation = detail.rotation?.enabled;
  const csName = (agentId?: number) => detail.rotation?.agents?.find(a => a.id === agentId)?.name || (agentId ? `#${agentId}` : '—');

  return (
    <Box sx={{ minWidth: 0, p: 1.25, border: '1px solid', borderColor: 'divider', borderRadius: 1 }}>
      <Typography variant="subtitle2" sx={{ fontWeight: 800 }}>Daftar Penerima</Typography>
      <Typography variant="caption" color="text.secondary">Cari atau filter berdasarkan hasil pengiriman.</Typography>
      <Stack direction="row" spacing={0.75} sx={{ my: 1, flexWrap: 'wrap', gap: 0.75 }}>
        {FILTERS.map(f => (
          <Chip key={f.k} size="small" label={f.label} onClick={() => setFilter(f.k)}
            color={filter === f.k ? 'primary' : 'default'} variant={filter === f.k ? 'filled' : 'outlined'} />
        ))}
      </Stack>
      <TextField size="small" fullWidth placeholder="Cari nomor…" value={search} onChange={e => setSearch(e.target.value)} sx={{ mb: 1 }} />
      <Box sx={{ display: { xs: 'none', sm: 'block' }, maxHeight: { sm: 420, md: 'min(48vh, 460px)' }, overflowY: 'auto', border: '1px solid', borderColor: 'divider', borderRadius: 1 }}>
        <Table size="small" stickyHeader>
          <TableHead>
            <TableRow>
              <TableCell>Nomor</TableCell>
              <TableCell>Nama</TableCell>
              {rotation && <TableCell>CS</TableCell>}
              <TableCell align="right">Status</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {shown.map(r => {
              const hasMsg = !!r.sent_message;
              const open = openMsgIds.has(r.id);
              return (
                <Fragment key={r.id}>
                  <TableRow hover={hasMsg} onClick={hasMsg ? () => toggleMsg(r.id) : undefined}
                    sx={hasMsg ? { cursor: 'pointer', '& > *': { borderBottom: open ? 'none' : undefined } } : undefined}>
                    <TableCell>
                      <Stack direction="row" sx={{ alignItems: 'center', gap: 0.5 }}>
                        {hasMsg && <ExpandMoreIcon fontSize="small" sx={{ color: 'text.secondary', transform: open ? 'rotate(180deg)' : 'none', transition: 'transform .15s' }} />}
                        <span>+{r.number}</span>
                      </Stack>
                    </TableCell>
                    <TableCell>{r.name || '-'}</TableCell>
                    {rotation && <TableCell><Typography variant="caption" color="text.secondary" noWrap>{csName(r.agent_id)}</Typography></TableCell>}
                    <TableCell align="right">
                      <Chip size="small" label={RCP_LABEL[r.status] ?? r.status} color={RCP_COLOR[r.status] ?? 'default'} />
                      {r.error && <Typography variant="caption" color={r.status === 'pending' ? 'warning.main' : 'error'} sx={{ display: 'block' }}>{r.error}</Typography>}
                    </TableCell>
                  </TableRow>
                  {hasMsg && (
                    <TableRow>
                      <TableCell colSpan={rotation ? 4 : 3} sx={{ py: 0, borderBottom: open ? undefined : 'none' }}>
                        <Collapse in={open} unmountOnExit>
                          <Box sx={{ my: 1, p: 1, bgcolor: 'action.hover', borderRadius: 1 }}>
                            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>Pesan yang diterima nomor ini</Typography>
                            <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{r.sent_message}</Typography>
                          </Box>
                        </Collapse>
                      </TableCell>
                    </TableRow>
                  )}
                </Fragment>
              );
            })}
          </TableBody>
        </Table>
      </Box>
      <Stack spacing={0.75} sx={{ display: { xs: 'flex', sm: 'none' }, maxHeight: '44svh', overflowY: 'auto', pr: 0.25 }}>
        {shown.map(r => {
          const hasMsg = !!r.sent_message;
          const open = openMsgIds.has(r.id);
          return (
            <Box key={r.id} onClick={hasMsg ? () => toggleMsg(r.id) : undefined}
              sx={{ p: 1, border: '1px solid', borderColor: 'divider', borderRadius: 1, cursor: hasMsg ? 'pointer' : 'default' }}>
              <Stack direction="row" sx={{ alignItems: 'flex-start', justifyContent: 'space-between', gap: 1 }}>
                <Box sx={{ minWidth: 0 }}>
                  <Typography variant="body2" sx={{ fontWeight: 700 }}>+{r.number}</Typography>
                  <Typography variant="caption" color="text.secondary">{r.name || 'Tanpa nama'}</Typography>
                  {rotation && <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>CS: {csName(r.agent_id)}</Typography>}
                  {r.error && <Typography variant="caption" color={r.status === 'pending' ? 'warning.main' : 'error'} sx={{ display: 'block' }}>{r.error}</Typography>}
                  {hasMsg && (
                    <Typography variant="caption" color="primary" sx={{ display: 'block', mt: 0.25 }}>
                      {open ? 'Sembunyikan pesan' : 'Lihat pesan yang diterima'}
                    </Typography>
                  )}
                </Box>
                <Chip size="small" label={RCP_LABEL[r.status] ?? r.status} color={RCP_COLOR[r.status] ?? 'default'} sx={{ flexShrink: 0 }} />
              </Stack>
              {hasMsg && (
                <Collapse in={open} unmountOnExit>
                  <Box sx={{ mt: 1, p: 1, bgcolor: 'action.hover', borderRadius: 1 }}>
                    <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{r.sent_message}</Typography>
                  </Box>
                </Collapse>
              )}
            </Box>
          );
        })}
      </Stack>
      {shown.length === 0 && <Typography variant="body2" color="text.secondary" sx={{ py: 3, textAlign: 'center' }}>Tidak ada penerima yang cocok.</Typography>}
      <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>Menampilkan {shown.length} dari {recs.length} penerima</Typography>
    </Box>
  );
}
