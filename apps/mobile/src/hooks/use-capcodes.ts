import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api, fetchUnknownCapcodes, type CapcodeSuggestionInput } from "../services/api-client"

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

export function useUnknownCapcodes() {
  return useQuery({
    queryKey: ["capcodes-unknown"],
    queryFn: () => fetchUnknownCapcodes(30, 200),
    staleTime: 2 * 60 * 1000,
  })
}

export function useMyCapcodeSuggestions() {
  return useQuery({
    queryKey: ["capcode-suggestions-mine"],
    queryFn: () => api.capcodeSuggestions.listMine(),
    staleTime: 60 * 1000,
  })
}

export function useCreateCapcodeSuggestion() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (data: CapcodeSuggestionInput) => api.capcodeSuggestions.create(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["capcodes-unknown"] })
      qc.invalidateQueries({ queryKey: ["capcode-suggestions-mine"] })
    },
  })
}
