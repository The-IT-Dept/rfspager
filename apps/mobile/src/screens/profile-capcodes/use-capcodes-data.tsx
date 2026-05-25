import { useEffect, useMemo } from "react"
import { useAgencies, useCapcodes } from "../../hooks"
import { useSubscriptionsStore } from "../../stores"
import type { Agency } from "../../services/api-client"

/**
 * Hook for the agency list screen — fetches all agencies with counts.
 */
export const useCapcodesData = () => {
  const loadSubscriptions = useSubscriptionsStore((s) => s.load)
  useEffect(() => {
    loadSubscriptions()
  }, [])

  const { data: agenciesList, isLoading } = useAgencies()

  const groupedData = useMemo(() => {
    if (!agenciesList) return []
    return agenciesList.map((a: Agency) => ({
      agency: a.name,
      agencyId: a.id,
      totalCount: a.capcode_count,
      capcodes: [],
      subscribedCount: 0,
      filteredCount: 0,
    }))
  }, [agenciesList])

  return {
    capcodes: [],
    groupedData,
    loading: isLoading,
    initialLoaded: !isLoading && groupedData.length > 0,
    getAgencyCapcodes: () => [],
    searchMatchesAliasInAgency: () => false,
    invalidateCache: () => {},
    cacheAge: null,
  }
}

/**
 * Hook for the capcode detail screen — fetches capcodes for a specific agency.
 */
export const useAgencyCapcodes = (agencyId: number) => {
  const {
    data,
    isLoading,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useCapcodes({ agencyId })

  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage) {
      fetchNextPage()
    }
  }, [hasNextPage, isFetchingNextPage])

  const capcodes = useMemo(() => {
    if (!data?.pages) return []
    return data.pages.flatMap((p) => p.data)
  }, [data])

  return { capcodes, loading: isLoading }
}
