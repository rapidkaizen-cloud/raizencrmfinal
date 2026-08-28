import { useState } from 'react';
import { useDraftState } from '../draft';
import {
  Alert, Box, Button, Checkbox, Chip, CircularProgress, FormControlLabel, IconButton, MenuItem, Paper, Stack,
  Switch, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, TextField, Tooltip, Typography,
  Dialog, DialogTitle, DialogContent, DialogActions,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/Delete';
import GroupIcon from '@mui/icons-material/GroupOutlined';
import KeyIcon from '@mui/icons-material/VpnKeyOutlined';
import InfoIcon from '@mui/icons-material/InfoOutlined';
import { useUsers, useCreateUser, useUpdateUser, useResetUserPassword, useDeleteUser } from '../hooks';
import { FEATURE_AI, FEATURE_AKUN, ROLE_LABEL, type User } from '../types';
import { swalConfirm, swalToast } from '../services/swal';
import PageHeader from './PageHeader';
import EmptyState from './common/EmptyState';

// Isian dialog akun. Password tidak ikut di sini karena tidak boleh disimpan sebagai draft.
interface UserForm {
  id?: number;
  name: string;
  username: string;
  email: string;
  phone: string;
  role: string;
  active: boolean;
  features: string[];
}

const EMPTY: UserForm = { name: '', username: '', email: '', phone: '', role: '', active: true, features: [] };

// Role yang boleh dibuat lewat halaman ini — super admin hanya dari .env, tidak lewat API.
const ROLE_OPTIONS: { value: string; label: string }[] = [
  { value: 'manager', label: 'Manager' },
  { value: 'cs', label: 'CS' },
];

// Centang fitur ekstra. Teks panjangnya sengaja menyebut menu yang dibuka supaya admin tidak salah kasih akses.
const FEATURE_OPTIONS: { value: string; label: string; helper: string }[] = [
  {
    value: FEATURE_AI,
    label: 'Akses fitur AI (Asisten AI + AI Learning)',
    helper: 'Bisa mengubah persona, knowledge, crawl website, dan data belajar AI.',
  },
  {
    value: FEATURE_AKUN,
    label: 'Akses section Akun (AI & Model, Widget, REST API, Pengaturan) — developer only',
    helper: 'Bisa menambah/menghapus CS, mengubah API key, dan setelan sistem. Beri hanya ke orang teknis.',
  },
];

// Warna chip per fitur supaya beda jelas saat dilihat sekilas di tabel.
const FEATURE_CHIP: Record<string, { label: string; color: 'secondary' | 'warning' }> = {
  [FEATURE_AI]: { label: 'AI', color: 'secondary' },
  [FEATURE_AKUN]: { label: 'Akun', color: 'warning' },
};

const SUPER_ADMIN_HINT = 'Akun super admin dikelola lewat .env';

// Backend sudah membalas pesan error dalam Bahasa Indonesia, jadi tampilkan apa adanya.
const errorMessage = (error: unknown, fallback: string): string =>
  (error as { response?: { data?: { error?: string } } } | null)?.response?.data?.error || fallback;

const roleColor = (u: User): 'primary' | 'info' | 'default' => {
  if (u.is_super_admin) return 'primary';
  return u.role === 'manager' ? 'info' : 'default';
};

export default function UsersPanel() {
  const { data: users, isLoading } = useUsers();
  const create = useCreateUser();
  const update = useUpdateUser();
  const resetPassword = useResetUserPassword();
  const del = useDeleteUser();

  // Panel di-unmount saat pindah tab, jadi isian dialog disimpan sebagai draft.
  const k = (name: string) => `users:${name}`;
  const [open, setOpen] = useDraftState(k('open'), false);
  const [form, setForm] = useDraftState<UserForm>(k('form'), EMPTY);
  const [errors, setErrors] = useState<Record<string, string>>({});
  // Password TIDAK pakai draft — jangan sampai tersimpan di sessionStorage.
  const [password, setPassword] = useState<string>('');

  // Dialog reset password terpisah, isinya cuma satu field.
  const [pwTarget, setPwTarget] = useState<User | null>(null);
  const [newPassword, setNewPassword] = useState<string>('');
  const [pwError, setPwError] = useState<string>('');

  const isEdit = !!form.id;

  const openNew = () => {
    setForm(EMPTY);
    setPassword('');
    setErrors({});
    setOpen(true);
  };

  const openEdit = (u: User) => {
    setForm({
      id: u.id,
      name: u.name || '',
      username: u.username || '',
      email: u.email || '',
      phone: u.phone || '',
      role: u.role || '',
      active: u.active !== false,
      features: u.features || [],
    });
    setPassword('');
    setErrors({});
    setOpen(true);
  };

  const clearError = (field: string) => {
    if (errors[field]) setErrors(prev => ({ ...prev, [field]: '' }));
  };

  const toggleFeature = (feature: string, checked: boolean) => {
    setForm(prev => ({
      ...prev,
      features: checked
        ? [...prev.features.filter(f => f !== feature), feature]
        : prev.features.filter(f => f !== feature),
    }));
  };

  const validate = () => {
    const e: Record<string, string> = {};
    const username = form.username.trim();
    if (!username) e.username = 'Wajib diisi';
    else if (/\s/.test(username)) e.username = 'Tidak boleh ada spasi';
    if (!form.role) e.role = 'Pilih role dulu';
    if (!isEdit && password.length < 8) e.password = 'Minimal 8 karakter';
    setErrors(e);
    return Object.keys(e).length === 0;
  };

  const submit = async () => {
    if (!validate()) return;
    const base = {
      name: form.name.trim(),
      email: form.email.trim(),
      phone: form.phone.trim(),
      role: form.role,
      features: form.features,
    };
    try {
      if (form.id) {
        // Endpoint update tidak menerima password; `active` wajib ikut supaya tidak ter-reset jadi nonaktif.
        await update.mutateAsync({ id: form.id, ...base, active: form.active });
        swalToast('Akun disimpan');
      } else {
        await create.mutateAsync({ ...base, username: form.username.trim(), password });
        swalToast('Akun ditambahkan');
      }
      setOpen(false);
      setForm(EMPTY);
      setPassword('');
    } catch (error) {
      swalToast(errorMessage(error, 'Akun belum bisa disimpan.'), 'error');
    }
  };

  // Switch aktif/nonaktif langsung menyimpan. Field lain ikut dikirim ulang supaya tidak terhapus.
  const toggleActive = (u: User, active: boolean) => {
    update.mutate(
      {
        id: u.id,
        name: u.name,
        email: u.email,
        phone: u.phone,
        role: u.role,
        active,
        features: u.features || [],
      },
      {
        onSuccess: () => swalToast(active ? 'Akun diaktifkan' : 'Akun dinonaktifkan'),
        onError: (error: unknown) => swalToast(errorMessage(error, 'Gagal mengubah status akun'), 'error'),
      },
    );
  };

  const openReset = (u: User) => {
    setPwTarget(u);
    setNewPassword('');
    setPwError('');
  };

  const submitReset = async () => {
    if (!pwTarget) return;
    if (newPassword.length < 8) {
      setPwError('Minimal 8 karakter');
      return;
    }
    try {
      await resetPassword.mutateAsync({ id: pwTarget.id, new_password: newPassword });
      swalToast('Password diganti');
      setPwTarget(null);
      setNewPassword('');
    } catch (error) {
      swalToast(errorMessage(error, 'Password belum bisa diganti.'), 'error');
    }
  };

  const remove = (u: User) => {
    swalConfirm('Hapus akun ini?', `Akun "${u.name || u.username}" tidak bisa login lagi.`)
      .then((ok: boolean) => {
        if (!ok) return;
        del.mutate(u.id, {
          onSuccess: () => swalToast('Akun dihapus'),
          onError: (error: unknown) => swalToast(errorMessage(error, 'Gagal menghapus akun'), 'error'),
        });
      });
  };

  if (isLoading) return <Box sx={{ display: 'flex', justifyContent: 'center', mt: 8 }}><CircularProgress /></Box>;

  return (
    <Box>
      <PageHeader title="Tim & Akses"
        subtitle="Buat akun untuk manager dan CS, atur siapa yang boleh membuka apa, dan nonaktifkan akun yang sudah tidak dipakai."
        action={<Button variant="contained" startIcon={<AddIcon />} onClick={openNew}>Tambah Akun</Button>} />

      <Alert severity="info" icon={<InfoIcon fontSize="small" />} sx={{ mb: 2 }}>
        <Typography variant="body2">
          Manager dan CS otomatis <b>tidak bisa</b> membuka Asisten AI, AI Learning, dan seluruh section Akun
          (AI &amp; Model, Widget, REST API, Pengaturan). Kalau ada yang memang perlu, beri centang fitur di form akunnya.
          Semua menu lain — Inbox, Kontak, Blast, Produk, Grup, Alur — terbuka untuk semua role.
        </Typography>
      </Alert>

      {(!users || users.length === 0) ? (
        <EmptyState
          icon={<GroupIcon sx={{ fontSize: 48 }} />}
          title="Belum ada akun tim"
          description="Tambahkan akun untuk manager atau CS supaya mereka bisa login dengan kredensial sendiri."
          actionLabel="Tambah Akun"
          onAction={openNew}
        />
      ) : (
        <Paper variant="outlined">
          {/* Tabel dibungkus TableContainer + minWidth: di layar kecil geser samping, bukan melebar keluar. */}
          <TableContainer sx={{ overflowX: 'auto' }}>
            <Table size="small" sx={{ minWidth: 720 }}>
              <TableHead>
                <TableRow>
                  <TableCell sx={{ fontWeight: 700 }}>Nama</TableCell>
                  <TableCell sx={{ fontWeight: 700 }}>Username</TableCell>
                  <TableCell sx={{ fontWeight: 700 }}>Role</TableCell>
                  <TableCell sx={{ fontWeight: 700 }}>Aktif</TableCell>
                  <TableCell sx={{ fontWeight: 700, minWidth: 140 }}>Fitur</TableCell>
                  <TableCell sx={{ fontWeight: 700, width: 132 }} align="right">Aksi</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {users.map(u => {
                  const locked = u.is_super_admin; // baris super admin cuma bisa dilihat
                  const features = u.features || [];
                  return (
                    <TableRow key={u.id} hover>
                      <TableCell>
                        <Typography sx={{ fontWeight: 600, fontSize: 13 }}>{u.name || '—'}</Typography>
                        {u.email && <Typography variant="caption" color="text.secondary">{u.email}</Typography>}
                      </TableCell>
                      <TableCell sx={{ fontSize: 13 }}>{u.username}</TableCell>
                      <TableCell>
                        <Chip size="small" color={roleColor(u)} label={ROLE_LABEL[u.role] || u.role} />
                      </TableCell>
                      <TableCell>
                        <Tooltip title={locked ? SUPER_ADMIN_HINT : (u.active !== false ? 'Klik untuk menonaktifkan' : 'Klik untuk mengaktifkan')}>
                          <span>
                            <Switch
                              size="small"
                              checked={u.active !== false}
                              disabled={locked || update.isPending}
                              onChange={e => toggleActive(u, e.target.checked)}
                            />
                          </span>
                        </Tooltip>
                      </TableCell>
                      <TableCell>
                        {locked ? (
                          <Chip size="small" color="success" variant="outlined" label="Semua akses" />
                        ) : features.length === 0 ? (
                          <Typography variant="body2" color="text.secondary">—</Typography>
                        ) : (
                          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5 }}>
                            {features.map(f => (
                              <Chip key={f} size="small" variant="outlined"
                                color={FEATURE_CHIP[f]?.color} label={FEATURE_CHIP[f]?.label || f} />
                            ))}
                          </Stack>
                        )}
                      </TableCell>
                      <TableCell align="right">
                        <Stack direction="row" spacing={0.25} sx={{ justifyContent: 'flex-end' }}>
                          <Tooltip title={locked ? SUPER_ADMIN_HINT : 'Edit akun'}>
                            <span>
                              <IconButton size="small" disabled={locked} onClick={() => openEdit(u)}>
                                <EditIcon fontSize="small" />
                              </IconButton>
                            </span>
                          </Tooltip>
                          <Tooltip title={locked ? SUPER_ADMIN_HINT : 'Reset password'}>
                            <span>
                              <IconButton size="small" disabled={locked} onClick={() => openReset(u)}>
                                <KeyIcon fontSize="small" />
                              </IconButton>
                            </span>
                          </Tooltip>
                          <Tooltip title={locked ? SUPER_ADMIN_HINT : 'Hapus akun'}>
                            <span>
                              <IconButton size="small" color="error" disabled={locked} onClick={() => remove(u)}>
                                <DeleteIcon fontSize="small" />
                              </IconButton>
                            </span>
                          </Tooltip>
                        </Stack>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </TableContainer>
        </Paper>
      )}

      <Dialog open={open} onClose={() => setOpen(false)} fullWidth maxWidth="sm">
        <DialogTitle>{isEdit ? 'Edit Akun' : 'Akun Baru'}</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField label="Nama" value={form.name} size="small"
              onChange={e => setForm({ ...form, name: e.target.value })}
              placeholder="Budi Santoso" helperText="Nama yang tampil di dashboard dan riwayat chat." />

            {/* Backend menyimpan username huruf kecil semua, jadi ditampilkan apa adanya
                di sini supaya admin melihat username final yang harus diserahkan ke CS. */}
            <TextField label="Username" value={form.username} size="small"
              onChange={e => { setForm({ ...form, username: e.target.value.toLowerCase() }); clearError('username'); }}
              placeholder="budi" disabled={isEdit} error={!!errors.username}
              helperText={errors.username || (isEdit ? 'Username tidak bisa diubah setelah akun dibuat.' : 'Dipakai untuk login. Otomatis jadi huruf kecil, tanpa spasi, harus unik.')} />

            <TextField label="Email" value={form.email} size="small" type="email"
              onChange={e => setForm({ ...form, email: e.target.value })}
              placeholder="budi@tokomu.com" helperText="Opsional, dipakai kalau nanti butuh pemulihan akun." />

            <TextField label="Nomor HP" value={form.phone} size="small"
              onChange={e => setForm({ ...form, phone: e.target.value })}
              placeholder="08123456789" helperText="Opsional, untuk kontak internal saja." />

            {!isEdit && (
              <TextField label="Password" value={password} size="small" type="password"
                onChange={e => { setPassword(e.target.value); clearError('password'); }}
                error={!!errors.password}
                helperText={errors.password || 'Minimal 8 karakter. Beritahukan ke orangnya, dia bisa ganti sendiri nanti.'} />
            )}

            <TextField label="Role" value={form.role} size="small" select
              onChange={e => { setForm({ ...form, role: e.target.value }); clearError('role'); }}
              error={!!errors.role}
              helperText={errors.role || 'Manager dan CS punya akses yang sama; bedanya cuma label tim.'}>
              {ROLE_OPTIONS.map(item => <MenuItem key={item.value} value={item.value}>{item.label}</MenuItem>)}
            </TextField>

            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
                Fitur tambahan (default: tidak diberikan)
              </Typography>
              <Stack>
                {FEATURE_OPTIONS.map(item => (
                  <Box key={item.value}>
                    <FormControlLabel
                      control={
                        <Checkbox
                          size="small"
                          checked={form.features.includes(item.value)}
                          onChange={e => toggleFeature(item.value, e.target.checked)}
                        />
                      }
                      label={<Typography variant="body2">{item.label}</Typography>}
                    />
                    <Typography variant="caption" color="text.secondary" sx={{ display: 'block', ml: 4, mb: 0.5 }}>
                      {item.helper}
                    </Typography>
                  </Box>
                ))}
              </Stack>
            </Box>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpen(false)}>Batal</Button>
          <Button variant="contained" onClick={submit} disabled={create.isPending || update.isPending}>Simpan</Button>
        </DialogActions>
      </Dialog>

      <Dialog open={!!pwTarget} onClose={() => setPwTarget(null)} fullWidth maxWidth="xs">
        <DialogTitle>Reset Password</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <Typography variant="body2" color="text.secondary">
              Password baru untuk akun <b>{pwTarget?.name || pwTarget?.username}</b>. Password lama langsung tidak berlaku.
            </Typography>
            <TextField label="Password baru" value={newPassword} size="small" type="password" autoFocus
              onChange={e => { setNewPassword(e.target.value); setPwError(''); }}
              error={!!pwError} helperText={pwError || 'Minimal 8 karakter.'} />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPwTarget(null)}>Batal</Button>
          <Button variant="contained" onClick={submitReset} disabled={resetPassword.isPending}>Simpan</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
