import { useQuery, useInfiniteQuery } from "@tanstack/react-query"
import { api } from "../services/api-client"

export function useAgencies() {
  return useQuery({
    queryKey: ["agencies"],
    queryFn: () => api.agencies.list(),
    staleTime: 5 * 60 * 1000, // agencies rarely change
  })
}

export function useCapcodes(params: { search?: string; agencyId?: number } = {}) {
  return useInfiniteQuery({
    queryKey: ["capcodes", params.search ?? "", params.agencyId ?? null],
    queryFn: async ({ pageParam = 1 }) => {
      return api.capcodes.list({
        page: pageParam,
        per_page: 100,
        search: params.search,
        agency_id: params.agencyId,
      })
    },
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.page < lastPage.total_pages ? lastPage.page + 1 : undefined,
  })
}
