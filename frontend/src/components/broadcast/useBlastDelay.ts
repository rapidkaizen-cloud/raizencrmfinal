import { useEffect, useRef } from 'react';
import { useDraftState } from '../../draft';
import { useMe, useSaveBlastDelay } from '../../hooks';
import { readBlastDelayCache, writeBlastDelayCache, type BlastDelay } from '../../types';
import { swalToast } from '../../services/swal';

/**
 * useBlastDelay menyatukan kabel "Jeda Kirim" yang dipakai tab Blast & Jadwal Blast:
 * draft form, nilai default milik akun, dan tombol simpan.
 *
 * Kenapa per akun, bukan per nomor CS: satu orang bisa memegang beberapa nomor, dan
 * ritme kirim yang aman itu kebiasaan orangnya — jadi setelan ikut akun.
 *
 * Urutan nilai awal (dari yang paling diutamakan):
 *   1. draft sessionStorage — user sedang mengetik, jangan pernah ditimpa
 *   2. cache localStorage   — dibaca sinkron, supaya render pertama tidak berkedip
 *   3. /me                  — sumber kebenaran; menimpa (2) hanya kalau field belum disentuh
 *
 * @param k penghasil key draft milik panel pemanggil, mis. `broadcast:3:minDelay`
 */
export function useBlastDelay(k: (name: string) => string) {
  const cached = readBlastDelayCache();
  const [minDelay, setMinDelay] = useDraftState(k('minDelay'), cached.min_delay);
  const [maxDelay, setMaxDelay] = useDraftState(k('maxDelay'), cached.max_delay);
  const [restEvery, setRestEvery] = useDraftState(k('restEvery'), cached.rest_every);
  const [restDuration, setRestDuration] = useDraftState(k('restDuration'), cached.rest_duration);

  const { data: me } = useMe();
  const saveMut = useSaveBlastDelay();

  // Nilai saat komponen pertama dipasang, untuk membedakan "belum disentuh user"
  // dari "sudah diubah user". initialRef sengaja tidak ikut berubah setelahnya.
  const initialRef = useRef(cached);
  const syncedRef = useRef(false);

  useEffect(() => {
    const d = me?.blast_delay;
    if (!d || syncedRef.current) return;
    syncedRef.current = true;
    writeBlastDelayCache(d); // supaya render pertama berikutnya sudah benar
    const awal = initialRef.current;
    const belumDisentuh = minDelay === awal.min_delay && maxDelay === awal.max_delay
      && restEvery === awal.rest_every && restDuration === awal.rest_duration;
    if (!belumDisentuh) return; // user sedang menyusun blast — jangan diganggu
    setMinDelay(d.min_delay);
    setMaxDelay(d.max_delay);
    setRestEvery(d.rest_every);
    setRestDuration(d.rest_duration);
  }, [me, minDelay, maxDelay, restEvery, restDuration,
    setMinDelay, setMaxDelay, setRestEvery, setRestDuration]);

  // Pesan validasi yang sama dipakai untuk menandai field DAN mengunci tombol simpan.
  const delayProblem = minDelay < 1 || maxDelay < 1
    ? 'Jeda harus minimal 1 detik'
    : maxDelay < minDelay
      ? 'Jeda maksimal harus lebih besar atau sama dengan jeda minimal'
      : '';

  const tersimpan: BlastDelay | undefined = me?.blast_delay;
  const savedAsDefault = !!tersimpan
    && tersimpan.min_delay === minDelay
    && tersimpan.max_delay === maxDelay
    && tersimpan.rest_every === restEvery
    && tersimpan.rest_duration === restDuration;

  const saveDefault = async () => {
    if (delayProblem) {
      swalToast(delayProblem, 'error');
      return;
    }
    try {
      await saveMut.mutateAsync({
        min_delay: minDelay, max_delay: maxDelay,
        rest_every: restEvery, rest_duration: restDuration,
      });
      swalToast('Jeda blast disimpan sebagai default akunmu', 'success');
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } } };
      swalToast(err.response?.data?.error || 'Gagal menyimpan pengaturan jeda', 'error');
    }
  };

  return {
    minDelay, maxDelay, restEvery, restDuration,
    setMinDelay, setMaxDelay, setRestEvery, setRestDuration,
    delayProblem,
    saveDefault,
    saving: saveMut.isPending,
    savedAsDefault,
  };
}
