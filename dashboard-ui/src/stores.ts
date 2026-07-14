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

  const reload = useCallback(async () => {
    setRefreshing(true)
    try {
      const s = await api<StoreInfo[]>('/api/stores')
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
