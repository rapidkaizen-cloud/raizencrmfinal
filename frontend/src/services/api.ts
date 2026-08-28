import axios from 'axios';

const api = axios.create({
  baseURL: '/api',
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

api.interceptors.response.use(
  (res) => res,
  (err) => {
    const requestUrl = err.config?.url || '';
    const isLoginRequest = requestUrl === '/login' || requestUrl.endsWith('/login');
    if (err.response?.status === 401 && !isLoginRequest) {
      localStorage.removeItem('token');
      window.location.href = '/login';
    }
    // Akun dinonaktifkan super admin saat user masih login: semua request jadi 403.
    // Tanpa ini dashboard-nya tetap tampil normal (data lama dari cache) tapi mati
    // total tanpa penjelasan, jadi langsung dikeluarkan ke halaman login.
    if (err.response?.status === 403 && err.response?.data?.account_disabled && !isLoginRequest) {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      window.location.href = '/login?disabled=1';
    }
    return Promise.reject(err);
  }
);

export default api;
