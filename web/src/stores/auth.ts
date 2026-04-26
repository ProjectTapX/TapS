import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type Role = 'admin' | 'user' | 'guest'
export interface User {
  id: number
  username: string
  role: Role
  mustChangePassword?: boolean
  hasPassword?: boolean
}

interface AuthState {
  token: string | null
  user: User | null
  setAuth: (token: string, user: User) => void
  setToken: (token: string) => void
  setUser: (user: User) => void
  logout: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      user: null,
      setAuth: (token, user) => set({ token, user }),
      setToken: (token) => set({ token }),
      setUser: (user) => set({ user }),
      logout: () => set({ token: null, user: null }),
    }),
    {
      name: 'taps-auth',
      // audit-2026-04-25 MED12: only persist the long-lived bits
      // (token + immutable identity). Volatile flags like
      // mustChangePassword / hasPassword would otherwise stick in
      // localStorage past the server-side change that cleared them
      // — an admin clearing mustChangePassword for a user wouldn't
      // take effect on the user's existing tab until next login.
      // Protected route refetches /auth/me on every mount so those
      // flags stay fresh.
      partialize: (s) => ({
        token: s.token,
        user: s.user
          ? { id: s.user.id, username: s.user.username, role: s.user.role }
          : null,
      }),
    },
  ),
)

export type ThemeMode = 'dark' | 'light'

interface PrefsState {
  theme: ThemeMode
  setTheme: (t: ThemeMode) => void
}

export const usePrefs = create<PrefsState>()(
  persist(
    (set) => ({
      theme: 'light',
      setTheme: (t) => set({ theme: t }),
    }),
    { name: 'taps-prefs' },
  ),
)
