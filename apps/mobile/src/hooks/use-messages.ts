import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query"
import { api } from "../services/api-client"

export function useMessages(params: {
  showOnlyFiltered?: boolean
  showUnknown?: boolean
  capcodeId?: number
  search?: string
  createdAfter?: string
}) {
  return useInfiniteQuery({
    queryKey: [
      "messages",
      params.showOnlyFiltered ?? false,
      params.showUnknown ?? false,
      params.capcodeId ?? null,
      params.search ?? "",
      params.createdAfter ?? "",
    ],
    queryFn: async ({ pageParam = 1 }) => {
      return api.messages.list({
        page: pageParam,
        per_page: 25,
        show_only_filtered: params.showOnlyFiltered,
        show_unknown: params.showUnknown,
        capcode_id: params.capcodeId,
        search: params.search,
        created_after: params.createdAfter,
      })
    },
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.page < lastPage.total_pages ? lastPage.page + 1 : undefined,
    // A search or filter change produces a new query key. Without this the
    // hook drops to isLoading with no data, the Pager swaps its list for a
    // spinner, and the FlatList unmounts. The screen's UIScrollView is what
    // drives the iOS 26 tab bar and header; a fresh one at offset 0 makes the
    // bar spring back open. Keeping the previous page visible until the new
    // one lands avoids the unmount entirely.
    placeholderData: keepPreviousData,
  })
}

export function useMessage(id: number | string) {
  return useQuery({
    queryKey: ["message", id],
    queryFn: () => api.messages.get(Number(id)),
    enabled: !!id,
  })
}

export function useMapMessages(params: { limit?: number; createdAfter?: string } = {}) {
  return useQuery({
    queryKey: ["mapMessages", params.limit ?? 100, params.createdAfter ?? ""],
    queryFn: () => api.messages.map({ limit: params.limit, created_after: params.createdAfter }),
  })
}
