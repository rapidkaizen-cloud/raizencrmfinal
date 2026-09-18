import { Box, Button, MenuItem, Stack, Table, TableBody, TableCell, TableHead, TableRow, TextField, Typography } from '@mui/material';
import { EMPTY_BROADCAST_FILTER, useBroadcastSummary } from '../../hooks';
import type { Agent, BroadcastHistoryFilter } from '../../types';
import DropdownField from '../DropdownField';

const STATUS_OPTIONS: [string, string][] = [
  ['', 'Semua status'], ['done', 'Selesai'], ['running', 'Berjalan'], ['pending', 'Antre'], ['failed', 'Gagal'],
  ['interrupted', 'Terhenti'], ['wa_restricted', 'Dijeda WhatsApp'], ['cancelled', 'Dibatalkan'],
];

const STATS: [BroadcastHistoryFilter['has'], string, 'broadcasts' | 'sent' | 'failed' | 'skipped' | 'pending'][] = [
  ['', 'Blast', 'broadcasts'], ['sent', 'Sudah di-blast', 'sent'], ['failed', 'Gagal', 'failed'],
  ['skipped', 'Dilewati', 'skipped'], ['pending', 'Menunggu', 'pending'],
];

function Stat({ label, value, active, onClick }: { label: string; value: number; active: boolean; onClick: () => void }) {
  return (
    <Box
      component="button"
      type="button"
      onClick={onClick}
      aria-pressed={active}
      sx={{
        minWidth: 92, px: 1, py: 0.5, textAlign: 'left', font: 'inherit', color: 'inherit', cursor: 'pointer',
        border: '1px solid', borderColor: active ? 'primary.main' : 'transparent', borderRadius: 1,
        bgcolor: active ? 'background.paper' : 'transparent', '&:hover': { bgcolor: 'background.paper' },
      }}
    >
      <Typography variant="h6" sx={{ fontWeight: 800, lineHeight: 1.1 }}>{value.toLocaleString('id-ID')}</Typography>
      <Typography variant="caption" color="text.secondary">{label}</Typography>
    </Box>
  );
}

// Header "Riwayat Blast": filter (periode, status, nomor pengirim) + ringkasan total & per nomor.
// Filter yang sama dipakai panel induk untuk daftar Blast di bawahnya.
export default function BroadcastHistorySummary({ agentId, agents, filter, onChange }: {
  agentId: number;
  /** Kandidat filter nomor pengirim. */
  agents: Agent[];
  filter: BroadcastHistoryFilter;
  onChange: (f: BroadcastHistoryFilter) => void;
}) {
  const { data } = useBroadcastSummary(agentId, filter);
  const set = (patch: Partial<BroadcastHistoryFilter>) => onChange({ ...filter, ...patch });
  const dirty = filter.from || filter.to || filter.status || filter.sender !== '' || filter.has;
  const perAgent = data?.per_agent || [];
  return (
    <Box sx={{ mb: 1.5 }}>
      <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap', alignItems: 'center', mb: 1.25 }}>
        <TextField size="small" type="date" label="Dari" value={filter.from} onChange={e => set({ from: e.target.value })} slotProps={{ inputLabel: { shrink: true } }} sx={{ width: 150 }} />
        <TextField size="small" type="date" label="Sampai" value={filter.to} onChange={e => set({ to: e.target.value })} slotProps={{ inputLabel: { shrink: true } }} sx={{ width: 150 }} />
        <DropdownField size="small" label="Status" value={filter.status} onChange={e => set({ status: e.target.value })} sx={{ minWidth: 150 }}>
          {STATUS_OPTIONS.map(([v, l]) => <MenuItem key={v} value={v}>{l}</MenuItem>)}
        </DropdownField>
        {agents.length > 1 && (
          <DropdownField size="small" label="Nomor pengirim" value={filter.sender} onChange={e => set({ sender: e.target.value === '' ? '' : Number(e.target.value) })} sx={{ minWidth: 170 }}>
            <MenuItem value="">Semua nomor</MenuItem>
            {agents.map(a => <MenuItem key={a.id} value={a.id}>{a.name || `Nomor ${a.id}`}{a.number ? ` · +${a.number}` : ''}</MenuItem>)}
          </DropdownField>
        )}
        {dirty && <Button size="small" onClick={() => onChange(EMPTY_BROADCAST_FILTER)}>Reset</Button>}
      </Stack>
      <Stack direction="row" sx={{ gap: 2, flexWrap: 'wrap', p: 1.25, bgcolor: 'action.hover', borderRadius: 1 }}>
        {/* Klik kotak = saring daftar ke Blast yang punya penerima berstatus itu; "Blast" = semua. */}
        {STATS.map(([has, label, key]) => (
          <Stat key={key} label={label} value={data?.[key] ?? 0} active={filter.has === has} onClick={() => set({ has: filter.has === has ? '' : has })} />
        ))}
      </Stack>
      {perAgent.length > 0 && (
        <Box sx={{ mt: 1, overflowX: 'auto' }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Nomor pengirim</TableCell>
                <TableCell align="right">Blast</TableCell>
                <TableCell align="right">Sudah di-blast</TableCell>
                <TableCell align="right">Gagal</TableCell>
                <TableCell align="right">Dilewati</TableCell>
                <TableCell align="right">Menunggu</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {perAgent.map(a => (
                <TableRow key={a.agent_id} hover sx={{ cursor: 'pointer' }} onClick={() => set({ sender: filter.sender === a.agent_id ? '' : a.agent_id })} selected={filter.sender === a.agent_id}>
                  <TableCell>
                    <Typography variant="body2" sx={{ fontWeight: 700 }}>{a.name}</Typography>
                    {a.number && <Typography variant="caption" color="text.secondary">+{a.number}</Typography>}
                  </TableCell>
                  <TableCell align="right">{a.broadcasts}</TableCell>
                  <TableCell align="right"><b>{a.sent.toLocaleString('id-ID')}</b></TableCell>
                  <TableCell align="right">{a.failed}</TableCell>
                  <TableCell align="right">{a.skipped}</TableCell>
                  <TableCell align="right">{a.pending}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
    </Box>
  );
}
