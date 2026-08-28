/*
 * Draft form: simpan isian form yang belum disimpan ke sessionStorage.
 *
 * Panel dashboard di-render dengan pola `{tab === 'x' && <Panel/>}`, jadi begitu user
 * pindah tab/menu komponennya di-unmount dan semua useState-nya ikut hilang.
 * useDraftState dipakai sebagai pengganti useState untuk field yang diketik user,
 * supaya isian balik lagi saat user kembali ke tab itu. Pakai sessionStorage, jadi
 * draft juga tahan refresh halaman tapi ikut hilang saat tab browser ditutup.
 */
import { useCallback, useEffect, useState, type Dispatch, type SetStateAction } from 'react';

const PREFIX = 'wai_draft:';

function storage(): Storage | null {
  try {
    return window.sessionStorage;
  } catch {
    return null; // mode privat / storage diblokir browser
  }
}

function load<T>(key: string, fallback: T): T {
  const s = storage();
  if (!s) return fallback;
  try {
    const raw = s.getItem(PREFIX + key);
    if (raw === null) return fallback;
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

function save(key: string, value: unknown) {
  const s = storage();
  if (!s) return;
  try {
    s.setItem(PREFIX + key, JSON.stringify(value));
  } catch {
    // kuota penuh / storage diblokir: draft dilewat, form tetap jalan normal.
  }
}

/** Hapus satu draft. */
export function clearDraft(key: string) {
  const s = storage();
  if (!s) return;
  try {
    s.removeItem(PREFIX + key);
  } catch {
    // abaikan
  }
}

/** Hapus semua draft yang key-nya diawali prefix ini (prefix kosong = semua draft, dipakai saat logout). */
export function clearDrafts(keyPrefix = '') {
  const s = storage();
  if (!s) return;
  try {
    const full = PREFIX + keyPrefix;
    const doomed: string[] = [];
    for (let i = 0; i < s.length; i++) {
      const k = s.key(i);
      if (k && k.startsWith(full)) doomed.push(k);
    }
    doomed.forEach(k => s.removeItem(k));
  } catch {
    // abaikan
  }
}

/**
 * Sama seperti useState, tapi nilainya ikut ditulis ke sessionStorage dengan key ini.
 * Key sebaiknya memuat agentId supaya draft tiap CS tidak saling tabrak; saat key
 * berubah, nilainya otomatis diambil dari draft milik key yang baru.
 */
export function useDraftState<T>(key: string, initial: T): [T, Dispatch<SetStateAction<T>>] {
  // `initial` ikut disimpan di state supaya nilainya tetap sama saat key berganti,
  // tanpa perlu ref (ref tidak boleh dibaca saat render).
  const [state, setState] = useState<{ key: string; value: T; initial: T }>(() => ({ key, value: load(key, initial), initial }));

  let value = state.value;
  if (state.key !== key) {
    // Ganti key (mis. pindah CS): pakai draft milik key baru, jangan bawa isian CS lama.
    value = load(key, state.initial);
    setState({ key, value, initial: state.initial });
  }

  useEffect(() => {
    // Nilai yang masih sama dengan default tidak perlu disimpan — biar sessionStorage
    // tidak menumpuk entri kosong (mis. draft balasan untuk tiap chat di Inbox).
    if (JSON.stringify(value) === JSON.stringify(state.initial)) clearDraft(key);
    else save(key, value);
  }, [key, value, state.initial]);

  const setValue = useCallback<Dispatch<SetStateAction<T>>>(next => {
    setState(prev => ({
      ...prev,
      key,
      value: typeof next === 'function'
        ? (next as (p: T) => T)(prev.key === key ? prev.value : load(key, prev.initial))
        : next,
    }));
  }, [key]);

  return [value, setValue];
}
