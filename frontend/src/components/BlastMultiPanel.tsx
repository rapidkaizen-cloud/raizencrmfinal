import { useMemo, useState, type ReactNode } from 'react';
import {
  Box, Typography, Card, CardContent, TextField, Button, Stack, Alert, Chip, Checkbox, Collapse,
  Divider, Switch, Table, TableBody, TableCell, TableHead, TableRow, CircularProgress, MenuItem, IconButton,
  Tabs, Tab, FormControlLabel, Pagination, Dialog, DialogTitle, DialogContent, DialogActions, InputAdornment,
} from '@mui/material';
import ManageAccountsIcon from '@mui/icons-material/ManageAccountsOutlined';
import * as XLSX from 'xlsx';
import SendIcon from '@mui/icons-material/Send';
import UploadFileIcon from '@mui/icons-material/UploadFileOutlined';
import EditNoteIcon from '@mui/icons-material/EditNoteOutlined';
import PeopleAltIcon from '@mui/icons-material/PeopleAltOutlined';
import ScheduleIcon from '@mui/icons-material/ScheduleOutlined';
import HistoryIcon from '@mui/icons-material/HistoryOutlined';
import RefreshIcon from '@mui/icons-material/Refresh';
import SyncAltIcon from '@mui/icons-material/SyncAltOutlined';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import TableChartIcon from '@mui/icons-material/TableChartOutlined';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import EditIcon from '@mui/icons-material/EditOutlined';
import DeleteIcon from '@mui/icons-material/DeleteOutlined';
import CloseIcon from '@mui/icons-material/Close';
import {
  useMe, useSaveMultiBlastStructure, useAgents, useAgentStatuses, useBroadcasts, useBroadcastDetail, useCancelBroadcast, useCreateBroadcast,
  useMultiBlastContacts, useMultiBlastContactIds, useImportMultiBlastContacts, useAssignMultiBlastContacts, useDistributeMultiBlastContacts, useDeleteMultiBlastContacts, useUpdateMultiBlastContact,
  EMPTY_BROADCAST_FILTER,
} from '../hooks';
import { useDraftState } from '../draft';
import { swalConfirm, swalConfirmDelete, swalToast } from '../services/swal';
import { normalizePhone, type Agent, type BlastDelay, type Broadcast, type BroadcastHistoryFilter, type MultiBlastColumn, type MultiBlastContact } from '../types';
import BroadcastHistorySummary from './broadcast/BroadcastHistorySummary';
import RecipientList from './broadcast/RecipientList';
import { defaultBroadcastSafetyForm } from '../services/broadcastSafety';
import WhatsAppEditor, { VAR_DRAG_TYPE } from './WhatsAppEditor';
import TemplatePicker from './TemplatePicker';
import PageHeader from './PageHeader';
import DelayFields from './broadcast/DelayFields';
import { useBlastDelay } from './broadcast/useBlastDelay';
import BroadcastProgress from './broadcast/BroadcastProgress';
import { STATUS_COLOR, STATUS_LABEL, spinPreview } from './BroadcastPanel';

// Batas nomor pengirim per Blast — sama dengan maxMultiBlastNumbers di backend.
const MAX_NUMBERS = 10;
const MAX_RECIPIENTS = 1000;
const PHONE_KEY = /(nomor|no_hp|^hp$|phone|telp|telepon|whatsapp|^wa$|number)/;
const NAME_KEY = /^(nama|name)/;

type Row = Record<string, string>;

// normKey: "No. Resi Pesanan" -> "no_resi_pesanan", dipakai sebagai nama placeholder {no_resi_pesanan}.
function normKey(label: string) {
  return label.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '');
}

// parseXlsx membaca sheet pertama. Nilai dipakai versi terformat (tanggal ikut format sel),
// kecuali angka besar yang terformat jadi notasi ilmiah (nomor HP) — pakai angka mentahnya.
async function parseXlsx(file: File): Promise<{ columns: MultiBlastColumn[]; rows: Row[] }> {
  const wb = XLSX.read(await file.arrayBuffer(), { type: 'array' });
  const ws = wb.Sheets[wb.SheetNames[0]];
  if (!ws) return { columns: [], rows: [] };
  const formatted = XLSX.utils.sheet_to_json<Record<string, unknown>>(ws, { defval: '', raw: false });
  const raw = XLSX.utils.sheet_to_json<Record<string, unknown>>(ws, { defval: '', raw: true });
  const labels = Object.keys(formatted[0] || {}).filter(l => l && !l.startsWith('__EMPTY'));
  const columns = labels.map(label => ({ key: normKey(label), label })).filter(h => h.key);
  const rows = formatted.map((r, i) => {
    const out: Row = {};
    for (const h of columns) {
      let v = String(r[h.label] ?? '').trim();
      const rv = raw[i]?.[h.label];
      if (typeof rv === 'number' && /e\+/i.test(v)) v = String(rv);
      out[h.key] = v;
    }
    return out;
  }).filter(r => Object.values(r).some(Boolean));
  return { columns, rows };
}

function guessPhoneCol(columns: MultiBlastColumn[], rows: Row[]) {
  const byName = columns.find(h => PHONE_KEY.test(h.key));
  if (byName) return byName.key;
  const sample = rows.slice(0, 50);
  const best = columns.find(h => sample.length && sample.filter(r => normalizePhone(r[h.key]).length >= 9).length >= sample.length / 2);
  return best?.key || '';
}

function parseVars(c: MultiBlastContact): Row {
  try { return c.vars_json ? JSON.parse(c.vars_json) as Row : {}; } catch { return {}; }
}

function errorText(error: unknown, fallback: string) {
  return (error as { response?: { data?: { error?: string } } })?.response?.data?.error || fallback;
}

function agentLabel(agents: Agent[], id: number) {
  const a = agents.find(x => x.id === id);
  return a ? (a.name || `Nomor ${a.id}`) : `#${id}`;
}

// VarChip = chip variabel pesan. Seret ke editor: WhatsAppEditor menyisipkan {kunci} di posisi huruf
// tempat chip dilepas (ditangani editor sendiri lewat VAR_DRAG_TYPE). Klik = tambahkan di akhir.
function VarChip({ name, onClick }: { name: string; onClick: () => void }) {
  return (
    <Chip size="small" variant="outlined" label={`{${name}}`} onClick={onClick} draggable
      onDragStart={e => { e.dataTransfer.setData(VAR_DRAG_TYPE, `{${name}}`); e.dataTransfer.effectAllowed = 'copy'; }}
      sx={{ cursor: 'grab', '&:active': { cursor: 'grabbing' } }} />
  );
}

function SectionTitle({ icon, title, subtitle, action }: { icon: ReactNode; title: string; subtitle?: string; action?: ReactNode }) {
  return (
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1, mb: 1 }}>
      <Box sx={{ width: 30, height: 30, display: 'grid', placeItems: 'center', borderRadius: 1, bgcolor: 'action.hover', color: 'primary.main', flexShrink: 0 }}>{icon}</Box>
      <Box sx={{ minWidth: 0, flex: 1 }}>
        <Typography variant="subtitle2" sx={{ fontWeight: 800 }}>{title}</Typography>
        {subtitle && <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>{subtitle}</Typography>}
      </Box>
      {action}
    </Stack>
  );
}

// Setelan khusus satu nomor: jeda, pesan sendiri (kosong = pesan global), penerima khusus.
type Override = BlastDelay & { message: string; numbers: string };

function AgentOverride({ agentId, value, placeholders, onChange }: {
  agentId: number; value: Override; placeholders: string[]; onChange: (v: Override) => void;
}) {
  const num = (k: keyof BlastDelay, label: string) => (
    <TextField type="number" size="small" label={label} value={value[k]} sx={{ width: 130 }}
      onChange={e => onChange({ ...value, [k]: Math.max(0, Number(e.target.value)) })} />
  );
  const ownCount = value.numbers.split('\n').filter(l => normalizePhone(l)).length;
  return (
    <Stack spacing={1.25} sx={{ mt: 1 }}>
      <Box>
        <Typography variant="caption" sx={{ fontWeight: 700 }}>Jeda khusus nomor ini</Typography>
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 0.5 }}>
          {num('min_delay', 'Jeda min (dtk)')}
          {num('max_delay', 'Jeda maks (dtk)')}
          {num('rest_every', 'Istirahat tiap (pesan)')}
          {num('rest_duration', 'Lama istirahat (dtk)')}
        </Stack>
      </Box>
      <Box>
        <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, mb: 0.5 }}>
          <Typography variant="caption" sx={{ fontWeight: 700 }}>Pesan khusus nomor ini{value.message.trim() ? '' : ' (kosong = pakai pesan global)'}</Typography>
          <TemplatePicker label="Template" agentId={agentId} onPick={b => onChange({ ...value, message: value.message ? value.message + '\n' + b : b })} />
        </Stack>
        <WhatsAppEditor value={value.message} rows={3} onChange={v => onChange({ ...value, message: v })}
          placeholder="Kosongkan untuk memakai pesan global. Variasi {a|b} dan variabel {nama} tetap berlaku." />
        <Stack direction="row" sx={{ alignItems: 'center', gap: 0.5, flexWrap: 'wrap', mt: 0.5 }}>
          {placeholders.map(p => (
            <VarChip key={p} name={p} onClick={() => onChange({ ...value, message: value.message + `{${p}}` })} />
          ))}
        </Stack>
      </Box>
      <TextField fullWidth multiline minRows={2} size="small" value={value.numbers}
        label={`Penerima khusus di luar data kontak (satu per baris)${ownCount ? ` · ${ownCount} nomor` : ''}`}
        helperText="Nomor di sini selalu dikirim oleh nomor ini. Untuk kontak dari data, assign lewat tabel."
        onChange={e => onChange({ ...value, numbers: e.target.value })} placeholder={'628123456789\n08987654321'} />
    </Stack>
  );
}

// PagerBar = paginasi tabel: rentang baris yang tampil, pilihan baris per halaman (opsional),
// dan nomor halaman dengan tombol awal/akhir.
function PagerBar({ page, total, pageSize, onPage, onPageSize, note }: {
  page: number; total: number; pageSize: number; onPage: (p: number) => void; onPageSize?: (n: number) => void; note?: string;
}) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const current = Math.min(page, pages);
  const from = total === 0 ? 0 : (current - 1) * pageSize + 1;
  const to = Math.min(total, current * pageSize);
  return (
    <Stack direction={{ xs: 'column', md: 'row' }} sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, mt: 1.25 }}>
      <Typography variant="caption" color="text.secondary">
        Menampilkan <b>{from}–{to}</b> dari <b>{total}</b>{note ? ` · ${note}` : ''}
      </Typography>
      <Stack direction="row" sx={{ alignItems: 'center', gap: 1.5, flexWrap: 'wrap', justifyContent: 'center' }}>
        {onPageSize && (
          <Stack direction="row" sx={{ alignItems: 'center', gap: 0.75 }}>
            <Typography variant="caption" color="text.secondary">Baris per halaman</Typography>
            <TextField select size="small" value={pageSize} onChange={e => onPageSize(Number(e.target.value))}
              sx={{ width: 76, '& .MuiSelect-select': { py: 0.5, fontSize: 13 } }}>
              {[10, 25, 50, 100].map(n => <MenuItem key={n} value={n}>{n}</MenuItem>)}
            </TextField>
          </Stack>
        )}
        {pages > 1 && (
          <Pagination count={pages} page={current} onChange={(_, p) => onPage(p)} color="primary" shape="rounded" size="small"
            showFirstButton showLastButton siblingCount={1} boundaryCount={1} />
        )}
      </Stack>
    </Stack>
  );
}

// ContactEditDialog = ubah satu kontak: nomor, nama, kolom impor, dan penanggung jawab.
// Kolom "Sudah di-blast" adalah riwayat, tidak bisa diubah.
function ContactEditDialog({ agentId, agents, columns, contact, onClose }: {
  agentId: number; agents: Agent[]; columns: MultiBlastColumn[]; contact: MultiBlastContact; onClose: () => void;
}) {
  const initialVars = useMemo(() => parseVars(contact), [contact]);
  const [number, setNumber] = useState(contact.number);
  const [name, setName] = useState(contact.name);
  const [vars, setVars] = useState<Row>(initialVars);
  const [owner, setOwner] = useState(contact.agent_id);
  const updateM = useUpdateMultiBlastContact(agentId);
  const cleanNumber = normalizePhone(number);
  const validNumber = cleanNumber.length >= 9;
  // Simpan hanya aktif kalau ada yang berubah — user tahu belum ada yang diubah.
  const dirty = cleanNumber !== contact.number || name.trim() !== contact.name || owner !== contact.agent_id
    || columns.some(c => (vars[c.key] || '') !== (initialVars[c.key] || ''));
  const save = async () => {
    if (!validNumber || !dirty) return;
    try {
      await updateM.mutateAsync({ id: contact.id, number: cleanNumber, name: name.trim(), vars, agent_id: owner });
      swalToast('Kontak disimpan.');
      onClose();
    } catch (error) { swalToast(errorText(error, 'Kontak belum bisa disimpan.'), 'error'); }
  };
  const lastBlast = contact.last_blast_at
    ? new Date(contact.last_blast_at).toLocaleString('id-ID', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })
    : null;
  const group = (title: string, children: ReactNode) => (
    <Box>
      <Typography variant="overline" color="text.secondary" sx={{ display: 'block', lineHeight: 1.6, mb: 0.75, letterSpacing: 0.6 }}>{title}</Typography>
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 1.5 }}>{children}</Box>
    </Box>
  );
  return (
    <Dialog open onClose={updateM.isPending ? undefined : onClose} fullWidth maxWidth="sm"
      component="form" onSubmit={e => { e.preventDefault(); void save(); }}>
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', gap: 1, pr: 1.5 }}>
        <Box sx={{ minWidth: 0, flex: 1 }}>
          Edit kontak
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontWeight: 400 }} noWrap>
            {contact.name ? `${contact.name} · ` : ''}+{contact.number}
          </Typography>
        </Box>
        <IconButton size="small" onClick={onClose} aria-label="Tutup" disabled={updateM.isPending}><CloseIcon fontSize="small" /></IconButton>
      </DialogTitle>
      <DialogContent>
        <Stack spacing={2.5} sx={{ pt: 0.5 }}>
          {group('Identitas', <>
            <TextField size="small" label="Nomor WhatsApp" value={number} onChange={e => setNumber(e.target.value)} autoFocus
              error={!validNumber} helperText={validNumber ? 'Awalan 0 otomatis jadi 62.' : 'Minimal 9 digit, contoh 628123456789.'}
              slotProps={{ input: { startAdornment: <InputAdornment position="start">+</InputAdornment>, inputMode: 'tel' } }} />
            <TextField size="small" label="Nama" value={name} onChange={e => setName(e.target.value)} placeholder="Nama pelanggan" />
          </>)}
          {columns.length > 0 && group('Data impor', columns.map(c => (
            <TextField key={c.key} size="small" label={c.label} value={vars[c.key] || ''}
              onChange={e => setVars({ ...vars, [c.key]: e.target.value })} />
          )))}
          {group('Pengiriman', <>
            <TextField select size="small" label="Ditangani oleh" value={owner} onChange={e => setOwner(Number(e.target.value))}
              helperText="Nomor yang mengirim ke kontak ini seterusnya.">
              <MenuItem value={0}><em>Belum ditentukan</em></MenuItem>
              {agents.map(a => <MenuItem key={a.id} value={a.id}>{a.name || `Nomor ${a.id}`}{a.number ? ` · +${a.number}` : ''}</MenuItem>)}
            </TextField>
            <Box sx={{ p: 1.25, borderRadius: 1, bgcolor: 'action.hover', alignSelf: 'start' }}>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>Sudah di-blast · riwayat, tidak bisa diubah</Typography>
              <Typography variant="body2" sx={{ fontWeight: 700 }}>{contact.blast_count}×{lastBlast ? <Typography component="span" variant="caption" color="text.secondary"> · terakhir {lastBlast}</Typography> : null}</Typography>
            </Box>
          </>)}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={updateM.isPending}>Batal</Button>
        <Button type="submit" variant="contained" disabled={!validNumber || !dirty || updateM.isPending}
          startIcon={updateM.isPending ? <CircularProgress size={14} color="inherit" /> : undefined}>
          {updateM.isPending ? 'Menyimpan…' : 'Simpan perubahan'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

// ContactTable = tabel data kontak berpaginasi. fixedAgent: hanya kontak yang ditangani nomor itu
// (tab per nomor); tanpa fixedAgent = semua kontak dengan filter (tabel global).
function ContactTable({ agentId, agents, columns, fixedAgent, selected, setSelected }: {
  agentId: number; agents: Agent[]; columns: MultiBlastColumn[]; fixedAgent?: number;
  selected: Set<number>; setSelected: (s: Set<number>) => void;
}) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState('');
  // '' = belum memilih tujuan; 0 = lepas (kembali ke master); >0 = id nomor tujuan.
  const [assignTarget, setAssignTarget] = useState<number | ''>('');
  const assignM = useAssignMultiBlastContacts(agentId);
  const deleteM = useDeleteMultiBlastContacts(agentId);
  const agentFilter = fixedAgent !== undefined ? String(fixedAgent) : filter;
  const { data } = useMultiBlastContacts(agentId, { page, q, limit: pageSize, agent_id: agentFilter });
  // Semua id yang cocok dengan filter tabel ini (lintas halaman). Centang global dipakai bersama
  // semua tab, jadi aksi massal & hitungan di sini dibatasi ke kontak milik tabel ini saja.
  const { data: scopeIds = [] } = useMultiBlastContactIds(agentId, { q, agent_id: agentFilter });
  const rows = data?.data || [];
  const selectedHere = useMemo(() => scopeIds.filter(id => selected.has(id)), [scopeIds, selected]);
  // Kolom nomor/nama sudah punya kolom sendiri di tabel.
  const extraColumns = columns.filter(c => !PHONE_KEY.test(c.key) && !NAME_KEY.test(c.key));

  const toggleRow = (id: number) => { const n = new Set(selected); if (!n.delete(id)) n.add(id); setSelected(n); };
  const allChecked = scopeIds.length > 0 && selectedHere.length === scopeIds.length;
  // Centang header = semua kontak yang cocok di SEMUA halaman, bukan hanya halaman ini.
  const toggleAll = () => {
    const n = new Set(selected);
    if (allChecked) scopeIds.forEach(id => n.delete(id)); else scopeIds.forEach(id => n.add(id));
    setSelected(n);
  };
  const clearHere = () => { const n = new Set(selected); selectedHere.forEach(id => n.delete(id)); setSelected(n); };
  const assignSelected = async () => {
    if (assignTarget === '') return;
    try {
      const res = await assignM.mutateAsync({ ids: selectedHere, agent_id: assignTarget });
      clearHere();
      setPage(1); // baris pindah tab; halaman terakhir bisa jadi kosong
      swalToast(assignTarget ? `${res.updated} kontak ditangani ${agentLabel(agents, assignTarget)}.` : `${res.updated} kontak dikembalikan ke master.`);
      setAssignTarget('');
    } catch (error) { swalToast(errorText(error, 'Gagal mengubah penanggung jawab.'), 'error'); }
  };
  const deleteSelected = async () => {
    if (!await swalConfirmDelete(`Hapus ${selectedHere.length} kontak?`, 'Kontak dihapus dari data ini. Riwayat Blast yang sudah terkirim tidak ikut terhapus.', `Hapus ${selectedHere.length} kontak`)) return;
    try {
      const res = await deleteM.mutateAsync({ ids: selectedHere });
      clearHere();
      setPage(1);
      swalToast(`${res.deleted} kontak dihapus.`);
    } catch (error) { swalToast(errorText(error, 'Gagal menghapus.'), 'error'); }
  };
  const [editing, setEditing] = useState<MultiBlastContact | null>(null);
  const deleteOne = async (r: MultiBlastContact) => {
    if (!await swalConfirmDelete('Hapus kontak ini?', `${r.name ? `${r.name} · ` : ''}+${r.number}. Riwayat Blast yang sudah terkirim tidak ikut terhapus.`)) return;
    try {
      await deleteM.mutateAsync({ ids: [r.id] });
      const n = new Set(selected); n.delete(r.id); setSelected(n);
      swalToast('Kontak dihapus.');
    } catch (error) { swalToast(errorText(error, 'Gagal menghapus.'), 'error'); }
  };

  return (
    <Box>
      <Stack direction={{ xs: 'column', md: 'row' }} spacing={1} sx={{ mb: 1, alignItems: { md: 'center' }, flexWrap: 'wrap' }}>
        <TextField size="small" placeholder="Cari nomor / nama…" value={q} onChange={e => { setQ(e.target.value); setPage(1); }} sx={{ minWidth: 200 }} />
        {fixedAgent === undefined && (
          <TextField select size="small" label="Penanggung jawab" value={filter} onChange={e => { setFilter(e.target.value); setPage(1); }} sx={{ minWidth: 190 }}>
            <MenuItem value="">Semua</MenuItem>
            <MenuItem value="0">Belum ditentukan</MenuItem>
            {agents.map(a => <MenuItem key={a.id} value={String(a.id)}>{a.name || `Nomor ${a.id}`}</MenuItem>)}
          </TextField>
        )}
        <Box sx={{ flex: 1 }} />
        <TextField select size="small" label={fixedAgent ? `Pindahkan ${selectedHere.length} terpilih ke` : `Assign ${selectedHere.length} terpilih ke`}
          value={assignTarget} disabled={selectedHere.length === 0}
          onChange={e => setAssignTarget(e.target.value === '' ? '' : Number(e.target.value))} sx={{ minWidth: 220 }}>
          {fixedAgent !== 0 && <MenuItem value={0}>Lepas (kembali ke master)</MenuItem>}
          {agents.filter(a => a.id !== fixedAgent).map(a => <MenuItem key={a.id} value={a.id}>{a.name || `Nomor ${a.id}`}{a.number ? ` · +${a.number}` : ''}</MenuItem>)}
        </TextField>
        <Button size="small" variant="contained" disabled={selectedHere.length === 0 || assignTarget === '' || assignM.isPending} onClick={() => { void assignSelected(); }}>Terapkan</Button>
        {selectedHere.length > 0 && (
          <Button size="small" color="error" variant="contained" startIcon={<DeleteIcon />} disabled={deleteM.isPending} onClick={() => { void deleteSelected(); }}>
            Hapus {selectedHere.length} terpilih
          </Button>
        )}
      </Stack>
      {editing && <ContactEditDialog agentId={agentId} agents={agents} columns={extraColumns} contact={editing} onClose={() => setEditing(null)} />}
      {rows.length === 0 ? (
        <Alert severity="info" icon={false}>
          {fixedAgent ? 'Belum ada kontak yang ditangani nomor ini. Assign dari tab master, atau biarkan terisi otomatis saat Blast.' : 'Tidak ada kontak yang menunggu ditentukan. Impor file .xlsx dengan tombol di atas; kontak yang sudah di-assign ada di tab nomornya.'}
        </Alert>
      ) : (
        <>
          <Box sx={{ overflowX: 'auto', border: '1px solid', borderColor: 'divider', borderRadius: 1 }}>
            <Table size="small" stickyHeader>
              <TableHead>
                <TableRow>
                  <TableCell padding="checkbox">
                    <Checkbox size="small" checked={allChecked} indeterminate={!allChecked && selectedHere.length > 0} onChange={toggleAll}
                      title={`Centang semua ${scopeIds.length} kontak di semua halaman`} />
                  </TableCell>
                  <TableCell>Nomor</TableCell>
                  <TableCell>Nama</TableCell>
                  {extraColumns.map(c => <TableCell key={c.key} sx={{ whiteSpace: 'nowrap' }}>{c.label}</TableCell>)}
                  {fixedAgent === undefined && <TableCell sx={{ whiteSpace: 'nowrap' }}>Ditangani oleh</TableCell>}
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>Sudah di-blast</TableCell>
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>Aksi</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map(r => {
                  const vars = parseVars(r);
                  return (
                    <TableRow key={r.id} hover selected={selected.has(r.id)} onClick={() => toggleRow(r.id)} sx={{ cursor: 'pointer' }}>
                      <TableCell padding="checkbox"><Checkbox size="small" checked={selected.has(r.id)} /></TableCell>
                      <TableCell sx={{ whiteSpace: 'nowrap' }}>+{r.number}</TableCell>
                      <TableCell>{r.name || '-'}</TableCell>
                      {extraColumns.map(c => <TableCell key={c.key} sx={{ whiteSpace: 'nowrap', maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis' }}>{vars[c.key] || ''}</TableCell>)}
                      {fixedAgent === undefined && (
                        <TableCell sx={{ whiteSpace: 'nowrap' }}>
                          {r.agent_id ? <Chip size="small" color="primary" variant="outlined" label={agentLabel(agents, r.agent_id)} /> : <Chip size="small" variant="outlined" label="Belum ditentukan" />}
                        </TableCell>
                      )}
                      <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                        {r.blast_count}×{r.last_blast_at ? <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>{new Date(r.last_blast_at).toLocaleString('id-ID', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })}</Typography> : null}
                      </TableCell>
                      <TableCell align="right" padding="none" sx={{ whiteSpace: 'nowrap', pr: 0.5 }} onClick={e => e.stopPropagation()}>
                        <IconButton size="small" title="Edit" onClick={() => setEditing(r)}><EditIcon fontSize="small" /></IconButton>
                        <IconButton size="small" color="error" title="Hapus" onClick={() => { void deleteOne(r); }}><DeleteIcon fontSize="small" /></IconButton>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Box>
          <PagerBar page={page} total={data?.total || 0} pageSize={pageSize} onPage={setPage}
            onPageSize={n => { setPageSize(n); setPage(1); }}
            note={selectedHere.length ? `${selectedHere.length} dicentang di tabel ini` : undefined} />
        </>
      )}
    </Box>
  );
}

// AgentTab = isi tab satu nomor: ikut blast atau tidak, setelan khusus, dan kontak yang ditangani.
function AgentTab({ agentId, agent, agents, columns, online, participating, load, override, placeholders, selected, setSelected, onToggleParticipate, onToggleOverride, onChangeOverride }: {
  agentId: number; agent: Agent; agents: Agent[]; columns: MultiBlastColumn[]; online: boolean;
  participating: boolean; load: number; override?: Override; placeholders: string[];
  selected: Set<number>; setSelected: (s: Set<number>) => void;
  onToggleParticipate: (on: boolean) => void; onToggleOverride: (on: boolean) => void; onChangeOverride: (v: Override) => void;
}) {
  return (
    <Stack spacing={1.5} sx={{ p: 1.25, border: '1px solid', borderColor: 'divider', borderTop: 'none', borderRadius: '0 0 8px 8px' }}>
      <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { sm: 'center' }, gap: 1, flexWrap: 'wrap' }}>
        <Box sx={{ minWidth: 0, flex: 1 }}>
          <Typography variant="body2" sx={{ fontWeight: 700 }}>{agent.name || `Nomor ${agent.id}`}</Typography>
          <Typography variant="caption" color="text.secondary">{agent.number ? `+${agent.number}` : 'Belum pernah ditautkan'} · menangani {load} kontak</Typography>
        </Box>
        <Chip size="small" variant="outlined" color={online ? 'success' : 'default'} label={online ? 'Tersambung' : 'Offline'} />
        <FormControlLabel label={<Typography variant="body2">Ikut mengirim di Blast ini</Typography>}
          control={<Switch size="small" checked={participating} onChange={e => onToggleParticipate(e.target.checked)} />} />
        {participating && (
          <FormControlLabel label={<Typography variant="body2">Setelan khusus</Typography>}
            control={<Switch size="small" checked={!!override} onChange={e => onToggleOverride(e.target.checked)} />} />
        )}
      </Stack>
      {participating && override && <AgentOverride agentId={agentId} value={override} placeholders={placeholders} onChange={onChangeOverride} />}
      <Divider />
      <Typography variant="subtitle2" sx={{ fontWeight: 800 }}>Kontak yang ditangani nomor ini</Typography>
      <ContactTable agentId={agentId} agents={agents} columns={columns} fixedAgent={agent.id} selected={selected} setSelected={setSelected} />
    </Stack>
  );
}

// Rincian per nomor untuk satu Blast di riwayat.
function MultiDetail({ agentId, bid }: { agentId: number; bid: number }) {
  const { data } = useBroadcastDetail(agentId, bid);
  if (!data) return <CircularProgress size={18} sx={{ m: 1 }} />;
  const agents = data.rotation?.agents || [];
  const lockedCount = data.recipients.filter(r => r.locked).length;
  return (
    <Box sx={{ p: 1, bgcolor: 'action.hover', borderRadius: 1 }}>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
        {lockedCount} penerima terkunci ke nomor penanggung jawabnya, sisanya dibagi rata.
      </Typography>
      <Stack spacing={0.5}>
        {agents.map(a => (
          <Stack key={a.id} direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', gap: 0.5, p: 0.75, bgcolor: 'background.paper', borderRadius: 1 }}>
            <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center', minWidth: 0 }}>
              <Typography variant="body2" sx={{ fontWeight: 700 }} noWrap>{a.name || `Nomor ${a.id}`}</Typography>
              <Typography variant="caption" color="text.secondary" noWrap>{a.number ? `+${a.number}` : ''}</Typography>
              <Chip size="small" variant="outlined" color={a.connected ? 'success' : 'default'} label={a.connected ? 'Tersambung' : 'Offline'} />
              {a.quarantine?.active && <Chip size="small" color="warning" variant="outlined" label={a.quarantine.reason_label} />}
            </Stack>
            <Typography variant="caption" color="text.secondary">
              Terkirim <b>{a.sent_count}</b> · Menunggu <b>{a.pending_count}</b> · Gagal <b>{a.failed_count}</b> · Dilewati <b>{a.skipped_count}</b>
            </Typography>
          </Stack>
        ))}
      </Stack>
      <Box sx={{ mt: 1 }}><RecipientList detail={data} /></Box>
    </Box>
  );
}

type BlastRole = 'none' | 'master' | `m:${number}`;

// MasterSetupDialog = atur struktur Blast Multiple Number se-tenant: nomor mana yang jadi master
// (mengelola data & anggota, tidak mengirim) dan nomor mana yang jadi anggota master tertentu.
function MasterSetupDialog({ agents, onClose }: { agents: Agent[]; onClose: () => void }) {
  const save = useSaveMultiBlastStructure();
  const [roles, setRoles] = useState<Record<number, BlastRole>>(() => Object.fromEntries(agents.map(a =>
    [a.id, a.is_blast_master ? 'master' : a.blast_master_id ? `m:${a.blast_master_id}` : 'none'] as [number, BlastRole])));
  const masterIds = agents.filter(a => roles[a.id] === 'master').map(a => a.id);
  // Anggota yang masternya baru saja diturunkan jatuh ke "tidak ikut".
  const roleOf = (id: number): BlastRole => {
    const r = roles[id] || 'none';
    return r.startsWith('m:') && !masterIds.includes(Number(r.slice(2))) ? 'none' : r;
  };
  const submit = async () => {
    try {
      await save.mutateAsync(agents.map(a => {
        const r = roleOf(a.id);
        return { agent_id: a.id, is_master: r === 'master', master_id: r.startsWith('m:') ? Number(r.slice(2)) : 0 };
      }));
      swalToast('Struktur master agent disimpan.');
      onClose();
    } catch (error) { swalToast(errorText(error, 'Struktur master belum bisa disimpan.'), 'error'); }
  };
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>Atur master agent & anggota</DialogTitle>
      <DialogContent dividers>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
          <b>Master</b> mengelola data kontak dan anggotanya, tidak ikut mengirim. <b>Anggota</b> adalah nomor pengirim milik satu master.
          Boleh ada beberapa master; tiap master punya data kontak sendiri.
        </Typography>
        <Stack spacing={1}>
          {agents.map(a => (
            <Stack key={a.id} direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { sm: 'center' }, gap: 1, p: 1, border: '1px solid', borderColor: roleOf(a.id) === 'master' ? 'primary.main' : 'divider', borderRadius: 1.5 }}>
              <Box sx={{ minWidth: 0, flex: 1 }}>
                <Typography variant="body2" sx={{ fontWeight: 700 }} noWrap>{a.name || `Nomor ${a.id}`}</Typography>
                <Typography variant="caption" color="text.secondary" noWrap>{a.number ? `+${a.number}` : 'Belum pernah ditautkan'}</Typography>
              </Box>
              <TextField select size="small" value={roleOf(a.id)} sx={{ minWidth: 230 }}
                onChange={e => setRoles(prev => ({ ...prev, [a.id]: e.target.value as BlastRole }))}>
                <MenuItem value="none">Tidak ikut</MenuItem>
                <MenuItem value="master">Master agent</MenuItem>
                {masterIds.filter(id => id !== a.id).map(id => (
                  <MenuItem key={id} value={`m:${id}`}>Anggota dari {agentLabel(agents, id)}</MenuItem>
                ))}
              </TextField>
            </Stack>
          ))}
        </Stack>
        <Alert severity="info" icon={false} sx={{ mt: 1.5 }}>
          Anggota yang dipindah ke master lain membawa kontak yang dipegangnya. Anggota yang dilepas mengembalikan kontaknya ke master lama.
        </Alert>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Batal</Button>
        <Button variant="contained" disabled={save.isPending} onClick={() => { void submit(); }}>{save.isPending ? 'Menyimpan…' : 'Simpan'}</Button>
      </DialogActions>
    </Dialog>
  );
}

// BlastMultiPanel = gerbang menu: hanya master agent yang membuka panel Blast. Nomor lain
// melihat posisinya di struktur; super admin bisa mengatur struktur dari sini.
export default function BlastMultiPanel({ agentId }: { agentId: number }) {
  const { data: agents = [], isLoading } = useAgents();
  const { data: me } = useMe();
  const [setupOpen, setSetupOpen] = useState(false);
  const self = agents.find(a => a.id === agentId);
  const masters = agents.filter(a => a.is_blast_master);
  return (
    <Box>
      <PageHeader title="Blast Multiple Number"
        subtitle="Kirim satu kampanye dari beberapa nomor sekaligus. Tiap kontak dipegang satu nomor seterusnya."
        action={me?.is_super_admin ? (
          <Button variant="outlined" startIcon={<ManageAccountsIcon />} onClick={() => setSetupOpen(true)}>Atur master & anggota</Button>
        ) : undefined} />
      {isLoading ? (
        <Box sx={{ textAlign: 'center', py: 4 }}><CircularProgress /></Box>
      ) : self?.is_blast_master ? (
        <MasterPanel key={agentId} agentId={agentId} />
      ) : (
        <Card>
          <CardContent>
            <Alert severity="info" sx={{ mb: 1.5 }}>
              {self?.blast_master_id
                ? <><b>{self.name || `Nomor ${agentId}`}</b> adalah anggota master <b>{agentLabel(agents, self.blast_master_id)}</b>. Blast dikelola dari master: pilih <b>{agentLabel(agents, self.blast_master_id)}</b> di dropdown Customer Service.</>
                : <><b>{self?.name || `Nomor ${agentId}`}</b> bukan master agent dan belum jadi anggota master mana pun.</>}
            </Alert>
            <Typography variant="subtitle2" sx={{ fontWeight: 800, mb: 0.75 }}>Struktur saat ini</Typography>
            {masters.length === 0 ? (
              <Typography variant="body2" color="text.secondary">
                Belum ada master agent. {me?.is_super_admin ? 'Klik "Atur master & anggota" untuk menentukannya.' : 'Minta super admin menentukan master agent.'}
              </Typography>
            ) : (
              <Stack spacing={1}>
                {masters.map(m => {
                  const members = agents.filter(a => a.blast_master_id === m.id);
                  return (
                    <Box key={m.id} sx={{ p: 1, border: '1px solid', borderColor: 'divider', borderRadius: 1.5 }}>
                      <Stack direction="row" sx={{ alignItems: 'center', gap: 0.75, mb: 0.5 }}>
                        <Typography variant="body2" sx={{ fontWeight: 700 }}>{m.name || `Nomor ${m.id}`}</Typography>
                        <Chip size="small" color="primary" variant="outlined" label="Master" />
                      </Stack>
                      <Stack direction="row" sx={{ gap: 0.5, flexWrap: 'wrap' }}>
                        {members.length === 0
                          ? <Typography variant="caption" color="text.secondary">Belum ada anggota.</Typography>
                          : members.map(a => <Chip key={a.id} size="small" variant="outlined" label={a.name || `Nomor ${a.id}`} />)}
                      </Stack>
                    </Box>
                  );
                })}
              </Stack>
            )}
          </CardContent>
        </Card>
      )}
      {setupOpen && <MasterSetupDialog agents={agents} onClose={() => setSetupOpen(false)} />}
    </Box>
  );
}

function MasterPanel({ agentId }: { agentId: number }) {
  const k = (name: string) => `blast-multi:${agentId}:${name}`;
  const [message, setMessage] = useDraftState(k('message'), '');
  const [numbersText, setNumbersText] = useDraftState(k('numbers'), '');
  const [extraIds, setExtraIds] = useDraftState<number[]>(k('extraIds'), []);
  const [overrides, setOverrides] = useDraftState<Record<number, Override>>(k('overrides'), {});
  const [activeTab, setActiveTab] = useDraftState<number>(k('tab'), 0);
  const [importing, setImporting] = useState(false);
  const [spinNonce, setSpinNonce] = useState(0);
  const [page, setPage] = useState(1);
  const [openId, setOpenId] = useState<number | null>(null);
  const [historyFilter, setHistoryFilter] = useState<BroadcastHistoryFilter>(EMPTY_BROADCAST_FILTER);
  // Centang di tabel global = penerima Blast.
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const {
    minDelay, maxDelay, restEvery, restDuration, setMinDelay, setMaxDelay, setRestEvery, setRestDuration,
    delayProblem, saveDefault, saving, savedAsDefault,
  } = useBlastDelay(k);

  const { data: agents = [] } = useAgents();
  const { data: statusMap = {} } = useAgentStatuses();
  const createBroadcast = useCreateBroadcast(agentId);
  const cancelBroadcast = useCancelBroadcast(agentId);
  const importM = useImportMultiBlastContacts(agentId);
  const distributeM = useDistributeMultiBlastContacts(agentId);
  const { data: bpage } = useBroadcasts(agentId, page, historyFilter);
  // Ringkasan data kontak (kolom, beban per nomor, jumlah) + baris untuk pratinjau.
  const { data: summary } = useMultiBlastContacts(agentId, { page: 1, q: '', agent_id: '' });
  const broadcasts = bpage?.data || [];
  const columns = summary?.columns || [];
  const loadOf = (id: number) => summary?.per_agent?.[String(id)] || 0;

  const isOnline = (id: number) => statusMap[id] === 'connected';
  // agentId = master agent: pemilik kampanye yang hanya mengatur, bukan pengirim.
  // Tab pengirim = nomor yang dipasang sebagai anggota master ini (lihat MasterSetupDialog).
  const orderedAgents = useMemo(() => agents.filter(a => a.blast_master_id === agentId), [agents, agentId]);
  const selectedIds = useMemo(
    () => extraIds.filter(id => orderedAgents.some(a => a.id === id)),
    [extraIds, orderedAgents],
  );
  const onlineCount = selectedIds.filter(isOnline).length;
  // Tab pertama = master (agentId): impor & assign. Tab lain = nomor pengirim.
  const tabValue = orderedAgents.some(a => a.id === activeTab) ? activeTab : agentId;
  const masterName = agents.find(a => a.id === agentId)?.name || 'CS Utama';

  // Penerima tambahan di luar tabel kontak: penerima khusus per nomor (terkunci) + textarea.
  const extraRecipients = useMemo(() => {
    const seen = new Set<string>();
    const out: { number: string; name: string; agent_id?: number }[] = [];
    const push = (r: (typeof out)[number]) => {
      if (!r.number || seen.has(r.number)) return;
      seen.add(r.number);
      out.push(r);
    };
    for (const id of selectedIds) {
      for (const line of (overrides[id]?.numbers || '').split('\n')) push({ number: normalizePhone(line), name: '', agent_id: id });
    }
    for (const line of numbersText.split('\n')) push({ number: normalizePhone(line), name: '' });
    return out;
  }, [numbersText, overrides, selectedIds]);
  // Kontak dari tabel: yang dicentang, atau semua kalau tidak ada yang dicentang.
  const contactCount = selected.size > 0 ? selected.size : (summary?.all || 0);
  const recipientCount = contactCount + extraRecipients.length;

  const placeholders = ['nama', ...columns.map(h => h.key).filter(key => key !== 'nama')];
  const previewRow = summary?.data?.[0];
  const preview = useMemo(() => {
    if (!message.trim()) return '';
    let s = spinPreview(message).replaceAll('{nama}', previewRow?.name || 'kak');
    for (const [key, v] of Object.entries(previewRow ? parseVars(previewRow) : {})) s = s.replaceAll(`{${key}}`, v);
    return s;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- spinNonce sengaja jadi pemicu acak ulang
  }, [message, previewRow, spinNonce]);

  const onFile = async (file?: File) => {
    if (!file) return;
    setImporting(true);
    try {
      const parsed = await parseXlsx(file);
      if (parsed.rows.length === 0) { swalToast('File kosong atau tidak terbaca.', 'warning'); return; }
      const phone = guessPhoneCol(parsed.columns, parsed.rows);
      if (!phone) { swalToast('Kolom nomor WhatsApp tidak ditemukan. Beri header seperti "Nomor" / "No HP".', 'error'); return; }
      const nameCol = parsed.columns.find(h => NAME_KEY.test(h.key))?.key || '';
      const rows = parsed.rows.map(r => ({ number: normalizePhone(r[phone] || ''), name: nameCol ? r[nameCol] || '' : '', vars: r })).filter(r => r.number);
      const res = await importM.mutateAsync({ columns: parsed.columns, rows });
      swalToast(`${res.inserted} kontak baru disimpan, ${res.skipped} sudah ada (dilewati).`);
    } catch (error) {
      swalToast(errorText(error, 'File .xlsx tidak bisa diimpor.'), 'error');
    } finally {
      setImporting(false);
    }
  };

  const toggleAgent = (id: number, on: boolean) => {
    if (on && selectedIds.length >= MAX_NUMBERS) { swalToast(`Maksimal ${MAX_NUMBERS} nomor.`, 'warning'); return; }
    setExtraIds(prev => on ? [...prev, id] : prev.filter(x => x !== id));
    if (!on) setOverrides(prev => { const next = { ...prev }; delete next[id]; return next; });
  };
  const toggleOverride = (id: number, on: boolean) => setOverrides(prev => {
    const next = { ...prev };
    if (on) next[id] = { min_delay: minDelay, max_delay: maxDelay, rest_every: restEvery, rest_duration: restDuration, message: '', numbers: '' };
    else delete next[id];
    return next;
  });

  // Target bagi rata: nomor yang di-switch "ikut Blast"; kalau belum ada, semua nomor pengirim.
  const distributeTargets = selectedIds.length ? selectedIds : orderedAgents.map(a => a.id);
  const distribute = async () => {
    const ok = await swalConfirm(
      `Bagi rata ${summary?.unassigned || 0} kontak yang belum di-assign?`,
      `Dibagi ke ${distributeTargets.length} nomor (${selectedIds.length ? 'yang ikut Blast' : 'semua nomor pengirim'}), memperhitungkan beban yang sudah ada.`,
    );
    if (!ok) return;
    try {
      const res = await distributeM.mutateAsync({ agent_ids: distributeTargets });
      swalToast(`${res.assigned} kontak dibagi ke ${Object.keys(res.per_agent).length} nomor.`);
    } catch (error) { swalToast(errorText(error, 'Gagal membagi kontak.'), 'error'); }
  };

  const problems: string[] = [];
  // Pesan global boleh kosong hanya kalau setiap nomor terpilih punya pesan khusus.
  if (!message.trim() && selectedIds.some(id => !overrides[id]?.message.trim())) problems.push('Pesan kosong');
  if (recipientCount === 0) problems.push('Belum ada penerima');
  if (recipientCount > MAX_RECIPIENTS) problems.push(`Maksimal ${MAX_RECIPIENTS} penerima, centang sebagian kontak`);
  if (selectedIds.length < 1) problems.push('Pilih minimal 1 nomor pengirim');
  if (onlineCount === 0) problems.push('Minimal satu nomor pengirim harus tersambung');
  if (delayProblem) problems.push(delayProblem);

  const doSend = async () => {
    if (problems.length) return;
    const ok = await swalConfirm(
      `Mulai Blast ke ${recipientCount} nomor dengan ${selectedIds.length} pengirim?`,
      `${selected.size > 0 ? `${selected.size} kontak yang dicentang` : 'Semua kontak di data'} ${extraRecipients.length ? `+ ${extraRecipients.length} nomor tambahan` : ''}. Kontak yang sudah punya penanggung jawab dikirim nomor itu; yang belum, dibagi rata lalu dipegang nomor pengirimnya seterusnya.`,
    );
    if (!ok) return;
    try {
      const res = await createBroadcast.mutateAsync({
        message, recipients: extraRecipients, min_delay: minDelay, max_delay: maxDelay, rest_every: restEvery, rest_duration: restDuration,
        file: null, safety: defaultBroadcastSafetyForm(), agent_ids: selectedIds, assign_mode: 'history',
        // numbers sudah dilebur ke recipients (agent_id); backend hanya butuh jeda + pesan.
        agent_settings: Object.fromEntries(Object.entries(overrides)
          .filter(([id]) => selectedIds.includes(Number(id)))
          .map(([id, o]) => [id, { min_delay: o.min_delay, max_delay: o.max_delay, rest_every: o.rest_every, rest_duration: o.rest_duration, message: o.message }])),
        contact_ids: selected.size > 0 ? [...selected] : undefined,
        contact_all: selected.size === 0,
      });
      setMessage(''); setNumbersText(''); setOverrides({}); setSelected(new Set()); setPage(1);
      const skipped = res.skipped_other_agent || 0;
      swalToast(`Blast dimulai untuk ${res.data.total} penerima.${skipped ? ` ${skipped} kontak dilewati karena ditangani nomor yang tidak ikut.` : ''}`, skipped ? 'warning' : 'success');
    } catch (error) {
      swalToast(errorText(error, 'Blast belum bisa dimulai.'), 'error');
    }
  };

  const cancel = async (b: Broadcast) => {
    if (!await swalConfirm('Batalkan Blast ini?', 'Pesan yang sudah terkirim tidak bisa ditarik.')) return;
    cancelBroadcast.mutate(b.id);
  };

  return (
    <Box>
      <Card sx={{ mb: 2 }}>
        <CardContent>
          <SectionTitle icon={<SyncAltIcon fontSize="small" />} title="Nomor pengirim"
            subtitle={`${selectedIds.length} dari maksimal ${MAX_NUMBERS} nomor ikut Blast, ${onlineCount} tersambung. Tab ${masterName} = master: impor data & assign kontak ke nomor, tidak mengirim.`} />
          <Tabs value={tabValue} onChange={(_, v: number) => setActiveTab(v)} variant="scrollable" scrollButtons="auto"
            sx={{ borderBottom: '1px solid', borderColor: 'divider', minHeight: 40, '& .MuiTab-root': { minHeight: 40, textTransform: 'none' } }}>
            <Tab value={agentId} iconPosition="start" icon={<TableChartIcon fontSize="small" />}
              label={<Stack direction="row" sx={{ alignItems: 'center', gap: 0.5 }}>
                <span style={{ fontWeight: 700 }}>{masterName}</span>
                <Chip size="small" color="primary" variant="outlined" label="Master" sx={{ height: 18, '& .MuiChip-label': { px: 0.75, fontSize: 11 } }} />
              </Stack>} />
            {orderedAgents.map(a => {
              const on = selectedIds.includes(a.id);
              return (
                <Tab key={a.id} value={a.id} iconPosition="start"
                  icon={on ? <CheckCircleIcon fontSize="small" color="primary" /> : <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: isOnline(a.id) ? 'success.main' : 'text.disabled', mr: 0.5 }} />}
                  label={<Stack direction="row" sx={{ alignItems: 'center', gap: 0.5 }}>
                    <span style={{ fontWeight: on ? 700 : 500 }}>{a.name || `Nomor ${a.id}`}</span>
                    <Chip size="small" label={loadOf(a.id)} sx={{ height: 18, '& .MuiChip-label': { px: 0.75, fontSize: 11 } }} />
                  </Stack>} />
              );
            })}
          </Tabs>
          {tabValue === agentId && (
            <Stack spacing={1.5} sx={{ p: 1.25, border: '1px solid', borderColor: 'divider', borderTop: 'none', borderRadius: '0 0 8px 8px' }}>
              <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { sm: 'center' }, gap: 1, flexWrap: 'wrap' }}>
                <Box sx={{ minWidth: 0, flex: 1 }}>
                  <Typography variant="body2" sx={{ fontWeight: 700 }}>{masterName} · Master</Typography>
                  <Typography variant="caption" color="text.secondary">
                    {summary?.all || 0} kontak, {summary?.unassigned || 0} belum punya penanggung jawab. Kontak yang di-assign langsung muncul di tab nomornya.
                  </Typography>
                </Box>
                <Button component="label" size="small" variant="contained" disabled={importing}
                  startIcon={importing ? <CircularProgress size={14} color="inherit" /> : <UploadFileIcon />}>
                  Impor file .xlsx
                  <input type="file" hidden accept=".xlsx,.xls" onChange={e => { void onFile(e.target.files?.[0]); e.target.value = ''; }} />
                </Button>
                <Button size="small" variant="outlined" disabled={distributeM.isPending || !summary?.unassigned || distributeTargets.length === 0} onClick={() => { void distribute(); }}>
                  Bagi rata {summary?.unassigned || 0} kontak baru ke {distributeTargets.length} nomor
                </Button>
              </Stack>
              <Typography variant="caption" color="text.secondary">
                Tabel ini hanya kontak yang belum ditentukan. Centang lalu pilih nomor untuk assign; kontak pindah ke tab nomor itu. Kontak yang dicentang di tab mana pun jadi penerima Blast.
              </Typography>
              {/* agents = nomor pengirim saja: master tidak boleh jadi tujuan assign. */}
              <ContactTable agentId={agentId} agents={orderedAgents} columns={columns} fixedAgent={0} selected={selected} setSelected={setSelected} />
            </Stack>
          )}
          {orderedAgents.filter(a => a.id === tabValue).map(a => (
            <AgentTab key={a.id} agentId={agentId} agent={a} agents={orderedAgents} columns={columns} selected={selected} setSelected={setSelected}
              online={isOnline(a.id)} participating={selectedIds.includes(a.id)} load={loadOf(a.id)}
              override={overrides[a.id]} placeholders={placeholders}
              onToggleParticipate={on => toggleAgent(a.id, on)}
              onToggleOverride={on => toggleOverride(a.id, on)}
              onChangeOverride={v => setOverrides(prev => ({ ...prev, [a.id]: v }))} />
          ))}
          {orderedAgents.length === 0 && <Alert severity="info" sx={{ mt: 1 }}>Belum ada nomor pengirim. Tambahkan nomor lewat menu Tim CS.</Alert>}
          {selectedIds.length > onlineCount && onlineCount > 0 && (
            <Alert severity="warning" sx={{ mt: 1 }}>
              {selectedIds.length - onlineCount} nomor yang ikut masih offline. Kontak yang dipegang nomor itu menunggu sampai nomornya tersambung; kontak baru dibagi ke nomor yang online.
            </Alert>
          )}
        </CardContent>
      </Card>

      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', lg: 'minmax(0, 1.55fr) minmax(300px, 0.85fr)' }, gap: 2, alignItems: 'start' }}>
        <Card>
          <CardContent>
            <Stack spacing={2.25}>
              <Box>
                <SectionTitle icon={<PeopleAltIcon fontSize="small" />} title="Penerima"
                  subtitle={`${contactCount} dari data kontak${extraRecipients.length ? ` + ${extraRecipients.length} nomor tambahan` : ''}`} />
                <Alert severity="info" icon={false} sx={{ mb: 1 }}>
                  {selected.size > 0
                    ? `${selected.size} kontak dicentang (tab master atau tab nomor) akan jadi penerima.`
                    : `Tidak ada kontak dicentang: semua ${summary?.all || 0} kontak di data jadi penerima. Centang sebagian di tab ${masterName} atau tab nomor untuk membatasi.`}
                </Alert>
                <TextField fullWidth multiline minRows={2} size="small" label="Nomor tambahan di luar data kontak (satu per baris)" value={numbersText}
                  onChange={e => setNumbersText(e.target.value)} placeholder={'628123456789\n08987654321'} />
              </Box>

              <Divider />

              <Box>
                <SectionTitle icon={<EditNoteIcon fontSize="small" />} title="Pesan global" subtitle={`${message.length}/2000 karakter. Dipakai nomor yang tidak punya pesan khusus.`}
                  action={<TemplatePicker label="Pakai template" agentId={agentId} onPick={b => setMessage(m => m ? m + '\n' + b : b)} />} />
                <WhatsAppEditor value={message} onChange={setMessage}
                  placeholder="Halo {nama}, pesanan kamu dengan resi {no_resi} dikirim tanggal {tanggal}…" />
                <Stack direction="row" sx={{ alignItems: 'center', gap: 0.5, flexWrap: 'wrap', mt: 1 }}>
                  <Typography variant="caption" color="text.secondary">Seret variabel ke pesan (atau klik):</Typography>
                  {placeholders.map(p => (
                    <VarChip key={p} name={p} onClick={() => setMessage(m => m + `{${p}}`)} />
                  ))}
                </Stack>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
                  Variasi kalimat: <code>{'{Halo|Hai|Selamat pagi}'}</code> memilih satu acak per penerima. Variabel diambil dari kolom data kontak.
                </Typography>
                {preview && (
                  <Alert severity="info" sx={{ mt: 1 }} action={<Button size="small" startIcon={<RefreshIcon />} onClick={() => setSpinNonce(n => n + 1)}>Acak lagi</Button>}>
                    <Typography variant="caption" sx={{ fontWeight: 700, display: 'block' }}>Pratinjau{previewRow ? ` untuk +${previewRow.number}` : ''}</Typography>
                    <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{preview}</Typography>
                  </Alert>
                )}
              </Box>

              <Divider />

              <Box>
                <SectionTitle icon={<ScheduleIcon fontSize="small" />} title="Setelan global" subtitle="Berlaku untuk semua nomor yang tidak punya setelan khusus." />
                <DelayFields minDelay={minDelay} maxDelay={maxDelay} restEvery={restEvery} restDuration={restDuration}
                  setMinDelay={setMinDelay} setMaxDelay={setMaxDelay} setRestEvery={setRestEvery} setRestDuration={setRestDuration}
                  error={delayProblem || undefined} onSave={saveDefault} saving={saving} savedAsDefault={savedAsDefault} />
              </Box>
            </Stack>
          </CardContent>
        </Card>

        <Card sx={{ position: { lg: 'sticky' }, top: 16 }}>
          <CardContent>
            <SectionTitle icon={<SendIcon fontSize="small" />} title="Review" subtitle="Ringkasan sebelum mengirim." />
            <Stack spacing={1}>
              <Typography variant="body2">Pengirim: <b>{selectedIds.length} nomor</b>{Object.keys(overrides).length ? ` (${Object.keys(overrides).length} setelan khusus)` : ''}</Typography>
              <Typography variant="body2">Penerima: <b>{recipientCount}</b> · {contactCount} dari data kontak{extraRecipients.length ? `, ${extraRecipients.length} tambahan` : ''}</Typography>
              <Typography variant="body2">Jeda global: <b>{minDelay}-{maxDelay} dtk</b>{restEvery > 0 ? `, istirahat ${restDuration} dtk tiap ${restEvery} pesan` : ''}</Typography>
              {problems.length > 0 && <Alert severity="warning" icon={false}>{problems.join(' · ')}</Alert>}
              <Button fullWidth variant="contained" startIcon={createBroadcast.isPending ? <CircularProgress size={16} /> : <SendIcon />}
                onClick={doSend} disabled={createBroadcast.isPending || problems.length > 0}>
                {createBroadcast.isPending ? 'Memulai Blast…' : `Mulai Blast (${recipientCount})`}
              </Button>
              <Typography variant="caption" color="text.secondary">
                Kontak yang sudah punya penanggung jawab selalu dikirim nomor itu. Kontak yang belum, dibagi rata dan langsung dipegang nomor pengirimnya untuk Blast berikutnya.
              </Typography>
            </Stack>
          </CardContent>
        </Card>
      </Box>

      <Card sx={{ mt: 2 }}>
        <CardContent>
          <SectionTitle icon={<HistoryIcon fontSize="small" />} title="Riwayat Blast" subtitle="Buka item untuk melihat pembagian per nomor." />
          <BroadcastHistorySummary agentId={agentId} agents={orderedAgents} filter={historyFilter} onChange={f => { setHistoryFilter(f); setPage(1); }} />
          {broadcasts.length === 0 ? (
            <Alert severity="info" icon={false}>Belum ada Blast untuk nomor ini.</Alert>
          ) : (
            <Stack spacing={1}>
              {broadcasts.map(b => {
                const open = openId === b.id;
                const canCancel = ['pending', 'running', 'resuming', 'interrupted', 'wa_restricted', 'cancel_requested'].includes(b.status);
                return (
                  <Box key={b.id} sx={{ p: 1.25, border: '1px solid', borderColor: 'divider', borderRadius: 1 }}>
                    <Stack direction="row" sx={{ alignItems: 'flex-start', gap: 1 }}>
                      <IconButton size="small" onClick={() => setOpenId(open ? null : b.id)}>
                        <ExpandMoreIcon fontSize="small" sx={{ transform: open ? 'rotate(180deg)' : 'none', transition: 'transform .15s' }} />
                      </IconButton>
                      <Box sx={{ minWidth: 0, flex: 1 }}>
                        <Stack direction="row" sx={{ gap: 0.75, alignItems: 'center', flexWrap: 'wrap' }}>
                          <Typography variant="caption" color="text.secondary">
                            {new Date(b.created_at).toLocaleString('id-ID', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })}
                          </Typography>
                          {b.assign_mode === 'history' ? <Chip size="small" color="primary" variant="outlined" label="Multi nomor" /> : b.agent_ids ? <Chip size="small" variant="outlined" label="Rotasi" /> : null}
                          <Chip label={STATUS_LABEL[b.status] ?? b.status} size="small" color={STATUS_COLOR[b.status] ?? 'default'} />
                        </Stack>
                        <Typography variant="body2" sx={{ fontWeight: 700, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{b.message}</Typography>
                        <BroadcastProgress broadcast={b} />
                      </Box>
                      {canCancel && (
                        <Button size="small" color="error" variant="outlined" disabled={cancelBroadcast.isPending} onClick={() => cancel(b)}>Batalkan</Button>
                      )}
                    </Stack>
                    <Collapse in={open} unmountOnExit><Box sx={{ mt: 1 }}><MultiDetail agentId={agentId} bid={b.id} /></Box></Collapse>
                  </Box>
                );
              })}
              <PagerBar page={page} total={bpage?.total || 0} pageSize={bpage?.limit || 10} onPage={setPage} />
            </Stack>
          )}
        </CardContent>
      </Card>
    </Box>
  );
}
