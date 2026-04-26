import { create } from 'zustand'
import { api } from '@/api/client'

interface BrandState {
  siteName: string
  hasFavicon: boolean
  faviconMime: string
  loaded: boolean
  load: () => Promise<void>
}

// useBrandStore caches the panel-level brand (site name + favicon).
// Refreshed on app boot and after admin saves; everything that
// renders the title or icon reads from here.
export const useBrandStore = create<BrandState>((set, get) => ({
  siteName: 'TapS',
  hasFavicon: false,
  faviconMime: '',
  loaded: false,
  load: async () => {
    try {
      const r = await api.get<{ siteName: string; hasFavicon: boolean; faviconMime: string }>('/brand')
      set({
        siteName: r.data.siteName || 'TapS',
        hasFavicon: !!r.data.hasFavicon,
        faviconMime: r.data.faviconMime || '',
        loaded: true,
      })
      applyToDOM(r.data.siteName || 'TapS', !!r.data.hasFavicon)
    } catch {
      set({ loaded: true })
    }
  },
}))

// applyToDOM mutates document.title and the <link rel="icon"> tag in
// place so even pages that don't import this hook (the bare HTML
// shell during initial paint) reflect the configured brand.
function applyToDOM(siteName: string, hasFavicon: boolean) {
  document.title = siteName
  const cacheBust = Date.now()
  const href = hasFavicon ? `/api/brand/favicon?_=${cacheBust}` : '/favicon.ico'
  let link = document.querySelector("link[rel~='icon']") as HTMLLinkElement | null
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }
  link.href = href
}
