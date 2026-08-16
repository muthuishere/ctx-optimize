import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import type { StoreInfo } from './types'

// Module-level cache of /api/stores so navigating away from Repos/Overview and
// back shows the last data INSTANTLY (no "loading…" flash) while a background
// refresh runs. reload() force-refetches and updates the cache for everyone.
let cache: StoreInfo[] | null = null

export function useStores() {
  const [stores, setStores] = useState<StoreInfo[] | null>(cache)
  const [err, setErr] = useState('')
  const [refreshing, setRefreshing] = useState(false)

  // TWO-STAGE LOAD. /api/stores has a cheap half that comes straight out of the
  // manifests (names, node and edge counts, summaries — most of what a card
  // shows) and an expensive half that scans every graph and shells out to git.
  // Measured on this machine at 875 stores: 0.43s against 8.7s cold.
  //
  // So the cheap half paints first and the full answer replaces it. The server
  // grew `?cheap=1` for exactly this and nothing was using it, which meant the
  // page still waited for the slow half before showing anything at all.
  const reload = useCallback(async (force = false) => {
    setRefreshing(true)
    try {
      if (!cache) {
        // only on a cold start: with a cache there is already something on
        // screen, and a cheap answer would REPLACE it with less information
        try {
          const quick = await api<StoreInfo[]>('/api/stores?cheap=1')
          if (!cache) setStores(quick)
        } catch { /* the full fetch below is the real one; this is a head start */ }
      }
      const s = await api<StoreInfo[]>('/api/stores' + (force ? '?refresh=1' : ''))
      cache = s
      setStores(s)
      setErr('')
    } catch (e: any) {
      setErr(String(e.message || e))
    } finally {
      setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    if (cache) setStores(cache) // paint cached data first
    reload() // then refresh in the background
  }, [reload])

  return { stores, err, refreshing, reload }
}
