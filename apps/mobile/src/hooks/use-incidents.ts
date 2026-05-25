import { useQuery, useInfiniteQuery } from "@tanstack/react-query"
import { api } from "../services/api-client"

export function useMajorIncidents(params: { perPage?: number } = {}) {
  return useQuery({
    queryKey: ["majorIncidents", params.perPage ?? 50],
    queryFn: () => api.incidents.majorIncidents({ per_page: params.perPage || 50 }),
  })
}

export function useMajorIncidentsPaginated() {
  return useInfiniteQuery({
    queryKey: ["majorIncidentsPaginated"],
    queryFn: async ({ pageParam = 1 }) => {
      return api.incidents.majorIncidents({ page: pageParam, per_page: 25 })
    },
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.page < lastPage.total_pages ? lastPage.page + 1 : undefined,
  })
}

export function useMajorIncident(id: number | string) {
  return useQuery({
    queryKey: ["majorIncident", id],
    queryFn: () => api.incidents.majorIncident(id),
    enabled: !!id,
  })
}

export function useHazardReductions(params: { perPage?: number } = {}) {
  return useQuery({
    queryKey: ["hazardReductions", params.perPage ?? 50],
    queryFn: () => api.incidents.hazardReductions({ per_page: params.perPage || 50 }),
  })
}
