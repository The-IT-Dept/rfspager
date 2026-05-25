import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "../services/api-client"

export function useAlertzones() {
  return useQuery({
    queryKey: ["alertzones"],
    queryFn: () => api.alertzones.list(),
  })
}

export function useCreateAlertzone() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (data: { name: string; lat: number; lng: number; radius: number }) =>
      api.alertzones.create(data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["alertzones"] }),
  })
}

export function useDeleteAlertzone() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.alertzones.delete(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["alertzones"] }),
  })
}
